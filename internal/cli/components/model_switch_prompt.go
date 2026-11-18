package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// ModelSwitchPromptProps configures the capability-mismatch confirmation prompt
// shown in <Slot /> when switching models drops active session modalities.
type ModelSwitchPromptProps struct {
	Request  store.ModelSwitchRequest
	OnForce  func()
	OnCancel func()
}

// ModelSwitchPrompt asks the user whether to proceed with a model switch that
// omits incompatible attachments on future turns or keep the current model.
var ModelSwitchPrompt = trail.FC("ModelSwitchPrompt", func(props ModelSwitchPromptProps) trail.Node {
	scheme := theme.UseTheme()

	missing := strings.Join(props.Request.MissingModalities, ", ")
	if missing == "" {
		missing = "media"
	}
	header := trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
	},
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Warning).WhiteSpace(style.WhiteSpacePreWrap),
		}, trail.Text(fmt.Sprintf("%s does not support %s used in this session; those attachments will be omitted on future turns.", props.Request.Model, missing))),
	)

	options := []uicomponents.Option{
		{
			Value:       "force",
			Label:       "Switch anyway",
			Description: "Omit unsupported attachments on future turns",
		},
		{
			Value:       "cancel",
			Label:       "Cancel",
			Description: "Keep the current model",
		},
	}

	return Picker(PickerProps{
		Title:  "Switch model?",
		Header: header,
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "navigate"},
			{Key: "enter", Label: "select"},
			{Key: "esc", Label: "cancel"},
		},
		Options: options,
		OnSelect: func(opt uicomponents.Option) {
			if opt.Value == "force" {
				if props.OnForce != nil {
					props.OnForce()
				}
				return
			}
			if props.OnCancel != nil {
				props.OnCancel()
			}
		},
		OnCancel: props.OnCancel,
	})
})
