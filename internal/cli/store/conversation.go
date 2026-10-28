package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
)

// Message preserves the order of its parts. ID is a stable view identity;
// SourceID is the persisted message ID.
type Message struct {
	ID        string
	SourceID  string
	Role      api.Role
	Parts     []MessagePart
	CreatedAt time.Time
	Streaming bool
}

type MessagePartType string

const (
	PartThought MessagePartType = "thought"
	PartText    MessagePartType = "text"
	PartTool    MessagePartType = "tool"
)

// MessagePart is a tagged union. Only the field identified by Type is set.
type MessagePart struct {
	ID      string
	Type    MessagePartType
	Thought *ThoughtPart
	Text    *TextPart
	Tool    *ToolPart
}

type ThoughtPart struct {
	Text      string
	Signature string
	Streaming bool
}

type TextPart struct {
	Text string
}

type ToolStatus string

const (
	ToolReceiving         ToolStatus = "receiving"
	ToolPending           ToolStatus = "pending"
	ToolRunning           ToolStatus = "running"
	ToolWaitingPermission ToolStatus = "waiting_permission"
	ToolSucceeded         ToolStatus = "succeeded"
	ToolFailed            ToolStatus = "failed"
	ToolInterrupted       ToolStatus = "interrupted"
)

// ToolPart joins a tool call and its response by ID.
type ToolPart struct {
	ID          string
	Call        *api.ToolCall
	Response    *api.Message
	Status      ToolStatus
	Permissions []api.PermissionRequest
	Question    *api.QuestionRecord
}

type StreamStatus string

const (
	StreamIdle              StreamStatus = "idle"
	StreamRunning           StreamStatus = "running"
	StreamWaitingPermission StreamStatus = "waiting_permission"
	StreamWaitingQuestion   StreamStatus = "waiting_question"
	StreamCompleted         StreamStatus = "completed"
	StreamFailed            StreamStatus = "failed"
)

// StreamState holds the active stream of in-flight messages and execution status.
type StreamState struct {
	Messages          *[]Message
	Streaming         bool
	WaitingPermission bool
	WaitingQuestion   bool
	Status            api.SessionStatus
	PendingCommand    string
	Queue             *[]api.QueueItem
	Todos             *[]api.TodoItem
}

var stream = kites.Create(StreamState{})

// UseStream subscribes a component to a comparable slice of StreamState.
func UseStream[U comparable](selector func(StreamState) U) U {
	return kites.Use(stream, selector)
}

// GetStream returns the current StreamState snapshot.
func GetStream() StreamState {
	return stream.Get()
}

// UseIsStreaming reports whether an LLM turn is currently streaming.
func UseIsStreaming() bool {
	return UseStream(func(s StreamState) bool {
		return s.Streaming
	})
}

// ResetStream resets StreamState to its initial empty state.
func ResetStream() {
	stream.Set(func(StreamState) StreamState {
		return StreamState{}
	})
	globalConversation.reset()
}

// ApplyEvent applies an API SSE event to the active conversation stream.
func ApplyEvent(event api.Event) {
	globalConversation.applyEvent(event)
}

type conversationMessage struct {
	id      string
	message api.Message
	draft   bool
	done    bool
}

// conversationAccumulator folds TimelineEntries and SSE events into view messages,
// splitting them into a finalized prefix flushed to Transcript and a mutable suffix in StreamState.
type conversationAccumulator struct {
	mu                sync.Mutex
	sessionID         string
	messages          []conversationMessage
	permissions       []api.PermissionRequest
	questions         []api.QuestionRequest
	resolvedQuestions map[string]bool
	questionRecords   map[string]api.QuestionRecord
	status            StreamStatus
	sessionStatus     api.SessionStatus
	err               error
	draft             int
	pendingUser       *Message
	pendingCommand    string
	pendingCommandID  string
	queue             []api.QueueItem
	todos             []api.TodoItem
	flushedIDs        map[string]bool
	identities        map[string]string
	nextDraft         uint64
}

var globalConversation = newConversationAccumulator()

func newConversationAccumulator() *conversationAccumulator {
	return &conversationAccumulator{
		draft:         -1,
		status:        StreamIdle,
		sessionStatus: api.StatusIdle,
		flushedIDs:    make(map[string]bool),
		identities:    make(map[string]string),
	}
}

func permSlotID(reqID string) string {
	return "perm-" + reqID
}

func questionSlotID(reqID string) string {
	return "question-" + reqID
}

func (acc *conversationAccumulator) clearQuestionsLocked() {
	for _, req := range acc.questions {
		RemoveSlot(questionSlotID(req.ID))
	}
	acc.questions = nil
}

func (acc *conversationAccumulator) clearPermissionsLocked() {
	for _, req := range acc.permissions {
		RemoveSlot(permSlotID(req.ID))
	}
	acc.permissions = nil
}

