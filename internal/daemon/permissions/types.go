package permissions

import (
	"errors"
	"slices"
)

// Sentinel errors returned by the permissions package.
var (
	ErrInvalidConfig = errors.New("permissions: invalid configuration")
	ErrInvalidRule   = errors.New("permissions: invalid rule")
	ErrInvalidScope  = errors.New("permissions: invalid scope")
)

// Scope defines the boundary level for permission rules.
type Scope string

const (
	ScopeSession Scope = "session"
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Effect defines whether a rule permits or forbids matching actions.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Permissions represents sets of allow and deny permission rules.
type Permissions struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Clone returns a deep copy of Permissions.
func (p Permissions) Clone() Permissions {
	return Permissions{
		Allow: slices.Clone(p.Allow),
		Deny:  slices.Clone(p.Deny),
	}
}

// Validate checks that every rule in Allow and Deny conforms to the "tool(pattern)" syntax.
func (p Permissions) Validate() error {
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
