package hooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

// Conversation is a read-only projection of one session. History is the
// finalized prefix; Stream holds the mutable suffix and execution state.
type Conversation struct {
	History     []Message
	Stream      Stream
	Permissions []api.PermissionRequest
	IsLoading   bool
	Error       error
	Refetch     func()
}

// Message preserves the order of its parts. ID is a stable view identity;
// SourceID is the persisted message ID, empty until a draft is finalized.
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

// ToolPart joins a call and its response by ID. Call is nil for an unmatched
// response. Permissions contains only requests observed as pending; the
// transport does not provide historical permission decisions.
type ToolPart struct {
	ID          string
	Call        *api.ToolCall
	Response    *api.Message
	Status      ToolStatus
	Permissions []api.PermissionRequest
}

type StreamStatus string

const (
	StreamIdle              StreamStatus = "idle"
	StreamRunning           StreamStatus = "running"
	StreamWaitingPermission StreamStatus = "waiting_permission"
	StreamCompleted         StreamStatus = "completed"
	StreamFailed            StreamStatus = "failed"
)

type Stream struct {
	Messages []Message
	Status   StreamStatus
	Error    error
}

type conversationKey struct {
	Client   uintptr
	Socket   string
	CWD      string
	Session  string
	Revision uint64
}

type conversationSnapshot struct {
	Key    conversationKey
	Data   Conversation
	Loaded bool
}

type conversationFailure struct {
	key conversationKey
	err error
}

func (e *conversationFailure) Error() string { return e.err.Error() }
func (e *conversationFailure) Unwrap() error { return e.err }

// UseConversation loads history and pending permissions, then folds every SSE
// event into the same message model. An empty session ID disables all requests.
// It requires client.Provider and wind.Provider. Sending messages and resolving
// permissions remain separate mutations; Refetch reloads authoritative state.
func UseConversation(sessionID string) Conversation {
	c := uiclient.UseClient()
	revision, setRevision := kitex.UseState(uint64(0))
	last := kitex.UseRef(conversationSnapshot{})

	key := conversationKey{
		Client: reflect.ValueOf(c).Pointer(), Socket: c.SocketPath(), CWD: c.CWD(),
		Session: strings.TrimSpace(sessionID),
	}

	identities := kitex.UseMemo(func() *conversationIdentities {
		return &conversationIdentities{}
	}, []any{key})

	key.Revision = revision()
	enabled := key.Session != ""
	result := wind.UseStream(key, func(ctx context.Context, key conversationKey) iter.Seq2[conversationSnapshot, error] {
		return conversationUpdates(ctx, c, key, identities)
	}, wind.StreamOptions{Enabled: enabled})

	conversation := Conversation{
		Stream:    Stream{Status: StreamIdle},
		IsLoading: enabled,
	}

	previousKey, currentKey := last.Current.Key, key
	previousKey.Revision, currentKey.Revision = 0, 0
	sameScope := previousKey == currentKey
	if enabled && sameScope {
		conversation = last.Current.Data
		conversation.IsLoading, conversation.Error = true, nil
	}
	if enabled && result.Data.Key == key {
		if result.Data.Loaded || !sameScope {
			conversation = result.Data.Data
			last.Current = result.Data
		} else {
			conversation = last.Current.Data
			conversation.IsLoading, conversation.Error = false, result.Data.Data.Error
		}
	}
	if enabled && result.Error != nil {
		var failure *conversationFailure
		if !errors.As(result.Error, &failure) || failure.key == key {
			conversation.Error = result.Error
		}
	}
	if conversation.Error != nil {
		conversation = failedConversation(conversation, conversation.Error)
	}
	if !enabled {
		last.Current = conversationSnapshot{}
	}
	conversation.Refetch = func() { setRevision(revision() + 1) }
	return conversation
}

func failedConversation(conversation Conversation, err error) Conversation {
	conversation.IsLoading, conversation.Error = false, err
	conversation.Stream.Status, conversation.Stream.Error = StreamFailed, err
	conversation.Stream.Messages = slices.Clone(conversation.Stream.Messages)
	for i := range conversation.Stream.Messages {
		message := &conversation.Stream.Messages[i]
		message.Streaming = false
		message.Parts = slices.Clone(message.Parts)
		for j := range message.Parts {
			part := &message.Parts[j]
			if part.Thought != nil {
				thought := *part.Thought
				thought.Streaming = false
				part.Thought = &thought
			}
			if part.Tool != nil && part.Tool.Response == nil {
				tool := *part.Tool
				tool.Status = ToolInterrupted
				part.Tool = &tool
			}
		}
	}
	return conversation
}