func (acc *conversationAccumulator) reset() {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.sessionID = ""
	acc.messages = nil
	acc.clearPermissionsLocked()
	acc.clearQuestionsLocked()
	acc.resolvedQuestions = nil
	acc.questionRecords = nil
	acc.status = StreamIdle
	acc.sessionStatus = api.StatusIdle
	acc.err = nil
	acc.draft = -1
	acc.pendingUser = nil
	acc.pendingCommand = ""
	acc.pendingCommandID = ""
	acc.queue = nil
	acc.todos = nil
	acc.flushedIDs = make(map[string]bool)
	acc.identities = make(map[string]string)
	acc.nextDraft = 0
}

func (acc *conversationAccumulator) setSessionID(id string) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.sessionID = id
	acc.messages = nil
	acc.clearPermissionsLocked()
	acc.clearQuestionsLocked()
	acc.resolvedQuestions = nil
	acc.questionRecords = nil
	acc.status = StreamIdle
	acc.sessionStatus = api.StatusIdle
	acc.err = nil
	acc.draft = -1
	acc.pendingUser = nil
	acc.pendingCommand = ""
	acc.pendingCommandID = ""
	acc.queue = nil
	acc.todos = nil
	acc.flushedIDs = make(map[string]bool)
	acc.identities = make(map[string]string)
	acc.nextDraft = 0
}

func (acc *conversationAccumulator) messageID(sourceID, draftID string) string {
	if sourceID == "" {
		acc.nextDraft++
		return "stream:" + acc.sessionID + ":" + strconv.FormatUint(acc.nextDraft, 10)
	}
	if id, ok := acc.identities[sourceID]; ok {
		return id
	}
	if draftID == "" {
		draftID = sourceID
	}
	acc.identities[sourceID] = draftID
	return draftID
}

func (acc *conversationAccumulator) addPendingUser(id, text string) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if acc.status == StreamRunning || acc.status == StreamWaitingPermission || acc.status == StreamWaitingQuestion {
		if !slices.ContainsFunc(acc.queue, func(item api.QueueItem) bool { return item.ID == id }) {
			acc.queue = append(acc.queue, api.QueueItem{
				ID:   id,
				Kind: api.QueueKindMessage,
				Payload: api.QueueMessagePayload{
					Message: api.Message{
						ID:        id,
						Role:      api.RoleUser,
						CreatedAt: time.Now().UTC(),
						Content: []api.Block{
							{
								Type: api.BlockText,
								Text: text,
							},
						},
					},
				},
				CreatedAt: time.Now().UTC(),
			})
		}
		acc.publishLocked()
		return
	}
	acc.status = StreamRunning
	acc.sessionStatus = api.StatusThinking
	acc.pendingUser = &Message{
		ID:        id,
		SourceID:  id,
		Role:      api.RoleUser,
		CreatedAt: time.Now(),
		Parts: []MessagePart{
			{
				ID:   id + ":0",
				Type: PartText,
				Text: &TextPart{Text: text},
			},
		},
	}
	acc.publishLocked()
}

func (acc *conversationAccumulator) addPendingCommand(id, cmd string) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if acc.status == StreamRunning || acc.status == StreamWaitingPermission || acc.status == StreamWaitingQuestion {
		if !slices.ContainsFunc(acc.queue, func(item api.QueueItem) bool { return item.ID == id }) {
			acc.queue = append(acc.queue, api.QueueItem{
				ID:   id,
				Kind: api.QueueKindCommand,
				Payload: api.QueueCommandPayload{
					Command: cmd,
				},
				CreatedAt: time.Now().UTC(),
			})
		}
		acc.publishLocked()
		return
	}
	acc.status = StreamRunning
	acc.sessionStatus = api.StatusExecuting
	acc.pendingCommandID = id
	acc.pendingCommand = cmd
	acc.publishLocked()
}

func (acc *conversationAccumulator) cancelPendingUser(id string, err error) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	wasPending := false
	if acc.pendingUser != nil && acc.pendingUser.ID == id {
		acc.pendingUser = nil
		wasPending = true
	}
	if acc.pendingCommandID == id {
		acc.pendingCommand = ""
		acc.pendingCommandID = ""
		wasPending = true
	}
	acc.queue = slices.DeleteFunc(acc.queue, func(item api.QueueItem) bool {
		return item.ID == id
	})
	if wasPending {
		acc.status = StreamFailed
		acc.sessionStatus = api.StatusIdle
		acc.err = err
	}
	acc.publishLocked()
	AppendError("Error", err.Error())
}

func (acc *conversationAccumulator) cancelTurn() {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.status = StreamIdle
	acc.sessionStatus = api.StatusIdle
	acc.err = nil
	acc.pendingUser = nil
	acc.pendingCommand = ""
	acc.pendingCommandID = ""
	acc.queue = nil
	acc.clearPermissionsLocked()
	acc.clearQuestionsLocked()
	acc.discardDraftLocked()
	acc.publishLocked()
}

// CancelTurn resets the active turn stream state to idle and discards drafts.
func CancelTurn() {
	globalConversation.cancelTurn()
}

