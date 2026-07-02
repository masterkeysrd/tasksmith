package formatter

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
)

func TestFormatFile(t *testing.T) {
	f := &resolver.ResolvedFile{
		FilePath:   "/workspace/bar.go",
		Content:    "package foo\nfunc Hello() {}",
		TotalLines: 2,
		MimeType:   "text/x-go",
		IsBinary:   false,
	}

	blocks := FormatFile(f)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	txtBlock, ok := blocks[0].(*message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock")
	}

	if !strings.Contains(txtBlock.Text, "bar.go") {
		t.Errorf("expected header to contain filename, got: %s", txtBlock.Text)
	}
	if !strings.Contains(txtBlock.Text, "1 | package foo") {
		t.Errorf("expected numbered lines, got: %s", txtBlock.Text)
	}
}
