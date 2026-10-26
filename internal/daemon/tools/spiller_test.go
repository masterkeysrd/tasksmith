package tools_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestNewSpillerValidation(t *testing.T) {
	_, err := tools.NewSpiller(tools.SpillerConfig{})
	if err == nil || !errors.Is(err, tools.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig when BaseDir and Workspace are missing, got: %v", err)
	}

	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{BaseDir: tmp})
	if err != nil {
		t.Fatalf("unexpected error with BaseDir: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Spiller")
	}
}

func TestSpillerShortOutputNoSpill(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  1000,
		MaxLines:  10,
		HeadLines: 2,
		TailLines: 2,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-1",
		ToolCallID: "call-1",
	})

	shortText := "line 1\nline 2\nline 3\n"
	res := s.Spill(ctx, shortText)
	if res != shortText {
		t.Fatalf("expected short text unchanged, got: %q", res)
	}

	// Verify no files written
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 files written for short output, found %d", len(entries))
	}
}

func TestSpillerExceedingLines(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  10000,
		MaxLines:  5,
		HeadLines: 2,
		TailLines: 2,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-1",
		ToolCallID: "call-lines",
	})

	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	fullText := strings.Join(lines, "\n") + "\n"

	res := s.Spill(ctx, fullText)

	// Check file on disk
	expectedFile := filepath.Join(tmp, "sessions", "sess-1", "outputs", "call-lines.txt")
	content, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", expectedFile, err)
	}
	if string(content) != fullText {
		t.Fatalf("file content does not match full text")
	}

	// Check returned preview structure
	if !strings.HasPrefix(res, "line 01\nline 02\n") {
		t.Errorf("expected head lines in result, got:\n%s", res)
	}
	if !strings.HasSuffix(res, "line 09\nline 10\n") {
		t.Errorf("expected tail lines in result, got:\n%s", res)
	}
	expectedNotice := "... [6 lines (48 bytes) truncated — full output saved to " + expectedFile
	if !strings.Contains(res, expectedNotice) {
		t.Errorf("expected notice %q in result, got:\n%s", expectedNotice, res)
	}
}

func TestSpillerHugeSingleLineExceedingBytes(t *testing.T) {
	tmp := t.TempDir()
	maxBytes := 200
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  maxBytes,
		MaxLines:  100,
		HeadLines: 10,
		TailLines: 10,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-2",
		ToolCallID: "call-bytes",
	})

	// Single huge line of 500 characters with multi-byte runes
	hugeText := strings.Repeat("世界abc", 100) // 900 bytes
	res := s.Spill(ctx, hugeText)

	expectedFile := filepath.Join(tmp, "sessions", "sess-2", "outputs", "call-bytes.txt")
	content, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", expectedFile, err)
	}
	if string(content) != hugeText {
		t.Fatalf("spilled file does not contain full huge text")
	}

	if !strings.Contains(res, "truncated — full output saved to "+expectedFile) {
		t.Fatalf("expected truncation notice in result, got:\n%s", res)
	}
	if !strings.Contains(res, "bytes (1 lines) truncated") {
		t.Fatalf("expected byte truncation format notice, got:\n%s", res)
	}
}

func TestSpillerDuplicateToolCallID(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  50,
		MaxLines:  2,
		HeadLines: 1,
		TailLines: 1,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-dup",
		ToolCallID: "call-dup",
	})

	text1 := "output 1 line 1\noutput 1 line 2\noutput 1 line 3\n"
	text2 := "output 2 line 1\noutput 2 line 2\noutput 2 line 3\n"

	res1 := s.Spill(ctx, text1)
	res2 := s.Spill(ctx, text2)

	file1 := filepath.Join(tmp, "sessions", "sess-dup", "outputs", "call-dup.txt")
	file2 := filepath.Join(tmp, "sessions", "sess-dup", "outputs", "call-dup-2.txt")

	data1, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("ReadFile file1: %v", err)
	}
	if string(data1) != text1 {
		t.Fatalf("file1 content mismatch")
	}

	data2, err := os.ReadFile(file2)
	if err != nil {
		t.Fatalf("ReadFile file2: %v", err)
	}
	if string(data2) != text2 {
		t.Fatalf("file2 content mismatch")
	}

	if !strings.Contains(res1, file1) {
		t.Errorf("res1 does not contain file1 path: %s", res1)
	}
	if !strings.Contains(res2, file2) {
		t.Errorf("res2 does not contain file2 path: %s", res2)
	}
}