func conversationUpdates(ctx context.Context, c *client.Client, key conversationKey, identities *conversationIdentities) iter.Seq2[conversationSnapshot, error] {
	return func(yield func(conversationSnapshot, error) bool) {
		parentCtx := ctx
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		state := conversationState{sessionID: key.Session, status: StreamIdle, draft: -1, identities: identities}
		loaded := false
		emit := func() bool {
			return yield(conversationSnapshot{Key: key, Data: state.view(), Loaded: loaded}, nil)
		}
		fail := func(err error) {
			if parentCtx.Err() != nil {
				return
			}
			state.status, state.err = StreamFailed, err
			if emit() {
				yield(conversationSnapshot{}, &conversationFailure{key: key, err: err})
			}
		}
		var loadErr error
		ready := func() {
			loadErr = state.load(ctx, c)
			loaded = loadErr == nil
			if loadErr != nil || !emit() {
				cancel()
			}
		}
		for event, err := range c.Watch(ctx, ready, api.SessionStreamTopic(key.Session)) {
			if ctx.Err() != nil {
				break
			}
			if err != nil {
				fail(fmt.Errorf("conversation: subscribe: %w", err))
				return
			}
			if err := state.apply(event); err != nil {
				fail(err)
				return
			}
			if !emit() {
				return
			}
		}
		if loadErr != nil {
			fail(loadErr)
		} else if ctx.Err() == nil {
			fail(fmt.Errorf("conversation: event stream closed: %w", io.EOF))
		}
	}
}

type conversationMessage struct {
	id      string
	message api.Message
	draft   bool
	done    bool
}

type conversationState struct {
	sessionID   string
	messages    []conversationMessage
	permissions []api.PermissionRequest
	status      StreamStatus
	err         error
	draft       int
	identities  *conversationIdentities
}

type conversationIdentities struct {
	mu        sync.Mutex
	messages  map[string]string
	nextDraft uint64
}

func (s *conversationState) messageID(sourceID, draftID string) string {
	if s.identities == nil {
		s.identities = &conversationIdentities{}
	}
	identities := s.identities
	identities.mu.Lock()
	defer identities.mu.Unlock()
	if sourceID == "" {
		identities.nextDraft++
		return "stream:" + s.sessionID + ":" + strconv.FormatUint(identities.nextDraft, 10)
	}
	if id, ok := identities.messages[sourceID]; ok {
		return id
	}
	if identities.messages == nil {
		identities.messages = make(map[string]string)
	}
	if draftID == "" {
		draftID = sourceID
	}
	identities.messages[sourceID] = draftID
	return draftID
}

func (s *conversationState) load(ctx context.Context, c *client.Client) error {
	entries, err := c.Timeline(ctx, s.sessionID)
	if err != nil {
		return fmt.Errorf("conversation: timeline: %w", err)
	}
	for _, entry := range entries {
		if entry.Kind == api.TimelineKindMessage && entry.Message != nil {
			if err := s.addMessage(*entry.Message); err != nil {
				return err
			}
		}
	}
	permissions, err := c.PendingPermissions(ctx, s.sessionID)
	if err != nil {
		return fmt.Errorf("conversation: permissions: %w", err)
	}
	if err := s.addPermissions(permissions); err != nil {
		return err
	}
	session, err := c.GetSession(ctx, s.sessionID)
	if err != nil {
		return fmt.Errorf("conversation: session: %w", err)
	}
	switch session.Status {
	case api.StatusIdle:
		s.status = StreamIdle
	case api.StatusRunning:
		s.status = StreamRunning
	case api.StatusWaitingPermission:
		s.status = StreamWaitingPermission
	default:
		return fmt.Errorf("conversation: unknown session status %q", session.Status)
	}
	return nil
}