func (acc *conversationAccumulator) applyEvent(event api.Event) {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	switch event.Type {
	case api.EventConnected:
		return

	case api.EventCompactionStarted:
		p, ok := event.Payload.(api.CompactionStartedEvent)
		if ok && acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.status = StreamRunning
		acc.sessionStatus = api.StatusCompacting
		acc.publishLocked()

	case api.EventContextUpdated:
		p, ok := event.Payload.(api.ContextUpdatedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		session.Set(func(s SessionState) SessionState {
			s.Context = p.Context
			if s.Session != nil {
				s.Session.Context = p.Context
			}
			return s
		})

	case api.EventStatusChanged:
		p, ok := event.Payload.(api.StatusChangedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.sessionStatus = p.Status
		switch p.Status {
		case api.StatusWaitingPermission:
			acc.status = StreamWaitingPermission
		case api.StatusWaitingQuestion:
			acc.status = StreamWaitingQuestion
		case api.StatusIdle:
			acc.status = StreamIdle
		default:
			acc.status = StreamRunning
		}
		acc.publishLocked()

	case api.EventEntryAdded:
		p, ok := event.Payload.(api.EntryAddedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}

		if p.Entry.Kind != api.TimelineKindMessage {
			if p.Entry.Kind == api.TimelineKindCommand {
				acc.pendingCommand = ""
				acc.pendingCommandID = ""
				acc.publishLocked()
				AppendTimelineEntry(p.Entry)
				return
			}
			if p.Entry.Kind == api.TimelineKindInterrupted {
				acc.status = StreamIdle
				acc.sessionStatus = api.StatusIdle
				acc.err = nil
				acc.pendingUser = nil
				acc.pendingCommand = ""
				acc.pendingCommandID = ""
				acc.clearPermissionsLocked()
				acc.clearQuestionsLocked()
				acc.discardDraftLocked()
				acc.publishLocked()
				AppendTimelineEntry(p.Entry)
				return
			}
			if p.Entry.Kind == api.TimelineKindQuestion && p.Entry.Question != nil {
				acc.recordQuestionLocked(*p.Entry.Question)
				acc.removeQuestionLocked(p.Entry.Question.Request.ID)
				acc.publishLocked()
				return
			}
			if p.Entry.Kind == api.TimelineKindPermission {
				if p.Entry.Permission != nil {
					reqID := p.Entry.Permission.Request.ID
					acc.permissions = slices.DeleteFunc(acc.permissions, func(r api.PermissionRequest) bool {
						return r.ID == reqID
					})
					RemoveSlot(permSlotID(reqID))
					if len(acc.permissions) == 0 && acc.status == StreamWaitingPermission {
						if len(acc.questions) > 0 {
							acc.status, acc.sessionStatus = StreamWaitingQuestion, api.StatusWaitingQuestion
						} else {
							acc.status = StreamRunning
						}
					}
					acc.publishLocked()
				}
				return
			}

			// Non-message timeline entry: flush to Transcript
			AppendTimelineEntry(p.Entry)
			switch p.Entry.Kind {
			case api.TimelineKindMode:
				if p.Entry.Mode != nil {
					SetSessionMode(p.Entry.Mode.To)
				}
			case api.TimelineKindModel:
				if p.Entry.Model != nil {
					SetModelInfo(p.Entry.Model.To, GetSession().Thinking)
				}
			case api.TimelineKindAgent:
				if p.Entry.Agent != nil {
					SetSessionAgent(p.Entry.Agent.To)
				}
			case api.TimelineKindEffort:
				if p.Entry.Effort != nil {
					SetModelInfo(GetSession().ModelLabel, p.Entry.Effort.To)
				}
			}
			return
		}

		if p.Entry.Message == nil {
			return
		}

		msg := *p.Entry.Message
		// Check if this confirms our pending user message
		if acc.pendingUser != nil && acc.pendingUser.ID == msg.ID {
			acc.pendingUser = nil
		}
		acc.queue = slices.DeleteFunc(acc.queue, func(item api.QueueItem) bool {
			return item.ID == msg.ID
		})

		_ = acc.addMessageLocked(msg)
		if msg.Role == api.RoleUser {
			acc.status, acc.err = StreamRunning, nil
			acc.sessionStatus = api.StatusThinking
		} else if msg.Role == api.RoleAssistant {
			hasToolCall := false
			for _, b := range msg.Content {
				if b.Type == api.BlockToolCall {
					hasToolCall = true
					break
				}
			}
			if hasToolCall {
				acc.sessionStatus = api.StatusExecuting
			}
		} else if msg.Role == api.RoleTool {
			acc.sessionStatus = api.StatusThinking
			if acc.status == StreamWaitingPermission && len(acc.permissions) == 0 {
				acc.status = StreamRunning
			}
		}
		acc.publishLocked()

	case api.EventChunk:
		p, ok := event.Payload.(api.ChunkEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		if slices.ContainsFunc(acc.messages, func(record conversationMessage) bool {
			return !record.draft && record.message.ID == p.Chunk.ID
		}) {
			return
		}
		_ = acc.addChunkLocked(p.Chunk)
		acc.status, acc.err = StreamRunning, nil
		hasGenerating := false
		hasThinking := false
		for _, b := range p.Chunk.Content {
			switch b.Type {
			case api.BlockText, api.BlockToolCall:
				hasGenerating = true
			case api.BlockReasoning:
				hasThinking = true
			}
		}
		if hasGenerating {
			acc.sessionStatus = api.StatusGenerating
		} else if hasThinking {
			acc.sessionStatus = api.StatusThinking
		} else if acc.sessionStatus == "" || acc.sessionStatus == api.StatusIdle {
			acc.sessionStatus = api.StatusGenerating
		}
		acc.publishLocked()

	case api.EventPermissionRequested:
		p, ok := event.Payload.(api.PermissionRequestedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		_ = acc.addPermissionsLocked(p.Permissions)
		if len(acc.permissions) > 0 {
			acc.status, acc.err = StreamWaitingPermission, nil
			acc.sessionStatus = api.StatusWaitingPermission
		}
		acc.publishLocked()

	case api.EventQuestionRequested:
		p, ok := event.Payload.(api.QuestionRequestedEvent)
		if !ok || (acc.sessionID != "" && p.SessionID != acc.sessionID) {
			return
		}
		acc.addQuestionsLocked(p.Questions)
		if len(acc.questions) > 0 && len(acc.permissions) == 0 {
			acc.status, acc.err = StreamWaitingQuestion, nil
			acc.sessionStatus = api.StatusWaitingQuestion
		}
		acc.publishLocked()

	case api.EventTaskUpdated:
		p, ok := event.Payload.(api.TaskUpdatedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		UpsertTask(p.Task)

	case api.EventTodosUpdated:
		p, ok := event.Payload.(api.TodosUpdatedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.todos = slices.Clone(p.Todos)
		acc.publishLocked()

	case api.EventCommandStarted:
		p, ok := event.Payload.(api.CommandStartedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.queue = slices.DeleteFunc(acc.queue, func(item api.QueueItem) bool {
			return item.ID == p.ID
		})
		acc.status = StreamRunning
		acc.sessionStatus = api.StatusExecuting
		acc.pendingCommandID = p.ID
		acc.pendingCommand = p.Command
		acc.publishLocked()

	case api.EventQueueUpdated:
		p, ok := event.Payload.(api.QueueUpdatedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.queue = slices.Clone(p.Queue)
		acc.publishLocked()

	case api.EventCompleted:
		p, ok := event.Payload.(api.CompletedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.pendingCommand = ""
		acc.pendingCommandID = ""
		acc.status, acc.err = StreamCompleted, nil
		acc.sessionStatus = api.StatusIdle
		acc.discardDraftLocked()
		acc.clearPermissionsLocked()
		acc.clearQuestionsLocked()
		acc.publishLocked()

	case api.EventError:
		p, ok := event.Payload.(api.ErrorEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		acc.pendingCommand = ""
		acc.pendingCommandID = ""
		acc.status = StreamFailed
		acc.sessionStatus = api.StatusIdle
		if p.Error == "" {
			acc.err = errors.New("server reported a failure without details")
		} else {
			acc.err = fmt.Errorf("%s", p.Error)
		}
		acc.discardDraftLocked()
		acc.publishLocked()
		AppendError("Error", p.Error)
	}
}

func (acc *conversationAccumulator) loadTimeline(entries []api.TimelineEntry, pending ...[]api.QuestionRequest) {
	acc.mu.Lock()
	defer acc.mu.Unlock()

	// Pass 1: Process state mutations and accumulate messages
	for _, entry := range entries {
		switch entry.Kind {
		case api.TimelineKindInterrupted:
			acc.status = StreamIdle
			acc.sessionStatus = api.StatusIdle
			acc.err = nil
			acc.pendingUser = nil
			acc.clearPermissionsLocked()
			acc.clearQuestionsLocked()
			acc.discardDraftLocked()
		case api.TimelineKindQuestion:
			if entry.Question != nil {
				acc.recordQuestionLocked(*entry.Question)
				acc.removeQuestionLocked(entry.Question.Request.ID)
			}
		case api.TimelineKindPermission:
			if entry.Permission != nil {
				reqID := entry.Permission.Request.ID
				acc.permissions = slices.DeleteFunc(acc.permissions, func(r api.PermissionRequest) bool {
					return r.ID == reqID
				})
				RemoveSlot(permSlotID(reqID))
			}
		case api.TimelineKindMode:
			if entry.Mode != nil {
				SetSessionMode(entry.Mode.To)
			}
		case api.TimelineKindModel:
			if entry.Model != nil {
				SetModelInfo(entry.Model.To, GetSession().Thinking)
			}
		case api.TimelineKindAgent:
			if entry.Agent != nil {
				SetSessionAgent(entry.Agent.To)
			}
		case api.TimelineKindEffort:
			if entry.Effort != nil {
				SetModelInfo(GetSession().ModelLabel, entry.Effort.To)
			}
		case api.TimelineKindMessage:
			if entry.Message != nil {
				if acc.pendingUser != nil && acc.pendingUser.ID == entry.Message.ID {
					acc.pendingUser = nil
				}
				acc.queue = slices.DeleteFunc(acc.queue, func(item api.QueueItem) bool {
					return item.ID == entry.Message.ID
				})
				_ = acc.addMessageLocked(*entry.Message)
			}
		}
	}

	if len(pending) > 0 {
		acc.addQuestionsLocked(pending[0])
		if len(acc.questions) > 0 && len(acc.permissions) == 0 {
			acc.status, acc.sessionStatus = StreamWaitingQuestion, api.StatusWaitingQuestion
		}
	}

	// Project history after all messages are accumulated so tool calls & responses are properly paired
	history, _ := acc.projectLocked()
	historyBySourceID := make(map[string]Message, len(history))
	for _, h := range history {
		if h.SourceID != "" {
			historyBySourceID[h.SourceID] = h
		}
	}

	// Pass 2: Flush in exact chronological timeline order
	for _, entry := range entries {
		if entry.Kind == api.TimelineKindMessage {
			if entry.Message != nil {
				if h, ok := historyBySourceID[entry.Message.ID]; ok && !acc.flushedIDs[h.ID] {
					acc.flushedIDs[h.ID] = true
					AppendMessage(h)
				}
			}
		} else if entry.Kind == api.TimelineKindCompacted || entry.Kind == api.TimelineKindCacheBroken || (entry.Kind != api.TimelineKindPermission && entry.Kind != api.TimelineKindQuestion) {
			AppendTimelineEntry(entry)
		}
	}

	acc.publishLocked()
}

// SeedPermissions imports pending permission requests (e.g. from session state) into the active turn.
func (acc *conversationAccumulator) SeedPermissions(requests []api.PermissionRequest) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	_ = acc.addPermissionsLocked(requests)
	if len(acc.permissions) > 0 {
		acc.status, acc.err = StreamWaitingPermission, nil
	}
	acc.publishLocked()
}

func (acc *conversationAccumulator) reconcilePermissions(requests []api.PermissionRequest) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	for _, existing := range acc.permissions {
		if !slices.ContainsFunc(requests, func(req api.PermissionRequest) bool { return req.ID == existing.ID }) {
			RemoveSlot(permSlotID(existing.ID))
		}
	}
	acc.permissions = slices.DeleteFunc(acc.permissions, func(existing api.PermissionRequest) bool {
		return !slices.ContainsFunc(requests, func(req api.PermissionRequest) bool { return req.ID == existing.ID })
	})
	_ = acc.addPermissionsLocked(requests)
	if len(acc.permissions) > 0 {
		acc.status, acc.sessionStatus, acc.err = StreamWaitingPermission, api.StatusWaitingPermission, nil
	} else if len(acc.questions) > 0 {
		acc.status, acc.sessionStatus = StreamWaitingQuestion, api.StatusWaitingQuestion
	} else if acc.status == StreamWaitingPermission {
		acc.status, acc.sessionStatus = StreamRunning, api.StatusThinking
	}
	acc.publishLocked()
}

// SeedQueue imports initial queue items (e.g. from session state) into the active conversation queue.
func SeedQueue(items []api.QueueItem) {
	globalConversation.SeedQueue(items)
}

// SeedQueue imports initial queue items (e.g. from session state) into the active conversation queue.
func (acc *conversationAccumulator) SeedQueue(items []api.QueueItem) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.queue = slices.Clone(items)
	acc.publishLocked()
}

// SeedTodos imports initial todos (e.g. from session state) into the active conversation checklist.
func SeedTodos(items []api.TodoItem) {
	globalConversation.SeedTodos(items)
}

// SeedTodos imports initial todos (e.g. from session state) into the active conversation checklist.
func (acc *conversationAccumulator) SeedTodos(items []api.TodoItem) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.todos = slices.Clone(items)
	acc.publishLocked()
}

func (acc *conversationAccumulator) SeedQuestions(requests []api.QuestionRequest) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	for _, existing := range acc.questions {
		if !slices.ContainsFunc(requests, func(r api.QuestionRequest) bool { return r.ID == existing.ID }) {
			RemoveSlot(questionSlotID(existing.ID))
		}
	}
	acc.questions = slices.DeleteFunc(acc.questions, func(r api.QuestionRequest) bool {
		return !slices.ContainsFunc(requests, func(req api.QuestionRequest) bool { return req.ID == r.ID })
	})
	for _, req := range requests {
		delete(acc.resolvedQuestions, req.ID)
	}
	acc.addQuestionsLocked(requests)
	if len(acc.questions) > 0 && len(acc.permissions) == 0 {
		acc.status, acc.sessionStatus = StreamWaitingQuestion, api.StatusWaitingQuestion
	} else if len(acc.questions) == 0 && acc.status == StreamWaitingQuestion {
		acc.status, acc.sessionStatus = StreamRunning, api.StatusThinking
	}
	acc.publishLocked()
}

func (acc *conversationAccumulator) addQuestionsLocked(requests []api.QuestionRequest) {
	for _, req := range requests {
		if req.ID == "" || req.ToolCallID == "" || len(req.Questions) == 0 || acc.resolvedQuestions[req.ID] {
			continue
		}
		if slices.ContainsFunc(acc.messages, func(m conversationMessage) bool {
			return m.message.Role == api.RoleTool && m.message.ToolCallID == req.ToolCallID
		}) {
			continue
		}
		if slices.ContainsFunc(acc.questions, func(r api.QuestionRequest) bool { return r.ID == req.ID }) {
			continue
		}
		acc.questions = append(acc.questions, req)
		EnqueueSlot(SlotItem{ID: questionSlotID(req.ID), Kind: SlotKindQuestion, Data: req})
	}
}

func (acc *conversationAccumulator) recordQuestionLocked(record api.QuestionRecord) {
	if record.Request.ToolCallID == "" {
		return
	}
	if acc.questionRecords == nil {
		acc.questionRecords = make(map[string]api.QuestionRecord)
	}
	acc.questionRecords[record.Request.ToolCallID] = record
}

func (acc *conversationAccumulator) removeQuestionLocked(id string) {
	if id == "" {
		return
	}
	if acc.resolvedQuestions == nil {
		acc.resolvedQuestions = make(map[string]bool)
	}
	acc.resolvedQuestions[id] = true
	RemoveSlot(questionSlotID(id))
	acc.questions = slices.DeleteFunc(acc.questions, func(r api.QuestionRequest) bool { return r.ID == id })
	if len(acc.questions) == 0 && len(acc.permissions) == 0 && acc.status == StreamWaitingQuestion {
		acc.status, acc.sessionStatus = StreamRunning, api.StatusThinking
	}
}

func (acc *conversationAccumulator) removePermission(reqID string) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.permissions = slices.DeleteFunc(acc.permissions, func(r api.PermissionRequest) bool {
		return r.ID == reqID
	})
	if len(acc.permissions) == 0 && acc.status == StreamWaitingPermission {
		if len(acc.questions) > 0 {
			acc.status, acc.sessionStatus = StreamWaitingQuestion, api.StatusWaitingQuestion
		} else {
			acc.status = StreamRunning
		}
	}
	acc.publishLocked()
}

func (acc *conversationAccumulator) addMessageLocked(message api.Message) error {
	if message.ID == "" || message.Role == "" {
		return errors.New("conversation: persisted messages require an ID and role")
	}
	calls := make(map[string]bool)
	for _, block := range message.Content {
		if err := validateConversationBlock(block, false); err != nil {
			return err
		}
		if message.Role != api.RoleTool && block.ToolCall != nil {
			calls[block.ToolCall.ID] = true
		}
	}
	for _, existing := range acc.messages {
		if existing.message.ID == message.ID {
			if message.Role == api.RoleAssistant {
				acc.discardDraftLocked()
			}
			return nil
		}
	}
	if message.Role == api.RoleAssistant && acc.draft >= 0 {
		record := acc.messages[acc.draft]
		record.id = acc.messageID(message.ID, record.id)
		record.message, record.draft, record.done = cloneConversationMessage(message), false, true
		acc.discardDraftLocked()
		acc.messages = append(acc.messages, record)
	} else {
		acc.messages = append(acc.messages, conversationMessage{
			id: acc.messageID(message.ID, ""), message: cloneConversationMessage(message), done: true,
		})
	}
	if message.Role == api.RoleTool {
		if message.Name == "todos" && len(message.StructuredContent) > 0 {
			var out api.TodosOutput
			if err := json.Unmarshal(message.StructuredContent, &out); err == nil && out.Todos != nil {
				acc.todos = slices.Clone(out.Todos)
			}
		}
		for _, req := range slices.Clone(acc.questions) {
			if req.ToolCallID == message.ToolCallID {
				acc.removeQuestionLocked(req.ID)
			}
		}
		for _, req := range acc.permissions {
			if req.ToolCallID == message.ToolCallID {
				RemoveSlot(permSlotID(req.ID))
			}
		}
		acc.permissions = slices.DeleteFunc(acc.permissions, func(request api.PermissionRequest) bool {
			return request.ToolCallID == message.ToolCallID
		})
	}
	return nil
}

func (acc *conversationAccumulator) discardDraftLocked() {
	if acc.draft >= 0 && acc.draft < len(acc.messages) {
		acc.messages = slices.Delete(acc.messages, acc.draft, acc.draft+1)
		acc.draft = -1
	}
}

func (acc *conversationAccumulator) addChunkLocked(chunk api.Chunk) error {
	for _, block := range chunk.Content {
		if err := validateConversationBlock(block, true); err != nil {
			return err
		}
	}
	if acc.draft < 0 {
		acc.draft = len(acc.messages)
		acc.messages = append(acc.messages, conversationMessage{
			id:      acc.messageID("", ""),
			message: api.Message{Role: api.RoleAssistant}, draft: true,
		})
	}
	record := &acc.messages[acc.draft]
	for _, block := range chunk.Content {
		record.message.Content = appendConversationBlock(record.message.Content, block)
	}
	record.done = chunk.Done
	return nil
}

func (acc *conversationAccumulator) addPermissionsLocked(requests []api.PermissionRequest) error {
	for _, request := range requests {
		if request.ID == "" || request.ToolCallID == "" {
			continue
		}
		resolved := slices.ContainsFunc(acc.messages, func(record conversationMessage) bool {
			return record.message.Role == api.RoleTool && record.message.ToolCallID == request.ToolCallID
		})
		if resolved {
			continue
		}
		index := slices.IndexFunc(acc.permissions, func(existing api.PermissionRequest) bool {
			return existing.ID == request.ID
		})
		request.GrantPatterns = slices.Clone(request.GrantPatterns)
		if index >= 0 {
			acc.permissions[index] = request
		} else {
			acc.permissions = append(acc.permissions, request)
		}
		EnqueueSlot(SlotItem{
			ID:   permSlotID(request.ID),
			Kind: SlotKindPermission,
			Data: request,
		})
	}
	return nil
}

func (acc *conversationAccumulator) publishLocked() {
	history, streamMsgs := acc.projectLocked()

	// Flush newly confirmed history messages to Transcript
	for _, h := range history {
		if !acc.flushedIDs[h.ID] {
			acc.flushedIDs[h.ID] = true
			AppendMessage(h)
		}
	}

	var msgsPtr *[]Message
	if len(streamMsgs) > 0 {
		cp := make([]Message, len(streamMsgs))
		copy(cp, streamMsgs)
		msgsPtr = &cp
	}

	isStreaming := acc.status == StreamRunning || acc.status == StreamWaitingPermission || acc.status == StreamWaitingQuestion || acc.pendingUser != nil || acc.pendingCommand != "" ||
		(msgsPtr != nil && len(*msgsPtr) > 0 && acc.status != StreamIdle && acc.status != StreamCompleted && acc.status != StreamFailed)
	isWaiting := acc.status == StreamWaitingPermission || len(acc.permissions) > 0
	isWaitingQuestion := acc.status == StreamWaitingQuestion || len(acc.questions) > 0

	sessionStatus := acc.sessionStatus
	if sessionStatus == "" {
		if isWaiting {
			sessionStatus = api.StatusWaitingPermission
		} else if isStreaming {
			sessionStatus = api.StatusThinking
		} else {
			sessionStatus = api.StatusIdle
		}
	} else if !isStreaming && !isWaiting {
		sessionStatus = api.StatusIdle
	}

	var queuePtr *[]api.QueueItem
	if len(acc.queue) > 0 {
		cp := slices.Clone(acc.queue)
		queuePtr = &cp
	}

	var todosPtr *[]api.TodoItem
	if len(acc.todos) > 0 {
		cp := slices.Clone(acc.todos)
		todosPtr = &cp
	}

	stream.Set(func(StreamState) StreamState {
		return StreamState{
			Messages:          msgsPtr,
			Streaming:         isStreaming,
			WaitingPermission: isWaiting,
			WaitingQuestion:   isWaitingQuestion,
			Status:            sessionStatus,
			PendingCommand:    acc.pendingCommand,
			Queue:             queuePtr,
			Todos:             todosPtr,
		}
	})
}

func (acc *conversationAccumulator) projectLocked() (history []Message, streamMsgs []Message) {
	messages := make([]Message, len(acc.messages))
	tools := make(map[string]*ToolPart)

	for i, record := range acc.messages {
		message := Message{
			ID: record.id, SourceID: record.message.ID, Role: record.message.Role,
			CreatedAt: record.message.CreatedAt,
			Streaming: record.draft && acc.status == StreamRunning && !record.done,
		}
		if message.Role != api.RoleTool {
			for _, block := range record.message.Content {
				part := MessagePart{ID: record.id + ":" + strconv.Itoa(len(message.Parts))}
				switch block.Type {
				case api.BlockText:
					part.Type, part.Text = PartText, &TextPart{Text: block.Text}
				case api.BlockReasoning:
					part.Type, part.Thought = PartThought, &ThoughtPart{Text: block.Text, Signature: block.Signature}
				case api.BlockToolCall:
					call := *block.ToolCall
					call.Args = maps.Clone(call.Args)
					part.Type, part.Tool = PartTool, &ToolPart{ID: call.ID, Call: &call}
					if call.ID != "" {
						tools[call.ID] = part.Tool
					}
				}
				message.Parts = append(message.Parts, part)
			}
			if message.Streaming && len(message.Parts) > 0 {
				if thought := message.Parts[len(message.Parts)-1].Thought; thought != nil {
					thought.Streaming = true
				}
			}
		}
		messages[i] = message
	}

	for i, record := range acc.messages {
		if record.message.Role != api.RoleTool {
			continue
		}
		response := cloneConversationMessage(record.message)
		if tool := tools[response.ToolCallID]; tool != nil {
			tool.Response = &response
		} else {
			messages[i].Parts = []MessagePart{{
				ID: "tool:" + response.ToolCallID, Type: PartTool,
				Tool: &ToolPart{ID: response.ToolCallID, Response: &response},
			}}
		}
	}

	for id, record := range acc.questionRecords {
		if tool := tools[id]; tool != nil {
			copy := record
			tool.Question = &copy
		}
	}

	isActive := acc.status == StreamRunning || acc.status == StreamWaitingPermission || acc.status == StreamWaitingQuestion
	mutableSuffix := false
	for i, message := range messages {
		if message.Role == api.RoleTool && len(message.Parts) == 0 {
			continue
		}
		mutable := acc.messages[i].draft
		for j := range message.Parts {
			tool := message.Parts[j].Tool
			if tool == nil {
				continue
			}
			for _, request := range acc.permissions {
				if request.ToolCallID == tool.ID {
					tool.Permissions = append(tool.Permissions, request)
				}
			}
			switch {
			case tool.Response != nil && tool.Response.IsError:
				tool.Status = ToolFailed
			case tool.Response != nil:
				tool.Status = ToolSucceeded
			case tool.Response == nil && !isActive:
				tool.Status = ToolInterrupted
			case acc.status == StreamFailed:
				tool.Status = ToolInterrupted
			case len(tool.Permissions) > 0:
				tool.Status = ToolWaitingPermission
			case acc.messages[i].draft:
				tool.Status = ToolReceiving
			case acc.status == StreamRunning:
				tool.Status = ToolRunning
			default:
				tool.Status = ToolPending
			}
			mutable = mutable || (isActive && (tool.Call == nil || tool.Response == nil || len(tool.Permissions) > 0))
		}
		mutableSuffix = mutableSuffix || mutable
		if mutableSuffix {
			streamMsgs = append(streamMsgs, message)
		} else {
			history = append(history, message)
		}
	}

	if acc.pendingUser != nil {
		streamMsgs = append([]Message{*acc.pendingUser}, streamMsgs...)
	}

	return history, streamMsgs
}

func validateConversationBlock(block api.Block, partial bool) error {
	switch block.Type {
	case api.BlockText, api.BlockReasoning:
		return nil
	case api.BlockToolCall:
		if block.ToolCall == nil || !partial && block.ToolCall.ID == "" {
			return errors.New("conversation: tool block requires a call and a final call ID")
		}
		return nil
	default:
		return fmt.Errorf("conversation: unsupported block type %q", block.Type)
	}
}

func appendConversationBlock(blocks []api.Block, block api.Block) []api.Block {
	if block.Type == api.BlockToolCall {
		for i, block0 := range slices.Backward(blocks) {
			call := block0.ToolCall
			if block0.Type != api.BlockToolCall || call == nil {
				continue
			}
			sameID := block.ToolCall.ID != "" && call.ID == block.ToolCall.ID
			adjacentPartial := i == len(blocks)-1 && (call.ID == "" || block.ToolCall.ID == "") &&
				(call.Name == "" || block.ToolCall.Name == "" || call.Name == block.ToolCall.Name)
			if !sameID && !adjacentPartial {
				continue
			}
			if block.ToolCall.ID != "" {
				call.ID = block.ToolCall.ID
			}
			if block.ToolCall.Name != "" {
				call.Name = block.ToolCall.Name
			}
			if block.ToolCall.Args != nil {
				call.Args = maps.Clone(block.ToolCall.Args)
			}
			return blocks
		}
	}
	if len(blocks) > 0 {
		last := &blocks[len(blocks)-1]
		if last.Type == block.Type {
			switch block.Type {
			case api.BlockText, api.BlockReasoning:
				last.Text += block.Text
				last.Signature += block.Signature
				return blocks
			}
		}
	}
	if block.ToolCall != nil {
		call := *block.ToolCall
		call.Args = maps.Clone(call.Args)
		block.ToolCall = &call
	}
	return append(blocks, block)
}

func cloneConversationMessage(message api.Message) api.Message {
	message.Content = slices.Clone(message.Content)
	message.StructuredContent = slices.Clone(message.StructuredContent)
	for i := range message.Content {
		if call := message.Content[i].ToolCall; call != nil {
			cloned := *call
			cloned.Args = maps.Clone(call.Args)
			message.Content[i].ToolCall = &cloned
		}
	}
	return message
}
