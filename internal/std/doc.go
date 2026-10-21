// Package std provides TaskSmith's Standard Operations Library for executing
// filesystem, process, and network operations strictly within an isolated
// [sandbox.Sandbox].
//
// # Architecture
//
// Every operation exposed by [Std] delegates underlying system access to the
// injected sandbox capabilities:
//   - Filesystem operations ([Std.View], [Std.List], [Std.Glob], [Std.Grep],
//     [Std.Write], [Std.Edit], [Std.Remove], [Std.SyncExternalChanges]) execute
//     exclusively through [sandbox.Sandbox.FS].
//   - Process execution ([Std.Bash]) runs exclusively through
//     [sandbox.Sandbox.Proc] and automatically synchronizes any external file
//     mutations made by the command upon completion.
//   - Network operations ([Std.Fetch], [Std.Download]) execute exclusively
//     through the policy-enforced HTTP client from [sandbox.Sandbox.Net].
//
// # Read-Before-Overwrite & Compare-And-Swap (CAS)
//
// Mutating operations on existing files ([Std.Write], [Std.Edit], [Std.Remove],
// and [Std.Download] when overwriting) enforce two safety invariants through the
// injected [FileTracker] unless explicitly bypassed via Force:
//  1. Read-Before-Overwrite ([ErrFileNotRead]): An existing file must have a
//     tracked content hash in the [FileTracker] before it can be modified or
//     removed.
//  2. Compare-And-Swap ([ErrFileModified]): The file's current on-disk SHA-256
//     digest must match the last tracked hash in the [FileTracker]. If the file
//     was altered outside the agent's last known state, the mutation is aborted.
//
// # Provenance Tracking (Agent vs. External)
//
// All file mutations are attributed with a [ChangeSource]:
//   - [SourceAgent]: Mutations performed directly via [Std.Write], [Std.Edit],
//     [Std.Remove], or [Std.Download].
//   - [SourceExternal]: Mutations detected on disk when re-reading a tracked
//     file in [Std.View] or scanning tracked paths in [Std.SyncExternalChanges]
//     (including after [Std.Bash] command execution).
package std
