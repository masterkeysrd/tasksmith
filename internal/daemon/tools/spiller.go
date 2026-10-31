package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Default thresholds and slice sizes for tool output spilling.
const (
	DefaultSpillMaxBytes     = 32 * 1024        // 32 KB
	DefaultSpillMaxLines     = 500              // 500 lines
	DefaultSpillHeadLines    = 100              // 100 lines
	DefaultSpillTailLines    = 100              // 100 lines
	DefaultSpillMaxDiskBytes = 16 * 1024 * 1024 // 16 MB safety cap on spilled file size
)

// SpillWriter streams tool output, spilling to disk if limits are exceeded,
// and produces a truncated preview.
type SpillWriter interface {
	io.WriteCloser
	// String returns the full output if within limits, or the truncated
	// head + notice + tail preview if spilled to disk.
	String() string
	// Spilled reports whether the output exceeded thresholds and was written to disk.
	Spilled() bool
	// Path returns the path to the spilled file on disk, or "" if not spilled.
	Path() string
}

// Spiller intercepts large tool output and spills the full content to disk,
// returning a truncated preview with navigational guidance.
type Spiller interface {
	Spill(ctx context.Context, text string) string
	NewWriter(ctx context.Context) SpillWriter
}

// SpillerConfig configures the Spiller.
type SpillerConfig struct {
	BaseDir      string // base directory (e.g. from xdg.WorkspaceDir(rootDir)); required if Workspace is nil
	Workspace    *workspace.Workspace
	MaxBytes     int
	MaxLines     int
	HeadLines    int
	TailLines    int
	MaxDiskBytes int
}

type spiller struct {
	mu           sync.Mutex
	baseDir      string
	maxBytes     int
	maxLines     int
	headLines    int
	tailLines    int
	maxDiskBytes int
}

var _ Spiller = (*spiller)(nil)

// NewSpiller constructs a new Spiller from cfg.
func NewSpiller(cfg SpillerConfig) (Spiller, error) {
	baseDir := strings.TrimSpace(cfg.BaseDir)
	if baseDir == "" && cfg.Workspace != nil {
		dir, err := xdg.WorkspaceDir(cfg.Workspace.RootDir())
		if err != nil {
			return nil, fmt.Errorf("spiller: resolve workspace dir: %w", err)
		}
		baseDir = dir
	}
	if baseDir == "" {
		return nil, fmt.Errorf("%w: base directory or workspace is required", ErrInvalidConfig)
	}

	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultSpillMaxBytes
	}
	maxLines := cfg.MaxLines
	if maxLines <= 0 {
		maxLines = DefaultSpillMaxLines
	}
	headLines := cfg.HeadLines
	if headLines <= 0 {
		headLines = DefaultSpillHeadLines
	}
	tailLines := cfg.TailLines
	if tailLines <= 0 {
		tailLines = DefaultSpillTailLines
	}
	maxDiskBytes := cfg.MaxDiskBytes
	if maxDiskBytes <= 0 {
		maxDiskBytes = DefaultSpillMaxDiskBytes
	}

	return &spiller{
		baseDir:      baseDir,
		maxBytes:     maxBytes,
		maxLines:     maxLines,
		headLines:    headLines,
		tailLines:    tailLines,
		maxDiskBytes: maxDiskBytes,
	}, nil
}

type spillerContextKey struct{}

// WithSpiller binds a Spiller into the context.
func WithSpiller(ctx context.Context, s Spiller) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, spillerContextKey{}, s)
}

// SpillerFrom extracts a Spiller from ctx if present.
func SpillerFrom(ctx context.Context) (Spiller, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ctx.Value(spillerContextKey{}).(Spiller)
	return s, ok
}

// Spill applies output spilling via the Spiller in ctx, or returns text unchanged
// if no Spiller is registered.
func Spill(ctx context.Context, text string) string {
	if s, ok := SpillerFrom(ctx); ok && s != nil {
		return s.Spill(ctx, text)
	}
	return text
}

// NewSpillWriter returns a streaming SpillWriter bound to the Spiller in ctx,
// or an in-memory passthrough writer if no Spiller is registered.
func NewSpillWriter(ctx context.Context) SpillWriter {
	if s, ok := SpillerFrom(ctx); ok && s != nil {
		return s.NewWriter(ctx)
	}
	return &nopSpillWriter{}
}

type nopSpillWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (w *nopSpillWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	return w.buf.Write(p)
}

func (w *nopSpillWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *nopSpillWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *nopSpillWriter) Spilled() bool {
	return false
}

func (w *nopSpillWriter) Path() string {
	return ""
}

func (s *spiller) Spill(ctx context.Context, text string) string {
	w := s.NewWriter(ctx)
	defer w.Close()
	_, _ = w.Write([]byte(text))
	return w.String()
}

func (s *spiller) NewWriter(ctx context.Context) SpillWriter {
	if ctx == nil {
		ctx = context.Background()
	}
	return &spillWriter{
		ctx: ctx,
		s:   s,
	}
}

type spillWriter struct {
	mu           sync.Mutex
	ctx          context.Context
	s            *spiller
	mem          bytes.Buffer
	file         *os.File
	filePath     string
	spilled      bool
	totalBytes   int
	newlines     int
	lastByte     byte
	hasLastByte  bool
	writtenDisk  int
	headBuf      bytes.Buffer
	headLinesCnt int
	tailBuf      []byte
	closed       bool
}

var _ SpillWriter = (*spillWriter)(nil)

func (w *spillWriter) calcLinesLocked() int {
	if w.totalBytes == 0 {
		return 0
	}
	if w.hasLastByte && w.lastByte == '\n' {
		return w.newlines
	}
	return w.newlines + 1
}

func (w *spillWriter) feedHeadLocked(p []byte) {
	maxHeadBytes := w.s.maxBytes / 2
	for _, b := range p {
		if w.headLinesCnt >= w.s.headLines || w.headBuf.Len() >= maxHeadBytes {
			break
		}
		w.headBuf.WriteByte(b)
		if b == '\n' {
			w.headLinesCnt++
		}
	}
}

func (w *spillWriter) appendTailLocked(p []byte) {
	w.tailBuf = append(w.tailBuf, p...)
	if len(w.tailBuf) > w.s.maxBytes {
		discard := len(w.tailBuf) - w.s.maxBytes
		copy(w.tailBuf, w.tailBuf[discard:])
		w.tailBuf = w.tailBuf[:w.s.maxBytes]
	}
}

func (w *spillWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(p) == 0 {
		return 0, nil
	}
	if w.closed {
		return 0, os.ErrClosed
	}

	w.totalBytes += len(p)
	for _, b := range p {
		if b == '\n' {
			w.newlines++
		}
	}
	w.lastByte = p[len(p)-1]
	w.hasLastByte = true

	if !w.spilled {
		w.mem.Write(p)
		totalLines := w.calcLinesLocked()
		if w.totalBytes <= w.s.maxBytes && totalLines <= w.s.maxLines {
			return len(p), nil
		}

		w.spilled = true
		memData := w.mem.Bytes()

		sc, _ := scope.From(w.ctx)
		sessionID := strings.TrimSpace(sc.SessionID)
		if sessionID == "" {
			sessionID = "unknown"
		}
		sessionID = filepath.Base(filepath.Clean(sessionID))
		if sessionID == "." || sessionID == "/" || sessionID == "" {
			sessionID = "unknown"
		}

		toolCallID := strings.TrimSpace(sc.ToolCallID)
		if toolCallID == "" {
			toolCallID = uuid.Must(uuid.NewV7()).String()
		}
		toolCallID = filepath.Base(filepath.Clean(toolCallID))
		if toolCallID == "." || toolCallID == "/" || toolCallID == "" {
			toolCallID = uuid.Must(uuid.NewV7()).String()
		}

		outDir := filepath.Join(w.s.baseDir, "sessions", sessionID, "outputs")
		_ = os.MkdirAll(outDir, 0o755)

		w.s.mu.Lock()
		candidate := filepath.Join(outDir, toolCallID+".txt")
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			w.filePath = candidate
			w.file = f
		} else if errors.Is(err, os.ErrExist) {
			for n := 2; n < 10000; n++ {
				candidate = filepath.Join(outDir, fmt.Sprintf("%s-%d.txt", toolCallID, n))
				f, err = os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
				if err == nil {
					w.filePath = candidate
					w.file = f
					break
				}
				if !errors.Is(err, os.ErrExist) {
					break
				}
			}
		}
		w.s.mu.Unlock()

		if w.file != nil {
			toWrite := len(memData)
			if w.writtenDisk+toWrite > w.s.maxDiskBytes {
				toWrite = w.s.maxDiskBytes - w.writtenDisk
			}
			if toWrite > 0 {
				nw, _ := w.file.Write(memData[:toWrite])
				w.writtenDisk += nw
			}
		}

		w.feedHeadLocked(memData)
		w.appendTailLocked(memData)
		w.mem = bytes.Buffer{}
		return len(p), nil
	}

	w.feedHeadLocked(p)

	if w.file != nil && w.writtenDisk < w.s.maxDiskBytes {
		toWrite := len(p)
		if w.writtenDisk+toWrite > w.s.maxDiskBytes {
			toWrite = w.s.maxDiskBytes - w.writtenDisk
		}
		if toWrite > 0 {
			nw, _ := w.file.Write(p[:toWrite])
			w.writtenDisk += nw
		}
	}

	w.appendTailLocked(p)
	return len(p), nil
}

