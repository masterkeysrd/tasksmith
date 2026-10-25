package command

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

// Handler executes a slash command with supplied arguments.
type Handler func(args []string) error

// Command defines a REPL slash command and its metadata.
type Command struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Handler     Handler
	Complete    autocomplete.Completer
}

// Registry maintains the collection of REPL slash commands.
type Registry struct {
	commands []Command
	byName   map[string]int
}

// NewRegistry constructs a Registry pre-populated with all built-in REPL commands.
func NewRegistry() *Registry {
	r := &Registry{
		byName: make(map[string]int),
	}
	r.registerBuiltins()
	return r
}

// Register adds or replaces a Command in the registry and indexes its name and aliases.
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
	} else {
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

// Lookup resolves a command by its primary name or alias, ignoring any leading "/".
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

// List returns all registered commands in registration order.
func (r *Registry) List() []Command {
	out := make([]Command, len(r.commands))
	copy(out, r.commands)
	return out
}

// Execute parses a raw slash command line and invokes the matching Handler.
func (r *Registry) Execute(line string) error {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	fields := strings.Fields(trimmed)
	cmd, ok := r.Lookup(fields[0])
	if !ok {
		return fmt.Errorf("unknown command: %s", trimmed)
	}
	if cmd.Handler == nil {
		return fmt.Errorf("command /%s has no handler", cmd.Name)
	}
	return cmd.Handler(fields[1:])
}

func normalizeCommandKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "/")))
}

func (r *Registry) registerBuiltins() {
	r.registerSystemCommands()
	r.registerSessionCommands()
	r.registerChangesCommands()
}
