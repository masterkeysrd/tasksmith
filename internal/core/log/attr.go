package log

import (
	"log/slog"
	"strings"
	"time"
)

// Level represents the minimum severity threshold for log messages.
type Level int

const (
	// LevelDebug enables verbose diagnostic messages and all higher severities.
	LevelDebug Level = -4
	// LevelInfo enables standard informational messages and all higher severities.
	LevelInfo Level = 0
	// LevelWarn enables warning and error messages.
	LevelWarn Level = 4
	// LevelError enables only error messages.
	LevelError Level = 8
)

// String returns the canonical uppercase name of the log level.
func (l Level) String() string {
	switch {
	case l <= LevelDebug:
		return "DEBUG"
	case l <= LevelInfo:
		return "INFO"
	case l <= LevelWarn:
		return "WARN"
	default:
		return "ERROR"
	}
}

func (l Level) toSlogLevel() slog.Level {
	switch {
	case l <= LevelDebug:
		return slog.LevelDebug
	case l <= LevelInfo:
		return slog.LevelInfo
	case l <= LevelWarn:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// ParseLevel parses a case-insensitive level string ("debug", "info", "warn", "warning",
// "error") into a Level. Unrecognized or empty strings default to LevelInfo.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "dbg":
		return LevelDebug
	case "info", "information":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error", "err":
		return LevelError
	default:
		return LevelInfo
	}
}

// ErrorKey is the attribute key used by Err.
const ErrorKey = "error"

// ComponentKey is the attribute key used by ForComponent.
const ComponentKey = "component"

// Attr represents a structured key-value pair attached to a log record.
type Attr struct {
	Key      string
	Value    any
	slogAttr slog.Attr
}

// String constructs an Attr with a string value.
func String(k, v string) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.String(k, v),
	}
}

// Int constructs an Attr with an int value.
func Int(k string, v int) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Int(k, v),
	}
}

// Int64 constructs an Attr with an int64 value.
func Int64(k string, v int64) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Int64(k, v),
	}
}

// Uint64 constructs an Attr with a uint64 value.
func Uint64(k string, v uint64) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Uint64(k, v),
	}
}

// Float64 constructs an Attr with a float64 value.
func Float64(k string, v float64) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Float64(k, v),
	}
}

// Bool constructs an Attr with a bool value.
func Bool(k string, v bool) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Bool(k, v),
	}
}

// Duration constructs an Attr with a time.Duration value.
func Duration(k string, v time.Duration) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Duration(k, v),
	}
}

// Time constructs an Attr with a time.Time value.
func Time(k string, v time.Time) Attr {
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Time(k, v),
	}
}

// Err constructs an Attr under the "error" key for the given error.
// If err is nil, the attribute value is nil.
func Err(err error) Attr {
	if err == nil {
		return Attr{
			Key:      ErrorKey,
			Value:    nil,
			slogAttr: slog.Any(ErrorKey, nil),
		}
	}
	msg := err.Error()
	return Attr{
		Key:      ErrorKey,
		Value:    msg,
		slogAttr: slog.String(ErrorKey, msg),
	}
}

// Any constructs an Attr with an arbitrary value.
func Any(k string, v any) Attr {
	if err, ok := v.(error); ok && err != nil {
		return Attr{
			Key:      k,
			Value:    err.Error(),
			slogAttr: slog.String(k, err.Error()),
		}
	}
	return Attr{
		Key:      k,
		Value:    v,
		slogAttr: slog.Any(k, v),
	}
}

func toSlogAttr(a Attr) slog.Attr {
	if a.slogAttr.Key != "" {
		return a.slogAttr
	}
	switch v := a.Value.(type) {
	case string:
		return slog.String(a.Key, v)
	case int:
		return slog.Int(a.Key, v)
	case int64:
		return slog.Int64(a.Key, v)
	case uint64:
		return slog.Uint64(a.Key, v)
	case float64:
		return slog.Float64(a.Key, v)
	case bool:
		return slog.Bool(a.Key, v)
	case time.Duration:
		return slog.Duration(a.Key, v)
	case time.Time:
		return slog.Time(a.Key, v)
	case error:
		if v == nil {
			return slog.Any(a.Key, nil)
		}
		return slog.String(a.Key, v.Error())
	default:
		return slog.Any(a.Key, v)
	}
}

func toSlogAttrs(attrs []Attr) []slog.Attr {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = toSlogAttr(a)
	}
	return out
}

func toSlogArgs(attrs []Attr) []any {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]any, len(attrs))
	for i, a := range attrs {
		out[i] = toSlogAttr(a)
	}
	return out
}
