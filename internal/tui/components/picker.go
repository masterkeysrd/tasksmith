package components

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// PickerItem represents a single selectable entry in the picker.
type PickerItem struct {
	ID       string
	Label    string
	Sublabel string
	Value    any
	Meta     string
	Disabled bool
}

// PickerGroup represents a named section within the picker.
type PickerGroup struct {
	Name  string
	Icon  string
	Items []PickerItem
}

// PickerAction represents a secondary action available in the picker footer.
type PickerAction struct {
	Label string
	Key   string
	Fn    func(PickerItem)
}

// PickerProps defines the properties for the Picker component.
type PickerProps struct {
	IsOpen        bool
	OnClose       func()
	OnSelect      func(PickerItem)
	Title         string
	Placeholder   string
	Groups        []PickerGroup
	Items         []PickerItem
	Footer        string
	FooterStyle   style.Style
	RenderItem    func(PickerItem) kitex.Node
	RenderPreview func(PickerItem) kitex.Node
	PreviewWidth  int
	Style         style.Style
	DisableSearch bool
	Actions       []PickerAction
	Attributes    map[string]string
}

var (
	PickerContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(85)).
				Height(style.Percent(80))

	PickerHeaderStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween).
				PaddingHorizontal(1).
				PaddingVertical(0)

	PickerInputContainerStyle = style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					PaddingHorizontal(1).
					PaddingVertical(0).
					MarginTop(0)

	PickerInputBorderedStyle = style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					PaddingHorizontal(1).
					PaddingVertical(0)

	PickerBodyStyle = style.S().
			Flex(4, 4, style.Cells(0)).
			MinHeight(style.Cells(0)).
			OverflowY(style.OverflowAuto)

	PickerFooterStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween).
				PaddingHorizontal(1)

	PickerFooterTipStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Height(style.Cells(1)).
				PaddingHorizontal(1).
				PaddingVertical(0)

	PickerItemStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			MinHeight(style.Cells(1)).
			PaddingHorizontal(1).
			Gap(1)

	PickerGroupHeaderStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Height(style.Cells(1)).
				PaddingHorizontal(1).
				PaddingVertical(0).
				Gap(1)

	PickerPreviewContainerStyle = style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					OverflowY(style.OverflowAuto).
					Padding(1)
)

