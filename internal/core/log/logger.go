package log

import (
	"context"
	"io"
	"log/slog"
	"sync"
)

// Interface defines the structured logging contract for TaskSmith components.
type Interface interface {
	Debug(msg string, attrs ...Attr)
	Info(msg string, attrs ...Attr)
	Warn(msg string, attrs ...Attr)
	Error(msg string, attrs ...Attr)
	ForComponent(component string) Interface
	With(attrs ...Attr) Interface
}

// Logger is a structured JSON Lines (JSONL) logger backed by log/slog.
type Logger struct {
	slogger *slog.Logger
	level   Level
}

var _ Interface = (*Logger)(nil)

// New creates a new structured JSONL Logger writing to w at the specified minimum level.
// If w is nil, io.Discard is used.
func New(w io.Writer, level Level) *Logger {
	if w == nil {
		w = io.Discard
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level.toSlogLevel(),
	})
	return &Logger{
		slogger: slog.New(handler),
		level:   level,
	}
}

// NewRotating creates a size-based RotatingWriter from cfg and returns both a Logger
// writing to it and the underlying RotatingWriter so the caller can close or sync it.
func NewRotating(cfg RotatingWriterConfig, level Level) (*Logger, *RotatingWriter, error) {
	rw, err := NewRotatingWriter(cfg)
	if err != nil {
		return nil, nil, err
	}
	return New(rw, level), rw, nil
}

// Debug logs a message at LevelDebug with optional structured attributes.
func (l *Logger) Debug(msg string, attrs ...Attr) {
	l.slogger.LogAttrs(context.Background(), slog.LevelDebug, msg, toSlogAttrs(attrs)...)
}

// Info logs a message at LevelInfo with optional structured attributes.
func (l *Logger) Info(msg string, attrs ...Attr) {
	l.slogger.LogAttrs(context.Background(), slog.LevelInfo, msg, toSlogAttrs(attrs)...)
}

// Warn logs a message at LevelWarn with optional structured attributes.
func (l *Logger) Warn(msg string, attrs ...Attr) {
	l.slogger.LogAttrs(context.Background(), slog.LevelWarn, msg, toSlogAttrs(attrs)...)
}

// Error logs a message at LevelError with optional structured attributes.
func (l *Logger) Error(msg string, attrs ...Attr) {
	l.slogger.LogAttrs(context.Background(), slog.LevelError, msg, toSlogAttrs(attrs)...)
}

// ForComponent returns a child Logger scoped to the given component name under the
// "component" attribute key.
func (l *Logger) ForComponent(component string) Interface {
	return l.With(String(ComponentKey, component))
}

// With returns a child Logger with the given attributes pre-bound to all subsequent entries.
func (l *Logger) With(attrs ...Attr) Interface {
	if len(attrs) == 0 {
		return &Logger{
			slogger: l.slogger,
			level:   l.level,
		}
	}
	return &Logger{
		slogger: l.slogger.With(toSlogArgs(attrs)...),
		level:   l.level,
	}
}

var (
	defaultMu     sync.RWMutex
	defaultLogger Interface = New(io.Discard, LevelInfo)
)

// Default returns the package-level default Logger.
// By default, it writes to io.Discard so uninitialized logging in a CLI or TUI never
// corrupts standard output.
func Default() Interface {
	defaultMu.RLock()
	l := defaultLogger
	defaultMu.RUnlock()
	return l
}

// SetDefault replaces the package-level default Logger.
// If l is nil, a silent logger writing to io.Discard is installed.
func SetDefault(l Interface) {
	if l == nil {
		l = New(io.Discard, LevelInfo)
	}
	defaultMu.Lock()
	defaultLogger = l
	defaultMu.Unlock()
}

// Debug logs a message at LevelDebug using the default logger.
func Debug(msg string, attrs ...Attr) {
	Default().Debug(msg, attrs...)
}

// Info logs a message at LevelInfo using the default logger.
func Info(msg string, attrs ...Attr) {
	Default().Info(msg, attrs...)
}

// Warn logs a message at LevelWarn using the default logger.
func Warn(msg string, attrs ...Attr) {
	Default().Warn(msg, attrs...)
}

// Error logs a message at LevelError using the default logger.
func Error(msg string, attrs ...Attr) {
	Default().Error(msg, attrs...)
}

// ForComponent returns a component-scoped Logger derived from the default logger.
func ForComponent(component string) Interface {
	return Default().ForComponent(component)
}

// With returns an attribute-scoped Logger derived from the default logger.
func With(attrs ...Attr) Interface {
	return Default().With(attrs...)
}
