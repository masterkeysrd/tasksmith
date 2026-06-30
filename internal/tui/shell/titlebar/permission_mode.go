package titlebar

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// PermissionMode renders the active permission mode indicator in the Title Bar.
var PermissionMode = kitex.FC("PermissionMode", func(props struct{}) kitex.Node {
	t := theme.UseTheme()
	activeSessionID := active.UseSessionID()
	sessionState := queries.UseGetSessionState(activeSessionID)

	mode := permissions.ModeDefault
	if sessionState.Data != nil && sessionState.Data.PermissionMode != "" {
		mode = sessionState.Data.PermissionMode
	}

	var modeIcon kitex.Node
	var modeLabel string
	var fgColor color.Color

	// Determine style based on permission mode
	switch mode {
	case permissions.ModeAuto:
		modeIcon = icon.Robot
		modeLabel = "Auto"
		fgColor = t.Color.Text.Magenta
		if fgColor == nil {
			fgColor = t.Color.Text.Purple
		}
	case permissions.ModeStrict:
		modeIcon = icon.Lock
		modeLabel = "Strict"
		if c, ok := t.Palette["cyan"]; ok {
			fgColor = c
		} else if c, ok := t.Palette["blue"]; ok {
			fgColor = c
		} else {
			fgColor = t.Color.Text.Primary
		}
	case permissions.ModeDefault:
		fallthrough
	default:
		modeIcon = icon.Shield
		modeLabel = "Default"
		fgColor = t.Color.Text.Secondary
	}

	modeStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(2).
		Foreground(fgColor)

	return kitex.Box(kitex.BoxProps{Style: modeStyle},
		kitex.Text("["),
		modeIcon,
		kitex.Text(" "+modeLabel+"]"),
	)
})
