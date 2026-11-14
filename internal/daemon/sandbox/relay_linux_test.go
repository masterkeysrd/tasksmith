//go:build linux

package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// freeLoopbackPort probes a kernel-allocated free loopback port. The caller
// releases it before RelayServe re-binds the same number (a benign probe race
// acceptable in tests).
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listen unavailable: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestRelayServeArgumentValidation(t *testing.T) {
	if err := RelayServe(context.Background(), "", 45999); err == nil ||
		!strings.Contains(err.Error(), "unix socket") {
		t.Fatalf("RelayServe(empty socket) = %v, want missing-socket error", err)
	}
	if err := RelayServe(context.Background(), "/tmp/whatever.sock", 0); err == nil ||
		!strings.Contains(err.Error(), "invalid relay port") {
		t.Fatalf("RelayServe(port 0) = %v, want invalid-port error", err)
	}
}

// TestRelayServeBridgesTCPToUnixSocket exercises the exact child-side flow:
// an HTTP client configured with the relay's loopback port as its proxy
// reaches a minimal HTTP-proxy responder through the Unix socket, which is
// the only path a closed network namespace has to the daemon proxy.
func TestRelayServeBridgesTCPToUnixSocket(t *testing.T) {
	sock := t.TempDir() + "/proxy.sock"
	proxyLn, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix socket listen unavailable: %v", err)
	}
	defer func() { _ = proxyLn.Close() }()

	// Minimal HTTP/1.1 proxy responder behind the socket: answers
	// absolute-URI GETs with a fixed body, closing the connection after.
	go func() {
		for {
			conn, aerr := proxyLn.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				req, perr := http.ReadRequest(bufio.NewReader(c))
				if perr != nil {
					return
				}
				body := "via-relay:" + req.URL.Host
				_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			}(conn)
		}
	}()

	port := freeLoopbackPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- RelayServe(ctx, sock, port) }()

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) {
				return &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}, nil
			},
		},
		Timeout: 10 * time.Second,
	}

	deadline := time.Now().Add(10 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = client.Get("http://example.invalid/probe")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("request through relay never succeeded: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "via-relay:example.invalid" {
		t.Fatalf("relay round-trip: got %d %q", resp.StatusCode, body)
	}

	cancel()
	select {
	case serr := <-serveErr:
		if serr != nil && ctx.Err() == nil {
			t.Fatalf("RelayServe exit error: %v", serr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RelayServe did not return after context cancel")
	}
}
