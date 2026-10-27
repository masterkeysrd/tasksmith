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
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Status represents the operational execution state of a [Session].
type Status string

const (
	StatusIdle              Status = "idle"
	StatusThinking          Status = "thinking"
	StatusGenerating        Status = "generating"
	StatusExecuting         Status = "executing"
	StatusWaitingPermission Status = "waiting_permission"
	StatusWaitingQuestion   Status = "waiting_question"
)

func (s *Session) setStatus(next Status) {
	s.Lock()
	if s.Status == next {
		s.Unlock()
		return
	}
	s.Status = next
	s.Running = (next == StatusThinking || next == StatusGenerating || next == StatusExecuting)
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), StatusChangedEvent{
		SessionID: s.ID,
		Status:    next,
	})
}

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

	Permissions permissions.Permissions    `json:"-"`
	PermManager permissions.SessionManager `json:"-"`
	permRoot    permissions.Manager        `json:"-"`

	Root     string            `json:"-"` // Path to <WorkspaceDir>/sessions/<id>
	Sandbox  sandbox.Sandbox   `json:"-"`
	Timeline timeline.Timeline `json:"-"`
	Journal  journal.Journal   `json:"-"`
	Tools    tools.Container   `json:"-"`
	Tasks    tasks.Manager     `json:"-"`
	Harness  harness.Harness   `json:"-"`
	Std      *std.Std          `json:"-"`
	spiller  tools.Spiller     `json:"-"`

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
		ID:          s.ID,
		Title:       s.Title,
		Config:      s.Config,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
		Permissions: s.Permissions.Clone(),
		PermManager: s.PermManager,
		permRoot:    s.permRoot,
		Root:        s.Root,
		Sandbox:     s.Sandbox,
		Timeline:    s.Timeline,
		Journal:     s.Journal,
		Tools:       s.Tools,
		Tasks:       s.Tasks,
		Harness:     s.Harness,
		Std:         s.Std,
		spiller:     s.spiller,
		db:          s.db,
		store:       s.store,
		workspace:   s.workspace,
		gateway:     s.gateway,
		publisher:   s.publisher,
		logger:      s.logger,
		bgCtx:       s.bgCtx,
		wg:          s.wg,
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
	outputsDir := ""
	if s.Root != "" {
		outputsDir = filepath.Join(s.Root, "outputs")
	} else if wsDir, err := xdg.WorkspaceDir(s.workspace.RootDir()); err == nil && s.ID != "" {
		outputsDir = filepath.Join(wsDir, "sessions", s.ID, "outputs")
	}

	if s.Tasks == nil {
		s.Tasks = tasks.New(tasks.Config{
			Context:    s.bgCtx,
			OutputsDir: outputsDir,
			OnUpdate:   s.onTaskUpdate,
		})
	}

	if s.Std == nil {
		stdInst, err := std.New(std.Config{
			Sandbox: s.Sandbox,
			Tracker: s.Journal,
			Logger:  s.logger,
		})
		if err != nil {
			return fmt.Errorf("session: initialize std tools: %w", err)
		}
		s.Std = stdInst
	}

	if s.spiller == nil {
		sp, err := tools.NewSpiller(tools.SpillerConfig{Workspace: s.workspace})
		if err != nil {
			return fmt.Errorf("session: initialize spiller: %w", err)
		}
		s.spiller = sp
	}

	if s.Tools == nil {
		var readDirs []string
		if outputsDir != "" {
			readDirs = []string{outputsDir}
		}

		guardInst := shellguard.New(shellguard.Config{
			WorkspaceDir: s.workspace.RootDir(),
			ReadDirs:     readDirs,
		})

		builtinTools, err := builtin.New(builtin.Config{
			Std:     s.Std,
			Sandbox: s.Sandbox,
			Guard:   guardInst,
			Tasks:   s.Tasks,
		})
		if err != nil {
			return fmt.Errorf("session: initialize builtin tools: %w", err)
		}

		container, err := tools.NewContainer(tools.Config{
			Tools:     builtinTools,
			Sandbox:   s.Sandbox,
			Workspace: s.workspace,
			Logger:    s.logger,
			Spiller:   s.spiller,
		})
		if err != nil {
			return fmt.Errorf("session: initialize tools container: %w", err)
		}

		s.Tools = container
	}
	s.PermManager = s.permRoot.Attach(s.Config.Mode, s.Permissions, func(p permissions.Permissions) error {
		s.Lock()
		s.Permissions = p.Clone()
		s.Unlock()
		return s.store.updateSessionPermissions(s.bgCtx, s.ID, p)
	})
	return nil
}

