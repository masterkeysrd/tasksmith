package shellguard

// ActionType represents the fundamental action of a command.
type ActionType string

const (
	ActionRead    ActionType = "read"    // Known read-only (cat, ls, git status)
	ActionWrite   ActionType = "write"   // Known write (touch, git commit) or explicit shell redirect (>)
	ActionDelete  ActionType = "delete"  // Known destructive (rm, git rm)
	ActionExec    ActionType = "exec"    // Known arbitrary execution (e.g., explicitly calling ./run.sh)
	ActionUnknown ActionType = "unknown" // Command not found in classifier rules (e.g., custom binaries)
)

// SafetyLevel represents the safety category of an operation.
type SafetyLevel string

const (
	SafetySafe    SafetyLevel = "safe"    // Inside workspace boundary
	SafetyUnsafe  SafetyLevel = "unsafe"  // Outside boundary or explicitly destructive
	SafetyUnknown SafetyLevel = "unknown" // Unresolvable dynamic path
)

// Operation represents an evaluation of a command target.
type Operation struct {
	Command *ParsedCommand // Link back to the AST node
	Action  ActionType
	Safety  SafetyLevel
	Path    string // Resolved virtual CWD or target file
	CWD     string // Virtual CWD at command execution
}
