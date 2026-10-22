package repl

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

func formatHistory(history []api.Message) string {
	var rows []string
	for _, msg := range history {
		formatted := ui.FormatMessage(msg)
		if strings.TrimSpace(formatted) != "" {
			rows = append(rows, formatted)
		}
	}
	return strings.Join(rows, "\n")
}

func formatChanges(changes []api.FileChange) string {
	if len(changes) == 0 {
		return "No tracked file changes."
	}
	rows := make([]string, len(changes))
	for i, c := range changes {
		rows[i] = fmt.Sprintf("%s\t%s\t%s\t+%d/-%d", c.ID, c.Kind, c.Path, c.Additions, c.Deletions)
	}
	return strings.Join(rows, "\n")
}
