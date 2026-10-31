package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// Status represents the operational execution state of a [Session].
type Status string

const (
	StatusIdle              Status = "idle"
	StatusThinking          Status = "thinking"
	StatusGenerating        Status = "generating"
	StatusExecuting         Status = "executing"
	StatusCompacting        Status = "compacting"
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
	s.Running = (next == StatusThinking || next == StatusGenerating || next == StatusExecuting || next == StatusCompacting)
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    api.SessionStatus(next),
	})
}

// SessionConfig holds configurable runtime settings for a [Session].
type SessionConfig struct {
	Agent    string               `json:"agent,omitempty"`
	Model    models.ID            `json:"model,omitempty"`
	Thinking *api.ThinkingSetting `json:"thinking,omitempty"`
	Mode     api.PermissionMode   `json:"mode,omitempty"`
}

// Session represents a persistent user-facing chat thread and its runtime state.
type Session struct {
	// We embed [State] here so that Session supports state fields and locking
	// directly, but marshal it as a struct in JSON.
	//
	// State also provides a [sync.Mutex] which is used as lock for both
	// the Session and State.
	*State `json:"state"`

	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Config      SessionConfig          `json:"config"`
	Diagnostics []api.SessionDiagnostic `json:"diagnostics,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`

	Permissions permissions.Permissions    `json:"-"`
	PermManager permissions.SessionManager `json:"-"`
	permRoot    permissions.Manager        `json:"-"`

	Root             string            `json:"-"` // Path to <WorkspaceDir>/sessions/<id>
	Sandbox          sandbox.Sandbox   `json:"-"`
	Timeline         timeline.Timeline `json:"-"`
	Journal          journal.Journal   `json:"-"`
	Tools            tools.Container   `json:"-"`
	Tasks            tasks.Manager     `json:"-"`
	Harness          harness.Harness   `json:"-"`
	Std              *std.Std          `json:"-"`
	spiller          tools.Spiller     `json:"-"`
	configGeneration uint64

	// turnIDs and turnStart track the turn in progress: the user messages that
	// started it and when the first of them was recorded. Guarded by the
	// session lock.
	turnIDs   []string
	turnStart time.Time

	db         *sql.DB               `json:"-"`
	store      *store                `json:"-"`
	workspace  *workspace.Workspace  `json:"-"`
	gateway    func() gateway.Gateway `json:"-"`
	catalog    func() *models.Catalog `json:"-"`
	metrics    metrics.Service       `json:"-"`
	reloadLock *sync.RWMutex         `json:"-"`
	publisher  Publisher             `json:"-"`
	logger     log.Interface         `json:"-"`
	bgCtx      context.Context       `json:"-"`
	wg         *sync.WaitGroup       `json:"-"`
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
		Diagnostics: slices.Clone(s.Diagnostics),
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
		catalog:     s.catalog,
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

func (s *Session) ResolvedModel() (models.Model, *api.ThinkingSetting, bool) {
	if !s.Config.Model.Valid() {
		return models.Model{}, nil, false
	}
	m, ok := s.catalog().Get(s.Config.Model)
	if !ok {
		return models.Model{}, nil, false
	}
	var thinking *api.ThinkingSetting
	if s.workspace != nil && s.workspace.Catalog() != nil {
		if agent, found := s.workspace.Catalog().GetAgent(s.Config.Agent); found {
			thinking = agent.Thinking
		}
	}
	return m, thinking, true
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
		jr, err := journal.New(ctx, s.db, s.ID, blobsDir, journal.WithMetrics(s.metrics))
		if err != nil {
			return fmt.Errorf("session: initialize journal: %w", err)
		}
		s.Journal = jr
	}
	outputsDir := s.outputsDir()
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
		initialAgent := s.Config.Agent
		if initialAgent == "" && s.workspace != nil && s.workspace.Catalog() != nil {
			initialAgent = s.workspace.Catalog().DefaultAgentName()
		}
		container, err := s.buildToolsForAgent(initialAgent)
		if err != nil {
			return err
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
	if s.reloadLock != nil {
		s.reloadLock.RLock()
		defer s.reloadLock.RUnlock()
	}
	return s.buildHarness(ctx)
}

func (s *Session) buildHarness(ctx context.Context) (harness.Harness, error) {
	s.Lock()
	if s.Harness != nil {
		h := s.Harness
		s.Unlock()
		return h, nil
	}
	config := s.Config
	generation := s.configGeneration
	agentName := config.Agent
	modelName := config.Model
	s.Unlock()

	model, found := s.catalog().Get(modelName)
	if !found {
		return nil, fmt.Errorf("session: model %q unavailable", modelName)
	}
	settings := s.modelSettings(config)
	cfg, _, err := model.Configure(settings)
	if err != nil {
		return nil, fmt.Errorf("session: configure model %q: %w", modelName, err)
	}
	llmModel, err := s.gateway().LLM(ctx, model, cfg)
	limits := model.Limits
	if err != nil {
		return nil, fmt.Errorf("session: resolve model %q: %w", modelName, err)
	}

	systemPrompt := s.renderSystemPrompt(agentName)
	var agentBreakdown api.ContextBreakdown
	if s.workspace != nil && s.workspace.Catalog() != nil {
		agentBreakdown = s.workspace.Catalog().AgentContext(agentName)
	}
	staticCtx := harness.StaticContextFromBreakdown(agentBreakdown)

	container, err := s.buildToolsForAgent(agentName)
	if err != nil {
		return nil, fmt.Errorf("session: build tools for agent %q: %w", agentName, err)
	}

	h, err := harness.New(harness.Config{
		ID:           s.ID,
		DB:           s.db,
		Model:        llmModel,
		Tools:        container,
		Bash:         s.Std,
		Spiller:      s.spiller,
		Permissions:  s.PermManager,
		SystemPrompt: systemPrompt,
		Queue:        s,
		Logger:       s.logger,
		Limits:       limits,
		Static:       staticCtx,
		Metrics:      s.metrics,
	})
	if err != nil {
		return nil, fmt.Errorf("session: construct harness: %w", err)
	}

	s.Lock()
	if generation != s.configGeneration {
		s.Unlock()
		return s.buildHarness(ctx)
	}
	s.Tools = container
	s.Harness = h
	s.Unlock()

	return h, nil
}

func (s *Session) renderSystemPrompt(agentName string) string {
	var wsPrompt string
	if s.workspace != nil && s.workspace.Catalog() != nil {
		if wsSpec := s.workspace.Catalog().WorkspaceSpec(); wsSpec != nil {
			wsPrompt, _ = wsSpec.Render(&warp.WorkspaceRenderOptions{})
		}
	}

	if agentName == "" && s.workspace != nil && s.workspace.Catalog() != nil {
		agentName = s.workspace.Catalog().DefaultAgentName()
	}

	globals := map[string]any{
		"HasTool": func(toolName string) bool {
			if s.workspace != nil && s.workspace.Catalog() != nil {
				if resolved, err := s.workspace.Catalog().ResolveAgent(agentName); err == nil && resolved != nil {
					for _, t := range resolved.Tools {
						if strings.EqualFold(t.Metadata.Name, toolName) {
							return true
						}
					}
				}
			}
			return false
		},
	}

	var agentPrompt string
	if s.workspace != nil && s.workspace.Catalog() != nil {
		if resolved, err := s.workspace.Catalog().ResolveAgent(agentName); err == nil && resolved != nil && resolved.Agent != nil {
			agentPrompt, _ = resolved.Agent.Render(&warp.AgentRenderOptions{
				Workspace: s.workspace.Catalog().WorkspaceSpec(),
				Project:   s.workspace.Catalog().Project(),
				Resolved:  resolved,
				Globals:   globals,
			})
		}
	}

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

	publishEntryAdded(s.publisher, s.ID, entry)

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
	if item.ID == "" && !uuid.Validate(item.ID) {
		item.ID = uuid.NewString()
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
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QueueUpdatedEvent{
			SessionID: s.ID,
			Queue:     toAPIQueueItems(vis),
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

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QueueUpdatedEvent{
		SessionID: s.ID,
		Queue:     toAPIQueueItems(vis),
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
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QueueUpdatedEvent{
			SessionID: s.ID,
			Queue:     toAPIQueueItems(vis),
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
	if input.Compact != nil {
		if input.Decision != nil || input.Question != nil || input.Message != nil || strings.TrimSpace(input.Command) != "" {
			return fmt.Errorf("%w: cannot specify compact with other input", ErrInvalidConfig)
		}
		s.Lock()
		if s.Running {
			s.Unlock()
			return ErrSessionRunning
		}
		s.Unlock()
		return s.startTurn(harness.Input{Compact: &harness.CompactInput{Focus: input.Compact.Focus}})
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

	msg := input.Message
	if msg != nil {
		renderedMsg, err := s.resolveSlashCommand(msg)
		if err != nil {
			return err
		}
		msg = renderedMsg
	}

	s.Lock()
	if strings.TrimSpace(s.Title) == "" && !s.titleGenerating {
		text := msg.GetContent().Text()
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
			ID:      msg.GetID(),
			Kind:    QueueKindMessage,
			Payload: QueueMessagePayload{Message: message.CloneMessage(msg)},
		})
		return nil
	}

	return s.startTurn(harness.Input{Message: msg})
}

func (s *Session) outputsDir() string {
	if s.Root != "" {
		return filepath.Join(s.Root, "outputs")
	}
	if s.workspace != nil && s.ID != "" {
		if wsDir, err := xdg.WorkspaceDir(s.workspace.RootDir()); err == nil {
			return filepath.Join(wsDir, "sessions", s.ID, "outputs")
		}
	}
	return ""
}

func (s *Session) buildToolsForAgent(agentName string) (tools.Container, error) {
	if agentName == "" && s.workspace != nil && s.workspace.Catalog() != nil {
		agentName = s.workspace.Catalog().DefaultAgentName()
	}

	var allowedToolNames []string
	var skills []warp.Skill

	if s.workspace != nil && s.workspace.Catalog() != nil && agentName != "" {
		if resolved, err := s.workspace.Catalog().ResolveAgent(agentName); err == nil && resolved != nil {
			if resolved.Tools != nil {
				allowedToolNames = make([]string, 0, len(resolved.Tools))
				for _, t := range resolved.Tools {
					allowedToolNames = append(allowedToolNames, t.Metadata.Name)
				}
			}
			skills = resolved.Skills
		}
	}

	var readDirs []string
	if outputsDir := s.outputsDir(); outputsDir != "" {
		readDirs = append(readDirs, outputsDir)
	}
	if builtinDir, err := xdg.BuiltinDir(); err == nil && builtinDir != "" {
		readDirs = append(readDirs, builtinDir)
	}

	guardInst := shellguard.New(shellguard.Config{
		WorkspaceDir: s.workspace.RootDir(),
		ReadDirs:     readDirs,
	})

	builtinTools, err := builtin.New(builtin.Config{
		Std:          s.Std,
		Sandbox:      s.Sandbox,
		Guard:        guardInst,
		Tasks:        s.Tasks,
		Workspace:    s.workspace,
		AllowedTools: allowedToolNames,
		Skills:       skills,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize builtin tools: %w", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:        builtinTools,
		Sandbox:      s.Sandbox,
		Workspace:    s.workspace,
		Logger:       s.logger,
		Spiller:      s.spiller,
		AllowedTools: allowedToolNames,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize tools container: %w", err)
	}

	return container, nil
}

func parseSlashCommandArgs(argsDef []warp.Argument, rawArgs []string) map[string]string {
	argsMap := make(map[string]string)
	var positional []string
	for _, arg := range rawArgs {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			k := strings.TrimPrefix(parts[0], "--")
			argsMap[k] = parts[1]
		} else {
			positional = append(positional, arg)
		}
	}
	for i, p := range positional {
		argsMap[fmt.Sprintf("%d", i+1)] = p
		if i < len(argsDef) {
			if _, exists := argsMap[argsDef[i].Name]; !exists {
				argsMap[argsDef[i].Name] = p
			}
		}
	}
	return argsMap
}

func (s *Session) resolveSlashCommand(msg message.Message) (message.Message, error) {
	if msg == nil {
		return nil, nil
	}
	cnt := msg.GetContent()
	text := strings.TrimSpace(cnt.Text())
	if !strings.HasPrefix(text, "/") || len(cnt) > 1 {
		return msg, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return msg, nil
	}

	rawKey := strings.TrimPrefix(fields[0], "/")
	cmdKey := strings.ToLower(rawKey)
	rawArgs := fields[1:]

	if s.workspace != nil && s.workspace.Catalog() != nil {
		// 1. Try resolving as Command
		if cmd, ok := s.workspace.Catalog().GetCommand(cmdKey); ok && cmd.Def != nil {
			argsMap := parseSlashCommandArgs(cmd.Def.Spec.Arguments, rawArgs)
			mergedGlobals := workspace.MergeStringArgs(argsMap, nil)
			positionalArgs := workspace.BuildPositionalArgs(cmd.Def.Spec.Arguments, argsMap)
			rendered, err := cmd.Def.Render(&warp.CommandRenderOptions{
				Workspace: s.workspace.Catalog().WorkspaceSpec(),
				Project:   s.workspace.Catalog().Project(),
				Args:      positionalArgs,
				Globals:   mergedGlobals,
			})
			if err != nil {
				return msg, fmt.Errorf("failed to render command %q: %w", cmd.Name, err)
			}
			newMsg := message.NewUser(&message.TextBlock{Text: rendered})
			if msg.GetID() != "" {
				newMsg.SetID(msg.GetID())
			}
			return newMsg, nil
		}

		// 2. Try resolving as Skill
		if sk, ok := s.workspace.Catalog().GetSkill(cmdKey); ok && sk.Def != nil {
			argsMap := parseSlashCommandArgs(sk.Def.Spec.Arguments, rawArgs)
			mergedGlobals := workspace.MergeStringArgs(argsMap, nil)
			positionalArgs := workspace.BuildPositionalArgs(sk.Def.Spec.Arguments, argsMap)
			rendered, err := sk.Def.Render(&warp.SkillRenderOptions{
				Workspace: s.workspace.Catalog().WorkspaceSpec(),
				Project:   s.workspace.Catalog().Project(),
				Args:      positionalArgs,
				Globals:   mergedGlobals,
			})
			if err != nil {
				return msg, fmt.Errorf("failed to render skill %q: %w", sk.Name, err)
			}

			skillPath := s.workspace.PortablePath(sk.Def.Directory)
			pathAttr := ""
			pathNote := ""
			if skillPath != "" {
				pathAttr = fmt.Sprintf(" path=%q", skillPath)
				pathNote = fmt.Sprintf("\nSupporting assets and files for this skill are located in %q.", skillPath)
			}

			promptText := fmt.Sprintf("<embedded_skill name=%q%s>\n<instructions>\n%s\n</instructions>\n</embedded_skill>\n\nThe user invoked: %s%s\nFollow the skill instructions above.", sk.Name, pathAttr, rendered, text, pathNote)

			newMsg := message.NewUserTextMeta(promptText, map[string]any{
				"skill":   sk.Name,
				"path":    skillPath,
				"command": text,
			})
			if msg.GetID() != "" {
				newMsg.SetID(msg.GetID())
			}
			return newMsg, nil
		}
	}

	return msg, nil
}

func (s *Session) waitingForInteractionLocked() bool {
	return s.WaitingPermission || s.WaitingQuestion || s.Status == StatusWaitingPermission || s.Status == StatusWaitingQuestion
}

func (s *Session) startTurn(hInput harness.Input) error {
	s.Lock()
	if s.Running {
		s.Unlock()
		return ErrSessionRunning
	}
	if s.waitingForInteractionLocked() && !hInput.HasInteractiveContent() {
		s.Unlock()
		if hInput.Compact != nil {
			return fmt.Errorf("%w: resolve pending permissions or questions before compacting", ErrInvalidConfig)
		}
		return nil
	}

	runCtx, cancel := context.WithCancel(s.bgCtx)
	done := make(chan struct{})

	initialStatus := StatusThinking
	if hInput.Compact != nil {
		initialStatus = StatusCompacting
	} else if hInput.Message == nil && hInput.Decisions == nil && len(s.queue) > 0 {
		if s.queue[0].Kind == QueueKindCommand {
			initialStatus = StatusExecuting
		}
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

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    api.SessionStatus(initialStatus),
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
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
					SessionID: s.ID,
					Status:    api.StatusIdle,
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

		s.Lock()
		agentName := s.Config.Agent
		s.Unlock()
		sCtx := scope.With(runCtx, scope.Scope{
			SessionID:     s.ID,
			WorkspaceRoot: s.workspace.RootDir(),
			Agent:         agentName,
		})
		reply, err := s.send(sCtx, h.Stream(sCtx, hInput))
		if err != nil {
			s.handleAsyncError(err)
			return
		}
		if reply != nil {
			s.finishTurn(api.TurnStatusDone)
		}

		for {
			s.Lock()
			hasMore := !s.WaitingPermission && !s.WaitingQuestion && runCtx.Err() == nil && len(s.queue) > 0
			s.Unlock()
			if !hasMore {
				break
			}

			reply, err := s.send(sCtx, h.Stream(sCtx, harness.Input{}))
			if err != nil {
				s.handleAsyncError(err)
				return
			}
			if reply != nil {
				s.finishTurn(api.TurnStatusDone)
			}
		}

		s.Lock()
		waiting := s.WaitingPermission || s.WaitingQuestion
		s.Unlock()

		if !waiting && runCtx.Err() == nil {
			s.setStatus(StatusIdle)
			s.publisher.Publish(api.SessionStreamTopic(s.ID), api.CompletedEvent{
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

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.TaskUpdatedEvent{
		SessionID: s.ID,
		Task:      toAPITask(snap),
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

func (s *Session) recordDecision(ctx context.Context, decision api.PermissionDecision) (bool, error) {
	switch decision.Kind {
	case api.DecisionAllowOnce, api.DecisionAllowSession, api.DecisionReject:
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

	var matched *api.PermissionRequest
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
		s.decisions = make(map[string]api.PermissionDecision, len(state.PendingPermissions))
	}
	s.decisions[matched.ID] = decision

	var remaining []api.PermissionRequest
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
	entry := timeline.NewPermissionEntry(s.ID, api.PermissionRecord{
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

	publishEntryAdded(s.publisher, s.ID, entry)

	return allResolved, nil
}

func (s *Session) recordQuestion(ctx context.Context, response api.QuestionResponse) (bool, error) {
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
	var matched *api.QuestionRequest
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
	entry := timeline.NewQuestionEntry(s.ID, api.QuestionRecord{Request: *matched.Clone(), Response: response})
	if err := s.Timeline.Append(ctx, entry); err != nil {
		return false, err
	}
	if err := s.store.touchSession(ctx, s.ID, entry.CreatedAt); err != nil {
		return false, err
	}
	s.Lock()
	if s.answers == nil {
		s.answers = make(map[string]api.QuestionResponse)
	}
	s.answers[matched.ID] = response
	var remaining []api.QuestionRequest
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
	publishEntryAdded(s.publisher, s.ID, entry)
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

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    api.StatusIdle,
	})

	status := api.TurnStatusFailed
	if errors.Is(err, context.Canceled) {
		status = api.TurnStatusCanceled
	}
	defer s.finishTurn(status)

	if errors.Is(err, context.Canceled) {
		entry := timeline.NewInterruptedEntry(s.ID)
		if appErr := s.Timeline.Append(s.bgCtx, entry); appErr == nil {
			_ = s.store.touchSession(s.bgCtx, s.ID, entry.CreatedAt)
			s.Lock()
			s.UpdatedAt = entry.CreatedAt
			s.Unlock()
			publishEntryAdded(s.publisher, s.ID, entry)
		}
	} else {
		s.logger.Error("session turn failed", log.String("session_id", s.ID), log.String("error", err.Error()))
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.ErrorEvent{
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
					if partialMsg.GetID() == "" || !uuid.Validate(partialMsg.GetID()) {
						partialMsg.SetID(uuid.NewString())
					}
					entry := timeline.NewMessageEntry(s.ID, partialMsg)
					if appErr := s.Timeline.Append(s.bgCtx, entry); appErr == nil {
						_ = s.store.touchSession(s.bgCtx, s.ID, entry.CreatedAt)
						s.Lock()
						s.UpdatedAt = entry.CreatedAt
						s.Unlock()
						publishEntryAdded(s.publisher, s.ID, entry)
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
				clonedChunk := message.CloneAssistantChunk(*hev.Chunk)
				apiChunk := toAPIChunk(&clonedChunk)
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.ChunkEvent{SessionID: s.ID, Chunk: *apiChunk})
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

				publishEntryAdded(s.publisher, s.ID, entry)

				if user, ok := hev.Message.(*message.User); ok && harness.SystemKindOf(user) == "" {
					s.beginTurn(user.GetID(), entry.CreatedAt)
				}

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
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.CommandStartedEvent{
					SessionID: s.ID,
					ID:        hev.CommandStarted.ID,
					Command:   hev.CommandStarted.Command,
				})
			}

		case harness.EventCommandCompleted:
			if hev.CommandCompleted != nil {
				entry := timeline.NewCommandEntry(s.ID, api.CommandRecord{
					Command:    hev.CommandCompleted.Command,
					Output:     hev.CommandCompleted.Output,
					ExitCode:   hev.CommandCompleted.ExitCode,
					DurationMs: hev.CommandCompleted.DurationMs,
				})
				if hev.CommandCompleted.ID != "" && uuid.Validate(hev.CommandCompleted.ID) {
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

				publishEntryAdded(s.publisher, s.ID, entry)
			}

		case harness.EventInterrupted:
			interrupted = true
			perms := make([]api.PermissionRequest, len(hev.Permissions))
			for i, p := range hev.Permissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}

			questions := make([]api.QuestionRequest, len(hev.Questions))
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

			s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
				SessionID: s.ID,
				Status:    api.SessionStatus(nextStatus),
			})
			if len(perms) > 0 {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.PermissionRequestedEvent{SessionID: s.ID, Permissions: perms})
			}
			if len(questions) > 0 {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QuestionRequestedEvent{SessionID: s.ID, Questions: questions})
			}

		case harness.EventTodosUpdated:
			s.Lock()
			s.Todos = slices.Clone(hev.Todos)
			s.Unlock()

			s.publisher.Publish(api.SessionStreamTopic(s.ID), api.TodosUpdatedEvent{
				SessionID: s.ID,
				Todos:     hev.Todos,
			})

		case harness.EventCompactionStarted:
			s.setStatus(StatusCompacting)
			var compID, trig, focus string
			if hev.CompactionStarted != nil {
				compID = hev.CompactionStarted.ID
				trig = hev.CompactionStarted.Trigger
				focus = hev.CompactionStarted.Focus
			}
			s.publisher.Publish(api.SessionStreamTopic(s.ID), api.CompactionStartedEvent{
				SessionID: s.ID,
				ID:        compID,
				Trigger:   trig,
				Focus:     focus,
			})

		case harness.EventCompactionCompleted:
			if hev.CompactionCompleted != nil {
				rec := api.CompactedRecord{
					Trigger:      hev.CompactionCompleted.Trigger,
					Focus:        hev.CompactionCompleted.Focus,
					Summary:      hev.CompactionCompleted.Summary,
					BeforeTokens: hev.CompactionCompleted.BeforeTokens,
					AfterTokens:  hev.CompactionCompleted.AfterTokens,
					DurationMs:   hev.CompactionCompleted.DurationMs,
				}
				entry := timeline.NewCompactedEntry(s.ID, rec)
				if hev.CompactionCompleted.ID != "" && uuid.Validate(hev.CompactionCompleted.ID) {
					entry.ID = hev.CompactionCompleted.ID
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

				publishEntryAdded(s.publisher, s.ID, entry)
			}

		case harness.EventCacheBroken:
			if hev.CacheBroken != nil {
				rec := api.CacheBrokenRecord{
					PrevCached:  hev.CacheBroken.PrevCached,
					CurrCached:  hev.CacheBroken.CurrCached,
					InputTokens: hev.CacheBroken.InputTokens,
				}
				entry := timeline.NewCacheBrokenEntry(s.ID, rec)
				if err := s.Timeline.Append(ctx, entry); err != nil {
					return nil, err
				}
				if err := s.store.touchSession(ctx, s.ID, entry.CreatedAt); err != nil {
					return nil, err
				}
				s.Lock()
				s.UpdatedAt = entry.CreatedAt
				s.Unlock()

				publishEntryAdded(s.publisher, s.ID, entry)
			}

		case harness.EventContextUpdated:
			if hev.Context != nil {
				s.Lock()
				s.Context = *hev.Context
				s.Unlock()

				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.ContextUpdatedEvent{
					SessionID: s.ID,
					Context:   *hev.Context,
				})
			}

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

	// Release the in-flight latch when this attempt produces no title so that a
	// later user message can retry (e.g. after a transient timeout or provider
	// error) instead of leaving the session permanently untitled.
	defer func() {
		s.Lock()
		if strings.TrimSpace(s.Title) == "" {
			s.titleGenerating = false
		}
		s.Unlock()
	}()

	var sysPrompt string
	var llmModel *llm.Model
	ready := func() bool {
		if s.reloadLock != nil {
			s.reloadLock.RLock()
			defer s.reloadLock.RUnlock()
		}
		if s.workspace == nil || s.workspace.Catalog() == nil {
			return false
		}
		cat := s.workspace.Catalog()
		resolvedTitle, err := cat.ResolveAgent(workspace.AgentNameTitleGenerator)
		if err != nil || resolvedTitle == nil || resolvedTitle.Agent == nil {
			if err != nil {
				s.logger.Warn("failed to resolve title generator agent", log.String("error", err.Error()))
			}
			return false
		}
		sysPrompt, err = resolvedTitle.Agent.Render(&warp.AgentRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Resolved:  resolvedTitle,
		})
		if err != nil || strings.TrimSpace(sysPrompt) == "" {
			if err != nil {
				s.logger.Warn("failed to render title generator prompt", log.String("error", err.Error()))
			}
			return false
		}

		catalog := s.catalog()
		var effective models.Model
		var found bool
		if len(resolvedTitle.Agent.Spec.Models) > 0 {
			effective, found = catalog.Resolve(models.RoleCompletion, resolvedTitle.Agent.Spec.Models...)
		}
		if !found {
			s.Lock()
			modelName := s.Config.Model
			s.Unlock()
			effective, found = catalog.Get(modelName)
		}
		if !found {
			return false
		}
		settings := s.modelSettings(SessionConfig{Agent: workspace.AgentNameTitleGenerator})
		cfg, _, err := effective.Configure(settings)
		if err == nil {
			llmModel, err = s.gateway().LLM(ctx, effective, cfg)
		}
		if err != nil {
			s.logger.Warn("failed to resolve LLM for title generation", log.String("error", err.Error()))
			return false
		}
		return true
	}()
	if !ready {
		return
	}

	msgs := []message.Message{
		message.NewSystemText(strings.TrimSpace(sysPrompt)),
		message.NewUserText(userText),
	}
	s.Lock()
	sessionAgent := s.Config.Agent
	s.Unlock()
	titleCtx := scope.With(ctx, scope.Scope{
		SessionID:     s.ID,
		WorkspaceRoot: s.workspace.RootDir(),
		Agent:         sessionAgent,
		Purpose:       string(metrics.PurposeTitle),
	})
	resp, err := llmModel.Invoke(titleCtx, msgs)
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

	var wsRoot string
	if s.workspace != nil {
		wsRoot = s.workspace.RootDir()
	}
	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: wsRoot,
		Session:   ToAPISession(s),
	})
}

// minTurnSummary is the shortest completed turn that gets a summary entry.
const minTurnSummary = 5 * time.Second

// beginTurn records that the user message id started (or joined) a turn.
func (s *Session) beginTurn(id string, at time.Time) {
	s.Lock()
	defer s.Unlock()
	if len(s.turnIDs) == 0 {
		s.turnStart = at
	}
	s.turnIDs = append(s.turnIDs, id)
}

// finishTurn appends a turn summary entry to the timeline when the turn in
// progress ends, with the elapsed time and the usage recorded for the turn.
// Completed turns shorter than [minTurnSummary] get no entry.
func (s *Session) finishTurn(status string) {
	s.Lock()
	ids := s.turnIDs
	start := s.turnStart
	s.turnIDs = nil
	s.turnStart = time.Time{}
	s.Unlock()
	if len(ids) == 0 {
		return
	}

	elapsed := time.Since(start)
	if status == api.TurnStatusDone && elapsed < minTurnSummary {
		return
	}

	rec := api.TurnCompletedRecord{TurnID: ids[len(ids)-1], Status: status, ElapsedMs: elapsed.Milliseconds()}
	if s.metrics != nil {
		ctx, cancel := context.WithTimeout(s.bgCtx, 5*time.Second)
		usage, err := s.metrics.TurnUsage(ctx, ids...)
		cancel()
		if err != nil {
			s.logger.Warn("failed to load turn usage", log.String("session_id", s.ID), log.String("error", err.Error()))
		} else {
			rec.InputTokens = usage.Tokens.Input
			rec.OutputTokens = usage.Tokens.Output
			rec.CostNano = usage.CostNano
			rec.Unpriced = usage.UnpricedCalls > 0
		}
	}

	entry := timeline.NewTurnCompletedEntry(s.ID, rec)
	if err := s.Timeline.Append(s.bgCtx, entry); err != nil {
		s.logger.Warn("failed to append turn summary", log.String("session_id", s.ID), log.String("error", err.Error()))
		return
	}
	_ = s.store.touchSession(s.bgCtx, s.ID, entry.CreatedAt)
	s.Lock()
	s.UpdatedAt = entry.CreatedAt
	s.Unlock()
	publishEntryAdded(s.publisher, s.ID, entry)
}
