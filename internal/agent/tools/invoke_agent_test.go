package tools

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/message"
)

type mockSubagentGraphRunner struct {
	runFunc func(ctx context.Context, opts SubagentOptions) (string, error)
}

func (m *mockSubagentGraphRunner) Run(ctx context.Context, opts SubagentOptions) (string, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, opts)
	}
	return "mock output", nil
}

func TestInvokeAgent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-invoke-agent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set up mock graph runner
	mockRunner := &mockSubagentGraphRunner{
		runFunc: func(ctx context.Context, opts SubagentOptions) (string, error) {
			_, _ = io.WriteString(opts.Stdout, "thinking monologue\n")
			_, _ = io.WriteString(opts.Stdout, "agent final text response")

			if r, ok := opts.Inbox.(*AgentRunner); ok {
				r.SetResult("agent final text response")
			}
			return "agent final text response", nil
		},
	}
	SubagentRunner = mockRunner
	defer func() { SubagentRunner = nil }()

	// Set up TaskManager
	taskMgr := NewTaskManager(tmpDir, nil)

	h := &ToolHandlers{
		CWD:         tmpDir,
		SessionID:   "parent_session",
		TaskManager: taskMgr,
	}

	ctx := context.Background()
	in := InvokeAgentArgs{
		AgentRef: "researcher",
		Task:     "search the docs",
		WaitMs:   1000,
		Mode:     "transient",
	}

	stream, err := h.InvokeAgent(ctx, in)
	if err != nil {
		t.Fatalf("InvokeAgent failed: %v", err)
	}

	var finalOutput string
	var finalStatus string

	done := make(chan struct{})
	go func() {
		stream(func(chunk message.ToolChunk, err error) bool {
			if chunk.StructuredContent != nil {
				if out, ok := chunk.StructuredContent.(InvokeAgentOutput); ok {
					finalOutput = out.Output
					finalStatus = out.Status
				}
			}
			return true
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream completion")
	}

	if finalStatus != "completed" {
		t.Errorf("expected status 'completed', got %q", finalStatus)
	}

	if finalOutput != "agent final text response" {
		t.Errorf("expected output 'agent final text response', got %q", finalOutput)
	}
}
