package commandbar

import (
	"context"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/widgets"
)

// Props defines properties for the CommandBar component.
type Props struct{}

// View is the bottom command and status bar component.
var View = kitex.FC("CommandBar", func(props Props) kitex.Node {
	t := theme.UseTheme()
	m := mode.Use()

	value, setValue := kitex.UseState("")
	commandError, setCommandError := kitex.UseState("")
	statusMessage := active.UseStatusMessage()
	inputRef := kitex.UseRef[dom.Element](nil)

	// Initialize autocomplete controller once for commands
	acController := kitex.UseMemo(func() any {
		return autocomplete.New(autocomplete.Config{
			Triggers:    map[string][]string{"": {"command"}},
			Prefixes:    map[string]string{},
			CycleInline: true,
		})
	}, []any{}).(*autocomplete.Controller)

	_ = acController.Use()

	isOpen := m == mode.Command

	// Focus the input when entering command mode, and reset value
	kitex.UseEffect(func() {
		if isOpen {
			setValue("")
			setCommandError("")
			if inputRef.Current != nil {
				if doc := inputRef.Current.OwnerDocument(); doc != nil {
					doc.Focus(inputRef.Current)
				}
			} else {
				kitex.PostMacro(func() {
					if inputRef.Current != nil {
						if doc := inputRef.Current.OwnerDocument(); doc != nil {
							doc.Focus(inputRef.Current)
						}
					}
				})
			}
		} else {
			acController.SetIsOpen(false)
		}
	}, []any{isOpen})

	val := value()

	// Get colors dynamically from the theme
	var bg color.Color
	var colorTextOpen, colorTextDimmed, colorTextExtraDim color.Color = color.Transparent, color.Transparent, color.Transparent

	if t != nil {
		if isOpen {
			bg = t.Color.Surface.BaseHover
		} else {
			bg = t.Color.Surface.Base
		}
		colorTextOpen = t.Color.Text.Primary
		colorTextDimmed = t.Color.Text.Tertiary
		colorTextExtraDim = t.Color.Border.Primary
	}

	outerStyle := style.S().
		Width(style.Percent(100)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Background(bg)

	barStyle := style.S().
		Width(style.Percent(100)).
		Height(style.Cells(1)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		PaddingHorizontal(2)

	// ── Layout ────────────────────────────────────────────────────────────

	boxProps := kitex.BoxProps{Style: outerStyle}
	if !isOpen {
		boxProps.OnClick = func(e event.Event) {
			mode.Set(mode.Command)
		}
	}

	var cursorOffset int
	if inputRef.Current != nil {
		if sr, ok := inputRef.Current.(interface{ SelectionRange() (int, int) }); ok {
			start, _ := sr.SelectionRange()
			cursorOffset = start
		} else {
			cursorOffset = len(val)
		}
	} else {
		cursorOffset = len(val)
	}

	return kitex.Box(boxProps,
		// Autocomplete Dropdown List as Overlay
		widgets.AutocompleteOverlay(widgets.AutocompleteOverlayProps{
			Anchor:     inputRef.Current,
			Controller: acController,
			Value:      val,
		}, widgets.AutocompleteMenu(widgets.AutocompleteMenuProps{
			Controller: acController,
			Value:      val,
			OnSelect: func(item autocomplete.Item) {
				newText, _ := acController.ApplySelection(val, cursorOffset, item)
				setValue(newText)
				acController.SetIsOpen(false)
			},
		})),
		// Bar Content
		kitex.Box(kitex.BoxProps{Style: barStyle},
			kitex.IfElse(isOpen,
				// Open / Command Mode UI
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Flex(1),
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(colorTextOpen).
							MarginRight(1),
					}, kitex.Text(":")),

					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							Flex(1, 1, style.Cells(0)),
					},
						components.Input(components.InputProps{
							Ref:         inputRef,
							Value:       val,
							Placeholder: "enter command...",
							Variant:     components.InputSolid,
							Style: style.S().
								Background(bg).
								Foreground(colorTextOpen).
								Border(false),
							PlaceholderStyle: style.S().
								Foreground(colorTextDimmed),
							OnChange: func(v string) {
								setValue(v)
								var cursorOffset int
								if inputRef.Current != nil {
									if sr, ok := inputRef.Current.(interface{ SelectionRange() (int, int) }); ok {
										start, _ := sr.SelectionRange()
										cursorOffset = start
									} else {
										cursorOffset = len(v)
									}
								} else {
									cursorOffset = len(v)
								}
								acController.HandleOnChange(v, cursorOffset)
							},
							OnBlur: func() {
								if isOpen {
									kitex.PostMacro(func() {
										if inputRef.Current != nil {
											if doc := inputRef.Current.OwnerDocument(); doc != nil {
												doc.Focus(inputRef.Current)
											}
										}
									})
								}
							},
							OnKeyDown: func(e event.Event) {
								ke, ok := e.(*event.KeyEvent)
								if !ok {
									return
								}

								// Show autocomplete dropdown on Ctrl+Space
								if (ke.Code == key.KeySpace || ke.Text == " ") && (ke.Mod&key.ModCtrl) != 0 {
									e.PreventDefault()
									e.StopPropagation()
									acController.HandleOnChange(val, cursorOffset)
									acController.SetIsOpen(true)
									return
								}

								// Intercept autocomplete controls
								if acController.HandleOnKeyDown(ke, val, setValue) {
									e.PreventDefault()
									e.StopPropagation()
									return
								}

								if ke.Code == key.KeyEscape {
									e.PreventDefault()
									e.StopPropagation()
									mode.Set(mode.Normal)
									setValue("")
									return
								}

								if ke.Code == key.KeyEnter {
									e.PreventDefault()
									e.StopPropagation()

									target := value()
									p := strings.Fields(target)
									if len(p) > 0 {
										cmdName := strings.TrimPrefix(p[0], ":")
										go func() {
											if err := command.Execute(context.Background(), cmdName, p[1:]...); err != nil {
												log.Error("failed to execute command", log.String("command", cmdName), log.Err(err))
												setCommandError(err.Error())
											}
										}()
									}

									mode.Set(mode.Normal)
									setValue("")
									return
								}
							},
						}),
					),

					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Gap(2).
							Foreground(colorTextDimmed).
							MarginLeft(2),
					},
						kitex.Text("COMMAND_MODE"),
						kitex.Text("│"),
						kitex.Text("ESC TO ABORT"),
					),
				),

				// Closed / Ready Mode UI
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Flex(1),
				},
					kitex.IfElse(commandError() != "",
						kitex.Fragment(
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									Foreground(t.Color.Text.Error).
									MarginRight(1),
							}, kitex.Text("✖")),
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									Foreground(t.Color.Text.Error).
									Bold(true),
							}, kitex.Text("ERROR: "+commandError())),
						),
						kitex.IfElse(statusMessage != "",
							kitex.Fragment(
								kitex.Box(kitex.BoxProps{
									Style: style.S().
										Foreground(t.Color.Surface.Success).
										MarginRight(1),
								}, kitex.Text("✔")),
								kitex.Box(kitex.BoxProps{
									Style: style.S().
										Foreground(t.Color.Surface.Success).
										Bold(true),
								}, kitex.Text(statusMessage)),
							),
							kitex.Fragment(
								kitex.Box(kitex.BoxProps{
									Style: style.S().
										Foreground(colorTextDimmed).
										MarginRight(1),
								}, kitex.Text("●")),
								kitex.Box(kitex.BoxProps{
									Style: style.S().
										Foreground(colorTextDimmed),
								}, kitex.Text("READY_FOR_DIRECTIVE")),
							),
						),
					),

					kitex.Box(kitex.BoxProps{
						Style: style.S().Flex(1),
					}),

					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(colorTextExtraDim),
					}, kitex.Text("PRESS ':' FOR SYSTEM_COMMANDS")),
				),
			),
		),
	)
})
