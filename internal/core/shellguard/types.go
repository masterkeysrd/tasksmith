package shellguard

// CommandChain represents a sequence of commands grouped by ;, &&, ||, or &.
type CommandChain struct {
	Pipelines []Pipeline
}

// Pipeline represents commands piped together (cmd1 | cmd2) or chained.
type Pipeline struct {
	Commands []ParsedCommand
	Operator string // "&&", "||", ";", "|", "&", or "" (empty string for last command)
}

// ParsedCommand represents a single command invocation.
type ParsedCommand struct {
	Env           []EnvVar       // Environment variable assignments, e.g. PORT=8080
	Executable    string         // Command executable, e.g. "git", "npm", "sudo"
	Args          []string       // Arguments, e.g. ["commit", "-m", "hello"]
	ArgsIsDynamic []bool         // Indicates if the corresponding argument contains variables/subshells
	Redirects     []Redirect     // Parsed redirects, e.g. > out.log
	SubCommand    *ParsedCommand // Recursive sub-command (if executable is a wrapper like sudo, npx, bash -c)
	HasCmdSubst   bool           // True if the command uses $(...) or `...`
}

type EnvVar struct {
	Name  string
	Value string
}

type Redirect struct {
	Op        string // ">", ">>", "<", "<&", ">&"
	Fd        int    // Source file descriptor (1 for stdout, 2 for stderr)
	Target    string // Target file path or stream
	IsDynamic bool   // True if target contains variables or command substitution
}

// Policy defines the declarative ruleset for command classification.
type Policy struct {
	DefaultCategory ActionType
	Rules           map[string]CommandRule
}

// CommandRule defines classification rules for a specific executable.
type CommandRule struct {
	DefaultCategory ActionType
	ArgRules        []ArgRule
}

// ArgRule defines conditional classification based on command arguments.
type ArgRule struct {
	Category    ActionType
	Subcommands []string // Matches if the first argument matches any of these
	ContainsAny []string // Matches if args contain any of these flags/words
	ContainsAll []string // Matches if args contain all of these flags/words
}