// Close closes session-scoped runtime resources like the Sandbox.
func (s *Session) Close() error {
	s.Lock()
	defer s.Unlock()
	if s.Tasks != nil {
		_ = s.Tasks.Close()
		s.Tasks = nil
	}
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
		Bash:         s.Std,
		Spiller:      s.spiller,
		Permissions:  s.PermManager,
		SystemPrompt: systemPrompt,
		Queue:        s,
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.Timeline.Messages(ctx)
}

// appendMessage appends a chat-only message directly to the session timeline.
func (s *Session) appendMessage(ctx context.Context, msg message.Message) error {
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
	s.queue = nil
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

func (s *Session) VisibleQueue() []QueueItem {
	s.Lock()
	defer s.Unlock()
	return s.visibleQueueLocked()
}

func (s *Session) visibleQueueLocked() []QueueItem {
	if len(s.queue) == 0 {
		return nil
	}
	var vis []QueueItem
	for _, it := range s.queue {
		if it.Kind == QueueKindMessage || it.Kind == QueueKindCommand {
			vis = append(vis, it)
		}
	}
	return vis
}

func (s *Session) enqueueItem(item QueueItem) {
	if item.ID == "" || uuid.Validate(item.ID) != nil {
		if u, err := uuid.NewV7(); err == nil {
			item.ID = u.String()
		}
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}

	var vis []QueueItem
	s.Lock()
	s.queue = append(s.queue, item)
	if item.Kind != QueueKindTask {
		vis = s.visibleQueueLocked()
	}
	s.Unlock()

	if item.Kind != QueueKindTask {
		s.publisher.Publish(api.SessionStreamTopic(s.ID), QueueUpdatedEvent{
			SessionID: s.ID,
			Queue:     vis,
		})
	}
}

func (s *Session) Peek() (QueueItem, bool) {
	s.Lock()
	defer s.Unlock()
	if len(s.queue) == 0 {
		return QueueItem{}, false
	}
	return s.queue[0], true
}

func (s *Session) PopCommand() (QueueItem, bool) {
	s.Lock()
	if len(s.queue) == 0 || s.queue[0].Kind != QueueKindCommand {
		s.Unlock()
		return QueueItem{}, false
	}
	item := s.queue[0]
	s.queue = s.queue[1:]
	vis := s.visibleQueueLocked()
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), QueueUpdatedEvent{
		SessionID: s.ID,
		Queue:     vis,
	})
	return item, true
}

func (s *Session) PopMessages() []message.Message {
	s.Lock()
	if len(s.queue) == 0 {
		s.Unlock()
		return nil
	}

	var popped []QueueItem
	var hadMessage bool
	for len(s.queue) > 0 {
		if s.queue[0].Kind == QueueKindCommand {
			break
		}
		item := s.queue[0]
		s.queue = s.queue[1:]
		if item.Kind == QueueKindMessage {
			hadMessage = true
		}
		popped = append(popped, item)
	}

	var vis []QueueItem
	if hadMessage {
		vis = s.visibleQueueLocked()
	}
	s.Unlock()

	if hadMessage {
		s.publisher.Publish(api.SessionStreamTopic(s.ID), QueueUpdatedEvent{
			SessionID: s.ID,
			Queue:     vis,
		})
	}

	if len(popped) == 0 {
		return nil
	}

	msgs := make([]message.Message, 0, len(popped))
	for _, it := range popped {
		switch it.Kind {
		case QueueKindMessage:
			if p, ok := it.Payload.(QueueMessagePayload); ok && p.Message != nil {
				msgs = append(msgs, p.Message)
			}
		case QueueKindTask:
			if p, ok := it.Payload.(QueueTaskPayload); ok && p.Message != nil {
				msgs = append(msgs, p.Message)
			}
		}
	}
	return msgs
}

