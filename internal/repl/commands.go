package repl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

// CommandEnv provides the active REPL session context to a [CommandHandler].
type CommandEnv struct {
	Client       *client.Client
	Workspace    string
	Session      *api.Session
	Mode         api.PermissionMode
	DefaultAgent string
	DefaultModel string
	DefaultTitle string
}

// CommandResult describes the output and state transitions produced by a slash command.
type CommandResult struct {
	Output        string
	SwitchTo      *api.Session
	ResetSession  bool
	ModelOverride string
	ModeChange    api.PermissionMode
	Clear         bool
	Quit          bool
}

// CommandHandler executes a slash command against the active [CommandEnv].
type CommandHandler func(ctx context.Context, env *CommandEnv, args []string) (CommandResult, error)

// Command defines a declarative REPL slash command and its metadata.
type Command struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Handler     CommandHandler
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

// Execute parses a raw slash command line and invokes the matching [CommandHandler].
func (r *Registry) Execute(ctx context.Context, env *CommandEnv, line string) (CommandResult, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return CommandResult{}, nil
	}

	fields := strings.Fields(trimmed)
	cmd, ok := r.Lookup(fields[0])
	if !ok {
		return CommandResult{}, fmt.Errorf("unknown command: %s", trimmed)
	}
	if cmd.Handler == nil {
		return CommandResult{}, fmt.Errorf("command /%s has no handler", cmd.Name)
	}
	return cmd.Handler(ctx, env, fields[1:])
}

func normalizeCommandKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "/")))
}

func (r *Registry) registerBuiltins() {
	r.Register(Command{
		Name:        "help",
		Aliases:     []string{"h", "?"},
		Usage:       "/help",
		Description: "Show available slash commands",
		Handler:     r.handleHelp,
	})
	r.Register(Command{
		Name:        "new",
		Usage:       "/new [model]",
		Description: "Create and switch to a new session",
		Handler:     handleNew,
	})
	r.Register(Command{
		Name:        "sessions",
		Aliases:     []string{"ls"},
		Usage:       "/sessions",
		Description: "List sessions in the current workspace",
		Handler:     handleSessions,
	})
	r.Register(Command{
		Name:        "switch",
		Usage:       "/switch <session_id>",
		Description: "Switch to an existing session by ID",
		Handler:     handleSwitch,
	})
	r.Register(Command{
		Name:        "history",
		Usage:       "/history",
		Description: "Show conversation history for the active session",
		Handler:     handleHistory,
	})
	r.Register(Command{
		Name:        "mode",
		Usage:       "/mode [default|accept_edits|plan|ask|auto]",
		Description: "Read or update the session permission mode",
		Handler:     handleMode,
	})
	r.Register(Command{
		Name:        "changes",
		Usage:       "/changes",
		Description: "List tracked file changes in the session",
		Handler:     handleChanges,
	})
	r.Register(Command{
		Name:        "diff",
		Usage:       "/diff <path> <entry_id>",
		Description: "Show unified diff for a tracked file change",
		Handler:     handleDiff,
	})
	r.Register(Command{
		Name:        "revert",
		Usage:       "/revert [path]",
		Description: "Revert tracked changes for a file or the entire session",
		Handler:     handleRevert,
	})
	r.Register(Command{
		Name:        "clear",
		Usage:       "/clear",
		Description: "Clear the terminal screen",
		Handler:     handleClear,
	})
	r.Register(Command{
		Name:        "exit",
		Aliases:     []string{"quit", "q"},
		Usage:       "/exit",
		Description: "Exit the REPL",
		Handler:     handleExit,
	})
}

func (r *Registry) handleHelp(_ context.Context, _ *CommandEnv, _ []string) (CommandResult, error) {
	cmds := r.List()
	var rows []string
	rows = append(rows, "Available commands:")
	for _, cmd := range cmds {
		usage := cmd.Usage
		if len(cmd.Aliases) > 0 {
			aliases := make([]string, len(cmd.Aliases))
			for i, a := range cmd.Aliases {
				aliases[i] = "/" + a
			}
			usage = fmt.Sprintf("%s (%s)", usage, strings.Join(aliases, ", "))
		}
		rows = append(rows, fmt.Sprintf("  %-44s %s", usage, cmd.Description))
	}
	return CommandResult{Output: strings.Join(rows, "\n")}, nil
}

