package repl

import (
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

func formatSessionList(sessions []api.Session, activeID string) string {
	if len(sessions) == 0 {
		return "No sessions found."
	}
	var rows []string
	for _, s := range sessions {
		marker := " "
		if activeID != "" && s.ID == activeID {
			marker = "*"
		}
		rows = append(rows, fmt.Sprintf("%s %s\tstatus=%s\tagent=%s\tmodel=%s\tupdated_at=%s",
			marker, s.ID, s.Status, s.Agent, s.Model, s.UpdatedAt.Format(time.RFC3339)))
	}
	return strings.Join(rows, "\n")
}

func formatHistory(history []api.Message) string {
	if len(history) == 0 {
		return "No messages in session history."
	}
	rows := make([]string, len(history))
	for i, msg := range history {
		rows[i] = ui.FormatMessage(msg)
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