// Send initiates an asynchronous conversational turn with a user message or resolves
// a pending permission decision.
func (s *Session) Send(ctx context.Context, input SendInput) error {
	if s == nil {
		return errors.New("session: uninitialized session")
	}
	if cmd := strings.TrimSpace(input.Command); cmd != "" {
		if input.Decision != nil || input.Question != nil || input.Message != nil {
			return fmt.Errorf("%w: cannot specify command with message or decision", ErrInvalidConfig)
		}
		s.enqueueItem(QueueItem{
			ID:      input.ID,
			Kind:    QueueKindCommand,
			Payload: QueueCommandPayload{Command: cmd},
		})
		s.Lock()
		running := s.Running
		s.Unlock()
		if running {
			return nil
		}
		return s.startTurn(harness.Input{})
	}
	if (input.Decision != nil && input.Message != nil) || (input.Question != nil && (input.Message != nil || input.Decision != nil)) {
		return fmt.Errorf("%w: cannot combine message, decision, or question", ErrInvalidConfig)
	}
	if input.Decision == nil && input.Question == nil && input.Message == nil {
		return ErrNilMessage
	}

	if input.Decision != nil || input.Question != nil {
		s.Lock()
		if s.Running {
			s.Unlock()
			return ErrSessionRunning
		}
		s.Unlock()

		var ready bool
		var err error
		if input.Decision != nil {
			ready, err = s.recordDecision(ctx, *input.Decision)
		} else {
			ready, err = s.recordQuestion(ctx, *input.Question)
		}
		if err != nil || !ready {
			return err
		}

		s.Lock()
		fullDecisions := maps.Clone(s.decisions)
		fullAnswers := maps.Clone(s.answers)
		clear(s.decisions)
		clear(s.answers)
		s.Unlock()

		return s.startTurn(harness.Input{Decisions: fullDecisions, Answers: fullAnswers})
	}

	s.Lock()
	if strings.TrimSpace(s.Title) == "" && !s.titleGenerating {
		text := input.Message.GetContent().Text()
		if text != "" {
			s.titleGenerating = true
			s.wg.Go(func() {
				s.generateTitle(text)
			})
		}
	}
	running := s.Running
	s.Unlock()

	if running {
		s.enqueueItem(QueueItem{
			ID:      input.Message.GetID(),
			Kind:    QueueKindMessage,
			Payload: QueueMessagePayload{Message: message.CloneMessage(input.Message)},
		})
		return nil
	}

	return s.startTurn(harness.Input{Message: input.Message})
}

func (s *Session) startTurn(hInput harness.Input) error {
	s.Lock()
	if s.Running {
		s.Unlock()
		return ErrSessionRunning
	}

	runCtx, cancel := context.WithCancel(s.bgCtx)
	done := make(chan struct{})

	initialStatus := StatusThinking
	if hInput.Message == nil && hInput.Decisions == nil && len(s.queue) > 0 && s.queue[0].Kind == QueueKindCommand {
		initialStatus = StatusExecuting
	}

	s.Status = initialStatus
	s.Running = true
	s.WaitingPermission = false
	s.WaitingQuestion = false
	if hInput.Message != nil {
		s.PendingPermissions = nil
		s.PendingQuestions = nil
		clear(s.decisions)
		clear(s.answers)
	}
	s.Error = ""
	s.err = nil
	s.StartedAt = time.Now().UTC()
	s.cancel = cancel
	s.done = done
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), StatusChangedEvent{
		SessionID: s.ID,
		Status:    initialStatus,
	})

	s.wg.Go(func() {
		defer func() {
			var statusChanged bool
			s.Lock()
			if s.done == done {
				s.Running = false
				if s.Status != StatusWaitingPermission && s.Status != StatusWaitingQuestion && s.Status != StatusIdle {
					s.Status = StatusIdle
					statusChanged = true
				}
				s.cancel = nil
				s.done = nil
			}
			s.Unlock()
			if statusChanged {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), StatusChangedEvent{
					SessionID: s.ID,
					Status:    StatusIdle,
				})
			}
			cancel()
			close(done)
		}()

		h, err := s.harness(runCtx)
		if err != nil {
			s.handleAsyncError(err)
			return
		}

		sCtx := scope.With(runCtx, scope.Scope{SessionID: s.ID})
		if _, err := s.send(sCtx, h.Stream(sCtx, hInput)); err != nil {
			s.handleAsyncError(err)
			return
		}

		for {
			s.Lock()
			hasMore := !s.WaitingPermission && !s.WaitingQuestion && runCtx.Err() == nil && len(s.queue) > 0
			s.Unlock()
			if !hasMore {
				break
			}

			if _, err := s.send(sCtx, h.Stream(sCtx, harness.Input{})); err != nil {
				s.handleAsyncError(err)
				return
			}
		}

		s.Lock()
		waiting := s.WaitingPermission || s.WaitingQuestion
		s.Unlock()

		if !waiting && runCtx.Err() == nil {
			s.setStatus(StatusIdle)
			s.publisher.Publish(api.SessionStreamTopic(s.ID), CompletedEvent{
				SessionID: s.ID,
			})
		}
	})
	return nil
}

