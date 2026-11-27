package clipboard

import (
	"testing"

	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/terminal"
	"github.com/masterkeysrd/kite/testenv"
)

type stubTextClipboard struct {
	terminal.Clipboard
	written []string
}

func (s *stubTextClipboard) WriteText(text string) *promise.Promise[struct{}] {
	s.written = append(s.written, text)
	return promise.Resolved(struct{}{})
}

func TestWriteText(t *testing.T) {
	if WriteText(nil, "hello") {
		t.Fatal("expected nil document to return false")
	}

	env := testenv.New(t, 40, 4)
	t.Cleanup(env.Close)

	stub := &stubTextClipboard{}
	env.Document().SetClipboardProvider(stub)

	if WriteText(env.Document(), "   ") {
		t.Fatal("expected blank text to return false")
	}
	if !WriteText(env.Document(), "https://example.com") {
		t.Fatal("expected WriteText to report success")
	}
	if len(stub.written) != 1 || stub.written[0] != "https://example.com" {
		t.Fatalf("written = %v, want [https://example.com]", stub.written)
	}
}
