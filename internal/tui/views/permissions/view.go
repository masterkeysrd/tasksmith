package permissionsview

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
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

type ManagerViewProps struct{}

// ManagerView is the Permission Manager modal component showing all saved permissions.
var ManagerView = kitex.FC("PermissionsManagerView", func(props ManagerViewProps) kitex.Node {
	isOpen := active.UseModal() == "permissionsmanager"
	if !isOpen {
		return nil
	}

	activeSessionID := active.UseSessionID()
	client := tuiapi.UseClient()
	windClient := wind.UseClient()

	// Query all permissions
	permsQuery := queries.UseGetPermissions(activeSessionID)

	var groups []components.PickerGroup

	if permsQuery.Data != nil && permsQuery.Data.Permissions != nil {
		scopeNames := []struct {
			scope string
			label string
		}{
			{string(permissions.ScopeSession), "Session Permissions (Temporary)"},
			{string(permissions.ScopeWorkspace), "Workspace Permissions (Saved to workspace config)"},
			{string(permissions.ScopeGlobal), "Global Permissions (Saved to global config)"},
		}

		for _, sn := range scopeNames {
			permsList := permsQuery.Data.Permissions[sn.scope]
			if len(permsList) == 0 {
				continue
			}

			var items []components.PickerItem
			for idx, perm := range permsList {
				methodLabel := ""
				if perm.MatchMethod != "exact" && perm.MatchMethod != "" {
					methodLabel = " (" + perm.MatchMethod + ")"
				}

				dirLabel := ""
				if perm.AllowedDirectory != "" && perm.AllowedDirectory != "*" {
					dirLabel = " in " + perm.AllowedDirectory
				}

				actionLabel := "󰗡 ALLOW"
				if perm.Action == permissions.ActionDeny {
					actionLabel = "󰅙 DENY"
				}

				label := fmt.Sprintf("%s  %s: %s%s%s", actionLabel, perm.Group, perm.Target, methodLabel, dirLabel)
				sublabel := fmt.Sprintf("Group: %s | Match: %s | Dir: %s", perm.Group, perm.MatchMethod, perm.AllowedDirectory)

				items = append(items, components.PickerItem{
					ID:       fmt.Sprintf("%s:%d", sn.scope, idx),
					Label:    label,
					Sublabel: sublabel,
					Value: struct {
						Scope      permissions.PermissionScope
						Permission permissions.Permission
					}{
						Scope:      permissions.PermissionScope(sn.scope),
						Permission: perm,
					},
				})
			}

			if len(items) > 0 {
				groups = append(groups, components.PickerGroup{
					Name:  sn.label,
					Items: items,
				})
			}
		}
	}

	if len(groups) == 0 {
		groups = []components.PickerGroup{
			{
				Name: "No Saved Permissions",
				Items: []components.PickerItem{
					{
						ID:       "empty",
						Label:    "No active permissions found",
						Sublabel: "Permissions granted during session/workspace runs will appear here",
						Disabled: true,
					},
				},
			},
		}
	}

	onClose := func() {
		active.SetModal("")
	}

	revokeFn := func(item components.PickerItem) {
		if item.ID == "empty" || item.Value == nil {
			return
		}
		val, ok := item.Value.(struct {
			Scope      permissions.PermissionScope
			Permission permissions.Permission
		})
		if !ok {
			return
		}

		promise.New(func(ctx context.Context) (any, error) {
			_, err := client.DeletePermission(ctx, api.DeletePermissionRequest{
				SessionID:  activeSessionID,
				Scope:      val.Scope,
				Permission: val.Permission,
			})
			return nil, err
		}).Then(func(any) {
			windClient.InvalidateQueries(api.GetPermissionsRequest{SessionID: activeSessionID})
		}, func(err error) {
			// handled by toastClient
		})
	}

	actions := []components.PickerAction{
		{
			Label: "REVOKE",
			Key:   "<C-d>",
			Fn:    revokeFn,
		},
		{
			Label: "REVOKE",
			Key:   "<C-x>",
			Fn:    revokeFn,
		},
	}

	pickerStyle := style.S().
		MaxWidth(style.Cells(120)).
		Height(style.Cells(25))

	return components.Picker(components.PickerProps{
		IsOpen:        true,
		Title:         "MANAGE PERMISSIONS",
		Groups:        groups,
		Style:         pickerStyle,
		DisableSearch: false,
		Placeholder:   "Search permissions by tool, target, match type...",
		Actions:       actions,
		OnClose:       onClose,
	})
})
