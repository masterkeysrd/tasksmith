package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

// Interface defines the logging capabilities for the application.
type Interface interface {
	// Debug logs a message at the debug level.
	Debug(msg string, attrs ...Attr)
	// Info logs a message at the info level.
	Info(msg string, attrs ...Attr)
	// Warn logs a message at the warn level.
	Warn(msg string, attrs ...Attr)
	// Error logs a message at the error level.
	Error(msg string, attrs ...Attr)

	// ForComponent returns a new logger with the component attribute set.
	ForComponent(string) Interface
}

// Logger is the default implementation of the Interface using slog.
type Logger struct {
	slog      *slog.Logger
	component string
}

// New creates a new Logger instance writing to the provided writer.
func New(w io.Writer) *Logger {
	logger := slog.New(slog.NewJSONHandler(w, nil))
	return &Logger{
		slog: logger,
	}
}

var defaultLogger Interface = New(os.Stdout)

// SetDefault sets the provided logger as the default logger.
func SetDefault(l Interface) {
	defaultLogger = l
}

func Default() Interface {
	return defaultLogger
}

// Debug logs a message at the debug level using the default logger.
func Debug(msg string, attrs ...Attr) {
	defaultLogger.Debug(msg, attrs...)
}

// Info logs a message at the info level using the default logger.
func Info(msg string, attrs ...Attr) {
	defaultLogger.Info(msg, attrs...)
}

// Warn logs a message at the warn level using the default logger.
func Warn(msg string, attrs ...Attr) {
	defaultLogger.Warn(msg, attrs...)
}

// Error logs a message at the error level using the default logger.
func Error(msg string, attrs ...Attr) {
	defaultLogger.Error(msg, attrs...)
}

// DefaultLogFilename returns a filename in the format log-YYYY-MM-DD.jsonl.
func DefaultLogFilename() string {
	return fmt.Sprintf("log-%s.jsonl", time.Now().Format("2006-01-02"))
}

// Debug logs a message at the debug level.
func (l *Logger) Debug(msg string, attrs ...Attr) {
	l.slog.LogAttrs(context.Background(), slog.LevelDebug, msg, toSlogAttrs(attrs)...)
}

// Info logs a message at the info level.
func (l *Logger) Info(msg string, attrs ...Attr) {
	l.slog.LogAttrs(context.Background(), slog.LevelInfo, msg, toSlogAttrs(attrs)...)
}

// Warn logs a message at the warn level.
func (l *Logger) Warn(msg string, attrs ...Attr) {
	l.slog.LogAttrs(context.Background(), slog.LevelWarn, msg, toSlogAttrs(attrs)...)
}

// Error logs a message at the error level.
func (l *Logger) Error(msg string, attrs ...Attr) {
	l.slog.LogAttrs(context.Background(), slog.LevelError, msg, toSlogAttrs(attrs)...)
}

// ForComponent returns a new logger with the component attribute set.
func (l *Logger) ForComponent(component string) Interface {
	return &Logger{
		slog:      l.slog.With(slog.String("component", component)),
		component: component,
	}
}

// toSlogAttrs converts internal Attr slices to slog.Attr slices.
func toSlogAttrs(attrs []Attr) []slog.Attr {
	slogAttrs := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		slogAttrs[i] = slog.Any(attr.Key, attr.Val)
	}
	return slogAttrs
}
