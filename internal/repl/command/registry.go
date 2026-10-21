package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

// Env provides the active REPL session context to a [Handler].
type Env struct {
	Client       *client.Client
	Workspace    string
	Session      *api.Session
	Mode         api.PermissionMode
	DefaultAgent string
	DefaultModel string
	DefaultTitle string
}

// Result describes the output and state transitions produced by a slash command.
type Result struct {
	Output        string
	SwitchTo      *api.Session
	ResetSession  bool
	ModelOverride string
	ModeChange    api.PermissionMode
	Clear         bool
	Quit          bool
	Picker        *ui.Picker
}

// Handler executes a slash command against the active [Env].
type Handler func(ctx context.Context, env *Env, args []string) (Result, error)

// Command defines a declarative REPL slash command and its metadata.
type Command struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Handler     Handler
	Options     func(ctx context.Context, env *Env) []ui.Option
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
// It returns true only if line begins with "/" and contains a space after the command identifier.
func (r *Registry) SuggestArgs(ctx context.Context, env *Env, rawLine string) (Command, []ui.Option, bool) {
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

	allOpts := cmd.Options(ctx, env)
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
func (r *Registry) Execute(ctx context.Context, env *Env, line string) (Result, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return Result{}, nil
	}

	fields := strings.Fields(trimmed)
	cmd, ok := r.Lookup(fields[0])
	if !ok {
		return Result{}, fmt.Errorf("unknown command: %s", trimmed)
	}
	if cmd.Handler == nil {
		return Result{}, fmt.Errorf("command /%s has no handler", cmd.Name)
	}
	return cmd.Handler(ctx, env, fields[1:])
}

func normalizeCommandKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "/")))
}

func requireEnvSession(env *Env, needSession bool) error {
	if env == nil || env.Client == nil {
		return errors.New("repl: client is required")
	}
	if strings.TrimSpace(env.Workspace) == "" {
		return errors.New("repl: workspace is required")
	}
	if needSession && (env.Session == nil || strings.TrimSpace(env.Session.ID) == "") {
		return errors.New("repl: active session is required")
	}
	return nil
}

func (r *Registry) registerBuiltins() {
	r.registerSystemCommands()
	r.registerSessionCommands()
	r.registerChangesCommands()
}
