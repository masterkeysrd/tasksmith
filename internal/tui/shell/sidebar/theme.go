package sidebar

import (
	"image/color"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type colors struct {
	background color.Color
	panel      color.Color
	surface    color.Color
	surfaceAlt color.Color
	footer     color.Color
	border     color.Color
	text       color.Color
	muted      color.Color
	subtle     color.Color
	accent     color.Color
	info       color.Color
	success    color.Color
	warning    color.Color
	error      color.Color
	inverse    color.Color
	magenta    color.Color
}

func useColors() colors {
	t := theme.UseTheme()
	if t == nil {
		return colors{}
	}

	palette := func(name string, fallback color.Color) color.Color {
		if t.Palette != nil {
			if c, ok := t.Palette[name]; ok {
				return c
			}
		}
		return fallback
	}

	background := palette("bg_dark", t.Color.Surface.BaseHover)
	panel := palette("bg_dark", background)
	surface := palette("bg_highlight", t.Color.Surface.BaseFocus)
	footer := palette("terminal_black", t.Color.Surface.BasePressed)
	text := palette("fg", t.Color.Text.Primary)
	muted := palette("fg_dark", t.Color.Text.Secondary)
	subtle := palette("comment", t.Color.Text.Tertiary)
	accent := palette("blue", t.Color.Surface.Info)
	info := palette("cyan", t.Color.Surface.Primary)
	success := palette("green", t.Color.Surface.Success)
	warning := palette("yellow", t.Color.Surface.Tertiary)
	error := palette("red", t.Color.Surface.Error)
	inverse := palette("bg_dark", t.Color.Text.InversePrimary)
	magenta := palette("magenta", t.Color.Surface.Secondary)

	return colors{
		background: background,
		panel:      panel,
		surface:    surface,
		surfaceAlt: surface,
		footer:     footer,
		border:     t.Color.Border.Primary,
		text:       text,
		muted:      muted,
		subtle:     subtle,
		accent:     accent,
		info:       info,
		success:    success,
		warning:    warning,
		error:      error,
		inverse:    inverse,
		magenta:    magenta,
	}
}

func countEnabledTools(authorized map[string]bool) int {
	count := 0
	for _, enabled := range authorized {
		if enabled {
			count++
		}
	}
	return count
}

type pathNode struct {
	Name     string
	FullPath string
	Children map[string]*pathNode
	IsDir    bool
}

func buildPathTree(paths []string) *pathNode {
	root := &pathNode{
		Children: map[string]*pathNode{},
		IsDir:    true,
	}

	for _, path := range paths {
		cleaned := filepath.Clean(path)
		if cleaned == "." || cleaned == "" {
			continue
		}

		parts := strings.FieldsFunc(cleaned, func(r rune) bool {
			return r == '/' || r == '\\'
		})
		if len(parts) == 0 {
			continue
		}

		current := root
		var fullParts []string
		for _, part := range parts {
			fullParts = append(fullParts, part)
			fullPath := string(filepath.Separator) + filepath.Join(fullParts...)
			child, ok := current.Children[part]
			if !ok {
				child = &pathNode{
					Name:     part,
					FullPath: fullPath,
					Children: map[string]*pathNode{},
					IsDir:    true,
				}
				current.Children[part] = child
			}
			current = child
		}
	}

	return root
}

func sortedChildNodes(node *pathNode) []*pathNode {
	children := make([]*pathNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		return children[i].Name < children[j].Name
	})
	return children
}

func sessionTitle(session api.Session) string {
	if strings.TrimSpace(session.Title) != "" {
		return session.Title
	}
	return session.ID
}
