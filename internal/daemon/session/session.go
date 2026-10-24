package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Status represents the operational execution state of a [Session].
type Status string

const (
	StatusIdle              Status = "idle"
	StatusRunning           Status = "running"
	StatusWaitingPermission Status = "waiting_permission"
)

// SessionConfig holds configurable runtime settings for a [Session].
type SessionConfig struct {
	Agent  string               `json:"agent,omitempty"`
	Model  string               `json:"model,omitempty"`
	Effort string               `json:"effort,omitempty"`
	Mode   tools.PermissionMode `json:"mode,omitempty"`
}

// Session represents a persistent user-facing chat thread and its runtime state.
type Session struct {
	// We embed [State] here so that Session supports state fields and locking
	// directly, but marshal it as a struct in JSON.
	//
	// State also provides a [sync.Mutex] which is used as lock for both
	// the Session and State.
	*State `json:"state"`

	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Config    SessionConfig `json:"config"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`

	Root     string            `json:"-"` // Path to <WorkspaceDir>/sessions/<id>
	Sandbox  sandbox.Sandbox   `json:"-"`
	Timeline timeline.Timeline `json:"-"`
	Journal  journal.Journal   `json:"-"`
	Tools    tools.Container   `json:"-"`
	Harness  harness.Harness   `json:"-"`

	db        *sql.DB             `json:"-"`
	store     *store              `json:"-"`
	workspace workspace.Workspace `json:"-"`
	gateway   gateway.Gateway     `json:"-"`
	publisher Publisher           `json:"-"`
	logger    log.Interface       `json:"-"`
	bgCtx     context.Context     `json:"-"`
	wg        *sync.WaitGroup     `json:"-"`
}

// Clone returns a deep copy of the session and its exported execution state,
// synchronizing on the session's embedded mutex.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	if s.State != nil {
		s.Lock()
		defer s.Unlock()
	}
	cp := &Session{
		ID:        s.ID,
		Title:     s.Title,
		Config:    s.Config,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Root:      s.Root,
		Sandbox:   s.Sandbox,
		Timeline:  s.Timeline,
		Journal:   s.Journal,
		Tools:     s.Tools,
		Harness:   s.Harness,
		db:        s.db,
		store:     s.store,
		workspace: s.workspace,
		gateway:   s.gateway,
		publisher: s.publisher,
		logger:    s.logger,
		bgCtx:     s.bgCtx,
		wg:        s.wg,
	}
	if s.State != nil {
		cp.State = s.State.Clone()
	} else {
		cp.State = NewState()
	}
	return cp
}

func (s *Session) initResources(ctx context.Context) error {
	if s.Sandbox == nil {
		sb, err := sandbox.New(sandbox.Config{
			ID:      s.ID,
			WorkDir: s.workspace.RootDir(),
			Mode:    sandbox.ModeLocal,
			Env:     s.workspace.Env(),
			Network: sandbox.NetworkPolicy{
				AllowLoopback: true,
			},
		})
		if err != nil {
			return fmt.Errorf("session: initialize sandbox: %w", err)
		}
		s.Sandbox = sb
	}
	if s.Timeline == nil {
		tl, err := timeline.New(ctx, s.db, s.ID)
		if err != nil {
			return fmt.Errorf("session: initialize timeline: %w", err)
		}
		s.Timeline = tl
	}
	if s.Journal == nil {
		blobsDir := filepath.Join(s.Root, "blobs")
		jr, err := journal.New(ctx, s.db, s.ID, blobsDir)
		if err != nil {
			return fmt.Errorf("session: initialize journal: %w", err)
		}
		s.Journal = jr
	}
	if s.Tools == nil {
		stdInst, err := std.New(std.Config{
			Sandbox: s.Sandbox,
			Tracker: s.Journal,
			Logger:  s.logger,
		})
		if err != nil {
			return fmt.Errorf("session: initialize std tools: %w", err)
		}

		guardInst := shellguard.New(shellguard.Config{
			WorkspaceDir: s.workspace.RootDir(),
		})

		builtinTools, err := builtin.New(builtin.Config{
			Std:     stdInst,
			Sandbox: s.Sandbox,
			Guard:   guardInst,
		})
		if err != nil {
			return fmt.Errorf("session: initialize builtin tools: %w", err)
		}

		container, err := tools.NewContainer(tools.Config{
			Tools:     builtinTools,
			Sandbox:   s.Sandbox,
			Workspace: s.workspace,
			Logger:    s.logger,
		})
		if err != nil {
			return fmt.Errorf("session: initialize tools container: %w", err)
		}

		if s.Config.Mode != "" {
			container.SetMode(s.Config.Mode)
		}
		s.Tools = container
	}
	return nil
}

