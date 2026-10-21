package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

func (r *Registry) registerSessionCommands() {
	r.Register(Command{
		Name:        "new",
		Usage:       "/new [model]",
		Description: "Create and switch to a new session",
		Handler:     HandleNew,
	})
	r.Register(Command{
		Name:        "sessions",
		Aliases:     []string{"ls"},
		Usage:       "/sessions",
		Description: "List sessions in the current workspace",
		Handler:     HandleSessions,
	})
	r.Register(Command{
		Name:        "switch",
		Usage:       "/switch <session_id>",
		Description: "Switch to an existing session by ID",
		Handler:     HandleSwitch,
	})
	r.Register(Command{
		Name:        "history",
		Usage:       "/history",
		Description: "Show conversation history for the active session",
		Handler:     HandleHistory,
	})
}

// HandleNew switches the REPL to a fresh session, optionally with a model override.
func HandleNew(_ context.Context, env *Env, args []string) (Result, error) {
	modelOverride := strings.TrimSpace(strings.Join(args, " "))
	var agent string
	if env != nil {
		agent = strings.TrimSpace(env.DefaultAgent)
		if modelOverride == "" {
			modelOverride = strings.TrimSpace(env.DefaultModel)
		}
	}

	return Result{
		Output:        fmt.Sprintf("Switched to new session (agent=%s model=%s)", agent, modelOverride),
		ResetSession:  true,
		ModelOverride: modelOverride,
	}, nil
}

// HandleSessions lists all sessions associated with the active workspace.
func HandleSessions(ctx context.Context, env *Env, _ []string) (Result, error) {
	if err := requireEnvSession(env, false); err != nil {
		return Result{}, err
	}

	sessions, err := env.Client.ListSessions(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("error listing sessions: %w", err)
	}
	if len(sessions) == 0 {
		return Result{Output: "No sessions found."}, nil
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
	return Result{Output: strings.Join(rows, "\n")}, nil
}

// HandleSwitch changes the active session to targetID.
func HandleSwitch(ctx context.Context, env *Env, args []string) (Result, error) {
	if err := requireEnvSession(env, false); err != nil {
		return Result{}, err
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return Result{}, errors.New("usage: /switch <session_id>")
	}

	targetID := strings.TrimSpace(args[0])
	s, err := env.Client.GetSession(ctx, targetID)
	if err != nil {
		return Result{}, fmt.Errorf("error switching session: %w", err)
	}

	return Result{
		Output:   fmt.Sprintf("Switched to session %s (agent=%s model=%s)", s.ID, s.Agent, s.Model),
		SwitchTo: s,
	}, nil
}

// HandleHistory renders the message history of the current session.
func HandleHistory(ctx context.Context, env *Env, _ []string) (Result, error) {
	if env == nil || env.Session == nil || strings.TrimSpace(env.Session.ID) == "" {
		return Result{Output: "No messages in session history."}, nil
	}
	if err := requireEnvSession(env, true); err != nil {
		return Result{}, err
	}

	history, err := env.Client.History(ctx, env.Session.ID)
	if err != nil {
		return Result{}, fmt.Errorf("error loading history: %w", err)
	}
	if len(history) == 0 {
		return Result{Output: "No messages in session history."}, nil
	}

	rows := make([]string, len(history))
	for i, msg := range history {
		rows[i] = ui.FormatMessage(msg)
	}
	return Result{Output: strings.Join(rows, "\n")}, nil
}
