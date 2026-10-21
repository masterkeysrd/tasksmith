package ui

import (
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// FormatDiff styles a unified diff string with green additions, red deletions,
// and cyan hunk headers.
func FormatDiff(diffText string) string {
	if diffText == "" {
		return ""
	}
	lines := strings.Split(diffText, "\n")
	styled := make([]string, len(lines))
	for i, line := range lines {
		styled[i] = styleDiffLine(line)
	}
	return strings.Join(styled, "\n")
}

func styleDiffLine(line string) string {
	if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
		return theme.DiffHeaderStyle.Render(line)
	}
	if strings.HasPrefix(line, "@@") {
		return theme.DiffHunkStyle.Render(line)
	}
	if strings.HasPrefix(line, "+") {
		return theme.DiffAddStyle.Render(line)
	}
	if strings.HasPrefix(line, "-") {
		return theme.DiffDelStyle.Render(line)
	}
	return line
}