func (w *spillWriter) closeLocked() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func (w *spillWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}

func (w *spillWriter) Spilled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.spilled
}

func (w *spillWriter) Path() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.filePath
}

func extractHeadLines(buf []byte, headCount int) (string, bool) {
	if headCount <= 0 || len(buf) == 0 {
		return "", false
	}
	linesFound := 0
	for i := range buf {
		if buf[i] == '\n' {
			linesFound++
			if linesFound == headCount {
				return string(buf[:i+1]), true
			}
		}
	}
	return "", false
}

func extractTailLines(buf []byte, tailCount int, totalBytes int) (string, bool) {
	if tailCount <= 0 || len(buf) == 0 {
		return "", false
	}
	tailStart := -1
	linesFound := 0
	idx := len(buf) - 1
	if idx >= 0 && buf[idx] == '\n' {
		idx--
	}
	for idx >= 0 {
		if buf[idx] == '\n' {
			linesFound++
			if linesFound == tailCount {
				tailStart = idx + 1
				break
			}
		}
		idx--
	}
	if idx < 0 {
		if totalBytes == len(buf) {
			linesFound++
			if linesFound == tailCount {
				tailStart = 0
			}
		}
	}
	if tailStart == -1 {
		return "", false
	}
	return string(buf[tailStart:]), true
}

func truncateUTF8Start(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func truncateUTF8End(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func (w *spillWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.closeLocked()

	if !w.spilled {
		return w.mem.String()
	}

	totalLines := w.calcLinesLocked()

	headStr, headOk := extractHeadLines(w.headBuf.Bytes(), w.s.headLines)
	tailStr, tailOk := extractTailLines(w.tailBuf, w.s.tailLines, w.totalBytes)

	if headOk && tailOk && totalLines > w.s.headLines+w.s.tailLines && len(headStr)+len(tailStr) <= w.s.maxBytes {
		omittedLines := totalLines - w.s.headLines - w.s.tailLines
		omittedBytes := w.totalBytes - len(headStr) - len(tailStr)
		var notice string
		if w.filePath != "" {
			notice = fmt.Sprintf("\n... [%d lines (%d bytes) truncated — full output saved to %s. Use view or grep to inspect] ...\n",
				omittedLines, omittedBytes, w.filePath)
		} else {
			notice = fmt.Sprintf("\n... [%d lines (%d bytes) truncated] ...\n",
				omittedLines, omittedBytes)
		}
		return headStr + notice + tailStr
	}

	half := w.s.maxBytes / 2
	headBytes := truncateUTF8Start(w.headBuf.String(), half)
	tailBytes := truncateUTF8End(string(w.tailBuf), half)
	omittedBytes := w.totalBytes - len(headBytes) - len(tailBytes)
	var notice string
	if w.filePath != "" {
		notice = fmt.Sprintf("\n... [%d bytes (%d lines) truncated — full output saved to %s. Use view or grep to inspect] ...\n",
			omittedBytes, totalLines, w.filePath)
	} else {
		notice = fmt.Sprintf("\n... [%d bytes (%d lines) truncated] ...\n",
			omittedBytes, totalLines)
	}
	return headBytes + notice + tailBytes
}
