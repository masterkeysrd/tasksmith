package log

import (
	"time"
)

// Attr represents a single key-value pair for structured logging.
type Attr struct {
	Key string
	Val any
}

// String creates a string attribute.
func String(key, val string) Attr {
	return Attr{Key: key, Val: val}
}

// Int creates an integer attribute.
func Int(key string, val int) Attr {
	return Attr{Key: key, Val: val}
}

// Bool creates a boolean attribute.
func Bool(key string, val bool) Attr {
	return Attr{Key: key, Val: val}
}

// Any creates an attribute with any value.
func Any(key string, val any) Attr {
	return Attr{Key: key, Val: val}
}

// Err creates an error attribute with the key "error".
func Err(err error) Attr {
	return Attr{Key: "error", Val: err}
}

// Duration creates a duration attribute.
func Duration(key string, val time.Duration) Attr {
	return Attr{Key: key, Val: val}
}

// Time creates a time attribute.
func Time(key string, val time.Time) Attr {
	return Attr{Key: key, Val: val}
}
