package shellguard

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
)

// DefaultMaxOperations is the default maximum number of individual operations
// Guard.Analyze will return before collapsing them into a single compound
// Operation to prevent approval prompt bloat.
const DefaultMaxOperations = 3

// MaxGranularOperations is an alias for DefaultMaxOperations.
const MaxGranularOperations = DefaultMaxOperations

// DefaultProtectedDirs is the default list of protected workspace directories
// where write, delete, or exec operations are marked SafetyUnsafe.
var DefaultProtectedDirs = guard.DefaultProtectedDirs

// ActionType classifies the fundamental category of a command operation.
type ActionType = guard.ActionType

const (
	ActionRead    = guard.ActionRead    // Known read-only (cat, ls, git status)
	ActionUnknown = guard.ActionUnknown // Command not found in classifier rules (e.g., custom binaries)
	ActionNet     = guard.ActionNet     // Network access operation
	ActionWrite   = guard.ActionWrite   // Known write (touch, git commit) or explicit shell redirect (>)
	ActionExec    = guard.ActionExec    // Known arbitrary execution (e.g., explicitly calling ./run.sh)
	ActionDelete  = guard.ActionDelete  // Known destructive (rm, git rm)
)

// SafetyLevel classifies the risk profile of an operation.
type SafetyLevel = guard.SafetyLevel

const (
	SafetySafe    = guard.SafetySafe    // Inside workspace boundary
	SafetyOutside = guard.SafetyOutside // Read-only access outside workspace (non-sensitive path)
	SafetyUnknown = guard.SafetyUnknown // Unresolvable dynamic path
	SafetyUnsafe  = guard.SafetyUnsafe  // Sensitive path, outside write/exec, or explicitly destructive
)

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
	ReadDirs      []string // Additional read-allowed directories outside WorkspaceDir (marked SafetySafe for reads)
	WriteDirs     []string // Additional read/write/delete-allowed directories outside WorkspaceDir (marked SafetySafe)
	MaxOperations int      // Defaults to DefaultMaxOperations (3)
	Policy        *Policy  // Defaults to DefaultPolicy()
}

// Guard performs AST-based shell command analysis and permission matching.
type Guard struct {
	workspaceDir  string
	homeDir       string
	protectedDirs []string
	readDirs      []string
	writeDirs     []string
	maxOperations int
	policy        Policy
}

func cleanDirList(dirs []string, homeDir string) []string {
	if len(dirs) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(dirs))
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		cleaned = append(cleaned, fsutils.ExpandAndCleanPath(d, "", homeDir))
	}
	return cleaned
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
		readDirs:      cleanDirList(cfg.ReadDirs, homeDir),
		writeDirs:     cleanDirList(cfg.WriteDirs, homeDir),
		maxOperations: maxOps,
		policy:        policy,
	}
}
