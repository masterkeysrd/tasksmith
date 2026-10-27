package session

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// State stores a session's live execution state and provides the [sync.Mutex]
// used to synchronize access to both [Session] and [State].
type State struct {
	sync.Mutex `json:"-"`

	Status             Status                    `json:"status"`
	Running            bool                      `json:"running"`
	WaitingPermission  bool                      `json:"waiting_permission"`
	PendingPermissions []tools.PermissionRequest `json:"pending_permissions,omitempty"`
	Tasks              []tasks.Snapshot          `json:"tasks,omitempty"`
	Error              string                    `json:"error,omitempty"`
	StartedAt          time.Time                 `json:"started_at,omitzero"`
	FinishedAt         time.Time                 `json:"finished_at,omitzero"`

	cancel          context.CancelFunc                  `json:"-"`
	done            chan struct{}                       `json:"-"`
	err             error                               `json:"-"`
	decisions       map[string]tools.PermissionDecision `json:"-"`
	queue           []QueueItem                         `json:"-"`
	titleGenerating bool                                `json:"-"`
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
	var perms []tools.PermissionRequest
	if len(s.PendingPermissions) > 0 {
		perms = make([]tools.PermissionRequest, len(s.PendingPermissions))
		for i, p := range s.PendingPermissions {
			p.GrantPatterns = slices.Clone(p.GrantPatterns)
			perms[i] = p
		}
	}
	var q []QueueItem
	if len(s.queue) > 0 {
		q = make([]QueueItem, len(s.queue))
		for i, it := range s.queue {
			switch p := it.Payload.(type) {
			case QueueMessagePayload:
				it.Payload = QueueMessagePayload{Message: message.CloneMessage(p.Message)}
			case QueueTaskPayload:
				it.Payload = QueueTaskPayload{Message: message.CloneMessage(p.Message)}
			}
			q[i] = it
		}
	}
	return &State{
		Status:             status,
		Running:            s.Running,
		WaitingPermission:  s.WaitingPermission,
		PendingPermissions: perms,
		Tasks:              slices.Clone(s.Tasks),
		Error:              s.Error,
		StartedAt:          s.StartedAt,
		FinishedAt:         s.FinishedAt,
		decisions:          make(map[string]tools.PermissionDecision),
		queue:              q,
	}
}
