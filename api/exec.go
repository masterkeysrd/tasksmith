package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// NDJSON framing shared by headless execution clients (`tasksmith exec`, `-p`)
// and external SDKs. Every frame is exactly one JSON object per line, and
// daemon-to-caller frames are verbatim [Event] values so an NDJSON stream and
// an SSE stream carry identical payloads with zero schema drift.

// MaxExecFrameBytes bounds a single NDJSON frame. Attachments and tool results
// can be large, so the reader allows far more than the default scanner token
// size while still refusing unbounded lines.
const MaxExecFrameBytes = 8 << 20

// ErrExecFrameTooLarge reports a frame exceeding [MaxExecFrameBytes].
var ErrExecFrameTooLarge = errors.New("api: exec frame exceeds maximum size")

// ExecInputType discriminates the caller-to-daemon control frames accepted on
// the stdin of a bidirectional `tasksmith exec --input-format jsonl` session.
type ExecInputType string

const (
	// ExecInputSend starts a turn, invokes a slash command, or enqueues a
	// follow-up message. It maps onto [SendRequest].
	ExecInputSend ExecInputType = "send"
	// ExecInputPermission answers a live permission_requested prompt. It maps
	// onto [PermissionDecision].
	ExecInputPermission ExecInputType = "permission"
	// ExecInputQuestion answers a live question_requested prompt. It maps onto
	// [QuestionResponse].
	ExecInputQuestion ExecInputType = "question"
	// ExecInputPlanReview answers a live plan_review_requested prompt. It maps
	// onto [PlanReviewDecision].
	ExecInputPlanReview ExecInputType = "plan_review"
	// ExecInputConfig updates session settings mid-session. It maps onto
	// [UpdateSessionRequest].
	ExecInputConfig ExecInputType = "config"
	// ExecInputCancel aborts the in-flight turn and keeps the session open.
	ExecInputCancel ExecInputType = "cancel"
	// ExecInputClose asks the process to finish the current work and exit.
	ExecInputClose ExecInputType = "close"
)

// ExecInputFrame is one caller-to-daemon control frame. The payload field
// matching Type carries the canonical api payload; Send additionally accepts a
// bare Content string so scripts can write `{"type":"send","content":"..."}`.
// SessionID optionally targets a child subagent session for permission,
// question, or plan_review replies; when omitted, exec routes by
// request_id / tool_call_id or defaults to the root session.
type ExecInputFrame struct {
	Type       ExecInputType         `json:"type"`
	SessionID  string                `json:"session_id,omitempty"`
	Send       *SendRequest          `json:"send,omitempty"`
	Content    string                `json:"content,omitempty"`
	Permission *PermissionDecision   `json:"permission,omitempty"`
	Question   *QuestionResponse     `json:"question,omitempty"`
	PlanReview *PlanReviewDecision   `json:"plan_review,omitempty"`
	Config     *UpdateSessionRequest `json:"config,omitempty"`
}

// SendPayload resolves the frame's send request, accepting either a full
// [SendRequest] or the bare Content shorthand. It returns nil when the frame
// carries no send payload.
func (f ExecInputFrame) SendPayload() *SendRequest {
	if f.Send != nil {
		return f.Send
	}
	if f.Content != "" {
		return &SendRequest{Content: f.Content}
	}
	return nil
}

// ExecHeaderType is the frame type of the [ExecHeaderFrame] that opens an exec
// NDJSON stream. It is distinct from [EventConnected], which reports the SSE
// subscriber identity.
const ExecHeaderType = "session"

// ExecHeaderFrame is the first frame of an exec NDJSON stream. It publishes the
// resolved session identity and effective configuration so callers can drive a
// session they never created (for example a piped one-shot run).
type ExecHeaderFrame struct {
	Type        string        `json:"type"`
	SessionID   string        `json:"session_id"`
	Workspace   string        `json:"workspace,omitempty"`
	Model       *Model        `json:"model,omitempty"`
	Config      SessionConfig `json:"config"`
	SandboxMode SandboxMode   `json:"sandbox_mode,omitempty"`
	DaemonVer   string        `json:"daemon_version,omitempty"`
}

// ExecCommandResultType is the frame type of an [ExecCommandResultFrame].
const ExecCommandResultType = "command_result"

// ExecResultType is the frame type of the terminal [ExecResult] frame in an
// NDJSON (`--output-format jsonl`) stream.
const ExecResultType = "result"

// ExecCommandResultFrame reports the outcome of a client-local `/slash` command
// (or a rejected command) in an NDJSON stream. Daemon turn events never use
// this type, so consumers can tell harness output apart from client output.
type ExecCommandResultFrame struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Output  string `json:"output,omitempty"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ExecResultStatus enumerates the terminal statuses of an [ExecResult].
const (
	ExecResultCompleted = "completed"
	ExecResultFailed    = "failed"
	ExecResultCanceled  = "canceled"
	// ExecResultNeedsInput reports that a non-interactive run stopped on a
	// permission or question it could not answer.
	ExecResultNeedsInput = "needs_input"
)

// ExecResult is the single-object envelope emitted by `--output-format json`.
// It summarizes the settled turn: final assistant text, session usage, file
// mutations, and any terminal error.
type ExecResult struct {
	Type      string        `json:"type,omitempty"`
	SessionID string        `json:"session_id"`
	Status    string        `json:"status"`
	Text      string        `json:"text"`
	Usage     *SessionUsage `json:"usage,omitempty"`
	Changes   []FileChange  `json:"changes,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// WriteNDJSON marshals v as a single NDJSON frame.
func WriteNDJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("api: marshal exec frame: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("api: write exec frame: %w", err)
	}
	return nil
}

// WriteEvent writes a daemon event as one NDJSON frame.
func WriteEvent(w io.Writer, ev Event) error { return WriteNDJSON(w, ev) }

// ExecFrameReader decodes NDJSON frames from a stream.
type ExecFrameReader struct {
	scanner *bufio.Scanner
}

// NewExecFrameReader wraps r with the enlarged frame budget.
func NewExecFrameReader(r io.Reader) *ExecFrameReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxExecFrameBytes)
	return &ExecFrameReader{scanner: scanner}
}

// Next decodes the next frame into v. Blank lines are skipped so hand-written
// input tolerates stray newlines. It returns [io.EOF] when the stream is
// exhausted and [ErrExecFrameTooLarge] when a frame exceeds the budget.
func (r *ExecFrameReader) Next(v any) error {
	for r.scanner.Scan() {
		line := r.scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := json.Unmarshal(line, v); err != nil {
			return fmt.Errorf("api: decode exec frame: %w", err)
		}
		return nil
	}
	if err := r.scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("%w: %v", ErrExecFrameTooLarge, err)
		}
		return err
	}
	return io.EOF
}

// NextEvent decodes the next frame as a daemon [Event].
func (r *ExecFrameReader) NextEvent() (Event, error) {
	var ev Event
	if err := r.Next(&ev); err != nil {
		return Event{}, err
	}
	return ev, nil
}

// NextInputFrame decodes the next frame as a caller-to-daemon control frame.
func (r *ExecFrameReader) NextInputFrame() (ExecInputFrame, error) {
	var f ExecInputFrame
	if err := r.Next(&f); err != nil {
		return ExecInputFrame{}, err
	}
	return f, nil
}
