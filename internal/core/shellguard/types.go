package shellguard

// CommandChain represents a sequence of commands grouped by ;, &&, ||, or &.
type CommandChain struct {
	Pipelines  []Pipeline
	IsCompound bool // True if the AST contains control flow or multi-statement inline scripts
}

// Pipeline represents commands piped together (cmd1 | cmd2) or chained.
type Pipeline struct {
	Commands []ParsedCommand
	Operator string // "&&", "||", ";", "|", "&", or "" (empty string for last command)
}

// ParsedCommand represents a single command invocation or subshell block.
type ParsedCommand struct {
	Env           []EnvVar        // Environment variable assignments, e.g. PORT=8080
	Executable    string          // Command executable, e.g. "git", "npm", "sudo"
	Args          []string        // Arguments, e.g. ["commit", "-m", "hello"]
	ArgsIsDynamic []bool          // Indicates if the corresponding argument contains variables/subshells
	Redirects     []Redirect      // Parsed redirects, e.g. > out.log
	SubCommand    *ParsedCommand  // Recursive sub-command (if executable is a wrapper like sudo, npx, bash -c)
	SubChain      *CommandChain   // Full parsed sub-chain for multi-command wrapper invocations (e.g., bash -c "a && b")
	Subshell      *CommandChain   // Isolated subshell command chain for (...) nodes
	SubstCommands []ParsedCommand // Commands extracted from command substitutions $(...) or `...`
	HasCmdSubst   bool            // True if the command uses $(...) or `...`
	Raw           string          // Raw command string (populated for collapsed compound summary commands)
}

// EnvVar represents a shell variable assignment prefixing a command.
type EnvVar struct {
	Name  string
	Value string
}

// Redirect represents an I/O redirection attached to a command.
type Redirect struct {
	Op        string // ">", ">>", "<", "<&", ">&"
	Fd        int    // Source file descriptor (1 for stdout, 2 for stderr)
	Target    string // Target file path or stream
	IsDynamic bool   // True if target contains variables or command substitution
}

// TargetMode specifies how positional arguments of a command are extracted as
// filesystem paths during safety analysis.
type TargetMode uint8

const (
	TargetsAll           TargetMode = iota // All positional args are paths (default)
	TargetsNone                            // Positional args are never paths (echo, printf, sleep, etc.)
	TargetsSkipFirst                       // First positional arg is pattern/script/mode; rest are paths (grep, rg, sed, chmod, chown, jq, yq)
	TargetsLast                            // Only the last positional arg is the destination path (cp)
	TargetsAfterDashDash                   // Only positional args after "--" are paths (git ref commands)
	TargetsPredicateStop                   // Positional args before the first "-" predicate/operator are paths (find)
)

// Policy defines the declarative ruleset for command classification and argument
// target extraction.
type Policy struct {
	DefaultCategory ActionType
	Rules           map[string]CommandRule
}

// CommandRule defines classification and target extraction rules for a specific executable.
type CommandRule struct {
	DefaultCategory ActionType
	TargetMode      TargetMode
	ValueFlags      []string              // Flags that consume the next arg as a non-path value (e.g. "-m", "-e", "-n")
	PathFlags       []string              // Flags that consume the next arg as a path target (e.g. "-C", "-f", "--file")
	SubcommandModes map[string]TargetMode // Optional per-subcommand TargetMode override (e.g. for git subcommands)
	ArgRules        []ArgRule
}

// ArgRule defines conditional classification based on command arguments.
type ArgRule struct {
	Category    ActionType
	Subcommands []string // Matches if the subcommand matches any of these
	ContainsAny []string // Matches if args contain any of these flags/words
	ContainsAll []string // Matches if args contain all of these flags/words
}

// TargetArg represents an extracted target file or directory argument.
type TargetArg struct {
	Path      string
	IsDynamic bool
}