func fuzzyScore(text, pattern string) (float64, bool) {
	if len(pattern) == 0 {
		return 1, true
	}
	if len(pattern) > len(text) {
		return 0, false
	}

	runeText := []rune(strings.ToLower(text))
	runePattern := []rune(strings.ToLower(pattern))

	score := 0.0
	consecutive := 0
	prevMatched := -1

	isPrefix := true
	for i, rp := range runePattern {
		if i >= len(runeText) || runeText[i] != rp {
			isPrefix = false
			break
		}
	}

	for _, rp := range runePattern {
		found := false
		for j := prevMatched + 1; j < len(runeText); j++ {
			if runeText[j] == rp {
				score += float64(len(runeText)-j) / float64(len(runeText))
				if j == prevMatched+1 {
					consecutive++
					score += float64(consecutive) * 0.5
				} else {
					consecutive = 1
				}
				if j == 0 || (j > 0 && unicode.IsSpace(runeText[j-1])) {
					score += 1.5
				}
				prevMatched = j
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}

	if isPrefix {
		score *= 1.5
	}

	return score, true
}

func filterItems(items []PickerItem, query string) []PickerItem {
	if query == "" {
		result := make([]PickerItem, 0, len(items))
		for _, item := range items {
			if !item.Disabled {
				result = append(result, item)
			}
		}
		if len(result) == 0 {
			return items
		}
		return result
	}

	type scoredItem struct {
		item  PickerItem
		score float64
	}

	var scored []scoredItem
	for _, item := range items {
		if item.Disabled {
			continue
		}
		s, matched := fuzzyScore(item.Label, query)
		if !matched {
			s, matched = fuzzyScore(item.Sublabel, query)
		}
		if !matched {
			s, matched = fuzzyScore(item.ID, query)
		}
		if matched {
			scored = append(scored, scoredItem{item, s})
		}
	}

	slices.SortFunc(scored, func(a, b scoredItem) int {
		if b.score != a.score {
			if b.score > a.score {
				return 1
			}
			return -1
		}
		return 0
	})

	result := make([]PickerItem, 0, len(scored))
	for _, s := range scored {
		result = append(result, s.item)
	}
	return result
}

func filterGroups(groups []PickerGroup, query string) []PickerGroup {
	if query == "" {
		result := make([]PickerGroup, 0, len(groups))
		for _, g := range groups {
			enabledItems := make([]PickerItem, 0, len(g.Items))
			for _, item := range g.Items {
				if !item.Disabled {
					enabledItems = append(enabledItems, item)
				}
			}
			if len(enabledItems) > 0 || len(g.Items) > 0 {
				g.Items = enabledItems
				result = append(result, g)
			}
		}
		return result
	}

	type scoredEntry struct {
		groupName string
		item      PickerItem
		score     float64
	}

	var allScored []scoredEntry

	for _, g := range groups {
		for _, item := range g.Items {
			if item.Disabled {
				continue
			}
			s, matched := fuzzyScore(item.Label, query)
			if !matched {
				s, matched = fuzzyScore(item.Sublabel, query)
			}
			if !matched {
				s, matched = fuzzyScore(item.ID, query)
			}
			if matched {
				allScored = append(allScored, scoredEntry{g.Name, item, s})
			}
		}
	}

	slices.SortFunc(allScored, func(a, b scoredEntry) int {
		if b.score != a.score {
			if b.score > a.score {
				return 1
			}
			return -1
		}
		return 0
	})

	groupOrder := make(map[string]int)
	var grouped []PickerGroup

	for _, entry := range allScored {
		if _, exists := groupOrder[entry.groupName]; !exists {
			groupOrder[entry.groupName] = len(grouped)
			grouped = append(grouped, PickerGroup{Name: entry.groupName, Items: nil})
		}
		idx := groupOrder[entry.groupName]
		grouped[idx].Items = append(grouped[idx].Items, entry.item)
	}

	return grouped
}

var Picker = kitex.FC("Picker", func(props PickerProps) kitex.Node {
	if !props.IsOpen {
		return nil
	}

	t := theme.UseTheme()
	inputRef := kitex.UseRef[dom.Element](nil)

	visibleCount := 15

	query, setQuery := kitex.UseState("")
	selectedIndex, setSelectedIndex := kitex.UseState(0)
	scrollOffset, setScrollOffset := kitex.UseState(0)

	// Refs to bridge react state to event listeners
	selectedIndexRef := kitex.UseRef(0)
	selectedIndexRef.Current = selectedIndex()

	totalItemsRef := kitex.UseRef(0)
	totalItemsRef.Current = 0

	var visibleItems []PickerItem
	var visibleGroups []PickerGroup

	q := query()
	if props.DisableSearch {
		q = ""
	}

	if len(props.Groups) > 0 {
		visibleGroups = filterGroups(props.Groups, q)
		for _, g := range visibleGroups {
			totalItemsRef.Current += len(g.Items)
		}
	} else {
		visibleItems = filterItems(props.Items, q)
		totalItemsRef.Current = len(visibleItems)
	}

	var flatVisible []PickerItem
	if len(visibleGroups) > 0 {
		for _, g := range visibleGroups {
			flatVisible = append(flatVisible, g.Items...)
		}
	} else {
		flatVisible = visibleItems
	}

	flatVisibleRef := kitex.UseRef[[]PickerItem](nil)
	flatVisibleRef.Current = flatVisible

	onCloseRef := kitex.UseRef[func()](nil)
	onCloseRef.Current = props.OnClose

	onSelectRef := kitex.UseRef[func(PickerItem)](nil)
	onSelectRef.Current = props.OnSelect

	actionsRef := kitex.UseRef[[]PickerAction](nil)
	actionsRef.Current = props.Actions

	// Reset selection when query changes
	kitex.UseEffect(func() {
		setSelectedIndex(0)
		setScrollOffset(0)
	}, []any{query()})

	// Focus input on open
	kitex.UseEffect(func() {
		if props.IsOpen && inputRef.Current != nil {
			if doc := inputRef.Current.OwnerDocument(); doc != nil {
				doc.Focus(inputRef.Current)
			}
		}
	}, []any{props.IsOpen})

	// Keyboard event handler
	kitex.UseEffectCleanup(func() func() {
		if !props.IsOpen {
			return nil
		}

		doc := inputRef.Current
		if doc == nil {
			return nil
		}

		sub := doc.AddEventListener(event.EventKeyDown, func(e event.Event) {
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}

			total := totalItemsRef.Current
			idx := selectedIndexRef.Current
			flat := flatVisibleRef.Current

			// Check if event matches any registered secondary actions
			keyStr := keymap.KeyToString(ke)
			for _, action := range actionsRef.Current {
				if action.Key == keyStr && action.Fn != nil {
					e.PreventDefault()
					e.StopPropagation()
					if total > 0 && idx < len(flat) {
						action.Fn(flat[idx])
					}
					return
				}
			}

			switch {
			case ke.Code == key.KeyEscape, ke.Text == "q":
				e.PreventDefault()
				e.StopPropagation()
				if onCloseRef.Current != nil {
					onCloseRef.Current()
				}
			case ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n":
				e.PreventDefault()
				e.StopPropagation()
				if total == 0 {
					return
				}
				if idx < len(flat) {
					item := flat[idx]
					if !item.Disabled {
						if onSelectRef.Current != nil {
							onSelectRef.Current(item)
						}
					}
				}
			case ke.Text == "j" || ke.Code == key.KeyDown:
				if total == 0 {
					return
				}
				e.PreventDefault()
				e.StopPropagation()
				newIdx := (idx + 1) % total
				setSelectedIndex(newIdx)
				selectedIndexRef.Current = newIdx

				if newIdx < scrollOffset() {
					setScrollOffset(newIdx)
				} else if newIdx >= scrollOffset()+visibleCount {
					setScrollOffset(newIdx - visibleCount + 1)
				}
			case ke.Text == "k" || ke.Code == key.KeyUp:
				if total == 0 {
					return
				}
				e.PreventDefault()
				e.StopPropagation()
				newIdx := (idx - 1 + total) % total
				setSelectedIndex(newIdx)
				selectedIndexRef.Current = newIdx

				if newIdx < scrollOffset() {
					setScrollOffset(newIdx)
				} else if newIdx >= scrollOffset()+visibleCount {
					setScrollOffset(newIdx - visibleCount + 1)
				}
			}
		})

		return func() {
			sub.Cancel()
		}
	}, []any{props.IsOpen, inputRef.Current != nil, visibleCount})

	if t == nil {
		return kitex.Dialog(kitex.DialogProps{
			ZIndex: 100,
		},
			kitex.Box(kitex.BoxProps{Style: PickerContainerStyle},
				kitex.Text(props.Title),
			),
		)
	}

	borderColor := t.Color.Border.Primary
	headerBg := t.Color.Surface.BaseFocus
	inputBg := t.Color.Surface.Base
	groupHeaderFg := t.Color.Text.Tertiary
	footerBg := t.Color.Surface.BaseDisabled
	textPrimary := t.Color.Text.Primary
	textTertiary := t.Color.Text.Tertiary
	textTertiaryHover := t.Color.Text.Tertiary

	headerLeft := kitex.Box(kitex.BoxProps{
		Style: style.S().Bold(true).Foreground(textPrimary),
	}, kitex.Text(props.Title))

	headerRight := kitex.Box(kitex.BoxProps{
		Style: style.S().Foreground(textTertiary),
	}, kitex.Text("ESC TO CLOSE"))

	total := totalItemsRef.Current

	var searchBarNode kitex.Node
	if !props.DisableSearch {
		chevronNode := kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				Foreground(textTertiary).
				MarginRight(1),
		}, kitex.Text(">"))

		inputNode := kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Flex(1, 1, style.Cells(0)).
				MinWidth(style.Cells(0)),
		},
			chevronNode,
			Input(InputProps{
				Ref:         inputRef,
				Value:       query(),
				Placeholder: props.Placeholder,
				Variant:     InputSolid,
				Style: style.S().
					Background(inputBg).
					Border(false).
					Padding(0).
					Height(style.Cells(1)),
				PlaceholderStyle: style.S().Foreground(textTertiary),
				OnChange: func(v string) {
					setQuery(v)
				},
			}),
		)

		searchBarNode = kitex.Box(kitex.BoxProps{
			Style: PickerInputBorderedStyle.Border(true, style.SingleBorder().Color(borderColor)),
		},
			inputNode,
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).AlignItems(style.AlignCenter).PaddingHorizontal(1).MinWidth(style.Cells(10)),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(textTertiary)},
					kitex.Text(strconv.Itoa(total)+" MATCHES"),
				),
			),
		)
	} else {
		searchBarNode = kitex.Box(kitex.BoxProps{
			Style: style.S().Width(style.Cells(0)).Height(style.Cells(0)).Overflow(style.OverflowHidden),
		},
			Input(InputProps{
				Ref:      inputRef,
				Value:    query(),
				Variant:  InputSolid,
				Style:    style.S().Background(color.Transparent).Border(false),
				OnChange: func(v string) {},
			}),
		)
	}

	var bodyNodes []kitex.Node
	flatIndex := 0
	start := scrollOffset()
	end := start + visibleCount

	if len(visibleGroups) > 0 {
		for _, group := range visibleGroups {
			groupVisibleItems := 0
			var groupItemsNodes []kitex.Node
			for _, item := range group.Items {
				if flatIndex >= start && flatIndex < end {
					groupItemsNodes = append(groupItemsNodes, renderPickerItem(t, item, flatIndex, selectedIndex(), props.RenderItem))
					groupVisibleItems++
				}
				flatIndex++
			}

			if groupVisibleItems > 0 {
				groupHeaderStyle := style.S().
					Foreground(groupHeaderFg).
					Bold(true).
					Background(t.Color.Surface.Base)

				groupIcon := kitex.Text("")
				if group.Icon != "" {
					groupIcon = kitex.Box(kitex.BoxProps{
						Style: style.S().MarginRight(1),
					}, kitex.Text(group.Icon))
				}

				bodyNodes = append(bodyNodes,
					kitex.Box(kitex.BoxProps{
						Style: PickerGroupHeaderStyle.Merge(groupHeaderStyle),
					},
						groupIcon,
						kitex.Text(strings.ToUpper(group.Name)),
					),
				)
				bodyNodes = append(bodyNodes, groupItemsNodes...)
			}
		}
	} else {
		for _, item := range visibleItems {
			if flatIndex >= start && flatIndex < end {
				bodyNodes = append(bodyNodes, renderPickerItem(t, item, flatIndex, selectedIndex(), props.RenderItem))
			}
			flatIndex++
		}
	}

	var footerNodes []kitex.Node

	if props.Footer != "" {
		footerTipStyle := PickerFooterTipStyle.Merge(props.FooterStyle).
			Background(t.Color.Surface.BaseDisabled).
			Foreground(textTertiary)

		footerNodes = append(footerNodes,
			kitex.Box(kitex.BoxProps{Style: footerTipStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().MarginRight(1),
				}, kitex.Text("\uf05a ")),
				kitex.Text(props.Footer),
			),
		)
	}

	var actionHints kitex.Node
	total = totalItemsRef.Current
	if total > 0 {
		var actionHintsNodes []kitex.Node
		actionHintsNodes = append(actionHintsNodes,
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(textTertiary)},
				kitex.Text(strconv.Itoa(selectedIndex()+1)+"/"+strconv.Itoa(total)),
			),
		)

		for _, action := range props.Actions {
			if action.Label != "" {
				hintStyle := style.S().Foreground(textTertiary)
				actionHintsNodes = append(actionHintsNodes,
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
					},
						kitex.Box(kitex.BoxProps{Style: hintStyle}, kitex.Text(action.Key+":")),
						kitex.Box(kitex.BoxProps{Style: hintStyle.Foreground(textTertiaryHover)}, kitex.Text(action.Label)),
					),
				)
			}
		}

		actionHints = kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(2),
		}, actionHintsNodes...)
	} else {
		actionHints = kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(textTertiary),
		}, kitex.Text("NO MATCHES"))
	}

	footerRight := kitex.Text("")
	if total > 0 && selectedIndex() < len(flatVisible) {
		item := flatVisible[selectedIndex()]
		if item.Meta != "" {
			footerRight = kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(textTertiary),
			}, kitex.Text(item.Meta))
		}
	}

	footerContent := kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Flex(1).MinWidth(style.Cells(0)),
	},
		actionHints,
		footerRight,
	)

	footerStyle := PickerFooterStyle.Merge(props.FooterStyle).
		Background(footerBg)

	footerNavStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Foreground(textTertiary).
		AlignItems(style.AlignCenter).
		Flex(1).
		Gap(2)

	footerNav := kitex.Box(kitex.BoxProps{
		Style: footerNavStyle,
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				Gap(1),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Background(t.Color.Surface.BaseFocus).
					TextAlign(style.TextAlignCenter)}, kitex.Text("↑"),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Background(t.Color.Surface.BaseFocus).
					TextAlign(style.TextAlignCenter)}, kitex.Text("↓"),
			),
			kitex.Text("NAVIGATE"),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				Gap(1),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Background(t.Color.Surface.BaseFocus).
					TextAlign(style.TextAlignCenter)}, kitex.Text("↵"),
			),
			kitex.Text("SELECT"),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				Gap(1),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Background(t.Color.Surface.BaseFocus).
					TextAlign(style.TextAlignCenter)}, kitex.Text("Esc"),
			),
			kitex.Text("CLOSE"),
		),
	)

	var previewNode kitex.Node
	if props.RenderPreview != nil && props.PreviewWidth > 0 && total > 0 && selectedIndex() < len(flatVisible) {
		item := flatVisible[selectedIndex()]
		previewNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Flex(6, 6, style.Cells(0)).
				MinWidth(style.Cells(props.PreviewWidth)).
				// BorderLeft(true, style.SingleBorder().Color(t.Color.Border.Primary)).
				Background(t.Color.Surface.BaseFocus),
		},
			props.RenderPreview(item),
		)
	}

	return kitex.Dialog(kitex.DialogProps{
		ZIndex: 100,
	},
		Paper(PaperProps{
			Color:      PaperBase,
			Variant:    PaperOutlined,
			Style:      PickerContainerStyle.Merge(props.Style),
			Attributes: props.Attributes,
		},
			kitex.Box(kitex.BoxProps{
				Style: PickerHeaderStyle.Background(headerBg),
			},
				headerLeft,
				headerRight,
			),
			searchBarNode,
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).Flex(1, 1, style.Cells(0)).MinHeight(style.Cells(0)).PaddingVertical(1),
			},
				kitex.Box(kitex.BoxProps{
					Style: PickerBodyStyle,
				},
					kitex.Fragment(bodyNodes...),
				),
				kitex.If(previewNode != nil, func() kitex.Node {
					return previewNode
				}),
			),
			kitex.If(len(footerNodes) > 0, func() kitex.Node {
				return kitex.Fragment(footerNodes...)
			}),
			kitex.Box(kitex.BoxProps{
				Style: footerStyle,
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Flex(1).
						Gap(2).JustifyContent(style.JustifyBetween),
				},
					footerNav,
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(2),
					},
						footerContent,
					),
				),
			),
		),
	)
})

