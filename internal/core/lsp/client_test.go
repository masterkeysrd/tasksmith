package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/lspx"
)

func TestLspxEnvironment(t *testing.T) {
	tmpDir := t.TempDir()
	envOutFile := filepath.Join(tmpDir, "env.txt")

	// Set up a mock LSP server that simply dumps its environment variables to a file and exits.
	// Since LSP servers communicate via stdin/stdout, we can write a script or command.
	// Let's run a bash command that prints env to envOutFile.
	serverCfg := lspx.ServerConfig{
		Name:          "env-dumper",
		Command:       []string{"sh", "-c", "env > " + envOutFile},
		FileTypes:     []string{"text"},
		ShareSessions: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	opts := lspx.ClientOptions{
		Servers: []lspx.ServerConfig{serverCfg},
		RootURI: "file://" + tmpDir,
	}

	// This will try to start the server. It will run and write to envOutFile.
	client, err := lspx.NewClient(ctx, opts)
	if err != nil {
		// It might fail because the server exits immediately, but it should still execute the command.
		t.Logf("NewClient returned expected error/status: %v", err)
	}
	if client != nil {
		_ = client.Close()
	}

	// Give a tiny moment for the process execution
	time.Sleep(100 * time.Millisecond)

	data, err := os.ReadFile(envOutFile)
	if err != nil {
		t.Fatalf("failed to read env output file: %v", err)
	}

	envStr := string(data)
	t.Logf("Dumped environment:\n%s", envStr)

	// Check if PATH environment variable is present
	if !strings.Contains(envStr, "PATH=") {
		t.Error("PATH environment variable is missing from spawned LSP server environment!")
	}
}