func TestContextSpillerHelpers(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  50,
		MaxLines:  2,
		HeadLines: 1,
		TailLines: 1,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	// Without spiller in context, Spill returns text unchanged
	plainText := "line 1\nline 2\nline 3\n"
	if got := tools.Spill(context.Background(), plainText); got != plainText {
		t.Errorf("expected plain text when Spiller not in context, got: %s", got)
	}

	// With spiller in context
	ctx := tools.WithSpiller(context.Background(), s)
	from, ok := tools.SpillerFrom(ctx)
	if !ok || from != s {
		t.Fatalf("expected SpillerFrom to return registered spiller")
	}

	res := tools.Spill(ctx, plainText)
	if res == plainText {
		t.Errorf("expected Spill to truncate when Spiller is present in context")
	}
	if !strings.Contains(res, "truncated — full output saved to") {
		t.Errorf("expected truncation notice in result: %s", res)
	}
}

func TestSpillWriterStreamingBelowThreshold(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  1000,
		MaxLines:  10,
		HeadLines: 2,
		TailLines: 2,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-stream-1",
		ToolCallID: "call-stream-1",
	})

	w := s.NewWriter(ctx)
	chunks := []string{"chunk 1\n", "chunk 2\n", "chunk 3\n", "chunk 4\n"}
	for _, c := range chunks {
		n, writeErr := w.Write([]byte(c))
		if writeErr != nil {
			t.Fatalf("Write(%q) unexpected error: %v", c, writeErr)
		}
		if n != len(c) {
			t.Fatalf("Write(%q) wrote %d bytes, expected %d", c, n, len(c))
		}
	}

	if w.Spilled() {
		t.Fatalf("expected Spilled() == false for short stream")
	}
	if w.Path() != "" {
		t.Fatalf("expected Path() == \"\" for short stream, got: %s", w.Path())
	}

	fullExpected := strings.Join(chunks, "")
	if got := w.String(); got != fullExpected {
		t.Fatalf("expected %q, got %q", fullExpected, got)
	}

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("unexpected Close error: %v", closeErr)
	}

	// Verify no files on disk
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries on disk, found %d", len(entries))
	}
}

func TestSpillWriterStreamingCrossingLines(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  10000,
		MaxLines:  5,
		HeadLines: 2,
		TailLines: 2,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-stream-lines",
		ToolCallID: "call-stream-lines",
	})

	w := s.NewWriter(ctx)
	var fullBuilder strings.Builder
	for i := 1; i <= 10; i++ {
		line := fmt.Sprintf("line %02d\n", i)
		fullBuilder.WriteString(line)
		n, writeErr := w.Write([]byte(line))
		if writeErr != nil {
			t.Fatalf("Write line %d error: %v", i, writeErr)
		}
		if n != len(line) {
			t.Fatalf("Write line %d wrote %d bytes, expected %d", i, n, len(line))
		}
	}

	if !w.Spilled() {
		t.Fatalf("expected Spilled() == true after exceeding MaxLines")
	}

	expectedFile := filepath.Join(tmp, "sessions", "sess-stream-lines", "outputs", "call-stream-lines.txt")
	if w.Path() != expectedFile {
		t.Fatalf("expected Path() == %q, got: %q", expectedFile, w.Path())
	}

	res := w.String()
	if !strings.HasPrefix(res, "line 01\nline 02\n") {
		t.Errorf("expected head lines in result, got:\n%s", res)
	}
	if !strings.HasSuffix(res, "line 09\nline 10\n") {
		t.Errorf("expected tail lines in result, got:\n%s", res)
	}
	expectedNotice := "... [6 lines (48 bytes) truncated — full output saved to " + expectedFile
	if !strings.Contains(res, expectedNotice) {
		t.Errorf("expected notice %q in result, got:\n%s", expectedNotice, res)
	}

	// Verify content on disk matches full stream
	diskContent, readErr := os.ReadFile(expectedFile)
	if readErr != nil {
		t.Fatalf("ReadFile %s: %v", expectedFile, readErr)
	}
	if string(diskContent) != fullBuilder.String() {
		t.Fatalf("disk content does not match full streamed input")
	}

	// Writing after String() / Close() returns os.ErrClosed
	_, writeAfterCloseErr := w.Write([]byte("more"))
	if !errors.Is(writeAfterCloseErr, os.ErrClosed) {
		t.Fatalf("expected os.ErrClosed writing after Close, got: %v", writeAfterCloseErr)
	}
}