func renderPickerItem(t *theme.Scheme, item PickerItem, index, selectedIndex int, customRenderer func(PickerItem) kitex.Node) kitex.Node {
	if customRenderer != nil {
		return customRenderer(item)
	}

	isSelected := index == selectedIndex
	itemStyle := PickerItemStyle

	if isSelected {
		itemStyle = itemStyle.Background(t.Color.Surface.BaseHover)
	}

	if item.Disabled {
		return kitex.Box(kitex.BoxProps{Style: itemStyle.Foreground(t.Color.Text.Tertiary)},
			kitex.Box(kitex.BoxProps{Style: style.S().Width(style.Cells(1)).MarginRight(1)}, kitex.Text(" ")),
			kitex.Box(kitex.BoxProps{Style: style.S().Flex(1)},
				kitex.Text("disabled"),
			),
		)
	}

	if isSelected {
		return kitex.Box(kitex.BoxProps{Style: itemStyle.Foreground(t.Color.Text.Primary)},
			kitex.Box(kitex.BoxProps{Style: style.S().Width(style.Cells(1)).MarginRight(1).Foreground(t.Color.Surface.Primary)}, kitex.Text("\u25cf")),
			kitex.Box(kitex.BoxProps{Style: style.S().Flex(1)},
				kitex.Text(item.Label),
			),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)},
				kitex.Text(item.Sublabel),
			),
		)
	}

	return kitex.Box(kitex.BoxProps{Style: itemStyle.Foreground(t.Color.Text.Secondary)},
		kitex.Box(kitex.BoxProps{Style: style.S().Width(style.Cells(1)).MarginRight(1)}, kitex.Text(" ")),
		kitex.Box(kitex.BoxProps{Style: style.S().Flex(1)},
			kitex.Text(item.Label),
		),
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)},
			kitex.Text(item.Sublabel),
		),
	)
}
