package command

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func (r *Registry) registerContextCommand() {
	r.Register(Command{
		Name:        "context",
		Usage:       "/context",
		Description: "Show context window token usage by category",
		Handler:     handleContext,
	})
}

func handleContext(_ []string) error {
	ctx := store.GetContext()
	sess := store.GetSession()
	modelName := sess.ModelLabel
	if modelName == "" {
		modelName = "default"
	}
	if sess.Thinking != "" {
		modelName = fmt.Sprintf("%s (%s)", modelName, sess.Thinking)
	}
	cfgModel := strings.TrimSpace(sess.Config.Model)
	if cfgModel != "" && cfgModel != sess.ModelLabel {
		modelName = fmt.Sprintf("%s · %s", modelName, cfgModel)
	}

	store.AppendContextUsage(store.ContextUsageData{
		ModelName: modelName,
		Context:   ctx,
	})
	return nil
}