// Enqueue adds a message to the session's execution queue or initiates an immediate turn if idle.
func (s *Session) Enqueue(msg message.Message) {
	if msg == nil {
		return
	}
	var item QueueItem
	if harness.SystemKindOf(msg) == harness.SystemKindTask {
		item = QueueItem{
			ID:      msg.GetID(),
			Kind:    QueueKindTask,
			Payload: QueueTaskPayload{Message: message.CloneMessage(msg)},
		}
	} else {
		item = QueueItem{
			ID:      msg.GetID(),
			Kind:    QueueKindMessage,
			Payload: QueueMessagePayload{Message: message.CloneMessage(msg)},
		}
	}
	s.enqueueItem(item)

	s.Lock()
	canStart := !s.Running && !s.WaitingPermission && !s.WaitingQuestion
	s.Unlock()
	if canStart {
		_ = s.startTurn(harness.Input{})
	}
}

func (s *Session) onTaskUpdate(snap tasks.Snapshot) {
	s.Lock()
	if s.Tasks != nil {
		s.State.Tasks = s.Tasks.List()
	}
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), TaskUpdatedEvent{
		SessionID: s.ID,
		Task:      snap,
	})

	if snap.Status == tasks.StatusCompleted || snap.Status == tasks.StatusFailed {
		var tail string
		if s.Tasks != nil {
			if t, ok := s.Tasks.Get(snap.ID); ok {
				tail, _ = t.Tail(100)
			}
		}
		text := fmt.Sprintf("<background_task_completed task_id=%q kind=%q title=%q status=%q exit_code=\"%d\" log_path=%q>\n%s\n</background_task_completed>",
			snap.ID, snap.Kind, snap.Title, snap.Status, snap.ExitCode, snap.LogPath, tail)
		s.Enqueue(message.NewUserTextMeta(text, map[string]any{harness.MetaSystemKind: harness.SystemKindTask}))
	}
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
	allResolved := len(remaining) == 0 && len(s.PendingQuestions) == 0
	s.WaitingPermission = len(remaining) > 0
	if !s.WaitingPermission && s.WaitingQuestion {
		s.Status = StatusWaitingQuestion
	}
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

func (s *Session) recordQuestion(ctx context.Context, response tools.QuestionResponse) (bool, error) {
	h, err := s.harness(ctx)
	if err != nil {
		return false, err
	}
	state, err := h.LoadState(ctx)
	if err != nil {
		return false, err
	}
	if len(state.PendingQuestions) == 0 {
		return false, ErrNoPendingQuestions
	}
	var matched *tools.QuestionRequest
	for i := range state.PendingQuestions {
		req := &state.PendingQuestions[i]
		if (response.RequestID != "" && response.RequestID == req.ID) || (response.ToolCallID != "" && response.ToolCallID == req.ToolCallID) {
			matched = req
			break
		}
	}
	if matched == nil {
		return false, ErrQuestionNotFound
	}
	if !response.Canceled && len(response.Answers) != len(matched.Questions) {
		return false, ErrInvalidQuestion
	}
	for i, answer := range response.Answers {
		if i >= len(matched.Questions) || answer.Question != matched.Questions[i].Question {
			return false, ErrInvalidQuestion
		}
	}
	response = *response.Clone()
	response.RequestID, response.ToolCallID = matched.ID, matched.ToolCallID
	entry := timeline.NewQuestionEntry(s.ID, timeline.QuestionRecord{Request: *matched.Clone(), Response: response})
	if err := s.Timeline.Append(ctx, entry); err != nil {
		return false, err
	}
	if err := s.store.touchSession(ctx, s.ID, entry.CreatedAt); err != nil {
		return false, err
	}
	s.Lock()
	if s.answers == nil {
		s.answers = make(map[string]tools.QuestionResponse)
	}
	s.answers[matched.ID] = response
	var remaining []tools.QuestionRequest
	for i := range state.PendingQuestions {
		if _, ok := s.answers[state.PendingQuestions[i].ID]; !ok {
			remaining = append(remaining, *state.PendingQuestions[i].Clone())
		}
	}
	s.PendingQuestions = remaining
	s.WaitingQuestion = len(remaining) > 0
	s.UpdatedAt = entry.CreatedAt
	ready := len(remaining) == 0 && len(s.PendingPermissions) == 0
	s.Unlock()
	s.publisher.Publish(api.SessionStreamTopic(s.ID), EntryAddedEvent{SessionID: s.ID, Entry: entry.Clone()})
	return ready, nil
}

