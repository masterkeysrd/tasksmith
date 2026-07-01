package sidebar

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type ExplorerPanelProps struct {
	Data          Data
	ExpandedPaths map[string]bool
	OnTogglePath  func(string)
	OnSelectFile  func(string)
}

func computeExplorerCacheKey(changes []api.FileChangeSummary, expanded map[string]bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "c:%d;", len(changes))
	for _, c := range changes {
		fmt.Fprintf(&sb, "%s:%d:%s;", c.Path, c.TotalEdits, c.LastChangedAt.Format(time.RFC3339Nano))
	}
	var expKeys []string
	for k, v := range expanded {
		if v {
			expKeys = append(expKeys, k)
		}
	}
	sort.Strings(expKeys)
	for _, k := range expKeys {
		fmt.Fprintf(&sb, "e:%s;", k)
	}
	return sb.String()
}

// ExplorerPanel renders the sidebar file tree explorer reactively.
var ExplorerPanel = kitex.FC("ExplorerPanel", func(props ExplorerPanelProps) kitex.Node {
	c := useColors()
	t := theme.UseTheme()

	projectRows := kitex.Map(props.Data.Projects, func(project api.Project, idx int) kitex.Node {
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

	var themeType string
	if t != nil {
		themeType = t.Type
	}
	cacheKey := computeExplorerCacheKey(props.Data.ChangedFiles, props.ExpandedPaths) + ":" + themeType

	fileTreeContent := kitex.UseMemo(func() kitex.Node {
		fileTree := buildFileTree(props.Data.ChangedFiles)
		if len(props.Data.ChangedFiles) == 0 {
			return emptyState("No changes in this session.")
		}
		return changedFileTree(fileTree, props.ExpandedPaths, props.OnTogglePath, props.OnSelectFile)
	}, []any{cacheKey})

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Background(c.panel).
			MinHeight(style.Percent(100)),
		Attributes: map[string]string{"data-context": "explorer"},
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
			}, kitex.Text(props.Data.WorkspaceName)),
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
			kitex.If(len(props.Data.Projects) > 0, func() kitex.Node {
				return projectRows
			}),
			kitex.If(len(props.Data.Projects) == 0, func() kitex.Node {
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
			fileTreeContent,
		),
	)
})

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

type flatNode struct {
	node       *fileNode
	depth      int
	isExpanded bool
}

func flattenTree(node *fileNode, expandedPaths map[string]bool, depth int, result *[]flatNode) {
	children := sortedFileChildren(node)
	for _, child := range children {
		isExpanded := expandedPaths[child.FullPath]
		*result = append(*result, flatNode{
			node:       child,
			depth:      depth,
			isExpanded: isExpanded,
		})
		if child.IsDir && isExpanded {
			flattenTree(child, expandedPaths, depth+1, result)
		}
	}
}

func changedFileTree(root *fileNode, expandedPaths map[string]bool, onTogglePath func(string), onSelectFile func(string)) kitex.Node {
	c := useColors()

	var flatList []flatNode
	flattenTree(root, expandedPaths, 0, &flatList)

	if len(flatList) == 0 {
		return nil
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
	}, kitex.Map(flatList, func(item flatNode, _ int) kitex.Node {
		child := item.node
		depth := item.depth
		isExpanded := item.isExpanded

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
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(c.subtle)}, icon.FileIcon(icon.FileIconProps{Path: child.OriginalPath})),
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
		)
	}))
}
