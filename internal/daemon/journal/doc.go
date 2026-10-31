// Package journal tracks per-session file reads and persists file change
// metadata in SQLite paired with content-addressable snapshots in the
// session's blobs directory, enabling on-demand diff generation, point-in-time
// recovery, and conflict-aware 3-way baseline reversion.
//
// # Architecture
//
// A [Journal] (created via [New]) is scoped to a single session and backed by
// two SQLite tables:
//   - `journal_files`: one row per tracked file, recording the baseline hash
//     and binary flag.
//   - `journal_entries`: one row per change event, recording the before/after
//     hashes, scope metadata (turn, message, tool call), source, kind, and
//     line-diff statistics.
//
// Content snapshots are stored as SHA-256-addressed blob files under the
// session's `blobs/` directory. The journal maintains an in-memory index of
// current file hashes for fast Read-Before-Overwrite and Compare-And-Swap
// verification via [Journal.LastHash] and [Journal.LastContent].
//
// # Recording Changes
//
// [Journal.RecordChange] persists a [std.FileChange] atomically: it writes
// before/after content blobs, inserts the file tracking row and the change
// entry in a single transaction, and updates the in-memory hash index. When
// the change source is [github.com/masterkeysrd/tasksmith/api.SourceAgent] and a [scope.Scope] is present in the
// context, the journal propagates turn, message, tool-call, and tool-name
// metadata from the scope onto the change.
//
// [Journal.RecordRead] records an observed content hash in memory only (no
// SQLite write), supporting the Read-Before-Overwrite invariant enforced by
// [std.Std].
//
// # Diffing & Recovery
//
//   - [Journal.DiffEntry] computes a unified diff for a specific entry or for
//     the whole file relative to the session baseline.
//   - [Journal.RecoverAt] restores a file to its state immediately before or
//     after a given entry by writing the corresponding blob to the sandbox.
//   - [Journal.RevertToBaseline] reverts agent changes back to the session
//     baseline, performing a 3-way merge when force is false and returning
//     [ErrRevertConflict] on conflicting external edits.
//   - [Journal.RevertSession] reverts all tracked files to their baselines.
//
// # Metrics Integration
//
// When configured with [WithMetrics], the journal records a
// [metrics.KindFileChange] record for every [Journal.RecordChange] call,
// enabling usage reporting and per-tool code-change attribution.
//
// # Integration
//
// [github.com/masterkeysrd/tasksmith/internal/daemon/session.Session] creates
// one [Journal] per session and exposes it via [Session.Journal]. The journal
// implements [github.com/masterkeysrd/tasksmith/internal/daemon/std.FileTracker]
// and is injected into [github.com/masterkeysrd/tasksmith/internal/daemon/std.Std]
// to enforce CAS safety on all mutating filesystem operations.
package journal