func (s *Session) handleAsyncError(err error) {
	s.Lock()
	s.Running = false
	s.WaitingPermission = false
	s.PendingPermissions = nil
	s.WaitingQuestion = false
	s.PendingQuestions = nil
	s.queue = nil
	s.Status = StatusIdle
	s.FinishedAt = time.Now().UTC()
	s.err = err
	if !errors.Is(err, context.Canceled) {
		s.Error = err.Error()
	}
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), StatusChangedEvent{
		SessionID: s.ID,
		Status:    StatusIdle,
	})

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
		s.logger.Error("session turn failed", log.String("session_id", s.ID), log.String("error", err.Error()))
		s.publisher.Publish(api.SessionStreamTopic(s.ID), ErrorEvent{
			SessionID: s.ID,
			Error:     err.Error(),
		})
	}
}

func (s *Session) send(ctx context.Context, stream iter.Seq2[harness.Event, error]) (*message.Assistant, error) {
	var reply *message.Assistant
	var interrupted bool
	var pendingTools int
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
				hasGenerating := false
				hasThinking := false
				for _, b := range hev.Chunk.Content {
					switch b.(type) {
					case *message.TextBlock, *message.ToolCall, *message.ToolCallChunk:
						hasGenerating = true
					case *message.ThinkingBlock:
						hasThinking = true
					}
				}
				if hasGenerating {
					s.setStatus(StatusGenerating)
				} else if hasThinking {
					s.setStatus(StatusThinking)
				}
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

				if asst, ok := hev.Message.(*message.Assistant); ok {
					if len(asst.ToolCalls()) > 0 {
						pendingTools = len(asst.ToolCalls())
						s.setStatus(StatusExecuting)
					}
				} else if _, ok := hev.Message.(*message.Tool); ok {
					if pendingTools > 0 {
						pendingTools--
						if pendingTools == 0 {
							s.setStatus(StatusThinking)
						}
					} else {
						s.setStatus(StatusThinking)
					}
				}
			}

		case harness.EventCommandStarted:
			if hev.CommandStarted != nil {
				s.setStatus(StatusExecuting)
				s.publisher.Publish(api.SessionStreamTopic(s.ID), CommandStartedEvent{
					SessionID: s.ID,
					ID:        hev.CommandStarted.ID,
					Command:   hev.CommandStarted.Command,
				})
			}

		case harness.EventCommandCompleted:
			if hev.CommandCompleted != nil {
				entry := timeline.NewCommandEntry(s.ID, timeline.CommandRecord{
					Command:    hev.CommandCompleted.Command,
					Output:     hev.CommandCompleted.Output,
					ExitCode:   hev.CommandCompleted.ExitCode,
					DurationMs: hev.CommandCompleted.DurationMs,
				})
				if hev.CommandCompleted.ID != "" && uuid.Validate(hev.CommandCompleted.ID) == nil {
					entry.ID = hev.CommandCompleted.ID
				}
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

			questions := make([]tools.QuestionRequest, len(hev.Questions))
			for i := range hev.Questions {
				questions[i] = *hev.Questions[i].Clone()
			}
			nextStatus := StatusWaitingQuestion
			if len(perms) > 0 {
				nextStatus = StatusWaitingPermission
			}
			s.Lock()
			s.Status = nextStatus
			s.Running = false
			s.WaitingPermission = len(perms) > 0
			s.PendingPermissions = perms
			s.WaitingQuestion = len(questions) > 0
			s.PendingQuestions = questions
			s.FinishedAt = time.Now().UTC()
			s.Unlock()

			s.publisher.Publish(api.SessionStreamTopic(s.ID), StatusChangedEvent{
				SessionID: s.ID,
				Status:    nextStatus,
			})
			if len(perms) > 0 {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), PermissionRequestedEvent{SessionID: s.ID, Permissions: perms})
			}
			if len(questions) > 0 {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), QuestionRequestedEvent{SessionID: s.ID, Questions: questions})
			}

		case harness.EventTodosUpdated:
			s.Lock()
			s.Todos = slices.Clone(hev.Todos)
			s.Unlock()

			s.publisher.Publish(api.SessionStreamTopic(s.ID), TodosUpdatedEvent{
				SessionID: s.ID,
				Todos:     slices.Clone(hev.Todos),
			})

		case harness.EventCompleted:
			if hev.Message != nil {
				if asst, ok := message.CloneMessage(hev.Message).(*message.Assistant); ok {
					reply = asst
				}
			}
		}
	}

	if interrupted {
		return nil, nil
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

func (s *Session) generateTitle(userText string) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Second)
	defer cancel()

	sysPrompt, err := s.workspace.RenderAgent(workspace.AgentNameTitleGenerator, nil, nil)
	if err != nil || strings.TrimSpace(sysPrompt) == "" {
		if err != nil {
			s.logger.Warn("failed to render title generator prompt", log.String("error", err.Error()))
		}
		return
	}

	ag, ok := s.workspace.GetAgent(workspace.AgentNameTitleGenerator)
	var modelName string
	if ok && len(ag.Models) > 0 {
		models := s.workspace.ResolveModels(workspace.RoleCompletion, ag.Models...)
		if len(models) > 0 {
			modelName = models[0]
		} else {
			modelName = ag.Models[0]
		}
	}
	if modelName == "" {
		s.Lock()
		modelName = s.Config.Model
		s.Unlock()
	}

	var opts []gateway.LLMOption
	if ok && ag.Temperature != nil {
		t32 := float32(*ag.Temperature)
		opts = append(opts, gateway.WithLLMConfig(&llm.ModelConfig{Temperature: &t32}))
	}

	llmModel, err := s.gateway.ResolveLLM(ctx, modelName, opts...)
	if err != nil {
		s.logger.Warn("failed to resolve LLM for title generation", log.String("error", err.Error()))
		return
	}

	msgs := []message.Message{
		message.NewSystemText(strings.TrimSpace(sysPrompt)),
		message.NewUserText(userText),
	}
	resp, err := llmModel.Invoke(ctx, msgs)
	if err != nil || resp == nil {
		if err != nil {
			s.logger.Warn("failed to invoke title generator model", log.String("error", err.Error()))
		}
		return
	}

	textContent := resp.GetContent().Text()

	var title string
	for line := range strings.SplitSeq(textContent, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "\"'`")
		line = strings.TrimSpace(line)
		if line != "" {
			title = line
			break
		}
	}

	runes := []rune(title)
	if len(runes) > 50 {
		title = strings.TrimSpace(string(runes[:50]))
	}

	if title == "" {
		return
	}

	s.Lock()
	if strings.TrimSpace(s.Title) != "" {
		s.Unlock()
		return
	}

	now := time.Now().UTC()
	dbSess := &Session{
		ID:          s.ID,
		Title:       title,
		Config:      s.Config,
		Permissions: s.Permissions.Clone(),
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   now,
	}
	if s.store != nil {
		if err := s.store.updateSession(ctx, dbSess); err != nil {
			s.Unlock()
			s.logger.Warn("failed to persist generated title", log.String("error", err.Error()))
			return
		}
	}
	s.Title = title
	s.UpdatedAt = now
	s.Unlock()

	cloned := s.Clone()
	var wsRoot string
	if s.workspace != nil {
		wsRoot = s.workspace.RootDir()
	}
	s.publisher.Publish(api.TopicSessions, UpdatedEvent{
		Workspace: wsRoot,
		Session:   cloned,
	})
}
