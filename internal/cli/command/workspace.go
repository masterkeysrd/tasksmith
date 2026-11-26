package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

const workspaceUsage = "/workspace [reload] [--force|-y]"

var workspaceForceFlags = []autocomplete.Candidate{
	{Value: "--force", Label: "--force", Description: "Reload without confirmation"},
	{Value: "-y", Label: "-y", Description: "Alias for --force"},
}

var completeWorkspace = autocomplete.Static(append([]autocomplete.Candidate{
	{Value: "reload", Label: "reload", Description: "Reload workspace resources from disk"},
	{Value: "refresh", Label: "refresh", Description: "Alias for reload"},
}, workspaceForceFlags...)...)

func (r *Registry) registerWorkspaceCommands() {
	r.Register(Command{
		Name:        "workspace",
		Aliases:     []string{"ws"},
		Usage:       workspaceUsage,
		Description: "Reload the active workspace resources from disk",
		Handler:     handleWorkspace,
		Complete:    completeWorkspace,
	})
}

func handleWorkspace(args []string) error {
	if len(args) > 0 && (strings.EqualFold(args[0], "reload") || strings.EqualFold(args[0], "refresh")) {
		args = args[1:]
	}
	force := len(args) == 1 && (args[0] == "--force" || args[0] == "-y")
	if len(args) > 0 && !force {
		return fmt.Errorf("unexpected workspace arguments %q; usage: %s", strings.Join(args, " "), workspaceUsage)
	}
	if force || !workspaceHasHistory() {
		triggerWorkspaceReload()
		return nil
	}
	cancel := func() { store.AppendCommandOutput("/workspace reload", "Workspace reload canceled.") }
	picker := components.NewPicker("Reload workspace?", []uicomponents.Option{
		{Value: "cancel", Label: "Cancel"},
		{Value: "reload", Label: "Reload workspace"},
	}, func(opt uicomponents.Option) {
		if opt.Value == "reload" {
			triggerWorkspaceReload()
		} else {
			cancel()
		}
	})
	picker.Subtitle = "Reloading AGENTS.md, rules, skills, tools, and MCP servers can change the system prompt or tool definitions. " +
		"This can invalidate the provider prompt cache. Previously cached input tokens may be billed at the uncached rate on the next turn."
	picker.OnCancel = cancel
	store.EnqueueSlot(store.SlotItem{Kind: store.SlotKindPicker, Data: picker})
	return nil
}

func workspaceHasHistory() bool {
	st := store.GetStream()
	if (st.Messages != nil && len(*st.Messages) > 0) || (st.Streaming && st.PendingCommand == "") {
		return true
	}
	s := store.GetSession()
	if s.Session == nil {
		return false
	}
	if s.Session.Status != "" && s.Session.Status != api.StatusIdle {
		return true
	}
	b := s.Context.Breakdown
	if b.UserMessages > 0 || b.AgentResponses > 0 || b.Summary > 0 || s.Context.InputTokens > 0 {
		return true
	}
	if items := store.GetTranscript().Items; items != nil {
		for _, item := range *items {
			if item.Kind == store.TranscriptKindMessage {
				return true
			}
		}
	}
	return false
}

// triggerWorkspaceReload instructs the daemon to reload workspace resources from
// disk and refreshes the store's cached agents, skills, commands, and warnings.
// The reload runs asynchronously so the REPL stays responsive, and the outcome
// is reported back to the transcript scrollback.
func triggerWorkspaceReload() {
	if store.Client() == nil {
		store.AppendError("Error", "Client unavailable.")
		return
	}

	store.AppendCommandOutput("/workspace reload", "Reloading workspace resources...")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.ReloadWorkspace(ctx); err != nil {
			store.AppendError("Error", err.Error())
			return
		}
		store.AppendOutput("Workspace reloaded successfully.")
	}()
}