func handleNew(_ context.Context, env *CommandEnv, args []string) (CommandResult, error) {
	modelOverride := strings.TrimSpace(strings.Join(args, " "))
	var agent string
	if env != nil {
		agent = strings.TrimSpace(env.DefaultAgent)
		if modelOverride == "" {
			modelOverride = strings.TrimSpace(env.DefaultModel)
		}
	}

	return CommandResult{
		Output:        fmt.Sprintf("Switched to new session (agent=%s model=%s)", agent, modelOverride),
		ResetSession:  true,
		ModelOverride: modelOverride,
	}, nil
}

func handleSessions(ctx context.Context, env *CommandEnv, _ []string) (CommandResult, error) {
	if err := requireEnvSession(env, false); err != nil {
		return CommandResult{}, err
	}

	sessions, err := env.Client.ListSessions(ctx, env.Workspace)
	if err != nil {
		return CommandResult{}, fmt.Errorf("error listing sessions: %w", err)
	}
	if len(sessions) == 0 {
		return CommandResult{Output: "No sessions found."}, nil
	}

	var rows []string
	for _, s := range sessions {
		marker := " "
		if env.Session != nil && s.ID == env.Session.ID {
			marker = "*"
		}
		rows = append(rows, fmt.Sprintf("%s %s\tstatus=%s\tagent=%s\tmodel=%s\tupdated_at=%s",
			marker, s.ID, s.Status, s.Agent, s.Model, s.UpdatedAt.Format(time.RFC3339)))
	}
	return CommandResult{Output: strings.Join(rows, "\n")}, nil
}

func handleSwitch(ctx context.Context, env *CommandEnv, args []string) (CommandResult, error) {
	if err := requireEnvSession(env, false); err != nil {
		return CommandResult{}, err
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return CommandResult{}, errors.New("usage: /switch <session_id>")
	}

	targetID := strings.TrimSpace(args[0])
	s, err := env.Client.GetSession(ctx, env.Workspace, targetID)
	if err != nil {
		return CommandResult{}, fmt.Errorf("error switching session: %w", err)
	}

	return CommandResult{
		Output:   fmt.Sprintf("Switched to session %s (agent=%s model=%s)", s.ID, s.Agent, s.Model),
		SwitchTo: s,
	}, nil
}

func handleHistory(ctx context.Context, env *CommandEnv, _ []string) (CommandResult, error) {
	if env == nil || env.Session == nil || strings.TrimSpace(env.Session.ID) == "" {
		return CommandResult{Output: "No messages in session history."}, nil
	}
	if err := requireEnvSession(env, true); err != nil {
		return CommandResult{}, err
	}

	history, err := env.Client.History(ctx, env.Workspace, env.Session.ID)
	if err != nil {
		return CommandResult{}, fmt.Errorf("error loading history: %w", err)
	}
	if len(history) == 0 {
		return CommandResult{Output: "No messages in session history."}, nil
	}

	rows := make([]string, len(history))
	for i, msg := range history {
		rows[i] = FormatMessage(msg)
	}
	return CommandResult{Output: strings.Join(rows, "\n")}, nil
}