func (s *conversationState) apply(event api.Event) error {
	switch event.Type {
	case api.EventConnected:
		return nil

	case api.EventEntryAdded:
		p, ok := event.Payload.(api.EntryAddedEvent)
		if !ok {
			return errors.New("conversation: entry_added event has invalid payload")
		}
		if p.SessionID != s.sessionID {
			return nil
		}
		if p.Entry.Kind != api.TimelineKindMessage {
			return nil
		}
		if p.Entry.Message == nil {
			return errors.New("conversation: message event has no message")
		}
		known := slices.ContainsFunc(s.messages, func(record conversationMessage) bool {
			return record.message.ID == p.Entry.Message.ID
		})
		if err := s.addMessage(*p.Entry.Message); err != nil {
			return err
		}
		if p.Entry.Message.Role == api.RoleUser && !known {
			s.status, s.err = StreamRunning, nil
		} else if p.Entry.Message.Role == api.RoleTool && s.status == StreamWaitingPermission && len(s.permissions) == 0 {
			s.status = StreamRunning
		}

	case api.EventChunk:
		p, ok := event.Payload.(api.ChunkEvent)
		if !ok {
			return errors.New("conversation: chunk event has invalid payload")
		}
		if p.SessionID != s.sessionID {
			return nil
		}
		if err := s.addChunk(p.Chunk); err != nil {
			return err
		}
		s.status, s.err = StreamRunning, nil

	case api.EventPermissionRequested:
		p, ok := event.Payload.(api.PermissionRequestedEvent)
		if !ok {
			return errors.New("conversation: permission_requested event has invalid payload")
		}
		if p.SessionID != s.sessionID {
			return nil
		}
		if err := s.addPermissions(p.Permissions); err != nil {
			return err
		}
		if len(s.permissions) > 0 {
			s.status, s.err = StreamWaitingPermission, nil
		}

	case api.EventCompleted:
		p, ok := event.Payload.(api.CompletedEvent)
		if !ok {
			return errors.New("conversation: completed event has invalid payload")
		}
		if p.SessionID != s.sessionID {
			return nil
		}
		if p.Message != nil {
			if err := s.addMessage(*p.Message); err != nil {
				return err
			}
		}
		s.status, s.err = StreamCompleted, nil
		if s.draft >= 0 {
			return errors.New("conversation: completed before the streamed message was persisted")
		}
		for _, message := range s.view().Stream.Messages {
			for _, part := range message.Parts {
				if part.Tool != nil && (part.Tool.Call == nil || part.Tool.Response == nil) {
					return fmt.Errorf("conversation: completed with unmatched tool %q", part.Tool.ID)
				}
			}
		}
		s.permissions = nil

	case api.EventError:
		p, ok := event.Payload.(api.ErrorEvent)
		if !ok {
			return errors.New("conversation: error event has invalid payload")
		}
		if p.SessionID != s.sessionID {
			return nil
		}
		s.status = StreamFailed
		if p.Error == "" {
			s.err = errors.New("conversation: server reported a failure without details")
		} else {
			s.err = fmt.Errorf("conversation: %s", p.Error)
		}

	default:
		return fmt.Errorf("conversation: unsupported event %q", event.Type)
	}
	return nil
}

func (s *conversationState) addMessage(message api.Message) error {
	if message.ID == "" || message.Role == "" {
		return errors.New("conversation: persisted messages require an ID and role")
	}
	if message.Role == api.RoleTool && message.ToolCallID == "" {
		return errors.New("conversation: tool response requires a tool call ID")
	}
	calls := make(map[string]bool)
	for _, block := range message.Content {
		if err := validateConversationBlock(block, false); err != nil {
			return err
		}
		if message.Role != api.RoleTool && block.ToolCall != nil {
			if calls[block.ToolCall.ID] {
				return fmt.Errorf("conversation: repeated tool call %q", block.ToolCall.ID)
			}
			calls[block.ToolCall.ID] = true
		}
	}
	for _, existing := range s.messages {
		if existing.message.ID == message.ID {
			if !reflect.DeepEqual(existing.message, message) {
				return fmt.Errorf("conversation: persisted message %q changed", message.ID)
			}
			if message.Role == api.RoleAssistant {
				s.discardDraft()
			}
			return nil
		}
		if message.Role == api.RoleTool && existing.message.Role == api.RoleTool &&
			existing.message.ToolCallID == message.ToolCallID {
			return fmt.Errorf("conversation: multiple responses for tool %q", message.ToolCallID)
		}
		if !existing.draft && existing.message.Role != api.RoleTool {
			for _, block := range existing.message.Content {
				if block.ToolCall != nil && calls[block.ToolCall.ID] {
					return fmt.Errorf("conversation: tool call %q belongs to multiple messages", block.ToolCall.ID)
				}
			}
		}
	}
	if message.Role == api.RoleAssistant && s.draft >= 0 {
		// Harness messages receive a new UUID after their chunks have streamed.
		record := s.messages[s.draft]
		record.id = s.messageID(message.ID, record.id)
		record.message, record.draft, record.done = cloneConversationMessage(message), false, true
		s.discardDraft()
		s.messages = append(s.messages, record)
	} else {
		s.messages = append(s.messages, conversationMessage{
			id: s.messageID(message.ID, ""), message: cloneConversationMessage(message), done: true,
		})
	}
	if message.Role == api.RoleTool {
		s.permissions = slices.DeleteFunc(s.permissions, func(request api.PermissionRequest) bool {
			return request.ToolCallID == message.ToolCallID
		})
	}
	return nil
}

