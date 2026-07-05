package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
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

func TestRecordBashChanges_BinaryMetadataOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bash-test-binary-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a text file
	txtPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(txtPath, []byte("some text\ncontent\n"), 0644); err != nil {
		t.Fatalf("failed to write test.txt: %v", err)
	}

	// Create a binary file (PNG signature)
	binPath := filepath.Join(tmpDir, "test.png")
	if err := os.WriteFile(binPath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0644); err != nil {
		t.Fatalf("failed to write test.png: %v", err)
	}

	ft := &mockBashFileTracker{}
	changes := []filetrack.Change{
		{Path: "./test.txt", Kind: filetrack.Created},
		{Path: "./test.png", Kind: filetrack.Created},
	}

	recordBashChanges(context.Background(), ft, tmpDir, changes)

	if len(ft.recorded) != 2 {
		t.Fatalf("expected 2 recorded changes, got %d", len(ft.recorded))
	}

	var txtChange, pngChange *filetrack.Change
	for i := range ft.recorded {
		c := &ft.recorded[i]
		switch c.Path {
		case "./test.txt":
			txtChange = c
		case "./test.png":
			pngChange = c
		}
	}

	if txtChange == nil {
		t.Fatal("expected test.txt to be recorded")
	}
	if txtChange.Additions == 0 {
		t.Error("expected test.txt to have additions > 0")
	}

	if pngChange == nil {
		t.Fatal("expected test.png to be recorded")
	}
	if pngChange.Additions != 0 || pngChange.Deletions != 0 {
		t.Errorf("expected 0 additions/deletions for binary file, got %+v", pngChange)
	}
}

func TestBashReadOnlyBypass(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bash-bypass-test-*")
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
		Command: "echo hello",
	}

	stream, err := h.Bash(ctx, in)
	if err != nil {
		t.Fatalf("Bash failed: %v", err)
	}

	// Consume stream
	stream(func(chunk message.ToolChunk, err error) bool {
		return true
	})

	if len(ft.recorded) > 0 {
		t.Errorf("expected no files to be recorded for read-only command 'echo hello', got %d", len(ft.recorded))
	}

	// Run a command that writes inside the workspace: touch test.txt.
	inWrite := BashArgs{
		Command: "touch test.txt",
	}
	ft.recorded = nil // reset
	streamWrite, err := h.Bash(ctx, inWrite)
	if err != nil {
		t.Fatalf("Bash failed: %v", err)
	}
	streamWrite(func(chunk message.ToolChunk, err error) bool {
		return true
	})

	if len(ft.recorded) == 0 {
		t.Errorf("expected file changes to be recorded for write command 'touch test.txt'")
	}
}
