package tools

import (
	"context"
	"os"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
)

type mockBashFileTracker struct {
	recorded []filetrack.Change
}

func (m *mockBashFileTracker) Record(ctx context.Context, change filetrack.Change, diff string, oldContent string) error {
	m.recorded = append(m.recorded, change)
	return nil
}

func (m *mockBashFileTracker) RevertToBaseline(ctx context.Context, path string, force bool) error {
	return nil
}

func (m *mockBashFileTracker) ExpectWrite(path string, hash string) {}
func (m *mockBashFileTracker) Close() error                         { return nil }

func (m *mockBashFileTracker) CheckConflict(ctx context.Context, path string) (bool, error) {
	return false, nil
}

func (m *mockBashFileTracker) RecordRead(ctx context.Context, path string) error {
	return nil
}

func (m *mockBashFileTracker) IsKnown(ctx context.Context, path string) (bool, error) {
	return false, nil
}

func (m *mockBashFileTracker) Summary(ctx context.Context) ([]filetrack.FileSummary, error) {
	return nil, nil
}

func (m *mockBashFileTracker) ReadJournal(ctx context.Context, path string) ([]filetrack.JournalEntry, error) {
	return nil, nil
}

func TestBashExecution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bash-exec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ft := &mockBashFileTracker{}
	h := &ToolHandlers{
		CWD:         tmpDir,
		FileTracker: ft,
		TaskManager: nil, // force synchronous path
	}

	ctx := context.Background()
	in := BashArgs{
		Command:     "echo hello",
		Description: "echo string",
	}

	stream, err := h.Bash(ctx, in)
	if err != nil {
		t.Fatalf("Bash failed: %v", err)
	}

	// Consume stream
	stream(func(chunk message.ToolChunk, err error) bool {
		return true
	})

	// Run a command that writes inside the workspace: touch test.txt.
	inWrite := BashArgs{
		Command:     "touch test.txt",
		Description: "touch file",
	}
	streamWrite, err := h.Bash(ctx, inWrite)
	if err != nil {
		t.Fatalf("Bash failed: %v", err)
	}
	streamWrite(func(chunk message.ToolChunk, err error) bool {
		return true
	})
}

func TestBashValidation(t *testing.T) {
	h := &ToolHandlers{
		CWD:         ".",
		FileTracker: &mockBashFileTracker{},
		TaskManager: nil,
	}

	ctx := context.Background()

	// Empty command should fail validation
	inEmptyCmd := BashArgs{
		Command:     "",
		Description: "test validation",
	}

	_, err := h.Bash(ctx, inEmptyCmd)
	if err == nil {
		t.Error("expected error for empty command validation, got nil")
	}

	// Empty description should fail validation
	inEmptyDesc := BashArgs{
		Command:     "echo hello",
		Description: "",
	}

	_, err = h.Bash(ctx, inEmptyDesc)
	if err == nil {
		t.Error("expected error for empty description validation, got nil")
	}
}
