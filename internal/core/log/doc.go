// Package log provides structured JSON Lines (JSONL) logging and size-based log file
// rotation for TaskSmith.
//
// # Silent by Default
//
// Because TaskSmith operates both as a background daemon and as an interactive CLI/TUI,
// accidental writes to standard output or standard error can corrupt terminal UI rendering
// or machine-readable command output. The package-level default logger therefore writes to
// [io.Discard] at [LevelInfo] until explicitly configured via [SetDefault].
//
// # Structured JSONL Output
//
// Loggers created with [New] or [NewRotating] emit structured JSON Lines records backed by
// the standard library's [log/slog] JSON handler. Callers attach strongly typed key-value
// metadata using [Attr] constructors such as [String], [Int], [Int64], [Uint64], [Float64],
// [Bool], [Duration], [Time], [Err], and [Any].
//
// # Component and Attribute Scoping
//
// Subsystems can derive scoped loggers using [Interface.ForComponent] (which attaches a
// standardized "component" attribute) and [Interface.With] (which pre-binds arbitrary
// attributes to all subsequent log entries).
//
// # Size-Based Rotation and Retention Pruning
//
// [RotatingWriter] implements a thread-safe [io.WriteCloser] that writes to an active
// stream file (<Dir>/<Stream>.jsonl) with 0600 permissions inside a 0700 log directory
// (defaulting to [xdg.LogsDir]). Before a write that would cause the active file to
// exceed MaxSizeBytes (default 10 MB), [RotatingWriter] closes and renames the current
// file to a timestamped backup (<Stream>-<YYYYMMDD-HHMMSS.000>.jsonl), opens a fresh
// active file, and prunes backup files for that stream that exceed MaxBackups (default 5)
// or are older than MaxAge (default 7 days).
package log
