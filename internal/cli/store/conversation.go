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

	// Delegated reports that this session is waiting on child subagents rather
	// than running a turn of its own, so the work shown as in flight belongs to
	// a subagent. It only applies while Streaming is true.
	Delegated bool

	// TurnStartedAt is when the current turn started streaming. BaseInput and
	// BaseOutput are the session's token totals at that moment, and CallInput
	// and CallOutput are the running totals of the LLM call in flight, so the
	// turn's tokens so far are (session totals - base) + call.
	TurnStartedAt time.Time
	BaseInput     int64
	BaseOutput    int64
	CallInput     int64
	CallOutput    int64
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

// refreshSubagentState republishes the stream so the delegated indicator follows
// the children's status. Subagent updates mutate the session store rather than
// the conversation accumulator, so without this the spinner would keep reporting
// a delegation after the last child finished.
func refreshSubagentState() {
	globalConversation.mu.Lock()
	defer globalConversation.mu.Unlock()
	globalConversation.refreshSubagentStateLocked()
}

// refreshSubagentStateLocked republishes when the number of running children has
// changed since the last publish. It must be called with acc.mu held.
func (acc *conversationAccumulator) refreshSubagentStateLocked() {
	if acc.runningSubagents == len(GetRunningSubagents()) {
		return
	}
	acc.publishLocked()
}

type conversationMessage struct {
	id      string
	message api.Message
	draft   bool
	done    bool
}

type conversationHooks struct {
	onAppendMessage    func(msg Message)
	onAppendTimeline   func(entry api.TimelineEntry)
	onEnqueueSlot      func(slot SlotItem)
	onRemoveSlot       func(slotID string)
	onPublishStream    func(state StreamState)
	onUsage            func(usage api.SessionUsage)
	onContext          func(ctx api.ContextState)
	onSessionUpdated   func(sess api.Session)
	onTimelineMetadata func(entry api.TimelineEntry)
	onTaskUpdated      func(task api.Task)
	onError            func(title, detail string)
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
	parentTurnActive  bool
	runningSubagents  int
	queue             []api.QueueItem
	todos             []api.TodoItem
	flushedIDs        map[string]bool
	identities        map[string]string
	nextDraft         uint64
	hooks             conversationHooks

	wasStreaming bool
	turnStart    time.Time
	baseInput    int64
	baseOutput   int64
	baseCalls    int64
	callInput    int64
	callOutput   int64
}

func (acc *conversationAccumulator) appendMessage(msg Message) {
	if acc.hooks.onAppendMessage != nil {
		acc.hooks.onAppendMessage(msg)
	}
}

func (acc *conversationAccumulator) appendTimeline(entry api.TimelineEntry) {
	if acc.hooks.onAppendTimeline != nil {
		acc.hooks.onAppendTimeline(entry)
	}
}

func (acc *conversationAccumulator) enqueueSlot(slot SlotItem) {
	if acc.hooks.onEnqueueSlot != nil {
		acc.hooks.onEnqueueSlot(slot)
	}
}

func (acc *conversationAccumulator) removeSlot(slotID string) {
	if acc.hooks.onRemoveSlot != nil {
		acc.hooks.onRemoveSlot(slotID)
	}
}

func (acc *conversationAccumulator) publishStream(state StreamState) {
	if acc.hooks.onPublishStream != nil {
		acc.hooks.onPublishStream(state)
	}
}

func (acc *conversationAccumulator) applyTimelineMetadata(entry api.TimelineEntry) {
	if acc.hooks.onTimelineMetadata != nil {
		acc.hooks.onTimelineMetadata(entry)
	}
}

func newConversationAccumulator() *conversationAccumulator {
	acc := &conversationAccumulator{
		draft:         -1,
		status:        StreamIdle,
		sessionStatus: api.StatusIdle,
		flushedIDs:    make(map[string]bool),
		identities:    make(map[string]string),
	}
	acc.hooks = conversationHooks{
		onAppendMessage:  AppendMessage,
		onAppendTimeline: AppendTimelineEntry,
		onEnqueueSlot:    EnqueueSlot,
		onRemoveSlot:     RemoveSlot,
		onPublishStream: func(state StreamState) {
			stream.Set(func(StreamState) StreamState { return state })
		},
		onUsage: SetUsage,
		onContext: func(c api.ContextState) {
			session.Set(func(s SessionState) SessionState {
				s.Context = c
				if s.Session != nil {
					s.Session.Context = c
				}
				return s
			})
		},
		onSessionUpdated: func(sess api.Session) {
			if acc.sessionID == "" || sess.ID == acc.sessionID {
				SetSession(&sess)
			} else if sess.ParentID != "" && sess.ParentID == acc.sessionID {
				UpsertSubagent(sess)
				acc.refreshSubagentStateLocked()
			}
		},
		onTimelineMetadata: func(entry api.TimelineEntry) {
			switch entry.Kind {
			case api.TimelineKindMode:
				if entry.Mode != nil && entry.Mode.To != "" {
					SetSessionMode(entry.Mode.To)
				}
			case api.TimelineKindModel:
				if entry.Model != nil && entry.Model.To != "" {
					SetModelInfo(entry.Model.To, GetSession().Thinking)
					SetActiveModelID(entry.Model.To)
				}
			case api.TimelineKindAgent:
				if entry.Agent != nil && entry.Agent.To != "" {
					SetSessionAgent(entry.Agent.To)
				}
			case api.TimelineKindThinking:
				if entry.Thinking != nil {
					ApplyThinkingChange(entry.Thinking.To)
				}
			case api.TimelineKindEffort:
				if entry.Effort != nil && entry.Effort.To != "" {
					SetModelInfo(GetSession().ModelLabel, entry.Effort.To)
				}
			}
		},
		onTaskUpdated: UpsertTask,
		onError:       AppendError,
	}
	return acc
}

