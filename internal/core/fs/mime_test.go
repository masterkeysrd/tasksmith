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

	sqlPath := filepath.Join(dir, "test.sql")
	if err := os.WriteFile(sqlPath, []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("failed to write test.sql: %v", err)
	}
	mimeType = DetectMIMEType(sqlPath)
	if mimeType != "text/x-sql" {
		t.Errorf("expected text/x-sql, got %s", mimeType)
	}
	if IsBinaryMIME(mimeType) {
		t.Error("expected SQL mime type to not be binary")
	}

	// Verify application/sql is also considered non-binary
	if IsBinaryMIME("application/sql") {
		t.Error("expected application/sql to not be binary")
	}

	tsPath := filepath.Join(dir, "test.ts")
	if err := os.WriteFile(tsPath, []byte("const x: number = 1;"), 0644); err != nil {
		t.Fatalf("failed to write test.ts: %v", err)
	}
	mimeType = DetectMIMEType(tsPath)
	if mimeType != "text/typescript" {
		t.Errorf("expected text/typescript, got %s", mimeType)
	}
	if IsBinaryMIME(mimeType) {
		t.Error("expected typescript mime type to not be binary")
	}

	tsxPath := filepath.Join(dir, "test.tsx")
	if err := os.WriteFile(tsxPath, []byte("const component = () => <div />;"), 0644); err != nil {
		t.Fatalf("failed to write test.tsx: %v", err)
	}
	mimeType = DetectMIMEType(tsxPath)
	if mimeType != "text/typescript-jsx" {
		t.Errorf("expected text/typescript-jsx, got %s", mimeType)
	}
	if IsBinaryMIME(mimeType) {
		t.Error("expected typescript-jsx mime type to not be binary")
	}
}
