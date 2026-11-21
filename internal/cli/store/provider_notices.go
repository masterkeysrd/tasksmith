package store

import (
	"fmt"

	"github.com/masterkeysrd/tasksmith/api"
)

func AppendSkippedPresetAliasNotices(skipped []api.SkippedPresetAlias) {
	for _, alias := range skipped {
		message := fmt.Sprintf("Skipped preset alias %s for %s", alias.Alias, alias.ModelID)
		switch {
		case alias.WorkspaceOverride && alias.Owner != "":
			message += "; WORKSPACE.md already points it to " + alias.Owner + ". Edit spec.modelAliases in WORKSPACE.md to change it."
		case alias.WorkspaceOverride:
			message += "; it is reserved in WORKSPACE.md. Edit spec.modelAliases there to change it."
		default:
			message += "; it already points to " + alias.Owner + ". To move it, use the F2 alias editor in /model or /providers."
		}
		AppendWarning(message)
	}
}
