package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func (r *Registry) registerSandboxCommands() {
	r.Register(Command{
		Name:        "sandbox",
		Usage:       "/sandbox",
		Description: "Inspect and edit sandbox isolation settings",
		Handler:     handleSandbox,
	})
}

func handleSandbox(args []string) error {
	if len(args) > 0 {
		return errors.New("/sandbox does not accept arguments")
	}
	c := store.Client()
	if c == nil {
		store.AppendNotice("Client unavailable.")
		return nil
	}

	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: SandboxBrowser,
	})
	return nil
}

type sandboxUpdateParams struct {
	scope    api.PermissionScope
	settings api.SandboxSettings
}

// sandboxLockedKnobs derives, per scope, which posture knobs the user cannot
// change from that scope because the opposite scope (or the workspace trust
// floor) already pins them via the monotonic merge rules. A scope never locks
// itself — the scope that wrote a lock must always be able to undo it — and
// the Global scope is never locked, since editing user-wide settings must stay
// possible even when a project or trust posture pins the effective value.
func sandboxLockedKnobs(resp api.SandboxResponse) map[api.PermissionScope]map[components.SandboxPolicyKnob]string {
	locked := map[api.PermissionScope]map[components.SandboxPolicyKnob]string{
		api.ScopeProject: {},
		api.ScopeGlobal:  {},
	}

	if resp.Global.FailIfUnavailable != nil && *resp.Global.FailIfUnavailable {
		locked[api.ScopeProject][components.KnobFailIfUnavailable] = "global scope pins it on"
	}

	globalAU := resp.Global.AllowUnsandboxed
	if resp.Restricted {
		locked[api.ScopeProject][components.KnobAllowUnsandboxed] = "forced off by workspace trust posture"
	} else if globalAU != nil && !*globalAU {
		locked[api.ScopeProject][components.KnobAllowUnsandboxed] = "global scope pins it off"
	}

	return locked
}

// SandboxBrowser renders an interactive sandbox configuration editor inside <Slot />.
var SandboxBrowser = trail.FC("SandboxBrowser", func(slotID string) trail.Node {
	c := store.Client()
	errMsg, setErrMsg := kitex.UseState("")
	saving, setSaving := kitex.UseState(false)

	sandboxQuery := wind.Use("sandbox", promise.WrapWithProps(func(ctx context.Context, _ string) (api.SandboxResponse, error) {
		if c == nil {
			return api.SandboxResponse{}, errors.New("client unavailable")
		}
		resp, err := c.GetSandbox(ctx)
		if err != nil {
			return api.SandboxResponse{}, err
		}
		if resp == nil {
			return api.SandboxResponse{}, nil
		}
		return *resp, nil
	}), wind.Options{Enabled: c != nil})

	updateMutation := wind.UseMutation(func(ctx context.Context, p sandboxUpdateParams) *promise.Promise[any] {
		return promise.Wrap(func(ctx context.Context) (any, error) {
			if c == nil {
				return nil, errors.New("client unavailable")
			}
			_, err := c.UpdateSandbox(ctx, api.UpdateSandboxRequest{Scope: p.scope, Settings: p.settings})
			return nil, err
		})(ctx)
	}, wind.MutationOptions[sandboxUpdateParams, any]{
		OnSuccess: func(_ any, _ sandboxUpdateParams, _ wind.MutationContext) {
			setSaving(false)
			sandboxQuery.Refetch()
			setErrMsg("")
		},
		OnError: func(err error, _ sandboxUpdateParams, _ wind.MutationContext) {
			setSaving(false)
			setErrMsg(err.Error())
		},
	})

	kitex.UseEffect(func() {
		if c == nil {
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		if sandboxQuery.IsLoading {
			return
		}
		if sandboxQuery.IsError {
			store.RemoveSlot(slotID)
			store.AppendError("Error", sandboxQuery.Error.Error())
		}
	}, []any{c == nil, sandboxQuery.IsLoading, sandboxQuery.IsError, sandboxQuery.Error})

	sessionMode := store.UseSession(func(s store.SessionState) api.SandboxMode { return s.SandboxMode })

	if c == nil {
		return nil
	}

	if sandboxQuery.IsLoading {
		return loadingHint("Loading sandbox settings...")
	}
	if sandboxQuery.IsError {
		return nil
	}

	resp := sandboxQuery.Data

	// scopeSettings returns a mutable copy of the settings stored for a scope
	// so one change can be merged in before the whole-scope PUT.
	scopeSettings := func(scope api.PermissionScope) api.SandboxSettings {
		if scope == api.ScopeGlobal {
			return resp.Global.Clone()
		}
		return resp.Project.Clone()
	}

	return components.SandboxConfigEditor(components.SandboxConfigEditorProps{
		Response:     resp,
		SessionMode:  sessionMode,
		LockedKnobs:  sandboxLockedKnobs(resp),
		Saving:       saving(),
		ErrorMessage: errMsg(),
		OnSetMode: func(scope api.PermissionScope, mode api.SandboxMode) {
			settings := scopeSettings(scope)
			settings.Mode = mode
			setSaving(true)
			updateMutation.Mutate(sandboxUpdateParams{scope: scope, settings: settings})
		},
		OnToggleKnob: func(scope api.PermissionScope, knob components.SandboxPolicyKnob, value *bool) {
			settings := scopeSettings(scope)
			switch knob {
			case components.KnobFailIfUnavailable:
				settings.FailIfUnavailable = value
			case components.KnobAllowUnsandboxed:
				settings.AllowUnsandboxed = value
			case components.KnobAutoAllowBash:
				settings.AutoAllowBash = value
			default:
				setSaving(false)
				setErrMsg(fmt.Sprintf("unknown sandbox setting %q", knob))
				return
			}
			setSaving(true)
			updateMutation.Mutate(sandboxUpdateParams{scope: scope, settings: settings})
		},
		OnAddExcluded: func(scope api.PermissionScope, pattern string) {
			settings := scopeSettings(scope)
			settings.ExcludedCommands = append(settings.ExcludedCommands, pattern)
			setSaving(true)
			updateMutation.Mutate(sandboxUpdateParams{scope: scope, settings: settings})
		},
		OnEditExcluded: func(scope api.PermissionScope, oldPattern, newPattern string) {
			settings := scopeSettings(scope)
			edited := make([]string, 0, len(settings.ExcludedCommands))
			replaced := false
			for _, p := range settings.ExcludedCommands {
				if p == oldPattern && !replaced {
					edited = append(edited, newPattern)
					replaced = true
					continue
				}
				if p == newPattern {
					continue
				}
				edited = append(edited, p)
			}
			settings.ExcludedCommands = edited
			setSaving(true)
			updateMutation.Mutate(sandboxUpdateParams{scope: scope, settings: settings})
		},
		OnRemoveExcluded: func(scope api.PermissionScope, pattern string) {
			settings := scopeSettings(scope)
			kept := make([]string, 0, len(settings.ExcludedCommands))
			for _, p := range settings.ExcludedCommands {
				if p != pattern {
					kept = append(kept, p)
				}
			}
			settings.ExcludedCommands = kept
			setSaving(true)
			updateMutation.Mutate(sandboxUpdateParams{scope: scope, settings: settings})
		},
		OnClearError: func() {
			setErrMsg("")
		},
		OnClose: func() {
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/sandbox", "Exited /sandbox command")
		},
	})
})
