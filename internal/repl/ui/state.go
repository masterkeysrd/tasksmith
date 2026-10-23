package ui

// State represents the interactive lifecycle phase of the REPL.
type State int

const (
	// StateIdle indicates the REPL is waiting for user prompt or slash command input.
	StateIdle State = iota
	// StateStreaming indicates the REPL is streaming an active assistant turn.
	StateStreaming
	// StateApproval indicates the REPL is paused awaiting user resolution of a tool permission request.
	StateApproval
	// StatePicker indicates an interactive command picker modal is active.
	StatePicker
)

// String returns the human-readable name of the [State].
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateStreaming:
		return "streaming"
	case StateApproval:
		return "approval"
	case StatePicker:
		return "picker"
	default:
		return "unknown"
	}
}