var globalConversation = newConversationAccumulator()

func newIsolatedConversationAccumulator(sessionID string) *conversationAccumulator {
	return &conversationAccumulator{
		sessionID:     sessionID,
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
		acc.removeSlot(questionSlotID(req.ID))
	}
	acc.questions = nil
}

func (acc *conversationAccumulator) clearPermissionsLocked() {
	for _, req := range acc.permissions {
		acc.removeSlot(permSlotID(req.ID))
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
	acc.parentTurnActive = false
	acc.runningSubagents = 0
	acc.queue = nil
	acc.todos = nil
	acc.flushedIDs = make(map[string]bool)
	acc.identities = make(map[string]string)
	acc.nextDraft = 0
	acc.endTurnLocked()
}

// endTurnLocked clears the per-turn spinner statistics.
func (acc *conversationAccumulator) endTurnLocked() {
	acc.wasStreaming = false
	acc.parentTurnActive = false
	acc.turnStart = time.Time{}
	acc.baseInput, acc.baseOutput, acc.baseCalls = 0, 0, 0
	acc.callInput, acc.callOutput = 0, 0
}

// markParentTurnLocked records that this session produced evidence of a turn of
// its own. While a subagent runs this is what separates "the parent is working"
// from "the parent is merely waiting on its children", so the spinner can say
// which one the user is watching. The caller's own message echo is deliberately
// not evidence: a command delegated to a subagent records it before the parent
// starts any turn.
func (acc *conversationAccumulator) markParentTurnLocked() {
	acc.parentTurnActive = true
}

func (acc *conversationAccumulator) setSessionID(id string) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.sessionID = id
}

func (acc *conversationAccumulator) promotePendingUserLocked() {
	if acc.pendingUser == nil {
		return
	}
	u := acc.pendingUser
	acc.pendingUser = nil
	var text string
	if len(u.Parts) > 0 && u.Parts[0].Text != nil {
		text = u.Parts[0].Text.Text
	}
	_ = acc.addMessageLocked(api.Message{
		ID:        u.ID,
		Role:      api.RoleUser,
		CreatedAt: u.CreatedAt,
		Content:   []api.Block{{Type: api.BlockText, Text: text}},
	})
}

