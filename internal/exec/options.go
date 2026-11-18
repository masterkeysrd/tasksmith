package exec

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

// InputFormat selects how a run consumes stdin.
type InputFormat string

const (
	// InputText treats stdin as one blob of contextual prompt text (or ignores
	// it when empty). Interactive prompts cannot be answered in this mode.
	InputText InputFormat = "text"
	// InputJSONL makes stdin a stream of NDJSON control frames, enabling a
	// bidirectional session. See [api.ExecInputFrame].
	InputJSONL InputFormat = "jsonl"
)

// OutputFormat selects how a run renders progress and its outcome on stdout.
type OutputFormat string

const (
	// OutputText prints only the final assistant text; diagnostics go to stderr.
	OutputText OutputFormat = "text"
	// OutputJSON prints a single [api.ExecResult] envelope after the run settles.
	OutputJSON OutputFormat = "json"
	// OutputJSONL prints NDJSON: a header frame, verbatim daemon events,
	// client command results, and a terminal result frame.
	OutputJSONL OutputFormat = "jsonl"
)

// ParseInputFormat validates a raw --input-format value.
func ParseInputFormat(raw string) (InputFormat, error) {
	switch f := InputFormat(strings.ToLower(strings.TrimSpace(raw))); f {
	case "", InputText:
		return InputText, nil
	case InputJSONL:
		return InputJSONL, nil
	default:
		return "", fmt.Errorf("invalid --input-format %q (want %q or %q)", raw, InputText, InputJSONL)
	}
}

// ParseOutputFormat validates a raw --output-format value.
func ParseOutputFormat(raw string) (OutputFormat, error) {
	switch f := OutputFormat(strings.ToLower(strings.TrimSpace(raw))); f {
	case "", OutputText:
		return OutputText, nil
	case OutputJSON, OutputJSONL:
		return f, nil
	default:
		return "", fmt.Errorf("invalid --output-format %q (want %q, %q, or %q)", raw, OutputText, OutputJSON, OutputJSONL)
	}
}

// Options fully configures a headless run. Callers own Stdin/Stdout/Stderr so
// tests (and embedding programs) can pipe frames without touching process
// streams; nil streams fall back to the process defaults.
type Options struct {
	// Socket is the daemon socket path; empty selects the XDG default.
	Socket string
	// Workspace is the folder the session operates on (client CWD header).
	Workspace string

	// Prompt is the initial text submitted when the run starts. It may be
	// empty in bidirectional (jsonl input) mode, where stdin drives everything.
	Prompt string
	// StdinText is piped stdin content prepended to the prompt as context.
	// Ignored in jsonl input mode.
	StdinText string

	InputFormat  InputFormat
	OutputFormat OutputFormat

	// SessionID resumes an existing session; empty creates a fresh one.
	SessionID string
	// Continue resolves an empty SessionID to the workspace's most recently
	// updated top-level session (`-c` / `--continue`). A workspace without
	// sessions silently falls back to a fresh session.
	Continue bool
	// Fork branches the resolved session (--session or -c) and runs on the
	// branch instead of mutating the source (`--fork`). Forking a workspace
	// with no sessions is a usage error.
	Fork bool
	// MaxTurns bounds how many daemon turn completions the run waits for
	// before it stops (0 = unbounded). It is enforced client-side because the
	// harness caps steps within a turn, not turns across a session.
	MaxTurns int
	// Ephemeral deletes the session after the run settles.
	Ephemeral bool
	// Force allows a resumed session's --model change even if it drops input
	// modalities present in active post-compaction context.
	Force bool
	// Title seeds the title of a newly created session.
	Title string
	// Config overlays the created/resumed session (agent, model, mode, sandbox).
	Config api.SessionConfig

	// Stdin/Stdout/Stderr default to the process streams when nil.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Interactive reports whether the run can answer daemon prompts (permission
// requests, questions) through stdin control frames.
func (o *Options) Interactive() bool { return o.InputFormat == InputJSONL }

// ExitCodeError carries a process exit code distinct from the generic failure
// code so `tasksmith exec` can signal needs-input and cancellation to callers.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exec: exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitCodeError) Unwrap() error { return e.Err }

// ExitCode extracts a run's process exit code from an error, reporting 0 when
// err is nil or carries no exec-specific code.
func ExitCode(err error) int {
	if exitErr, ok := errors.AsType[*ExitCodeError](err); ok {
		return exitErr.Code
	}
	if err != nil {
		return ExitError
	}
	return ExitOK
}

// Process exit codes for a headless run.
const (
	// ExitOK marks a completed run.
	ExitOK = 0
	// ExitError marks any generic failure (connection, session, turn error).
	ExitError = 1
	// ExitUsage marks invalid flags or a run with no prompt at all.
	ExitUsage = 2
	// ExitNeedsInput marks a non-interactive run that stopped on a permission
	// or question it could not answer.
	ExitNeedsInput = 4
	// ExitCanceled marks a run whose turn was canceled (cancel frame or signal).
	ExitCanceled = 5
)
