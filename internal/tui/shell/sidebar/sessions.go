package sidebar

import (
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/format"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	sessionRowStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			PaddingHorizontal(1)

	sessionRowActionsStyle = style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				Gap(1)

	sessionRowActionBtnStyle = style.S().
					PaddingHorizontal(0).
					PaddingVertical(0)
)

type sessionRowProps struct {
	Session   api.Session
	IsActive  bool
	IsLoading bool
	OnSelect  func()
	OnRename  func(title string)
	OnArchive func()
	OnDelete  func()
}

var sessionRow = kitex.FC("SessionRow", func(props sessionRowProps) kitex.Node {
	c := useColors()
	isEditing, setIsEditing := kitex.UseState(false)
	editValue, setEditValue := kitex.UseState(sessionTitle(props.Session))
	inputRef := kitex.UseRef[dom.Element](nil)
	confirmingArchive, setConfirmingArchive := kitex.UseState(false)
	confirmingDelete, setConfirmingDelete := kitex.UseState(false)
	spinnerFrame, setSpinnerFrame := kitex.UseState(0)

	kitex.UseEffect(func() {
		if isEditing() {
			kitex.PostMacro(func() {
				if inputRef.Current != nil {
					if doc := inputRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(inputRef.Current)
					}
				}
			})
		}
	}, []any{isEditing()})

	interval := 100 * time.Millisecond
	if !props.IsLoading {
		interval = 24 * time.Hour
	}

	kitex.UseInterval(func() {
		if props.IsLoading {
			setSpinnerFrame((spinnerFrame() + 1) % len(spinnerFrames))
		}
	}, interval, []any{props.IsLoading})

	borderColor := c.border
	if props.IsActive {
		borderColor = c.success
	}

	rowStyle := sessionRowStyle.
		Merge(style.S().BorderLeft(true, style.SingleBorder(), borderColor))

	if isEditing() {
		return kitex.Box(kitex.BoxProps{Style: rowStyle},
			components.Input(components.InputProps{
				Ref:     inputRef,
				Variant: components.InputSolid,
				Value:   editValue(),
				Style: style.S().
					Width(style.Percent(100)).
					PaddingHorizontal(0),
				OnChange: func(v string) { setEditValue(v) },
				OnBlur: func() {
					setEditValue(sessionTitle(props.Session))
					setIsEditing(false)
				},
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					switch ke.Code {
					case key.KeyEnter:
						e.PreventDefault()
						e.StopPropagation()
						if props.OnRename != nil {
							props.OnRename(editValue())
						}
						setIsEditing(false)
					case key.KeyEscape:
						e.PreventDefault()
						e.StopPropagation()
						setEditValue(sessionTitle(props.Session))
						setIsEditing(false)
					}
				},
			}),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(c.subtle),
			}, kitex.Text("Enter to save · Esc to cancel")),
		)
	}

	titleColor := c.text
	if props.IsActive {
		titleColor = c.success
	}

	return kitex.Box(kitex.BoxProps{Style: rowStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween),
		},
			// Session title (clickable)
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				Style: style.S().
					Flex(1).
					JustifyContent(style.JustifyStart).
					Foreground(titleColor).
					Bold(props.IsActive).
					PaddingHorizontal(0),
				OnClick: props.OnSelect,
			}, kitex.Text(sessionTitle(props.Session))),

			// Action buttons
			kitex.Box(kitex.BoxProps{Style: sessionRowActionsStyle},
				actionBtn(icon.Pencil, c.muted, c.accent, func() { setIsEditing(true) }),
				actionBtn(icon.Package, c.muted, c.warning, func() { setConfirmingArchive(true) }),
				actionBtn(icon.Error, c.muted, c.error, func() { setConfirmingDelete(true) }),
			),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(c.subtle),
		}, func() kitex.Node {
			if props.IsLoading {
				return kitex.Text(spinnerFrames[spinnerFrame()] + " switching…")
			}
			return kitex.Text(format.RelativeTime(props.Session.UpdatedAt))
		}()),

		// Archive confirmation overlay
		kitex.If(confirmingArchive(), func() kitex.Node {
			return components.ConfirmDialog(components.ConfirmDialogProps{
				Message:      "Archive \"" + sessionTitle(props.Session) + "\"?",
				ConfirmLabel: "Archive",
				ConfirmColor: components.ButtonTertiary,
				OnConfirm: func() {
					setConfirmingArchive(false)
					if props.OnArchive != nil {
						props.OnArchive()
					}
				},
				OnCancel: func() { setConfirmingArchive(false) },
			})
		}),

		// Delete confirmation overlay
		kitex.If(confirmingDelete(), func() kitex.Node {
			return components.ConfirmDialog(components.ConfirmDialogProps{
				Message:      "Delete \"" + sessionTitle(props.Session) + "\"?",
				ConfirmLabel: "Delete",
				ConfirmColor: components.ButtonError,
				OnConfirm: func() {
					setConfirmingDelete(false)
					if props.OnDelete != nil {
						props.OnDelete()
					}
				},
				OnCancel: func() { setConfirmingDelete(false) },
			})
		}),
	)
})

func actionBtn(icn kitex.Node, defaultColor, hoverColor interface {
	RGBA() (uint32, uint32, uint32, uint32)
}, onClick func()) kitex.Node {
	return components.Button(components.ButtonProps{
		Variant: components.ButtonText,
		Color:   components.ButtonBase,
		Style: sessionRowActionBtnStyle.
			Merge(style.S().Foreground(defaultColor)),
		HoverStyle: style.S().Foreground(hoverColor),
		OnClick:    onClick,
	}, icn)
}

func sessionsPanel(
	data Data,
	onSelectSession func(string),
	onCreateSession func(),
	onRenameSession func(id, title string),
	onArchiveSession func(id string),
	onDeleteSession func(id string),
) kitex.Node {
	c := useColors()

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1).
			Padding(1).
			Background(c.panel),
	},
		components.Button(components.ButtonProps{
			Variant:   components.ButtonOutline,
			Color:     components.ButtonInfo,
			StartIcon: icon.Plus,
			Style: style.S().
				Width(style.Percent(100)).
				JustifyContent(style.JustifyCenter),
			OnClick: onCreateSession,
		}, kitex.Text("Add New Session")),

		kitex.If(len(data.Sessions) == 0, func() kitex.Node {
			return emptyState("No sessions yet. Create one to start chatting.")
		}),

		kitex.Map(data.Sessions, func(session api.Session, _ int) kitex.Node {
			isActive := session.ID == data.ActiveSessionID
			return sessionRow(sessionRowProps{
				Session:   session,
				IsActive:  isActive,
				IsLoading: session.ID == data.SwitchingToID,
				OnSelect: func() {
					if onSelectSession != nil {
						onSelectSession(session.ID)
					}
				},
				OnRename: func(title string) {
					if onRenameSession != nil {
						onRenameSession(session.ID, title)
					}
				},
				OnArchive: func() {
					if onArchiveSession != nil {
						onArchiveSession(session.ID)
					}
				},
				OnDelete: func() {
					if onDeleteSession != nil {
						onDeleteSession(session.ID)
					}
				},
			})
		}),

		components.Card(components.CardProps{
			Color:   components.PaperBase,
			Variant: components.CardOutlined,
			Style:   style.S().Background(c.surface),
		},
			components.CardContent(components.CardContentProps{
				Style: style.S().Padding(1).Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.warning).Bold(true)},
					kitex.Text("SESSION POWER OPERATIONS")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)},
					kitex.Text("Swap contexts quickly without leaving the shell.")),
			),
		),
	)
}
