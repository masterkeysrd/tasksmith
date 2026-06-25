package sidebar

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
)

func explorerPanel(data Data, expandedPaths map[string]bool, onTogglePath func(string), onSelectFile func(string)) kitex.Node {
	c := useColors()

	projectRows := kitex.Map(data.Projects, func(project api.Project, idx int) kitex.Node {
		name := project.DisplayName
		if name == "" {
			name = project.Name
		}
		isActive := idx == 0
		projectColor := c.muted
		projectMark := "󰄱"
		if isActive {
			projectColor = c.success
			projectMark = "󰄲"
		}
		return kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				PaddingVertical(1).
				Foreground(projectColor),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Width(style.Cells(2)).
					Foreground(projectColor).
					Bold(true).
					TextAlign(style.TextAlignCenter),
			}, kitex.Text(projectMark)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Flex(1).
					Foreground(projectColor).
					Bold(true),
			}, kitex.Text(name)),
		)
	})

	fileTree := buildFileTree(data.ChangedFiles)

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Background(c.panel).
			MinHeight(style.Percent(100)),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Foreground(c.subtle).
					Bold(true),
			}, kitex.Text(data.WorkspaceName)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				explorerActionButton(icon.Search),
				explorerActionButton(icon.Plus),
			),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Background(c.panel),
		},
			kitex.If(len(data.Projects) > 0, func() kitex.Node {
				return projectRows
			}),
			kitex.If(len(data.Projects) == 0, func() kitex.Node {
				return emptyState("No projects configured yet.")
			}),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Foreground(c.warning).
					Bold(true),
			}, kitex.Text("CHANGED FILES [SESSION]")),
			kitex.Box(kitex.BoxProps{
				Style: style.S(),
			}, kitex.IfElse(len(data.ChangedFiles) == 0, emptyState("No changes in this session."), changedFileTree(fileTree, expandedPaths, onTogglePath, onSelectFile, 0))),
		),
	)
}

func explorerActionButton(icon kitex.Node) kitex.Node {
	c := useColors()
	return components.Button(components.ButtonProps{
		Variant: components.ButtonText,
		Color:   components.ButtonBase,
		Style: style.S().
			Width(style.Cells(3)).
			JustifyContent(style.JustifyCenter).
			PaddingHorizontal(0).
			Foreground(c.subtle),
		HoverStyle: style.S().
			Foreground(c.text).
			Background(c.surface),
	}, icon)
}

func emptyState(message string) kitex.Node {
	c := useColors()
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Padding(1).
			Foreground(c.subtle),
	}, kitex.Text(message))
}

type fileNode struct {
	Name         string
	FullPath     string
	IsDir        bool
	Children     map[string]*fileNode
	HasMetadata  bool
	Kind         string
	Additions    int
	Deletions    int
	OriginalPath string
}

func buildFileTree(changes []api.FileChangeSummary) *fileNode {
	root := &fileNode{IsDir: true, Children: map[string]*fileNode{}}
	for _, change := range changes {
		path := change.Path
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
		for i, part := range parts {
			fullParts = append(fullParts, part)
			fullPath := "/" + strings.Join(fullParts, "/")
			child, ok := current.Children[part]
			if !ok {
				child = &fileNode{
					Name:     part,
					FullPath: fullPath,
					IsDir:    i < len(parts)-1,
					Children: map[string]*fileNode{},
				}
				current.Children[part] = child
			}
			if i == len(parts)-1 {
				child.IsDir = false
				child.HasMetadata = true
				child.Kind = change.Kind
				child.Additions = change.NetAdditions
				child.Deletions = change.NetDeletions
				child.OriginalPath = change.Path
			}
			current = child
		}
	}
	return root
}

func sortedFileChildren(node *fileNode) []*fileNode {
	children := make([]*fileNode, 0, len(node.Children))
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

func changedFileTree(root *fileNode, expandedPaths map[string]bool, onTogglePath func(string), onSelectFile func(string), depth int) kitex.Node {
	c := useColors()
	children := sortedFileChildren(root)
	if len(children) == 0 {
		return nil
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
	}, kitex.Map(children, func(child *fileNode, _ int) kitex.Node {
		isExpanded := expandedPaths[child.FullPath]
		if !child.IsDir {
			badge := "M"
			badgeColor := c.warning
			if child.Kind == "created" {
				badge = "A"
				badgeColor = c.success
			} else if child.Kind == "deleted" {
				badge = "D"
				badgeColor = c.error
			}

			linesText := ""
			if child.Additions > 0 || child.Deletions > 0 {
				linesText = fmt.Sprintf(" +%d -%d", child.Additions, child.Deletions)
			}

			return components.Button(components.ButtonProps{
				Key:     child.FullPath,
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				Style: style.S().
					Width(style.Percent(100)).
					JustifyContent(style.JustifyStart).
					PaddingHorizontal(0).
					Background(c.panel),
				HoverStyle: style.S().
					Background(c.surface),
				OnClick: func() {
					if onSelectFile != nil && child.OriginalPath != "" {
						onSelectFile(child.OriginalPath)
					}
				},
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						AlignItems(style.AlignCenter).
						Gap(1).
						PaddingLeft(depth).
						PaddingRight(2),
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Width(style.Cells(2)).
							Foreground(badgeColor).
							Bold(true).
							TextAlign(style.TextAlignCenter),
					}, kitex.Text(badge)),
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(c.subtle),
					}, kitex.Text("󰈙")),
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(c.text),
					}, kitex.Text(child.Name)),
					kitex.If(linesText != "", func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(c.subtle),
						}, kitex.Text(linesText))
					}),
				),
			)
		}

		return kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
		},
			components.Button(components.ButtonProps{
				Key:     child.FullPath,
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				Style: style.S().
					Width(style.Percent(100)).
					JustifyContent(style.JustifyStart).
					PaddingHorizontal(0).
					Background(c.panel),
				HoverStyle: style.S().
					Background(c.surface),
				OnClick: func() {
					if onTogglePath != nil {
						onTogglePath(child.FullPath)
					}
				},
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						AlignItems(style.AlignCenter).
						Gap(1).
						PaddingLeft(depth),
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(c.subtle),
					}, kitex.IfElse(isExpanded, icon.ChevronDown, icon.ChevronRight)),
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(c.info),
					}, kitex.IfElse(isExpanded, icon.DirectoryOpen, icon.Folder)),
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(c.text).Bold(true),
					}, kitex.Text(child.Name)),
				),
			),
			kitex.If(isExpanded, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						MarginLeft(depth),
				}, changedFileTree(child, expandedPaths, onTogglePath, onSelectFile, depth+1))
			}),
		)
	}))
}
