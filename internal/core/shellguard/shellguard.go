package shellguard

import (
	"os"
	"path/filepath"
)

// DefaultMaxOperations is the default maximum number of individual operations
// Guard.Analyze will return before collapsing them into a single compound
// Operation to prevent approval prompt bloat.
const DefaultMaxOperations = 3

// DefaultProtectedDirs returns the default list of protected workspace directories
// where write, delete, or exec operations are marked SafetyUnsafe.
var DefaultProtectedDirs = []string{".git", ".bare", ".agents"}

// ActionType represents the fundamental action of a command, ordered from
// lowest to highest severity.
type ActionType uint8

const (
	ActionRead    ActionType = iota // Known read-only (cat, ls, git status)
	ActionUnknown                   // Command not found in classifier rules (e.g., custom binaries)
	ActionWrite                     // Known write (touch, git commit) or explicit shell redirect (>)
	ActionExec                      // Known arbitrary execution (e.g., explicitly calling ./run.sh)
	ActionDelete                    // Known destructive (rm, git rm)
)

// String returns the canonical lowercase name of the action type.
func (a ActionType) String() string {
	switch a {
	case ActionRead:
		return "read"
	case ActionUnknown:
		return "unknown"
	case ActionWrite:
		return "write"
	case ActionExec:
		return "exec"
	case ActionDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// SafetyLevel represents the safety category of an operation, ordered from
// safest to most unsafe.
type SafetyLevel uint8

const (
	SafetySafe    SafetyLevel = iota // Inside workspace boundary
	SafetyUnknown                    // Unresolvable dynamic path
	SafetyUnsafe                     // Outside boundary or explicitly destructive
)

// String returns the canonical lowercase name of the safety level.
func (s SafetyLevel) String() string {
	switch s {
	case SafetySafe:
		return "safe"
	case SafetyUnknown:
		return "unknown"
	case SafetyUnsafe:
		return "unsafe"
	default:
		return "unknown"
	}
}

// Operation represents an evaluation of a command target.
type Operation struct {
	Command    *ParsedCommand // Link back to the AST node (or summary command when collapsed)
	Action     ActionType
	Safety     SafetyLevel
	Path       string // Resolved virtual CWD or target file
	CWD        string // Virtual CWD at command execution
	IsCompound bool   // True when collapsed from a complex script or > MaxOperations
}

// Config configures a Guard instance.
type Config struct {
	WorkspaceDir  string
	HomeDir       string   // Optional; used for ~ expansion (falls back to os.UserHomeDir once at New() time)
	ProtectedDirs []string // Defaults to DefaultProtectedDirs (.git, .bare, .agents)
	MaxOperations int      // Defaults to DefaultMaxOperations (3)
	Policy        *Policy  // Defaults to DefaultPolicy()
}

// Guard performs AST-based shell command analysis and permission matching.
type Guard struct {
	workspaceDir  string
	homeDir       string
	protectedDirs []string
	maxOperations int
	policy        Policy
}

// New constructs a Guard with the provided configuration, applying sensible
// defaults for any omitted fields.
func New(cfg Config) *Guard {
	wsDir := cfg.WorkspaceDir
	if wsDir != "" {
		wsDir = filepath.Clean(wsDir)
	}

	homeDir := cfg.HomeDir
	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = filepath.Clean(h)
		}
	} else {
		homeDir = filepath.Clean(homeDir)
	}

	protected := cfg.ProtectedDirs
	if protected == nil {
		protected = DefaultProtectedDirs
	}

	maxOps := cfg.MaxOperations
	if maxOps <= 0 {
		maxOps = DefaultMaxOperations
	}

	policy := defaultPolicy
	if cfg.Policy != nil {
		policy = *cfg.Policy
	}

	return &Guard{
		workspaceDir:  wsDir,
		homeDir:       homeDir,
		protectedDirs: protected,
		maxOperations: maxOps,
		policy:        policy,
	}
}
