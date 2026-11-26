//go:build linux

package sandbox

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
)

// TestMain intercepts the hidden sandbox-relay subcommand when the Go test
// binary itself is re-exec'd inside a Bubblewrap network namespace on Linux.
// Without this hook, go test ignores the relay flags and runs the test suite
// inside the sandbox instead of bridging proxy traffic.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == SandboxRelayArg {
		os.Exit(runTestSandboxRelay(os.Args[2:]))
	}
	os.Exit(testutils.RunWithXDG(m, "tasksmith"))
}

func runTestSandboxRelay(args []string) int {
	fs := flag.NewFlagSet(SandboxRelayArg, flag.ContinueOnError)
	var (
		serve  bool
		socket string
		port   int
	)
	fs.BoolVar(&serve, "serve", false, "Run the relay server itself instead of supervising a command")
	fs.StringVar(&socket, "socket", "", "Proxy Unix socket path")
	fs.IntVar(&port, "port", 0, "Loopback port to bridge")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if serve {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := RelayServe(ctx, socket, port); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "sandbox-relay: missing target command")
		return 2
	}
	if err := RunSandboxRelay(socket, port, rest[0], rest[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
