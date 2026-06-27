package cmdclassify

// Category represents the safety classification of a command.
type Category string

const (
	ReadOnly    Category = "readonly"    // E.g. git status, cat, grep
	SafeWrite   Category = "safewrite"   // Writes strictly inside workspace (e.g. touch, go build)
	UnsafeWrite Category = "unsafewrite" // Writes/redirects outside workspace (e.g. > /etc/hosts)
	Destructive Category = "destructive" // E.g. rm -rf, git reset --hard, sudo
	Unknown     Category = "unknown"     // Custom runtimes/scripts (e.g. ./run.sh, python app.py)
)

// Policy defines the declarative ruleset for command classification.
type Policy struct {
	DefaultCategory Category
	Rules           map[string]CommandRule
}

// CommandRule defines classification rules for a specific executable.
type CommandRule struct {
	DefaultCategory Category
	ArgRules        []ArgRule
}

// ArgRule defines conditional classification based on command arguments.
type ArgRule struct {
	Category    Category
	Subcommands []string // Matches if the first argument matches any of these
	ContainsAny []string // Matches if args contain any of these flags/words
	ContainsAll []string // Matches if args contain all of these flags/words
}
