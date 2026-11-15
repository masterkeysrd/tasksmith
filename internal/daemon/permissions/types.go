package permissions

import (
	"errors"

	"github.com/masterkeysrd/tasksmith/api"
)

// Sentinel errors returned by the permissions package.
var (
	ErrInvalidConfig = errors.New("permissions: invalid configuration")
	ErrInvalidRule   = errors.New("permissions: invalid rule")
	ErrInvalidScope  = errors.New("permissions: invalid scope")
)

// ValidatePermissions checks that every rule in Allow and Deny conforms to the "tool(pattern)" syntax.
func ValidatePermissions(p api.Permissions) error {
	for _, r := range p.Allow {
		if _, _, err := ParseRule(r); err != nil {
			return err
		}
	}
	for _, r := range p.Deny {
		if _, _, err := ParseRule(r); err != nil {
			return err
		}
	}
	return nil
}