func (s *conversationState) discardDraft() {
	if s.draft >= 0 {
		s.messages = slices.Delete(s.messages, s.draft, s.draft+1)
		s.draft = -1
	}
}

func (s *conversationState) addChunk(chunk api.Chunk) error {
	for _, block := range chunk.Content {
		if err := validateConversationBlock(block, true); err != nil {
			return err
		}
	}
	if s.draft < 0 {
		s.draft = len(s.messages)
		s.messages = append(s.messages, conversationMessage{
			id:      s.messageID("", ""),
			message: api.Message{Role: api.RoleAssistant}, draft: true,
		})
	}
	record := &s.messages[s.draft]
	for _, block := range chunk.Content {
		record.message.Content = appendConversationBlock(record.message.Content, block)
	}
	record.done = chunk.Done
	return nil
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

func (s *conversationState) addPermissions(requests []api.PermissionRequest) error {
	for _, request := range requests {
		if request.ID == "" || request.ToolCallID == "" {
			return errors.New("conversation: permission requires request and tool call IDs")
		}
	}
	for _, request := range requests {
		resolved := slices.ContainsFunc(s.messages, func(record conversationMessage) bool {
			return record.message.Role == api.RoleTool && record.message.ToolCallID == request.ToolCallID
		})
		if resolved {
			continue
		}
		index := slices.IndexFunc(s.permissions, func(existing api.PermissionRequest) bool {
			return existing.ID == request.ID
		})
		request.GrantPatterns = slices.Clone(request.GrantPatterns)
		if index >= 0 {
			s.permissions[index] = request
		} else {
			s.permissions = append(s.permissions, request)
		}
	}
	return nil
}

func (s *conversationState) view() Conversation {
	conversation := Conversation{Stream: Stream{Status: s.status, Error: s.err}, Error: s.err}
	for _, request := range s.permissions {
		request.GrantPatterns = slices.Clone(request.GrantPatterns)
		conversation.Permissions = append(conversation.Permissions, request)
	}
	messages := make([]Message, len(s.messages))
	tools := make(map[string]*ToolPart)
	for i, record := range s.messages {
		message := Message{
			ID: record.id, SourceID: record.message.ID, Role: record.message.Role,
			CreatedAt: record.message.CreatedAt,
			Streaming: record.draft && s.status == StreamRunning && !record.done,
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
	for i, record := range s.messages {
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
	mutableSuffix := false
	for i, message := range messages {
		if message.Role == api.RoleTool && len(message.Parts) == 0 {
			continue
		}
		mutable := s.messages[i].draft
		for j := range message.Parts {
			tool := message.Parts[j].Tool
			if tool == nil {
				continue
			}
			for _, request := range conversation.Permissions {
				if request.ToolCallID == tool.ID {
					tool.Permissions = append(tool.Permissions, request)
				}
			}
			switch {
			case tool.Response != nil && tool.Response.IsError:
				tool.Status = ToolFailed
			case tool.Response != nil:
				tool.Status = ToolSucceeded
			case s.status == StreamFailed:
				tool.Status = ToolInterrupted
			case len(tool.Permissions) > 0:
				tool.Status = ToolWaitingPermission
			case s.messages[i].draft:
				tool.Status = ToolReceiving
			case s.status == StreamRunning:
				tool.Status = ToolRunning
			default:
				tool.Status = ToolPending
			}
			mutable = mutable || tool.Call == nil || tool.Response == nil || len(tool.Permissions) > 0
		}
		mutableSuffix = mutableSuffix || mutable
		if mutableSuffix {
			conversation.Stream.Messages = append(conversation.Stream.Messages, message)
		} else {
			conversation.History = append(conversation.History, message)
		}
	}
	return conversation
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
