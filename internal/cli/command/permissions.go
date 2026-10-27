package command

import (
	"context"
	"errors"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func (r *Registry) registerPermissionCommands() {
	r.Register(Command{
		Name:        "permissions",
		Aliases:     []string{"perm", "perms"},
		Usage:       "/permissions",
		Description: "View and edit project and global permission rules",
		Handler:     handlePermissions,
	})
}

func handlePermissions(_ []string) error {
	c := store.Client()
	if c == nil {
		store.AppendNotice("Client unavailable.")
		return nil
	}

	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: PermissionBrowser,
	})
	return nil
}

type editRuleParams struct {
	scope   api.PermissionScope
	effect  api.PermissionEffect
	oldRule string
	newRule string
}

// PermissionBrowser renders an interactive permission configuration editor inside <Slot />.
var PermissionBrowser = trail.FC("PermissionBrowser", func(slotID string) trail.Node {
	c := store.Client()
	errMsg, setErrMsg := kitex.UseState("")

	permsQuery := wind.Use("permissions", promise.WrapWithProps(func(ctx context.Context, _ string) (api.PermissionsResponse, error) {
		if c == nil {
			return api.PermissionsResponse{}, errors.New("client unavailable")
		}
		resp, err := c.Permissions(ctx)
		if err != nil {
			return api.PermissionsResponse{}, err
		}
		if resp == nil {
			return api.PermissionsResponse{}, nil
		}
		return *resp, nil
	}), wind.Options{Enabled: c != nil})

	addMutation := wind.UseMutation(func(ctx context.Context, req api.PermissionRuleRequest) *promise.Promise[any] {
		return promise.Wrap(func(ctx context.Context) (any, error) {
			if c == nil {
				return nil, errors.New("client unavailable")
			}
			_, err := c.AddPermissionRule(ctx, req)
			return nil, err
		})(ctx)
	}, wind.MutationOptions[api.PermissionRuleRequest, any]{
		OnSuccess: func(_ any, _ api.PermissionRuleRequest, _ wind.MutationContext) {
			permsQuery.Refetch()
			setErrMsg("")
		},
		OnError: func(err error, _ api.PermissionRuleRequest, _ wind.MutationContext) {
			setErrMsg(err.Error())
		},
	})

	removeMutation := wind.UseMutation(func(ctx context.Context, req api.PermissionRuleRequest) *promise.Promise[any] {
		return promise.Wrap(func(ctx context.Context) (any, error) {
			if c == nil {
				return nil, errors.New("client unavailable")
			}
			_, err := c.RemovePermissionRule(ctx, req)
			return nil, err
		})(ctx)
	}, wind.MutationOptions[api.PermissionRuleRequest, any]{
		OnSuccess: func(_ any, _ api.PermissionRuleRequest, _ wind.MutationContext) {
			permsQuery.Refetch()
			setErrMsg("")
		},
		OnError: func(err error, _ api.PermissionRuleRequest, _ wind.MutationContext) {
			setErrMsg(err.Error())
		},
	})

	editMutation := wind.UseMutation(func(ctx context.Context, p editRuleParams) *promise.Promise[any] {
		return promise.Wrap(func(ctx context.Context) (any, error) {
			if c == nil {
				return nil, errors.New("client unavailable")
			}
			addReq := api.PermissionRuleRequest{
				Scope:  p.scope,
				Effect: p.effect,
				Rule:   p.newRule,
			}
			if _, err := c.AddPermissionRule(ctx, addReq); err != nil {
				return nil, err
			}
			removeOldReq := api.PermissionRuleRequest{
				Scope:  p.scope,
				Effect: p.effect,
				Rule:   p.oldRule,
			}
			if _, err := c.RemovePermissionRule(ctx, removeOldReq); err != nil {
				return nil, err
			}
			return nil, nil
		})(ctx)
	}, wind.MutationOptions[editRuleParams, any]{
		OnSuccess: func(_ any, _ editRuleParams, _ wind.MutationContext) {
			permsQuery.Refetch()
			setErrMsg("")
		},
		OnError: func(err error, _ editRuleParams, _ wind.MutationContext) {
			setErrMsg(err.Error())
		},
	})

	kitex.UseEffect(func() {
		if c == nil {
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		if permsQuery.IsLoading {
			return
		}
		if permsQuery.IsError {
			store.RemoveSlot(slotID)
			store.AppendError("Error", permsQuery.Error.Error())
		}
	}, []any{c == nil, permsQuery.IsLoading, permsQuery.IsError, permsQuery.Error})

	if c == nil {
		return nil
	}

	if permsQuery.IsLoading {
		return loadingHint("Loading permissions...")
	}
	if permsQuery.IsError {
		return nil
	}

	return components.PermissionConfigEditor(components.PermissionConfigEditorProps{
		Permissions:  permsQuery.Data,
		ErrorMessage: errMsg(),
		OnAddRule: func(scope api.PermissionScope, effect api.PermissionEffect, rule string) {
			addMutation.Mutate(api.PermissionRuleRequest{
				Scope:  scope,
				Effect: effect,
				Rule:   rule,
			})
		},
		OnEditRule: func(scope api.PermissionScope, effect api.PermissionEffect, oldRule, newRule string) {
			editMutation.Mutate(editRuleParams{
				scope:   scope,
				effect:  effect,
				oldRule: oldRule,
				newRule: newRule,
			})
		},
		OnRemoveRule: func(scope api.PermissionScope, effect api.PermissionEffect, rule string) {
			removeMutation.Mutate(api.PermissionRuleRequest{
				Scope:  scope,
				Effect: effect,
				Rule:   rule,
			})
		},
		OnClearError: func() {
			setErrMsg("")
		},
		OnClose: func() {
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/permissions", "Exited /permissions command")
		},
	})
})