func TestSpillWriterStreamingCrossingBytes(t *testing.T) {
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  120,
		MaxLines:  50,
		HeadLines: 2,
		TailLines: 2,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-stream-bytes",
		ToolCallID: "call-stream-bytes",
	})

	w := s.NewWriter(ctx)
	// Write a single long line split into small 15-byte chunks
	fullText := strings.Repeat("0123456789", 25) // 250 bytes
	chunkSize := 15
	for offset := 0; offset < len(fullText); offset += chunkSize {
		end := min(offset+chunkSize, len(fullText))
		chunk := fullText[offset:end]
		n, writeErr := w.Write([]byte(chunk))
		if writeErr != nil {
			t.Fatalf("Write chunk error: %v", writeErr)
		}
		if n != len(chunk) {
			t.Fatalf("Write chunk wrote %d bytes, expected %d", n, len(chunk))
		}
	}

	if !w.Spilled() {
		t.Fatalf("expected Spilled() == true after exceeding MaxBytes")
	}

	expectedFile := filepath.Join(tmp, "sessions", "sess-stream-bytes", "outputs", "call-stream-bytes.txt")
	res := w.String()
	if !strings.Contains(res, "truncated — full output saved to "+expectedFile) {
		t.Fatalf("expected truncation notice in result, got:\n%s", res)
	}

	diskContent, readErr := os.ReadFile(expectedFile)
	if readErr != nil {
		t.Fatalf("ReadFile %s: %v", expectedFile, readErr)
	}
	if string(diskContent) != fullText {
		t.Fatalf("disk content mismatch for byte stream")
	}
}

func TestSpillWriterMaxDiskBytesCap(t *testing.T) {
	tmp := t.TempDir()
	maxDisk := 512
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:      tmp,
		MaxBytes:     100,
		MaxLines:     10,
		MaxDiskBytes: maxDisk,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-disk-cap",
		ToolCallID: "call-disk-cap",
	})

	w := s.NewWriter(ctx)
	// Write 4 chunks of 256 bytes = 1024 bytes total (exceeds maxDisk of 512)
	chunk := strings.Repeat("A", 256)
	for i := range 4 {
		n, writeErr := w.Write([]byte(chunk))
		if writeErr != nil {
			t.Fatalf("Write chunk %d error: %v", i, writeErr)
		}
		if n != len(chunk) {
			t.Fatalf("Write chunk %d wrote %d bytes, expected %d", i, n, len(chunk))
		}
	}

	res := w.String()
	if !w.Spilled() {
		t.Fatalf("expected Spilled() == true")
	}

	expectedFile := filepath.Join(tmp, "sessions", "sess-disk-cap", "outputs", "call-disk-cap.txt")
	fi, statErr := os.Stat(expectedFile)
	if statErr != nil {
		t.Fatalf("Stat %s: %v", expectedFile, statErr)
	}
	if fi.Size() != int64(maxDisk) {
		t.Fatalf("expected file size to be capped at %d bytes, got %d", maxDisk, fi.Size())
	}

	// Result notice should still report total omission stats accurately based on totalBytes (1024)
	expectedOmitted := 1024 - 100 // half=50 head, half=50 tail -> 100 retained
	expectedStat := fmt.Sprintf("%d bytes (1 lines) truncated", expectedOmitted)
	if !strings.Contains(res, expectedStat) {
		t.Fatalf("expected notice to mention %q, got:\n%s", expectedStat, res)
	}
}

