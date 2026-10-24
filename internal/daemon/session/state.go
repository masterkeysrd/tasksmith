package session

import (
	"context"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// State stores a session's live execution state and provides the [sync.Mutex]
// used to synchronize access to both [Session] and [State].
type State struct {
	sync.Mutex `json:"-"`

	Status            Status    `json:"status"`
	Running           bool      `json:"running"`
	WaitingPermission bool      `json:"waiting_permission"`
	Error             string    `json:"error,omitempty"`
	StartedAt         time.Time `json:"started_at,omitzero"`
	FinishedAt        time.Time `json:"finished_at,omitzero"`

	cancel    context.CancelFunc                  `json:"-"`
	done      chan struct{}                       `json:"-"`
	err       error                               `json:"-"`
	decisions map[string]tools.PermissionDecision `json:"-"`
}

// NewState constructs a new [State] initialized to [StatusIdle].
func NewState() *State {
	return &State{
		Status:    StatusIdle,
		decisions: make(map[string]tools.PermissionDecision),
	}
}

// Clone returns a copy of the exported state fields with [Status] defaulted
// to [StatusIdle] if empty. Caller must hold the state lock if s is shared.
func (s *State) Clone() *State {
	if s == nil {
		return NewState()
	}
	status := s.Status
	if status == "" {
		status = StatusIdle
	}
	return &State{
		Status:            status,
		Running:           s.Running,
		WaitingPermission: s.WaitingPermission,
		Error:             s.Error,
		StartedAt:         s.StartedAt,
		FinishedAt:        s.FinishedAt,
		decisions:         make(map[string]tools.PermissionDecision),
	}
}
