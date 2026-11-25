package components

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// SlotComponent is a functional slot node generator parameterized by slotID.
type SlotComponent func(slotID string) trail.Node

// Slot is the host component that renders the currently active slot item.
var Slot = trail.SimpleFC("Slot", func() trail.Node {
	active := store.UseActiveSlot()
	mode := store.UseMode()
	if active == nil {
		return nil
	}

	var child trail.Node
	switch active.Kind {
	case store.SlotKindPermission:
		if p, ok := active.Data.(PermissionPromptProps); ok {
			child = PermissionPrompt(p)
		} else if req, ok := active.Data.(api.PermissionRequest); ok {
			props := PermissionPromptProps{
				Request:    req,
				OnDecision: store.DecidePermission,
			}
			if req.Action == api.ActionWrite && req.Safety != api.SafetyUnsafe && !req.BypassSandbox &&
				mode != api.ModeAcceptEdits && mode != api.ModeAuto {
				props.OnAcceptEdits = store.ApprovePermissionAndAcceptEdits
			}
			child = PermissionPrompt(props)
		}
	case store.SlotKindQuestion:
		if props, ok := active.Data.(QuestionPromptProps); ok {
			child = QuestionPrompt(props)
		} else if req, ok := active.Data.(api.QuestionRequest); ok {
			child = QuestionPrompt(QuestionPromptProps{Request: req, OnSubmit: store.SubmitQuestion})
		}
	case store.SlotKindPlanReview:
		if props, ok := active.Data.(PlanReviewPromptProps); ok {
			child = PlanReviewPrompt(props)
		} else if req, ok := active.Data.(api.PlanReviewRequest); ok {
			child = PlanReviewPrompt(PlanReviewPromptProps{Request: req, OnSubmit: store.SubmitPlanReview})
		}
	case store.SlotKindPicker:
		switch v := active.Data.(type) {
		case PickerProps:
			origSelect := v.OnSelect
			v.OnSelect = func(opt uicomponents.Option) {
				store.RemoveSlot(active.ID)
				if origSelect != nil {
					origSelect(opt)
				}
			}
			origCancel := v.OnCancel
			v.OnCancel = func() {
				store.RemoveSlot(active.ID)
				if origCancel != nil {
					origCancel()
				}
			}
			child = Picker(v)
		case SlotComponent:
			child = v(active.ID)
		case func(slotID string) trail.Node:
			child = v(active.ID)
		case trail.Node:
			child = v
		}
	case store.SlotKindAutocomplete:
		// Placeholder until Autocomplete component is added.
		child = nil
	case store.SlotKindModelSwitch:
		if req, ok := active.Data.(store.ModelSwitchRequest); ok {
			child = ModelSwitchPrompt(ModelSwitchPromptProps{
				Request: req,
				OnForce: func() {
					store.RemoveSlot(active.ID)
					store.ForceSetModel(req.Model)
				},
				OnCancel: func() {
					store.RemoveSlot(active.ID)
					store.AppendCommandOutput("/model "+req.Model, "Canceled model switch")
				},
			})
		}
	case store.SlotKindProviderReauth:
		if req, ok := active.Data.(store.ProviderReauthRequest); ok {
			child = ProviderReauthPrompt(ProviderReauthPromptProps{
				Request: req,
				OnSignIn: func() {
					store.RemoveSlot(active.ID)
					store.LaunchProviderAuth(req.Provider)
				},
				OnDismiss: func() {
					store.RemoveSlot(active.ID)
				},
			})
		}
	default:
		return nil
	}

	if child == nil {
		return nil
	}

	return trail.Box(trail.BoxProps{
		Key: active.ID,
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, child)
})
