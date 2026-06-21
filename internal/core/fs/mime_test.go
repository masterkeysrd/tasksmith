package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMimeDetection(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(txtPath, []byte("plain text"), 0644); err != nil {
		t.Fatalf("failed to write test.txt: %v", err)
	}

	mimeType := DetectMIMEType(txtPath)
	if mimeType != "text/plain" {
		t.Errorf("expected text/plain, got %s", mimeType)
	}
	if IsBinaryMIME(mimeType) {
		t.Error("expected text/plain to not be binary")
	}

	pngPath := filepath.Join(dir, "test.png")
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if err := os.WriteFile(pngPath, pngBytes, 0644); err != nil {
		t.Fatalf("failed to write test.png: %v", err)
	}

	mimeType = DetectMIMEType(pngPath)
	if mimeType != "image/png" {
		t.Errorf("expected image/png, got %s", mimeType)
	}
	if !IsBinaryMIME(mimeType) {
		t.Error("expected image/png to be binary")
	}
}
