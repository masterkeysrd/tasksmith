package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
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
	skill       bool
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

// RegisterWorkspace registers workspace commands and skills as interactive slash commands.
func (r *Registry) RegisterWorkspace(commands []api.Command, skills []api.Skill) {
	for _, cmd := range commands {
		r.RegisterCommandResource(cmd)
	}
	for _, sk := range skills {
		r.RegisterSkillResource(sk)
	}
}

// RegisterCommandResource registers a workspace Command manifest as a REPL slash command.
func (r *Registry) RegisterCommandResource(cmd api.Command) {
	usage := "/" + cmd.Name
	if cmd.ArgumentHint != "" {
		hint := strings.TrimSpace(cmd.ArgumentHint)
		if strings.HasPrefix(hint, "/") {
			usage = hint
		} else {
			usage = "/" + cmd.Name + " " + hint
		}
	} else if len(cmd.Arguments) > 0 {
		usage = formatUsageFromArguments("/"+cmd.Name, cmd.Arguments)
	}

	r.Register(Command{
		Name:        cmd.Name,
		Usage:       usage,
		Description: cmd.Description,
		Handler: func(args []string) error {
			if err := validateArguments(cmd.Arguments, args, usage); err != nil {
				return err
			}
			line := "/" + cmd.Name
			if len(args) > 0 {
				line += " " + strings.Join(args, " ")
			}
			store.Send(line)
			return nil
		},
		Complete: argumentCompleter(cmd.Arguments),
	})
}

// RegisterSkillResource registers a workspace Skill manifest as a REPL slash command.
func (r *Registry) RegisterSkillResource(sk api.Skill) {
	if strings.ToLower(sk.Name) == "skills" {
		return
	}
	usage := "/" + sk.Name
	if sk.ArgumentHint != "" {
		hint := strings.TrimSpace(sk.ArgumentHint)
		if strings.HasPrefix(hint, "/") {
			usage = hint
		} else {
			usage = "/" + sk.Name + " " + hint
		}
	} else if len(sk.Arguments) > 0 {
		usage = formatUsageFromArguments("/"+sk.Name, sk.Arguments)
	}

	r.Register(Command{
		Name:        sk.Name,
		Usage:       usage,
		Description: sk.Description,
		skill:       true,
		Handler: func(args []string) error {
			if err := validateArguments(sk.Arguments, args, usage); err != nil {
				return err
			}
			line := "/" + sk.Name
			if len(args) > 0 {
				line += " " + strings.Join(args, " ")
			}
			store.Send(line)
			return nil
		},
		Complete: argumentCompleter(sk.Arguments),
	})
}

func validateArguments(argDefs []api.Argument, args []string, usage string) error {
	if len(argDefs) == 0 {
		return nil
	}

	named := make(map[string]string)
	var positional []string
	for _, arg := range args {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			k := strings.TrimPrefix(parts[0], "--")
			named[strings.ToLower(k)] = parts[1]
		} else {
			positional = append(positional, arg)
		}
	}

	for i, def := range argDefs {
		if !def.Required || def.Default != "" {
			continue
		}
		// Check named
		if val, ok := named[strings.ToLower(def.Name)]; ok && strings.TrimSpace(val) != "" {
			continue
		}
		// Check positional
		if i < len(positional) && strings.TrimSpace(positional[i]) != "" {
			continue
		}
		return fmt.Errorf("missing required argument %q. Usage: %s", def.Name, usage)
	}
	return nil
}

func formatUsageFromArguments(prefix string, args []api.Argument) string {
	parts := []string{prefix}
	for _, a := range args {
		if a.Required && a.Default == "" {
			parts = append(parts, "<"+a.Name+">")
		} else if a.Default != "" {
			parts = append(parts, "["+a.Name+"="+a.Default+"]")
		} else {
			parts = append(parts, "["+a.Name+"]")
		}
	}
	return strings.Join(parts, " ")
}

func argumentCompleter(argDefs []api.Argument) autocomplete.Completer {
	if len(argDefs) == 0 {
		return nil
	}
	return func(ctx context.Context, req autocomplete.Request) ([]autocomplete.Candidate, error) {
		candidates := make([]autocomplete.Candidate, 0, len(argDefs))
		for _, a := range argDefs {
			label := a.Name
			if a.Required {
				label += " (required)"
			} else if a.Default != "" {
				label += " (default: " + a.Default + ")"
			}
			desc := a.Description
			candidates = append(candidates, autocomplete.Candidate{
				Value:       a.Name,
				Label:       label,
				Description: desc,
			})
		}
		return candidates, nil
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
	r.registerPermissionCommands()
	r.registerTaskCommands()
	r.registerCompactCommand()
	r.registerContextCommand()
	r.registerUsageCommand()
	r.registerSkillCommands()
}
