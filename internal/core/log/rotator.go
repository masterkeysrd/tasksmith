package log

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

const (
	// DefaultStream is the default log stream name when RotatingWriterConfig.Stream is empty.
	DefaultStream = "tasksmith"
	// DefaultMaxSizeBytes is the default maximum size (10 MB) of an active log file before rotation.
	DefaultMaxSizeBytes int64 = 10 * 1024 * 1024
	// DefaultMaxBackups is the default maximum number of rotated backup files retained per stream.
	DefaultMaxBackups = 5
	// DefaultMaxAge is the default maximum retention duration (7 days) for rotated backup files.
	DefaultMaxAge = 7 * 24 * time.Hour

	backupTimeFormat = "20060102-150405.000"
	dirPerm          = 0o700
	filePerm         = 0o600
)

// RotatingWriterConfig configures a size-based RotatingWriter.
type RotatingWriterConfig struct {
	// Dir is the directory where active and rotated log files are stored.
	// If empty, it is resolved via xdg.LogsDir().
	Dir string
	// Stream is the base stream name (e.g. "daemon" or "client").
	// Defaults to "tasksmith", producing "<Dir>/<Stream>.jsonl".
	Stream string
	// MaxSizeBytes is the maximum size in bytes of the active log file before it is rotated.
	// Defaults to 10 MB (10 * 1024 * 1024) when <= 0.
	MaxSizeBytes int64
	// MaxBackups is the maximum number of rotated backup files to retain for the stream.
	// Defaults to 5 when <= 0.
	MaxBackups int
	// MaxAge is the maximum age of rotated backup files before they are pruned.
	// Defaults to 7 days when <= 0.
	MaxAge time.Duration
}

// RotatingWriter is a thread-safe io.WriteCloser that appends to an active JSONL log file
// and rotates it when its size would exceed MaxSizeBytes.
type RotatingWriter struct {
	mu           sync.Mutex
	dir          string
	stream       string
	path         string
	maxSizeBytes int64
	maxBackups   int
	maxAge       time.Duration
	file         *os.File
	currentSize  int64
	closed       bool
}

// NewRotatingWriter creates and opens a RotatingWriter configured by cfg.
func NewRotatingWriter(cfg RotatingWriterConfig) (*RotatingWriter, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		resolved, err := xdg.LogsDir()
		if err != nil {
			return nil, fmt.Errorf("resolve logs directory: %w", err)
		}
		dir = resolved
	}

	stream := sanitizeStream(cfg.Stream)
	maxSize := cfg.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = DefaultMaxSizeBytes
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = DefaultMaxBackups
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}

	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", dir, err)
	}

	activePath := filepath.Join(dir, stream+".jsonl")
	w := &RotatingWriter{
		dir:          dir,
		stream:       stream,
		path:         activePath,
		maxSizeBytes: maxSize,
		maxBackups:   maxBackups,
		maxAge:       maxAge,
	}

	if err := w.openLocked(); err != nil {
		return nil, err
	}

	return w, nil
}

func sanitizeStream(stream string) string {
	s := strings.TrimSpace(stream)
	s = strings.TrimSuffix(s, ".jsonl")
	if s == "" {
		return DefaultStream
	}
	base := filepath.Base(s)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return DefaultStream
	}
	return base
}

func (w *RotatingWriter) openLocked() error {
	if err := os.MkdirAll(w.dir, dirPerm); err != nil {
		return fmt.Errorf("create log directory %q: %w", w.dir, err)
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", w.path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log file %q: %w", w.path, err)
	}

	w.file = f
	w.currentSize = info.Size()
	return nil
}

// Path returns the filesystem path of the active log file (<Dir>/<Stream>.jsonl).
func (w *RotatingWriter) Path() string {
	return w.path
}

// Write writes p to the active log file, rotating it first if the write would cause the
// file size to exceed MaxSizeBytes and the file is currently non-empty.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, os.ErrClosed
	}

	if w.file == nil {
		if err := w.openLocked(); err != nil {
			return 0, err
		}
	}

	if w.currentSize > 0 && w.currentSize+int64(len(p)) > w.maxSizeBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.currentSize += int64(n)
	return n, err
}

// Sync flushes the active log file's in-memory state to disk.
func (w *RotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return os.ErrClosed
	}
	if w.file == nil {
		return nil
	}
	return w.file.Sync()
}

// Close closes the active log file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	w.currentSize = 0
	return err
}

func (w *RotatingWriter) rotateLocked() error {
	if w.file != nil {
		_ = w.file.Sync()
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("close active log file %q before rotation: %w", w.path, err)
		}
		w.file = nil
		w.currentSize = 0
	}

	backupPath := w.nextBackupPathLocked(time.Now().UTC())
	if err := os.Rename(w.path, backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rename active log file %q to %q: %w", w.path, backupPath, err)
	}

	if err := w.openLocked(); err != nil {
		return err
	}

	w.pruneLocked(time.Now().UTC())
	return nil
}

func (w *RotatingWriter) nextBackupPathLocked(now time.Time) string {
	ts := now
	for {
		name := fmt.Sprintf("%s-%s.jsonl", w.stream, ts.Format(backupTimeFormat))
		candidate := filepath.Join(w.dir, name)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		ts = ts.Add(time.Millisecond)
	}
}

type backupEntry struct {
	name       string
	path       string
	modTime    time.Time
	parsedTime time.Time
}

func (w *RotatingWriter) pruneLocked(now time.Time) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	prefix := w.stream + "-"
	suffix := ".jsonl"
	activeName := w.stream + ".jsonl"

	var backups []backupEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == activeName || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		tsPart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		parsed, _ := time.Parse(backupTimeFormat, tsPart)

		backups = append(backups, backupEntry{
			name:       name,
			path:       filepath.Join(w.dir, name),
			modTime:    info.ModTime().UTC(),
			parsedTime: parsed,
		})
	}

	// Prune backups older than MaxAge first.
	var retained []backupEntry
	for _, b := range backups {
		if w.maxAge > 0 {
			expiredByModTime := now.Sub(b.modTime) > w.maxAge
			expiredByName := !b.parsedTime.IsZero() && now.Sub(b.parsedTime) > w.maxAge
			if expiredByModTime || expiredByName {
				_ = os.Remove(b.path)
				continue
			}
		}
		retained = append(retained, b)
	}

	if w.maxBackups <= 0 || len(retained) <= w.maxBackups {
		return
	}

	// Sort oldest first so we remove the oldest excess backups.
	slices.SortFunc(retained, func(a, b backupEntry) int {
		if !a.modTime.Equal(b.modTime) {
			if a.modTime.Before(b.modTime) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.name, b.name)
	})

	excess := len(retained) - w.maxBackups
	for i := range excess {
		_ = os.Remove(retained[i].path)
	}
}
