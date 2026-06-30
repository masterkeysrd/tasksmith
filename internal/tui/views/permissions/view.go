package permissionsview

import (
	"context"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

type ViewProps struct{}

// View is the Permission Picker modal component.
var View = kitex.FC("PermissionPickerView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "permissionpicker"
	if !isOpen {
		return nil
	}

	activeSessionID := active.UseSessionID()
	client := tuiapi.UseClient()
	windClient := wind.UseClient()

	groups := []components.PickerGroup{
		{
			Name: "Permission Modes",
			Items: []components.PickerItem{
				{
					ID:       string(permissions.ModeAuto),
					Label:    "󰚩 Auto",
					Sublabel: "Auto-approve all tool calls unless dangerous",
					Value:    permissions.ModeAuto,
				},
				{
					ID:       string(permissions.ModeDefault),
					Label:    "󰒃 Default",
					Sublabel: "Follow user rules, prompt otherwise",
					Value:    permissions.ModeDefault,
				},
				{
					ID:       string(permissions.ModeStrict),
					Label:    "󰌆 Strict",
					Sublabel: "Always prompt for sensitive tools, ignore broad grants",
					Value:    permissions.ModeStrict,
				},
			},
		},
	}

	onSelect := func(item components.PickerItem) {
		mode, ok := item.Value.(permissions.PermissionMode)
		if !ok {
			return
		}

		promise.New(func(ctx context.Context) (any, error) {
			_, err := client.SetPermissionMode(ctx, api.SetPermissionModeRequest{
				SessionID: activeSessionID,
				Mode:      mode,
				Scope:     permissions.ScopeSession, // TUI changes are session-scoped by default
			})
			return nil, err
		}).Then(func(any) {
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: activeSessionID})
			active.SetModal("")
		}, func(err error) {
			// handled by toastClient
		})
	}

	onClose := func() {
		active.SetModal("")
	}

	pickerStyle := style.S().
		MaxWidth(style.Cells(70)).
		Height(style.Cells(15))

	return components.Picker(components.PickerProps{
		IsOpen:        true,
		Title:         "SWITCH PERMISSION MODE",
		Groups:        groups,
		Style:         pickerStyle,
		DisableSearch: true,
		OnSelect:      onSelect,
		OnClose:       onClose,
	})
})