// resolvePendingUserLocked settles an optimistic user message when a turn ends
// without the daemon ever confirming it. Commands delegated to a subagent run in
// a child session, so the invoking message is never echoed back on this session's
// stream; without this the spinner would keep running after the turn completed.
func (acc *conversationAccumulator) resolvePendingUserLocked() {
	if acc.pendingUser == nil {
		return
	}
	if acc.pendingCommandID != "" && acc.pendingUser.SourceID == acc.pendingCommandID {
		acc.pendingCommand = ""
		acc.pendingCommandID = ""
	}
	acc.promotePendingUserLocked()
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
				Message: &api.Message{
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
				ID:        id,
				Kind:      api.QueueKindCommand,
				Command:   cmd,
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

	case api.EventUsageUpdated:
		p, ok := event.Payload.(api.UsageUpdatedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		if acc.hooks.onUsage != nil {
			acc.hooks.onUsage(p.Usage)
		}
		// A finished LLM call is now part of the session totals; drop its running count.
		if p.Usage.Totals.Calls > acc.baseCalls {
			acc.baseCalls = p.Usage.Totals.Calls
			acc.callInput, acc.callOutput = 0, 0
			acc.publishLocked()
		}

	case api.EventContextUpdated:
		p, ok := event.Payload.(api.ContextUpdatedEvent)
		if !ok {
			return
		}
		if acc.sessionID != "" && p.SessionID != acc.sessionID {
			return
		}
		if acc.hooks.onContext != nil {
			acc.hooks.onContext(p.Context)
		}

	case api.EventSessionUpdated:
		p, ok := event.Payload.(api.SessionUpdatedEvent)
		if !ok {
			return
		}
		if acc.hooks.onSessionUpdated != nil {
			acc.hooks.onSessionUpdated(p.Session)
		}

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
			acc.markParentTurnLocked()
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
				acc.appendTimeline(p.Entry)
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
				acc.appendTimeline(p.Entry)
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
					acc.removeSlot(permSlotID(reqID))
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
			acc.appendTimeline(p.Entry)
			acc.applyTimelineMetadata(p.Entry)
			return
		}

		if p.Entry.Message == nil {
			return
		}

		msg := *p.Entry.Message
		// Check if this confirms our pending user message
		if acc.pendingUser != nil && acc.pendingUser.ID == msg.ID {
			acc.pendingUser = nil
		} else if msg.Role != api.RoleUser {
			acc.promotePendingUserLocked()
		}
		acc.queue = slices.DeleteFunc(acc.queue, func(item api.QueueItem) bool {
			return item.ID == msg.ID
		})

		_ = acc.addMessageLocked(msg)
		if msg.Role == api.RoleUser {
			// A delegated command echoes its invocation after the turn that carried
			// it may already have ended, so a late confirmation must not restart the
			// spinner or discard the turn's outcome.
			if acc.status != StreamCompleted && acc.status != StreamFailed {
				acc.status, acc.err = StreamRunning, nil
				acc.sessionStatus = api.StatusThinking
			}
		} else if msg.Role == api.RoleAssistant {
			acc.markParentTurnLocked()
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
			acc.markParentTurnLocked()
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
		acc.markParentTurnLocked()
		acc.promotePendingUserLocked()
		_ = acc.addChunkLocked(p.Chunk)
		if p.Chunk.Usage != nil {
			// Each chunk carries the call's cumulative usage, so it replaces the last value.
			acc.callInput, acc.callOutput = int64(p.Chunk.Usage.Input), int64(p.Chunk.Usage.Output)
		}
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
		if acc.hooks.onTaskUpdated != nil {
			acc.hooks.onTaskUpdated(p.Task)
		}

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
		acc.markParentTurnLocked()
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
		acc.resolvePendingUserLocked()
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
		acc.resolvePendingUserLocked()
		if p.Error == "" {
			acc.err = errors.New("server reported a failure without details")
		} else {
			acc.err = fmt.Errorf("%s", p.Error)
		}
		acc.discardDraftLocked()
		acc.publishLocked()
		if acc.hooks.onError != nil {
			acc.hooks.onError("Error", p.Error)
		}
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
				acc.removeSlot(permSlotID(reqID))
			}
		case api.TimelineKindMode, api.TimelineKindModel, api.TimelineKindAgent, api.TimelineKindEffort:
			acc.applyTimelineMetadata(entry)
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
					acc.appendMessage(h)
				}
			}
		} else if entry.Kind == api.TimelineKindCompacted || entry.Kind == api.TimelineKindCacheBroken || (entry.Kind != api.TimelineKindPermission && entry.Kind != api.TimelineKindQuestion) {
			acc.appendTimeline(entry)
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
		acc.enqueueSlot(SlotItem{ID: questionSlotID(req.ID), Kind: SlotKindQuestion, Data: req})
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
	acc.removeSlot(questionSlotID(id))
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
	acc.removeSlot(permSlotID(reqID))
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
				acc.removeSlot(permSlotID(req.ID))
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
		acc.enqueueSlot(SlotItem{
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
			acc.appendMessage(h)
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

	if isStreaming && !acc.wasStreaming {
		totals := GetUsage().Totals
		acc.turnStart = time.Now()
		acc.baseInput, acc.baseOutput, acc.baseCalls = totals.Tokens.Input, totals.Tokens.Output, totals.Calls
		acc.callInput, acc.callOutput = 0, 0
	} else if !isStreaming && acc.wasStreaming {
		acc.endTurnLocked()
	}
	acc.wasStreaming = isStreaming

	// A session with running children but no turn of its own is waiting on them,
	// which is a different thing to tell the user than "I am thinking".
	running := len(GetRunningSubagents())
	acc.runningSubagents = running
	delegated := isStreaming && !acc.parentTurnActive && running > 0

	turnStart, baseIn, baseOut, callIn, callOut := acc.turnStart, acc.baseInput, acc.baseOutput, acc.callInput, acc.callOutput
	acc.publishStream(StreamState{
		TurnStartedAt:     turnStart,
		BaseInput:         baseIn,
		BaseOutput:        baseOut,
		CallInput:         callIn,
		CallOutput:        callOut,
		Messages:          msgsPtr,
		Streaming:         isStreaming,
		WaitingPermission: isWaiting,
		WaitingQuestion:   isWaitingQuestion,
		Status:            sessionStatus,
		PendingCommand:    acc.pendingCommand,
		Queue:             queuePtr,
		Todos:             todosPtr,
		Delegated:         delegated,
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
