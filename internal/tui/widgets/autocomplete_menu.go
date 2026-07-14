package widgets

import (
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/cursor"
	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/geom"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// AutocompleteMenuProps defines properties for the AutocompleteMenu widget.
type AutocompleteMenuProps struct {
	Controller *autocomplete.Controller
	OnSelect   func(autocomplete.Item)
	Style      style.Style
	HideIcons  bool
	HideBadges bool
	SessionID  string
	Value      string // The full current input value
}

var (
	overlayCardBaseStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Cells(64)). // Increased from 58
				Padding(0)              // Reduced from 0, 1

	menuTitleStyle = style.S().
			Bold(true).
			Margin(0, 0, 1, 0)

	menuListStyle = style.S().
			ListStyleType(style.ListStyleNone).
			Padding(0).
			Margin(0).
			MaxHeight(style.Cells(8)).
			OverflowY(style.OverflowAuto)

	menuRowStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Padding(0, 1).
			Gap(1).
			Height(style.Cells(1))

	// Fixed-width column styles to achieve a clean tabular alignment
	menuIconColStyle = style.S().
				Width(style.Cells(2))

	menuBadgeColStyle = style.S().
				Width(style.Cells(5)). // 5-char badges (CLASS, CONST, FIELD, etc.)
				Bold(true)

	menuLabelColStyle = style.S().
				Width(style.Cells(22)).
				Bold(true).
				Overflow(style.OverflowHidden).
				WhiteSpace(style.WhiteSpaceNoWrap)

	menuDetailColStyle = style.S().
				Flex(1, 1, style.Cells(0)).
				Overflow(style.OverflowHidden).
				WhiteSpace(style.WhiteSpaceNoWrap)

	menuFooterStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Padding(0, 1).
			Height(style.Cells(1)).
			Overflow(style.OverflowHidden)
)

