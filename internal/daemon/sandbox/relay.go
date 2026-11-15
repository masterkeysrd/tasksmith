//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// SandboxRelayArg is the hidden TaskSmith subcommand children run inside a
// sandboxed Linux network namespace to bridge loopback traffic to the
// daemon-side allowlist proxy.
const SandboxRelayArg = "sandbox-relay"

// RelayServe bridges TCP connections on 127.0.0.1:port to the allowlist
// proxy's Unix socket at unixSocket. It runs inside the child's closed
// network namespace on Linux, where the only path to the daemon-side proxy is
// the Unix socket bind-mounted inside the sandbox temp dir: children keep
// talking to the same http://127.0.0.1:<port> proxy URL the sandbox injects,
// and the relay forwards every byte to the real proxy over the socket.
//
// The listener is bound before RelayServe returns so a supervising process
// can exec its target the moment the port answers. It blocks until ctx is
// canceled and closes the listener and the socket file before returning.
func RelayServe(ctx context.Context, unixSocket string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("sandbox: invalid relay port %d", port)
	}
	if unixSocket == "" {
		return errors.New("sandbox: relay requires a proxy unix socket path")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("sandbox: relay cannot listen on loopback port %d: %w", port, err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, lerr := ln.Accept()
		if lerr != nil {
			if ctx.Err() != nil || errors.Is(lerr, net.ErrClosed) {
				break
			}
			// A transient accept failure must not tear the bridge down.
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			relayConn(conn, unixSocket)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// RunSandboxRelay supervises one sandboxed command inside the closed network
// namespace: it raises the loopback interface, starts a relay server for the
// proxy socket as a background process, waits until the loopback proxy port
// answers, then execs the real target in place. When the target exits the
// process group is killed, which tears the relay server down with it.
func RunSandboxRelay(unixSocket string, port int, target string, args []string) error {
	// Keep Start and Exec on the same OS thread: Linux evaluates Pdeathsig
	// against the thread that forked the helper, and Exec tears down every
	// other thread in the thread group before launching target.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := bringLoopbackUp(); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("sandbox: locate TaskSmith binary for network relay: %w", err)
	}
	serve := exec.Command(self, SandboxRelayArg, "--serve",
		"--socket", unixSocket, "--port", strconv.Itoa(port))
	// The helper shares the command's stderr pipe; Pdeathsig guarantees it
	// dies with the exec'd target (which replaces this process) so output
	// draining never waits on a lingering helper.
	serve.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	serve.Stdin = nil
	serve.Stdout = nil
	serve.Stderr = os.Stderr
	if err := serve.Start(); err != nil {
		return fmt.Errorf("sandbox: start network relay server: %w", err)
	}

	if err := waitForRelayPort(port, 10*time.Second); err != nil {
		_ = serve.Process.Kill()
		_ = serve.Wait()
		return err
	}

	err = syscall.Exec(target, append([]string{target}, args...), os.Environ())
	// Exec only returns on failure; clean up the relay server either way.
	_ = serve.Process.Kill()
	_ = serve.Wait()
	return fmt.Errorf("sandbox: relay exec %s: %w", target, err)
}

// waitForRelayPort polls the loopback relay port until it accepts a
// connection or the deadline passes.
func waitForRelayPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sandbox: network relay never became ready on %s: %w", addr, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// relayConn pairs an accepted loopback connection with the proxy socket and
// copies bytes in both directions until either side closes.
func relayConn(child net.Conn, unixSocket string) {
	defer func() { _ = child.Close() }()
	upstream, err := net.Dial("unix", unixSocket)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()

	upDone := make(chan struct{})
	go func() {
		defer close(upDone)
		_, _ = io.Copy(upstream, child)
		closeWrite(upstream)
	}()
	_, _ = io.Copy(child, upstream)
	closeWrite(child)
	<-upDone
}

// closeWrite half-closes a connection's write side so the peer observes EOF
// while reads may still drain.
func closeWrite(c net.Conn) {
	if hw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = hw.CloseWrite()
	}
}