func TestNewSpillWriterFallback(t *testing.T) {
	// 1. Without spiller in context
	plainWriter := tools.NewSpillWriter(context.Background())
	if plainWriter == nil {
		t.Fatal("expected non-nil SpillWriter")
	}
	if plainWriter.Spilled() {
		t.Error("expected Spilled() == false for fallback writer")
	}
	if plainWriter.Path() != "" {
		t.Errorf("expected empty Path() for fallback writer, got %q", plainWriter.Path())
	}

	input := "fallback line 1\nfallback line 2\n"
	n, writeErr := plainWriter.Write([]byte(input))
	if writeErr != nil || n != len(input) {
		t.Fatalf("Write failed: err=%v, n=%d", writeErr, n)
	}
	if plainWriter.String() != input {
		t.Fatalf("String() mismatch: got %q, want %q", plainWriter.String(), input)
	}
	if err := plainWriter.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 2. With spiller in context
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:  tmp,
		MaxBytes: 50,
		MaxLines: 2,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	ctx := tools.WithSpiller(context.Background(), s)
	ctx = scope.With(ctx, scope.Scope{
		SessionID:  "sess-fallback-ctx",
		ToolCallID: "call-fallback-ctx",
	})

	activeWriter := tools.NewSpillWriter(ctx)
	largeInput := strings.Repeat("line\n", 10)
	_, writeErr = activeWriter.Write([]byte(largeInput))
	if writeErr != nil {
		t.Fatalf("activeWriter Write: %v", writeErr)
	}
	if !activeWriter.Spilled() {
		t.Fatalf("expected activeWriter.Spilled() == true")
	}
	if activeWriter.Path() == "" {
		t.Fatalf("expected activeWriter.Path() != \"\"")
	}
	if !strings.Contains(activeWriter.String(), activeWriter.Path()) {
		t.Fatalf("expected String() to contain spilled path: %s", activeWriter.String())
	}
}

func TestSpillWriterEdgeCases(t *testing.T) {
	// 1. nopSpillWriter returns os.ErrClosed after Close
	nopWriter := tools.NewSpillWriter(context.Background())
	if err := nopWriter.Close(); err != nil {
		t.Fatalf("nopWriter.Close: %v", err)
	}
	if _, err := nopWriter.Write([]byte("test")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected os.ErrClosed writing to closed nopSpillWriter, got %v", err)
	}

	// 2. Unspilled spillWriter returns os.ErrClosed after Close
	tmp := t.TempDir()
	s, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:  tmp,
		MaxBytes: 1000,
		MaxLines: 100,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}
	w := s.NewWriter(context.Background())
	if _, err := w.Write([]byte("short")); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("unspilled Close: %v", err)
	}
	if _, err := w.Write([]byte("more")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected os.ErrClosed writing to closed unspilled writer, got %v", err)
	}

	// 3. Spilled writer with uncreatable disk path (e.g. file where dir should be)
	// should produce truncation notice without empty "full output saved to" notice
	blockingFile := filepath.Join(tmp, "sessions")
	if err := os.WriteFile(blockingFile, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	blockedSpiller, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   tmp,
		MaxBytes:  20,
		MaxLines:  2,
		HeadLines: 1,
		TailLines: 1,
	})
	if err != nil {
		t.Fatalf("blocked NewSpiller: %v", err)
	}
	bw := blockedSpiller.NewWriter(scope.With(context.Background(), scope.Scope{
		SessionID:  "blocked-sess",
		ToolCallID: "blocked-call",
	}))
	_, _ = bw.Write([]byte("line 1\nline 2\nline 3\nline 4\nline 5\n"))
	out := bw.String()
	if strings.Contains(out, "full output saved to") {
		t.Fatalf("expected truncation notice without 'full output saved to' when path is empty, got: %s", out)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation notice in output, got: %s", out)
	}
}
