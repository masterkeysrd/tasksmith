package session

import (
	"errors"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
)

// Sentinel errors returned by the session package.
var (
	ErrInvalidConfig        = errors.New("session: invalid configuration")
	ErrSessionNotFound      = errors.New("session: session not found")
	ErrEmptySessionID       = errors.New("session: session ID must not be empty")
	ErrNilMessage           = errors.New("session: message must not be nil")
	ErrJournalNotFound      = journal.ErrNotFound
	ErrEntryNotFound        = journal.ErrEntryNotFound
	ErrBlobNotFound         = journal.ErrBlobNotFound
	ErrRevertConflict       = journal.ErrRevertConflict
	ErrNoPendingPermissions = errors.New("session: no pending permissions")
	ErrPermissionNotFound   = errors.New("session: permission request not found")
	ErrInvalidDecision      = errors.New("session: invalid permission decision")
	ErrNoPendingQuestions   = errors.New("session: no pending questions")
	ErrQuestionNotFound     = errors.New("session: question request not found")
	ErrInvalidQuestion      = errors.New("session: invalid question response")
	ErrInvalidSessionID     = errors.New("session: session ID must be a canonical lowercase UUIDv7")
	ErrSessionConflict      = errors.New("session: session already exists with different parameters")
	ErrSessionRunning       = errors.New("session: session already has an active turn")
)

// SendInput provides the input to Session.Send, carrying either a new user message or a permission decision.
type SendInput struct {
	ID       string
	Message  message.Message
	Command  string
	Decision *api.PermissionDecision
	Question *api.QuestionResponse
	Compact  *api.CompactRequest
}

type QueueKind string

const (
	QueueKindMessage QueueKind = "message"
	QueueKindCommand QueueKind = "command"
	QueueKindTask    QueueKind = "task"
)

type QueueItem struct {
	ID        string
	Kind      QueueKind
	Message   message.Message
	Command   string
	CreatedAt time.Time
}

// Publisher broadcasts real-time session events to a topic.
type Publisher interface {
	Publish(topic string, event broker.Event)
	DeleteTopic(topic string)
}