// AutocompleteMenu renders the floating dropdown list of completion suggestions in a tabular format.
var AutocompleteMenu = kitex.FC("AutocompleteMenu", func(props AutocompleteMenuProps) kitex.Node {
	acState := props.Controller.Use()
	scrollOffset, setScrollOffset := kitex.UseState(0)
	visibleCount := 8

	// Reactive auto-scrolling to keep the selected suggestion in the viewport
	kitex.UseEffect(func() {
		idx := acState.SelectedIndex
		items := acState.FilteredItems
		total := len(items)
		if total == 0 {
			setScrollOffset(0)
			return
		}

		currScroll := scrollOffset()
		if idx < 0 {
			setScrollOffset(0)
		} else if idx < currScroll {
			setScrollOffset(idx)
		} else if idx >= currScroll+visibleCount {
			setScrollOffset(idx - visibleCount + 1)
		}
	}, []any{acState.SelectedIndex, len(acState.FilteredItems)})

	// Fetch query results reactively when query or open status changes with a 100ms debounce
	kitex.UseEffectCleanup(func() func() {
		if !acState.IsOpen {
			props.Controller.SetItems(nil)
			return nil
		}

		// Strip trigger characters and namespace prefixes before querying the provider
		var strippedQuery string
		var sources []string
		var matched bool
		strippedQuery, sources, matched = props.Controller.Parse(acState.Query)
		if !matched {
			sources = nil
		}

		// Setup a 100ms debounce timer to prevent query spamming
		debounceDuration := 100 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())

		timer := time.AfterFunc(debounceDuration, func() {
			promise.New(func(ctx context.Context) (*[]autocomplete.Item, error) {
				p := autocomplete.GetPlugin()
				if p == nil {
					return nil, nil
				}
				items, err := p.Query(ctx, autocomplete.QueryReq{
					Query:     strippedQuery,
					Sources:   sources,
					SessionID: props.SessionID,
					FullText:  props.Value,
				})
				if err != nil {
					return nil, err
				}
				return &items, nil
			}).Then(func(items *[]autocomplete.Item) {
				// Only update the state if the context has not been cancelled/superseded
				if ctx.Err() == nil && items != nil {
					if props.Controller.CycleInline() && len(*items) == 1 && props.Controller.IsOpen() {
						selectedItem := (*items)[0]
						if props.OnSelect != nil {
							props.OnSelect(selectedItem)
						} else {
							props.Controller.SetItems(nil)
							props.Controller.SetIsOpen(false)
						}
					} else {
						props.Controller.SetItems(*items)
					}
				}
			}, func(err error) {
				// Ignore query errors
			})
		})

		return func() {
			timer.Stop()
			cancel()
		}
	}, []any{acState.Query, acState.IsOpen})

	if !acState.IsOpen || len(acState.FilteredItems) == 0 {
		return nil
	}

	t := theme.UseTheme()

	var bg, borderColor, textCol, detailCol color.Color
	var activeBg, activeFg, activeDetailCol color.Color

	if t != nil {
		bg = t.Color.Surface.BaseFocus
		borderColor = t.Color.Border.Primary
		textCol = t.Color.Text.Secondary
		detailCol = t.Color.Text.Tertiary

		// Use BaseHover for a subtle selection block to avoid washing out text
		activeBg = t.Color.Surface.BaseHover
		activeFg = t.Color.Text.Primary
		activeDetailCol = t.Color.Text.Secondary
	} else {
		// Fallbacks from demo styles
		bg = color.RGBA{R: 24, G: 28, B: 38, A: 255}
		borderColor = color.RGBA{R: 108, G: 124, B: 171, A: 255}
		textCol = color.RGBA{R: 200, G: 210, B: 230, A: 255}
		detailCol = color.RGBA{R: 142, G: 151, B: 178, A: 255}
		activeBg = color.RGBA{R: 63, G: 84, B: 145, A: 255}
		activeFg = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		activeDetailCol = color.RGBA{R: 210, G: 220, B: 240, A: 255}
	}

	containerStyle := overlayCardBaseStyle.
		Background(bg).
		Border(style.SingleBorder().Color(borderColor)).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{
		Style: containerStyle,
	},
		kitex.IfElse(len(acState.FilteredItems) == 0,
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(detailCol).Padding(0, 1),
			}, kitex.Text("No matches")),
			kitex.UL(kitex.ULProps{
				Style: menuListStyle,
			},
				func() kitex.Node {
					items := acState.FilteredItems
					if len(items) == 0 {
						return nil
					}
					start := scrollOffset()
					if start < 0 {
						start = 0
					}
					if start >= len(items) {
						start = len(items) - 1
					}
					end := start + visibleCount
					if end > len(items) {
						end = len(items)
					}
					visibleItems := items[start:end]

					return kitex.Fragment(
						kitex.Map(visibleItems, func(item autocomplete.Item, localIdx int) kitex.Node {
							globalIdx := start + localIdx
							isSelected := globalIdx == acState.SelectedIndex

							rowStyle := menuRowStyle
							var currentLabelCol, currentDetailCol color.Color
							if isSelected {
								rowStyle = rowStyle.Background(activeBg).Foreground(activeFg)
								currentLabelCol = activeFg
								currentDetailCol = activeDetailCol
							} else {
								rowStyle = rowStyle.Foreground(textCol)
								currentLabelCol = textCol
								currentDetailCol = detailCol
							}

							// 1. Kind Icon Column (Fixed Width: 2)
							var iconNode kitex.Node
							if !props.HideIcons && t != nil {
								var iNode kitex.Node
								if item.Kind != "" {
									iNode, _ = icon.LspKindIcon(item.Kind, t)
								}
								// Always render the Box to align subsequent columns
								iconNode = kitex.Box(kitex.BoxProps{
									Style: menuIconColStyle,
								}, kitex.IfElse(iNode != nil, iNode, kitex.Text(" ")))
							}

							// 2. Category Badge Column (Fixed Width: 4)
							var badgeNode kitex.Node
							if !props.HideBadges {
								var bText string
								var badgeCol color.Color
								if item.Badge != "" {
									bText = item.Badge
									if t != nil {
										switch bText {
										case "FILE ", "DIR  ":
											badgeCol = t.Color.Surface.Success
										case "CMD  ":
											badgeCol = t.Color.Surface.Tertiary
										default:
											badgeCol = t.Color.Surface.Info
										}
									} else {
										badgeCol = color.RGBA{R: 123, G: 205, B: 165, A: 255}
									}
								}
								// Always render the Box to align subsequent columns
								badgeNode = kitex.Box(kitex.BoxProps{
									Style: menuBadgeColStyle.Foreground(badgeCol),
								}, kitex.Text(bText))
							}

							// 3. Name / Label Column (Fixed Width: 22)
							labelNode := kitex.Box(kitex.BoxProps{
								Style: menuLabelColStyle.Foreground(currentLabelCol),
							}, kitex.Text(item.Label))

							// 4. Sublabel / Detail Column (Flex: 1)
							detailNode := kitex.Box(kitex.BoxProps{
								Style: menuDetailColStyle.Foreground(currentDetailCol),
							}, kitex.Text(item.Sublabel))

							return kitex.LI(kitex.LIProps{
								Key:   "item-" + item.ID,
								Style: rowStyle,
								OnClick: func(e event.Event) {
									if props.OnSelect != nil {
										props.OnSelect(item)
									}
								},
							},
								// Icon
								kitex.If(iconNode != nil, func() kitex.Node {
									return iconNode
								}),
								// Category Badge
								kitex.If(badgeNode != nil, func() kitex.Node {
									return badgeNode
								}),
								// Label (Symbol/Filename)
								labelNode,
								// Detail description or directory path
								detailNode,
							)
						}),
					)
				}(),
			),
		),
		func() kitex.Node {
			if acState.SelectedIndex >= 0 && acState.SelectedIndex < len(acState.FilteredItems) {
				selectedItem := acState.FilteredItems[acState.SelectedIndex]
				var footerText string
				if strings.HasPrefix(selectedItem.Badge, "FILE") || strings.HasPrefix(selectedItem.Badge, "DIR") {
					footerText = "Path: " + selectedItem.ID
				} else if strings.HasPrefix(selectedItem.Badge, "CMD") {
					footerText = "Cmd: " + selectedItem.Label
				} else {
					// Symbol coordinates
					parts := strings.Split(selectedItem.ID, ":")
					isLocal := len(parts) > 0 && !filepath.IsAbs(parts[0]) && !strings.HasPrefix(parts[0], "..")
					if len(parts) >= 2 && isLocal {
						lineNum := 1
						if l, err := strconv.Atoi(parts[1]); err == nil {
							lineNum = l + 1
						}
						var declName string
						if len(parts) >= 5 {
							declName = " (" + parts[4] + ")"
						}
						footerText = fmt.Sprintf("Decl: %s:%d%s", parts[0], lineNum, declName)
					} else {
						var declName string
						if len(parts) >= 5 {
							declName = parts[4]
						} else {
							declName = selectedItem.Label
						}
						footerText = "Decl: " + declName + " (external)"
					}
				}

				var bgDarker color.Color
				if t != nil {
					bgDarker = t.Color.Surface.Base
				} else {
					bgDarker = color.RGBA{R: 16, G: 20, B: 28, A: 255}
				}

				return kitex.Box(kitex.BoxProps{
					Style: menuFooterStyle.Background(bgDarker).Foreground(detailCol),
				}, kitex.Text(footerText))
			}
			return nil
		}(),
	)
})

