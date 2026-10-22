package command

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

// Event represents an emitted action or directive from a slash command.
type Event any

// Env provides a lightweight read-only snapshot of REPL state to commands.
type Env struct {
	Mode         api.PermissionMode
	Session      *api.Session
	DefaultAgent string
	DefaultModel string
	ListSessions func() ([]api.Session, error)
}

// PrintMsg indicates plain text output from a slash command.
type PrintMsg struct {
	Text string
}

// OutputMsg is an alias to PrintMsg for backwards compatibility.
type OutputMsg = PrintMsg

// OpenPickerMsg directs the UI to display an interactive command picker.
type OpenPickerMsg struct {
	Picker ui.PickerProps
}

// ChangeModeMsg requests changing the session permission mode.
type ChangeModeMsg struct {
	Mode   api.PermissionMode
	Silent bool
}

// NewSessionMsg requests creating a new session.
type NewSessionMsg struct {
	Model string
}

// SwitchSessionMsg requests switching to an existing session.
type SwitchSessionMsg struct {
	SessionID string
}

// RenameSessionMsg requests renaming an existing session.
type RenameSessionMsg struct {
	SessionID string
	Title     string
}

// DeleteSessionMsg requests deleting an existing session.
type DeleteSessionMsg struct {
	SessionID string
}

// ListChangesMsg requests listing tracked file changes.
type ListChangesMsg struct{}

// DiffChangeMsg requests displaying a unified diff for a tracked file change.
type DiffChangeMsg struct {
	Path    string
	EntryID string
}

// ShowDiffMsg is an alias to DiffChangeMsg for backwards compatibility.
type ShowDiffMsg = DiffChangeMsg

// RevertChangesMsg requests reverting file changes.
type RevertChangesMsg struct {
	Path    string
	EntryID string
}

// RevertMsg is an alias to RevertChangesMsg for backwards compatibility.
type RevertMsg = RevertChangesMsg

// ClearScreenMsg requests clearing the terminal screen.
type ClearScreenMsg struct{}

// ExitMsg requests terminating the REPL.
type ExitMsg struct{}

// QuitMsg is an alias to ExitMsg for backwards compatibility.
type QuitMsg = ExitMsg

// Handler executes a slash command against the active [Env], emitting an intent event.
type Handler func(env Env, args []string) (Event, error)

// Command defines a declarative REPL slash command and its metadata.
type Command struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Handler     Handler
	Options     func(env Env) []ui.Option
	OnSelect    func(opt ui.Option) Event
}

// Registry maintains the ordered collection of REPL slash commands and aliases.
type Registry struct {
	commands []Command
	byName   map[string]int
}

// NewRegistry constructs a [Registry] pre-populated with all built-in REPL commands.
func NewRegistry() *Registry {
	r := &Registry{
		byName: make(map[string]int),
	}
	r.registerBuiltins()
	return r
}

// Register adds or replaces a [Command] in the registry and indexes its name and aliases.
func (r *Registry) Register(cmd Command) {
	cmd.Name = normalizeCommandKey(cmd.Name)
	if cmd.Name == "" {
		return
	}
	if cmd.Usage == "" {
		cmd.Usage = "/" + cmd.Name
	}

	idx, exists := r.byName[cmd.Name]
	if !exists {
		idx = len(r.commands)
		r.commands = append(r.commands, cmd)
	}
	if exists {
		r.commands[idx] = cmd
	}

	r.byName[cmd.Name] = idx
	for _, alias := range cmd.Aliases {
		key := normalizeCommandKey(alias)
		if key != "" {
			r.byName[key] = idx
		}
	}
}

// Lookup resolves a command by its primary name or any alias, ignoring a leading "/".
func (r *Registry) Lookup(name string) (Command, bool) {
	key := normalizeCommandKey(name)
	if key == "" {
		return Command{}, false
	}
	idx, ok := r.byName[key]
	if !ok || idx < 0 || idx >= len(r.commands) {
		return Command{}, false
	}
	return r.commands[idx], true
}

// List returns a copy of all registered commands in registration order.
func (r *Registry) List() []Command {
	out := make([]Command, len(r.commands))
	copy(out, r.commands)
	return out
}

// Suggest returns all registered commands whose name or alias matches prefix.
func (r *Registry) Suggest(prefix string) []Command {
	raw := strings.TrimSpace(prefix)
	if strings.Contains(raw, " ") {
		fields := strings.Fields(raw)
		if len(fields) > 0 {
			raw = fields[0]
		}
	}
	clean := normalizeCommandKey(raw)

	var matches []Command
	for _, cmd := range r.commands {
		if matchesCommandPrefix(cmd, clean) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

// SuggestArgs returns argument completion options for a partially typed slash command line.
func (r *Registry) SuggestArgs(env Env, rawLine string) (Command, []ui.Option, bool) {
	trimmedLeft := strings.TrimLeft(rawLine, " \t")
	if !strings.HasPrefix(trimmedLeft, "/") {
		return Command{}, nil, false
	}

	cmdPart, argPart, hasSpace := strings.Cut(trimmedLeft, " ")
	if !hasSpace {
		return Command{}, nil, false
	}

	cmd, ok := r.Lookup(cmdPart)
	if !ok || cmd.Options == nil {
		return Command{}, nil, false
	}

	allOpts := cmd.Options(env)
	query := strings.ToLower(strings.TrimSpace(argPart))
	if query == "" {
		return cmd, allOpts, true
	}

	var filtered []ui.Option
	for _, opt := range allOpts {
		val := strings.ToLower(opt.Value)
		lbl := strings.ToLower(opt.Label)
		if strings.HasPrefix(val, query) || strings.HasPrefix(lbl, query) || strings.Contains(val, query) {
			filtered = append(filtered, opt)
		}
	}
	return cmd, filtered, true
}

func matchesCommandPrefix(cmd Command, prefix string) bool {
	if strings.HasPrefix(cmd.Name, prefix) {
		return true
	}
	for _, alias := range cmd.Aliases {
		if strings.HasPrefix(normalizeCommandKey(alias), prefix) {
			return true
		}
	}
	return false
}

// Execute parses a raw slash command line and invokes the matching [Handler].
func (r *Registry) Execute(env Env, line string) (Event, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, nil
	}

	fields := strings.Fields(trimmed)
	cmd, ok := r.Lookup(fields[0])
	if !ok {
		return nil, fmt.Errorf("unknown command: %s", trimmed)
	}
	if cmd.Handler == nil {
		return nil, fmt.Errorf("command /%s has no handler", cmd.Name)
	}
	return cmd.Handler(env, fields[1:])
}

func normalizeCommandKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "/")))
}

func (r *Registry) registerBuiltins() {
	r.registerSystemCommands()
	r.registerSessionCommands()
	r.registerChangesCommands()
}
