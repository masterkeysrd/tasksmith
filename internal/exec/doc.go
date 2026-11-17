// Package exec implements headless, scriptable TaskSmith runs: the one-shot
// engine behind `tasksmith exec` and the root `-p/--print` shorthand.
//
// A [Run] connects to (or spawns) the Brain Daemon through
// [github.com/masterkeysrd/tasksmith/client.ConnectOrStart], resolves or creates
// a session, subscribes to the session's event stream *before* sending the
// prompt, and renders the settled turn in one of three formats:
//
//   - text: final assistant text on stdout, diagnostics on stderr, plain exit codes.
//   - json: a single [api.ExecResult] envelope with text, session usage, and file changes.
//   - jsonl: NDJSON — a [api.ExecHeaderFrame], verbatim daemon [api.Event] frames,
//     [api.ExecCommandResultFrame]s for client-local commands, and a terminal
//     [api.ExecResult] frame.
//
// With `--input-format jsonl` the run is bidirectional: stdin control frames
// ([api.ExecInputFrame]) map 1:1 onto daemon send, permission, question,
// config, and cancel operations, letting an external process answer interactive
// prompts and drive follow-up turns over the same pipe. In text-input mode an
// interactive prompt cannot be answered, so the run cancels the turn and exits
// with [ExitNeedsInput] instead of hanging.
//
// Client-local `/slash` commands (`/model`, `/mode`, `/agent`, `/compact`, ...)
// are resolved without a UI so the TUI, exec, and external SDKs share one
// command surface; anything unrecognized is forwarded to the daemon, which
// owns workspace commands, skills, and `/mcp:<server>:<prompt>` references.
package exec