type AutocompleteOverlayProps struct {
	Anchor      dom.Element
	InputAnchor dom.Element
	Controller  *autocomplete.Controller
	Value       string
	Children    []kitex.Node
}

// AutocompleteOverlay renders a Box that registers itself as a document overlay on mount,
// bypassing the static positioning logic of kitex.Overlay and positioning itself relative to the anchor.
var AutocompleteOverlay = kitex.FCC("AutocompleteOverlay", func(props AutocompleteOverlayProps) kitex.Node {
	elRef := kitex.UseRef[dom.Node](nil)
	docFunc := kitex.UseDocument()
	doc := docFunc()

	acState := props.Controller.Use()

	kitex.UseEffectCleanup(func() func() {
		node := elRef.Current
		log.Info("AutocompleteOverlay UseEffectCleanup: evaluate", log.Bool("isOpen", acState.IsOpen), log.Bool("hasNode", node != nil))
		if node != nil && doc != nil && acState.IsOpen {
			elVal := node.(dom.Element)
			log.Info("AutocompleteOverlay ShowOverlay: register overlay")
			doc.ShowOverlay(elVal, 999)
			return func() {
				log.Info("AutocompleteOverlay ShowOverlay cleanup: HideOverlay")
				doc.HideOverlay(elVal)
			}
		}
		return nil
	}, []any{elRef.Current, acState.IsOpen})

	// Restore focus to input anchor when autocomplete closes
	kitex.UseEffect(func() {
		log.Info("AutocompleteOverlay Focus UseEffect: check", log.Bool("isOpen", acState.IsOpen))
		if !acState.IsOpen {
			inputAnchor := props.InputAnchor
			if inputAnchor == nil {
				inputAnchor = props.Anchor
			}
			if inputAnchor != nil && doc != nil {
				log.Info("AutocompleteOverlay Focus UseEffect: calling Focus")
				doc.Focus(inputAnchor)
			}
		}
	}, []any{acState.IsOpen})

	var marginLeft int
	var marginTop int
	display := style.DisplayFlex

	if !acState.IsOpen {
		display = style.DisplayNone
	} else if props.Anchor != nil {
		if rect, ok := props.Anchor.GetBoundingClientRect(); ok {
			inputAnchor := props.InputAnchor
			if inputAnchor == nil {
				inputAnchor = props.Anchor
			}

			var cursorX int
			var cursorY int
			if cs, ok := inputAnchor.(interface{ CursorState() cursor.State }); ok {
				cursorState := cs.CursorState()
				cursorX = cursorState.X
				cursorY = cursorState.Y
			}

			var inputRect geom.Rect
			if ir, ok := inputAnchor.GetBoundingClientRect(); ok {
				inputRect = ir
			} else {
				inputRect = rect
			}

			var docWidth int
			var docHeight int
			if doc != nil {
				if view := doc.DefaultView(); view != nil {
					sz := view.ViewportSize()
					docWidth = sz.Width
					docHeight = sz.Height
				}
			}

			menuWidth := 64

			// Dynamic Height Calculation (includes border + list items):
			numItems := len(acState.FilteredItems)
			if numItems > 8 {
				numItems = 8
			}
			menuHeight := numItems + 3

			// Get the cursor offset from inputAnchor
			var cursorOffset int
			if sr, ok := inputAnchor.(interface{ SelectionRange() (int, int) }); ok {
				start, _ := sr.SelectionRange()
				cursorOffset = start
			} else {
				cursorOffset = len(props.Value)
			}

			startIdx := autocomplete.FindTriggerStart(props.Value, cursorOffset)
			marginLeft = inputRect.Origin.X + cursorX - (cursorOffset - startIdx)

			// Horizontal boundary flipping/clamping
			if docWidth > 0 && marginLeft+menuWidth > docWidth {
				marginLeft = docWidth - menuWidth - 1
			}
			if marginLeft < 0 {
				marginLeft = 0
			}

			// Vertical placement: default below the cursor line (+1)
			cursorLineY := inputRect.Origin.Y + cursorY
			marginTop = cursorLineY + 1
			if docHeight > 0 && marginTop+menuHeight > docHeight {
				// Flip: place completely above the cursor line
				marginTop = cursorLineY - menuHeight
			}
			if marginTop < 0 {
				marginTop = 0
			}
		}
	}

	return kitex.Box(kitex.BoxProps{
		Ref: elRef,
		Style: style.S().
			Display(display).
			MarginLeft(marginLeft).
			MarginTop(marginTop).
			Width(style.Cells(64)).
			FlexDirection(style.FlexColumn).
			Background(color.Transparent),
	}, props.Children...)
})