// Close closes session-scoped runtime resources like the Sandbox.
func (s *Session) Close() error {
	s.Lock()
	defer s.Unlock()
	if s.Sandbox != nil {
		err := s.Sandbox.Close()
		s.Sandbox = nil
		return err
	}
	return nil
}

func (s *Session) harness(ctx context.Context) (harness.Harness, error) {
	s.Lock()
	if s.Harness != nil {
		h := s.Harness
		s.Unlock()
		return h, nil
	}
	agentName := s.Config.Agent
	modelName := s.Config.Model
	s.Unlock()

	llmModel, err := s.gateway.ResolveLLM(ctx, modelName)
	if err != nil {
		return nil, fmt.Errorf("session: resolve model %q: %w", modelName, err)
	}

	systemPrompt := s.renderSystemPrompt(agentName)

	h, err := harness.New(harness.Config{
		ID:           s.ID,
		DB:           s.db,
		Model:        llmModel,
		Tools:        s.Tools,
		SystemPrompt: systemPrompt,
		Logger:       s.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("session: construct harness: %w", err)
	}

	s.Lock()
	s.Harness = h
	s.Unlock()

	return h, nil
}

func (s *Session) renderSystemPrompt(agentName string) string {
	wsPrompt, _ := s.workspace.RenderWorkspace(nil)

	if agentName == "" {
		agentName = s.workspace.DefaultAgentName()
	}

	globals := map[string]any{
		"HasTool": func(toolName string) bool {
			if ag, ok := s.workspace.GetAgent(agentName); ok {
				if slices.Contains(ag.Tools, toolName) {
					return true
				}
			}
			return false
		},
	}

	agentPrompt, _ := s.workspace.RenderAgent(agentName, nil, globals)

	var parts []string
	if s := strings.TrimSpace(wsPrompt); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(agentPrompt); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

// History returns the conversation messages persisted for this session.
func (s *Session) History(ctx context.Context) (message.MessageList, error) {
	if s == nil || s.Timeline == nil {
		return nil, errors.New("session: uninitialized session")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.Timeline.Messages(ctx)
}

// appendMessage appends a chat-only message directly to the session timeline.
func (s *Session) appendMessage(ctx context.Context, msg message.Message) error {
	if s == nil || s.Timeline == nil {
		return errors.New("session: uninitialized session")
	}
	if msg == nil {
		return ErrNilMessage
	}

	entry := timeline.NewMessageEntry(s.ID, msg)
	if err := s.Timeline.Append(ctx, entry); err != nil {
		return err
	}

	if err := s.store.touchSession(ctx, s.ID, entry.CreatedAt); err != nil {
		return err
	}
	s.Lock()
	s.UpdatedAt = entry.CreatedAt
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), EntryAddedEvent{
		SessionID: s.ID,
		Entry:     entry.Clone(),
	})

	return nil
}

// Cancel interrupts any active conversational turn executing on this session.
func (s *Session) Cancel(ctx context.Context) error {
	if s == nil {
		return errors.New("session: uninitialized session")
	}
	s.Lock()
	cancel := s.cancel
	done := s.done
	s.Unlock()

	if done == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Send initiates an asynchronous conversational turn with a user message or resolves
// a pending permission decision.
func (s *Session) Send(ctx context.Context, input SendInput) error {
	if s == nil {
		return errors.New("session: uninitialized session")
	}
	if input.Decision != nil && input.Message != nil {
		return fmt.Errorf("%w: cannot specify both message and decision", ErrInvalidConfig)
	}
	if input.Decision == nil && input.Message == nil {
		return ErrNilMessage
	}

	var hInput harness.Input
	if input.Decision != nil {
		s.Lock()
		if s.Running {
			s.Unlock()
			return ErrSessionRunning
		}
		s.Unlock()

		ready, err := s.recordDecision(ctx, *input.Decision)
		if err != nil || !ready {
			return err
		}

		s.Lock()
		fullDecisions := maps.Clone(s.decisions)
		clear(s.decisions)
		s.Unlock()

		hInput = harness.Input{Decisions: fullDecisions}
	} else {
		hInput = harness.Input{Message: input.Message}
	}

	s.Lock()
	if s.Running {
		s.Unlock()
		return ErrSessionRunning
	}

	runCtx, cancel := context.WithCancel(s.bgCtx)
	done := make(chan struct{})

	s.Status = StatusRunning
	s.Running = true
	s.WaitingPermission = false
	if input.Message != nil {
		s.PendingPermissions = nil
		clear(s.decisions)
	}
	s.Error = ""
	s.err = nil
	s.StartedAt = time.Now().UTC()
	s.cancel = cancel
	s.done = done
	s.Unlock()

	s.wg.Add(1)
	go func() {
		defer func() {
			s.Lock()
			if s.done == done {
				s.Running = false
				if s.Status == StatusRunning {
					s.Status = StatusIdle
				}
				s.cancel = nil
				s.done = nil
			}
			s.Unlock()
			cancel()
			close(done)
			s.wg.Done()
		}()

		h, err := s.harness(runCtx)
		if err != nil {
			s.handleAsyncError(err)
			return
		}

		sCtx := scope.With(runCtx, scope.Scope{SessionID: s.ID})
		if _, err := s.send(sCtx, h.Stream(sCtx, hInput)); err != nil {
			s.handleAsyncError(err)
		}
	}()
	return nil
}

func (s *Session) recordDecision(ctx context.Context, decision tools.PermissionDecision) (bool, error) {
	switch decision.Kind {
	case tools.DecisionAllowOnce, tools.DecisionAllowSession, tools.DecisionReject:
	default:
		return false, fmt.Errorf("%w: unsupported decision kind %q", ErrInvalidDecision, decision.Kind)
	}

	h, err := s.harness(ctx)
	if err != nil {
		return false, err
	}

	state, err := h.LoadState(ctx)
	if err != nil {
		return false, fmt.Errorf("session: load harness state for permissions: %w", err)
	}

	var matched *tools.PermissionRequest
	for i := range state.PendingPermissions {
		req := &state.PendingPermissions[i]
		if (decision.RequestID != "" && req.ID == decision.RequestID) ||
			(decision.ToolCallID != "" && req.ToolCallID == decision.ToolCallID) {
			matched = req
			break
		}
	}
	if matched == nil {
		return false, ErrPermissionNotFound
	}

	decision.RequestID = matched.ID
	decision.ToolCallID = matched.ToolCallID

	s.Lock()
	if s.decisions == nil {
		s.decisions = make(map[string]tools.PermissionDecision, len(state.PendingPermissions))
	}
	s.decisions[matched.ID] = decision

	var remaining []tools.PermissionRequest
	for _, req := range state.PendingPermissions {
		if _, ok := s.decisions[req.ID]; !ok {
			req.GrantPatterns = slices.Clone(req.GrantPatterns)
			remaining = append(remaining, req)
		}
	}
	s.PendingPermissions = remaining
	allResolved := len(remaining) == 0
	s.Unlock()

	matchedReq := *matched
	matchedReq.GrantPatterns = slices.Clone(matched.GrantPatterns)
	fullDec := decision
	entry := timeline.NewPermissionEntry(s.ID, timeline.PermissionRecord{
		Request:  matchedReq,
		Decision: fullDec,
	})
	if err := s.Timeline.Append(ctx, entry); err != nil {
		s.Lock()
		delete(s.decisions, matched.ID)
		s.Unlock()
		return false, err
	}
	if err := s.store.touchSession(ctx, s.ID, entry.CreatedAt); err != nil {
		s.Lock()
		delete(s.decisions, matched.ID)
		s.Unlock()
		return false, err
	}
	s.Lock()
	s.UpdatedAt = entry.CreatedAt
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), EntryAddedEvent{
		SessionID: s.ID,
		Entry:     entry.Clone(),
	})

	return allResolved, nil
}

