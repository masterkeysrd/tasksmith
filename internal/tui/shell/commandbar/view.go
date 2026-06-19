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
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// Props defines properties for the CommandBar component.
type Props struct{}

// View is the bottom command and status bar component.
var View = kitex.FC("CommandBar", func(props Props) kitex.Node {
	t := theme.UseTheme()
	m := mode.Use()

	value, setValue := kitex.UseState("")
	inputRef := kitex.UseRef[dom.Element](nil)

	isOpen := m == mode.Command

	// Focus the input when entering command mode, and reset value
	kitex.UseEffect(func() {
		if isOpen {
			setValue("")
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

	return kitex.Box(boxProps,
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
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(colorTextDimmed).
							MarginRight(1),
					}, kitex.Text("●")),

					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(colorTextDimmed),
					}, kitex.Text("READY_FOR_DIRECTIVE")),

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