func extractPackageFromPath(path string) string {
	path = filepath.ToSlash(path)

	// Go package mod cache
	if idx := strings.Index(path, "go/pkg/mod/"); idx != -1 {
		sub := path[idx+len("go/pkg/mod/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		if atIdx := strings.Index(sub, "@"); atIdx != -1 {
			afterAt := sub[atIdx:]
			if nextSlash := strings.Index(afterAt, "/"); nextSlash != -1 {
				sub = sub[:atIdx] + afterAt[nextSlash:]
			} else {
				sub = sub[:atIdx]
			}
		}
		return sub
	}

	// Go standard library
	if idx := strings.LastIndex(path, "/src/"); idx != -1 {
		sub := path[idx+len("/src/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			return sub[:lastSlash]
		}
		return sub
	}

	// Node/TypeScript node_modules
	if idx := strings.Index(path, "node_modules/"); idx != -1 {
		sub := path[idx+len("node_modules/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		if strings.HasPrefix(sub, "@") {
			parts := strings.Split(sub, "/")
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		} else {
			parts := strings.Split(sub, "/")
			if len(parts) >= 1 {
				return parts[0]
			}
		}
		return sub
	}

	// Python site-packages & dist-packages
	if idx := strings.Index(path, "site-packages/"); idx != -1 {
		sub := path[idx+len("site-packages/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		parts := strings.Split(sub, "/")
		if len(parts) >= 1 {
			return parts[0]
		}
		return sub
	}
	if idx := strings.Index(path, "dist-packages/"); idx != -1 {
		sub := path[idx+len("dist-packages/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		parts := strings.Split(sub, "/")
		if len(parts) >= 1 {
			return parts[0]
		}
		return sub
	}

	// Fallback to directory name
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return ""
	}
	return filepath.Base(dir)
}