func (s *Session) handleAsyncError(err error) {
	s.Lock()
	s.Running = false
	s.WaitingPermission = false
	s.PendingPermissions = nil
	s.Status = StatusIdle
	s.FinishedAt = time.Now().UTC()
	s.err = err
	if !errors.Is(err, context.Canceled) {
		s.Error = err.Error()
	}
	s.Unlock()

	if errors.Is(err, context.Canceled) {
		entry := timeline.NewInterruptedEntry(s.ID)
		if appErr := s.Timeline.Append(s.bgCtx, entry); appErr == nil {
			_ = s.store.touchSession(s.bgCtx, s.ID, entry.CreatedAt)
			s.Lock()
			s.UpdatedAt = entry.CreatedAt
			s.Unlock()
			s.publisher.Publish(api.SessionStreamTopic(s.ID), EntryAddedEvent{
				SessionID: s.ID,
				Entry:     entry.Clone(),
			})
		}
	} else {
		s.publisher.Publish(api.SessionStreamTopic(s.ID), ErrorEvent{
			SessionID: s.ID,
			Error:     err.Error(),
		})
	}
}

func (s *Session) send(ctx context.Context, stream iter.Seq2[harness.Event, error]) (*message.Assistant, error) {
	var reply *message.Assistant
	var interrupted bool
	agg := message.NewAssistantAggregator()

	for hev, err := range stream {
		if err != nil {
			if errors.Is(err, context.Canceled) {
				partialMsg, buildErr := agg.Build()
				if buildErr == nil && partialMsg != nil && len(partialMsg.Content) > 0 {
					if partialMsg.GetID() == "" || uuid.Validate(partialMsg.GetID()) != nil {
						if u, uErr := uuid.NewV7(); uErr == nil {
							partialMsg.SetID(u.String())
						}
					}
					entry := timeline.NewMessageEntry(s.ID, partialMsg)
					if appErr := s.Timeline.Append(s.bgCtx, entry); appErr == nil {
						_ = s.store.touchSession(s.bgCtx, s.ID, entry.CreatedAt)
						s.Lock()
						s.UpdatedAt = entry.CreatedAt
						s.Unlock()
						s.publisher.Publish(api.SessionStreamTopic(s.ID), EntryAddedEvent{
							SessionID: s.ID,
							Entry:     entry.Clone(),
						})
					}
				}
			}
			return nil, err
		}
		switch hev.Type {
		case harness.EventChunk:
			if hev.Chunk != nil {
				agg.Add(hev.Chunk)
				s.publisher.Publish(api.SessionStreamTopic(s.ID), ChunkEvent{
					SessionID: s.ID,
					Chunk:     message.CloneAssistantChunk(*hev.Chunk),
				})
			}

		case harness.EventMessage:
			if hev.Message != nil {
				agg = message.NewAssistantAggregator()
				entry := timeline.NewMessageEntry(s.ID, hev.Message)
				if err := s.Timeline.Append(ctx, entry); err != nil {
					return nil, err
				}
				if err := s.store.touchSession(ctx, s.ID, entry.CreatedAt); err != nil {
					return nil, err
				}
				s.Lock()
				s.UpdatedAt = entry.CreatedAt
				s.Unlock()

				s.publisher.Publish(api.SessionStreamTopic(s.ID), EntryAddedEvent{
					SessionID: s.ID,
					Entry:     entry.Clone(),
				})
			}

		case harness.EventInterrupted:
			interrupted = true
			perms := make([]tools.PermissionRequest, len(hev.Permissions))
			for i, p := range hev.Permissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}

			s.Lock()
			s.Status = StatusWaitingPermission
			s.Running = false
			s.WaitingPermission = true
			s.PendingPermissions = perms
			s.FinishedAt = time.Now().UTC()
			s.Unlock()

			s.publisher.Publish(api.SessionStreamTopic(s.ID), PermissionRequestedEvent{
				SessionID:   s.ID,
				Permissions: perms,
			})

		case harness.EventCompleted:
			if hev.Message != nil {
				if asst, ok := message.CloneMessage(hev.Message).(*message.Assistant); ok {
					reply = asst
				}
			}

			s.Lock()
			s.Status = StatusIdle
			s.Running = false
			s.WaitingPermission = false
			s.PendingPermissions = nil
			s.FinishedAt = time.Now().UTC()
			s.Unlock()

			s.publisher.Publish(api.SessionStreamTopic(s.ID), CompletedEvent{
				SessionID: s.ID,
			})
		}
	}

	if interrupted {
		return nil, nil
	}
	if reply == nil {
		return nil, fmt.Errorf("session: harness returned nil assistant reply for session %q", s.ID)
	}
	return reply, nil
}

// Wait blocks until an in-flight background turn completes or ctx cancels.
func (s *Session) Wait(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.Lock()
	done := s.done
	s.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		s.Lock()
		defer s.Unlock()
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
