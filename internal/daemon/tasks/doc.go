// Package tasks provides the daemon's background task management and execution,
// used to run workloads (notably the `bash` tool) that may outlive the synchronous
// wait window of a single tool call.
//
// # Core Types
//
// A [Task] is a smart unit of work defined by a [Func] workload plus metadata
// (ID, Kind, Title, Command, and a Wait window). It owns its execution lifecycle
// directly via [Task.Run], [Task.Kill], [Task.WriteInput], and [Task.Cancel], and
// implements [io.Writer] for streaming process output.
//
// A [Store] (created via [New]) is an interface that manages the task registry
// for a session and exposes [Store.Add], [Store.Get], [Store.List], and [Store.Close].
//
// # Foreground vs. Background
//
// [Task.Run] consults [Task.Wait] to decide how long to block before returning:
//   - When Wait is zero or negative, the task is backgrounded immediately and
//     Run returns nil so the caller can proceed without waiting.
//   - When Wait is positive, Run blocks until the task finishes, the caller's
//     context is canceled, or the timer elapses. If the timer elapses first,
//     the task is backgrounded and Run returns nil; otherwise Run returns the
//     task's terminal error.
//
// Backgrounded tasks are the only ones returned by [Store.List], which returns
// them sorted by start time ascending, then ID ascending.
//
// # Lifecycle & Status
//
// A task transitions through [github.com/masterkeysrd/tasksmith/api.TaskStatus]
// values: pending (default) → running → one of completed, failed, or canceled.
// A task is marked canceled when it is explicitly killed or when its context is
// canceled; a nonzero exit code or a non-nil error otherwise yields failed.
// Callers inspect terminal state via [Task.Snapshot], [Task.Status],
// [Task.ExitCode], [Task.Err], [Task.Duration], and [Task.LogPath].
//
// # Output Capture
//
// Every write to the task ([Task.Write]) is fanned out to three sinks:
//  1. An in-memory rolling buffer capped at [MaxLogMemBytes] (64 KB), used for
//     fast recent-history access and live tailing.
//  2. A disk log at `<OutputsDir>/<id>.log` capped at [MaxLogDiskBytes]
//     (16 MB); once the cap is reached a truncation marker is appended and no
//     further bytes are written.
//  3. Live output subscribers registered via [Task.SubscribeOutput], each
//     receiving streamed chunks over a buffered channel that is closed when the
//     task finishes.
//
// [Task.Tail] returns the last N lines from the disk log (falling back to the
// in-memory buffer), and [Task.SubscribeOutput] returns the initial buffered
// content alongside the live channel and an unsubscribe function.
//
// # Input & Cancellation
//
// Each running task is given a stdin pipe. [Task.WriteInput] streams data into
// that pipe while the task is running, returning [ErrNotRunning] otherwise.
// [Task.Cancel] closes the stdin pipe and cancels the task's context;
// [Task.Kill] validates that the task is currently running or pending before canceling;
// and [Store.Close] cancels every tracked task.
//
// # Integration
//
// [github.com/masterkeysrd/tasksmith/internal/daemon/session.Session] creates
// one [Store] per session (via [New]) with the session's background context,
// per-session outputs directory, and an update callback that publishes task
// transitions. The builtin `bash` and `tasks` tools
// ([github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin]) consume
// the [Store] to register tasks and to list, inspect, feed, and kill background tasks.
package tasks