func handleMode(ctx context.Context, env *CommandEnv, args []string) (CommandResult, error) {
	if err := requireEnvSession(env, false); err != nil {
		return CommandResult{}, err
	}

	hasSession := env.Session != nil && strings.TrimSpace(env.Session.ID) != ""
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		if !hasSession {
			mode := env.Mode
			if mode == "" {
				mode = api.ModeDefault
			}
			return CommandResult{
				Output:     fmt.Sprintf("Permission mode: %s", mode),
				ModeChange: mode,
			}, nil
		}
		mode, err := env.Client.PermissionMode(ctx, env.Workspace, env.Session.ID)
		if err != nil {
			return CommandResult{}, fmt.Errorf("error reading permission mode: %w", err)
		}
		return CommandResult{
			Output:     fmt.Sprintf("Permission mode: %s", mode),
			ModeChange: mode,
		}, nil
	}

	nextMode := api.PermissionMode(strings.TrimSpace(args[0]))
	if !hasSession {
		if !isValidPermissionMode(nextMode) {
			return CommandResult{}, fmt.Errorf("error setting permission mode: invalid permission mode %q (valid: default, accept_edits, plan, ask, auto)", nextMode)
		}
		return CommandResult{
			Output:     fmt.Sprintf("Permission mode set to: %s", nextMode),
			ModeChange: nextMode,
		}, nil
	}

	if err := env.Client.SetPermissionMode(ctx, env.Workspace, env.Session.ID, nextMode); err != nil {
		return CommandResult{}, fmt.Errorf("error setting permission mode: %w (valid: default, accept_edits, plan, ask, auto)", err)
	}
	return CommandResult{
		Output:     fmt.Sprintf("Permission mode set to: %s", nextMode),
		ModeChange: nextMode,
	}, nil
}

func isValidPermissionMode(mode api.PermissionMode) bool {
	switch mode {
	case api.ModeDefault, api.ModeAcceptEdits, api.ModePlan, api.ModeAsk, api.ModeAuto:
		return true
	default:
		return false
	}
}

func handleChanges(ctx context.Context, env *CommandEnv, _ []string) (CommandResult, error) {
	if env == nil || env.Session == nil || strings.TrimSpace(env.Session.ID) == "" {
		return CommandResult{Output: "No tracked file changes."}, nil
	}
	if err := requireEnvSession(env, true); err != nil {
		return CommandResult{}, err
	}

	changes, err := env.Client.Changes(ctx, env.Workspace, env.Session.ID)
	if err != nil {
		return CommandResult{}, fmt.Errorf("error listing file changes: %w", err)
	}
	if len(changes) == 0 {
		return CommandResult{Output: "No tracked file changes."}, nil
	}

	rows := make([]string, len(changes))
	for i, c := range changes {
		rows[i] = fmt.Sprintf("%s\t%s\t%s\t+%d/-%d", c.ID, c.Kind, c.Path, c.Additions, c.Deletions)
	}
	return CommandResult{Output: strings.Join(rows, "\n")}, nil
}

func handleDiff(ctx context.Context, env *CommandEnv, args []string) (CommandResult, error) {
	if err := requireEnvSession(env, true); err != nil {
		return CommandResult{}, err
	}
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return CommandResult{}, errors.New("usage: /diff <path> <entry_id>")
	}

	diffText, err := env.Client.DiffChange(ctx, env.Workspace, env.Session.ID, strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
	if err != nil {
		return CommandResult{}, fmt.Errorf("error fetching diff: %w", err)
	}
	if strings.TrimSpace(diffText) == "" {
		return CommandResult{Output: "No diff changes."}, nil
	}
	return CommandResult{Output: FormatDiff(strings.TrimRight(diffText, "\n"))}, nil
}

func handleRevert(ctx context.Context, env *CommandEnv, args []string) (CommandResult, error) {
	if err := requireEnvSession(env, true); err != nil {
		return CommandResult{}, err
	}

	var path string
	if len(args) > 0 {
		path = strings.TrimSpace(args[0])
	}
	if err := env.Client.Revert(ctx, env.Workspace, env.Session.ID, api.RevertRequest{
		Path:  path,
		Force: true,
	}); err != nil {
		return CommandResult{}, fmt.Errorf("error reverting changes: %w", err)
	}

	if path == "" {
		return CommandResult{Output: "Reverted all session file changes."}, nil
	}
	return CommandResult{Output: fmt.Sprintf("Reverted %s.", path)}, nil
}

func handleClear(_ context.Context, _ *CommandEnv, _ []string) (CommandResult, error) {
	return CommandResult{Clear: true}, nil
}

func handleExit(_ context.Context, _ *CommandEnv, _ []string) (CommandResult, error) {
	return CommandResult{Quit: true}, nil
}

func requireEnvSession(env *CommandEnv, needSession bool) error {
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
