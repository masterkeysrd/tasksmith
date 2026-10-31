package session

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
)

// State stores a session's live execution state and provides the [sync.Mutex]
// used to synchronize access to both [Session] and [State].
type State struct {
	sync.Mutex `json:"-"`

	Status             api.SessionStatus       `json:"status"`
	Running            bool                    `json:"running"`
	PendingPermissions []api.PermissionRequest `json:"pending_permissions,omitempty"`
	PendingQuestions   []api.QuestionRequest   `json:"pending_questions,omitempty"`
	Tasks              []api.Task              `json:"tasks,omitempty"`
	Todos              []api.TodoItem          `json:"todos,omitempty"`
	Context            api.ContextState        `json:"context"`
	Error              string                  `json:"error,omitempty"`
	StartedAt          time.Time               `json:"started_at,omitzero"`
	FinishedAt         time.Time               `json:"finished_at,omitzero"`

	cancel          context.CancelFunc                `json:"-"`
	done            chan struct{}                     `json:"-"`
	err             error                             `json:"-"`
	decisions       map[string]api.PermissionDecision `json:"-"`
	answers         map[string]api.QuestionResponse   `json:"-"`
	queue           []QueueItem                       `json:"-"`
	titleGenerating bool                              `json:"-"`
}

// NewState constructs a new [State] initialized to [api.StatusIdle].
func NewState() *State {
	return &State{
		Status:    api.StatusIdle,
		decisions: make(map[string]api.PermissionDecision),
		answers:   make(map[string]api.QuestionResponse),
	}
}

// Clone returns a copy of the exported state fields with [Status] defaulted
// to [api.StatusIdle] if empty. Caller must hold the state lock if s is shared.
func (s *State) Clone() *State {
	if s == nil {
		return NewState()
	}
	status := s.Status
	if status == "" {
		status = api.StatusIdle
	}
	var perms []api.PermissionRequest
	if len(s.PendingPermissions) > 0 {
		perms = make([]api.PermissionRequest, len(s.PendingPermissions))
		for i, p := range s.PendingPermissions {
			p.GrantPatterns = slices.Clone(p.GrantPatterns)
			perms[i] = p
		}
	}
	var questions []api.QuestionRequest
	for i := range s.PendingQuestions {
		questions = append(questions, *s.PendingQuestions[i].Clone())
	}
	var q []QueueItem
	if len(s.queue) > 0 {
		q = make([]QueueItem, len(s.queue))
		for i, it := range s.queue {
			if it.Message != nil {
				it.Message = message.CloneMessage(it.Message)
			}
			q[i] = it
		}
	}
	return &State{
		Status:             status,
		Running:            s.Running,
		PendingPermissions: perms,
		PendingQuestions:   questions,
		Tasks:              slices.Clone(s.Tasks),
		Todos:              slices.Clone(s.Todos),
		Context:            s.Context,
		Error:              s.Error,
		StartedAt:          s.StartedAt,
		FinishedAt:         s.FinishedAt,
		decisions:          make(map[string]api.PermissionDecision),
		answers:            make(map[string]api.QuestionResponse),
		queue:              q,
	}
}
