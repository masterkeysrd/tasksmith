package components

import (
	"cmp"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
)

type KeyAction struct {
	Keys     []string
	Hint     string
	Label    string
	Disabled bool
	OnPress  func()
}

func UseKeyMap(actions []KeyAction, isActive bool) []KeyBinding {
	trail.UseInput(func(_ string, k event.KeyEvent) {
		for _, a := range actions {
			if !a.Disabled && a.OnPress != nil && len(a.Keys) > 0 && k.MatchString(a.Keys...) {
				a.OnPress()
				return
			}
		}
	}, trail.InputOptions{IsActive: isActive})

	if !isActive {
		return nil
	}
	var bindings []KeyBinding
	for _, a := range actions {
		if a.Disabled || a.Label == "" {
			continue
		}
		var firstKey string
		if len(a.Keys) > 0 {
			firstKey = a.Keys[0]
		}
		if key := cmp.Or(a.Hint, firstKey); key != "" {
			bindings = append(bindings, KeyBinding{Key: key, Label: a.Label})
		}
	}
	return bindings
}
