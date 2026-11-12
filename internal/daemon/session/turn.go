package session

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
)

var _ harness.Queue = (*Session)(nil)

// minTurnSummary is the shortest completed turn that gets a summary entry.
const minTurnSummary = 5 * time.Second

// Send initiates an asynchronous conversational turn with a user message or resolves
// a pending permission decision.
func (s *Session) Send(ctx context.Context, req api.SendRequest) error {
	if s == nil {
		return errors.New("session: uninitialized session")
	}

	id := strings.TrimSpace(req.ID)
	if id != "" && (req.Question != nil || req.Decision != nil) {
		return fmt.Errorf("%w: cannot combine question/decision with id", ErrInvalidConfig)
	}
	if id != "" && !uuid.Validate(id) {
		return fmt.Errorf("%w: invalid message id %q", ErrInvalidConfig, id)
	}

	if req.Compact != nil {
		if req.Question != nil || req.Decision != nil || strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 || strings.TrimSpace(req.Command) != "" {
			return fmt.Errorf("%w: cannot combine compaction request with other input", ErrInvalidConfig)
		}
		s.Lock()
		if s.Running {
			s.Unlock()
			return ErrSessionRunning
		}
		s.Unlock()
		return s.startTurn(harness.Input{Compact: req.Compact})
	}

	if req.Question != nil {
		if req.Decision != nil || strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 || strings.TrimSpace(req.Command) != "" {
			return fmt.Errorf("%w: cannot combine question response with other input", ErrInvalidConfig)
		}
		return s.resumeInteraction(ctx, func(c context.Context) (bool, error) {
			return s.recordQuestion(c, *req.Question)
		})
	}

	if req.Decision != nil {
		if strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 || strings.TrimSpace(req.Command) != "" {
			return fmt.Errorf("%w: cannot specify both message and decision", ErrInvalidConfig)
		}
		return s.resumeInteraction(ctx, func(c context.Context) (bool, error) {
			return s.recordDecision(c, *req.Decision)
		})
	}

	if cmd := strings.TrimSpace(req.Command); cmd != "" {
		if strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 {
			return fmt.Errorf("%w: cannot specify both message and command", ErrInvalidConfig)
		}
		s.enqueueItem(queueItem{
			ID:      id,
			Kind:    api.QueueKindCommand,
			Command: cmd,
		})
		s.Lock()
		running := s.Running
		s.Unlock()
		if running {
			return nil
		}
		return s.startTurn(harness.Input{})
	}

	var blocks []message.Block
	if strings.TrimSpace(req.Content) != "" {
		blocks = append(blocks, &message.TextBlock{Text: req.Content})
	}
	for i, b := range req.Blocks {
		if b.Type != api.BlockText {
			return fmt.Errorf("%w: block %d of type %q is not supported in user messages", ErrInvalidConfig, i, b.Type)
		}
		converted, ok := FromAPIBlock(b)
		if !ok {
			return fmt.Errorf("%w: invalid or unsupported block %d of type %q", ErrInvalidConfig, i, b.Type)
		}
		blocks = append(blocks, converted)
	}
	if len(blocks) == 0 {
		return ErrEmptyMessage
	}

	userMsg := message.NewUser(blocks...)
	if id != "" {
		userMsg.SetID(id)
	}

	s.Lock()
	attResolver := s.attachments
	s.Unlock()
	normalizedAtts, err := attResolver.Validate(ctx, req.Attachments)
	if err != nil {
		if errors.Is(err, harness.ErrInvalidConfig) {
			return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
		return err
	}
	if len(normalizedAtts) > 0 {
		harness.SetAttachments(userMsg, normalizedAtts)
	}

	var msg message.Message = userMsg

	exec, err := s.resolveCommandExecution(msg)
	if err != nil {
		return err
	}

	if exec != nil && exec.Message != nil {
		if execUser, ok := exec.Message.(*message.User); ok && len(normalizedAtts) > 0 {
			harness.SetAttachments(execUser, normalizedAtts)
		}
	}

	// A command that requests a forked subagent (clean/fork context or a
	// different target agent) executes in a child session rather than on the
	// current thread. The invocation itself still belongs to this session's
	// timeline, so record what the caller sent to keep the thread complete. It
	// is best-effort on the daemon-owned context so a client disconnect can
	// never prevent the command from running.
	if exec != nil && exec.Subagent {
		_ = s.appendTimeline(s.bgCtx, timeline.NewMessageEntry(msg))
		return s.dispatchCommandSubagent(ctx, exec)
	}

	if exec != nil {
		msg = exec.Message
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
		s.enqueueItem(queueItem{
			ID:      msg.GetID(),
			Kind:    api.QueueKindMessage,
			Message: message.CloneMessage(msg),
		})
		return nil
	}

	var hInput harness.Input
	hInput.Message = msg
	if exec != nil && exec.TargetModel != "" && !strings.EqualFold(exec.TargetModel, s.Config.Model) {
		hInput.ModelOverride = exec.TargetModel
	}

	return s.startTurn(hInput)
}

// Cancel interrupts any active conversational turn executing on this session.
func (s *Session) Cancel(ctx context.Context) error {
	if s == nil {
		return errors.New("session: uninitialized session")
	}
	for _, ch := range s.Children() {
		_ = ch.Cancel(ctx)
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

func (s *Session) VisibleQueue() []api.QueueItem {
	s.Lock()
	defer s.Unlock()
	return s.visibleQueueLocked()
}

func (s *Session) visibleQueueLocked() []api.QueueItem {
	if len(s.queue) == 0 {
		return nil
	}
	var vis []api.QueueItem
	for _, it := range s.queue {
		if it.Kind == api.QueueKindMessage || it.Kind == api.QueueKindCommand {
			if apiItem, ok := toAPIQueueItem(it); ok {
				vis = append(vis, apiItem)
			}
		}
	}
	return vis
}

func (s *Session) enqueueItem(item queueItem) {
	if item.ID == "" && !uuid.Validate(item.ID) {
		item.ID = uuid.NewString()
	}

	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}

	var vis []api.QueueItem
	s.Lock()
	s.queue = append(s.queue, item)
	isVisible := item.Kind == api.QueueKindMessage || item.Kind == api.QueueKindCommand
	if isVisible {
		vis = s.visibleQueueLocked()
	}
	s.Unlock()

	if isVisible {
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QueueUpdatedEvent{
			SessionID: s.ID,
			Queue:     vis,
		})
	}
}

// HasItems reports whether the execution queue has pending items.
func (s *Session) HasItems() bool {
	s.Lock()
	defer s.Unlock()
	return len(s.queue) > 0
}

// HasLeadingCommand reports whether the next pending item in the queue is a command.
func (s *Session) HasLeadingCommand() bool {
	s.Lock()
	defer s.Unlock()
	return len(s.queue) > 0 && s.queue[0].Kind == api.QueueKindCommand
}

// PopCommand pops and returns the leading command item if present.
func (s *Session) PopCommand() (id, command string, ok bool) {
	s.Lock()
	if len(s.queue) == 0 || s.queue[0].Kind != api.QueueKindCommand {
		s.Unlock()
		return "", "", false
	}
	item := s.queue[0]
	s.queue = s.queue[1:]
	vis := s.visibleQueueLocked()
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QueueUpdatedEvent{
		SessionID: s.ID,
		Queue:     vis,
	})
	return item.ID, item.Command, true
}

// PopMessages pops and returns all leading messages up to the next command item.
func (s *Session) PopMessages() []message.Message {
	s.Lock()
	if len(s.queue) == 0 {
		s.Unlock()
		return nil
	}

	var popped []queueItem
	var hadMessage bool
	for len(s.queue) > 0 {
		if s.queue[0].Kind == api.QueueKindCommand {
			break
		}
		item := s.queue[0]
		s.queue = s.queue[1:]
		if item.Kind == api.QueueKindMessage {
			hadMessage = true
		}
		popped = append(popped, item)
	}

	var vis []api.QueueItem
	if hadMessage {
		vis = s.visibleQueueLocked()
	}
	s.Unlock()

	if hadMessage {
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QueueUpdatedEvent{
			SessionID: s.ID,
			Queue:     vis,
		})
	}

	if len(popped) == 0 {
		return nil
	}

	msgs := make([]message.Message, 0, len(popped))
	for _, it := range popped {
		if it.Message != nil {
			msgs = append(msgs, it.Message)
		}
	}
	return msgs
}

func (s *Session) waitingForInteractionLocked() bool {
	return len(s.PendingPermissions) > 0 || len(s.PendingQuestions) > 0 || s.Status == api.StatusWaitingPermission || s.Status == api.StatusWaitingQuestion
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

	initialStatus := api.StatusThinking
	if hInput.Compact != nil {
		initialStatus = api.StatusCompacting
	} else if hInput.Message == nil && hInput.Decisions == nil && len(s.queue) > 0 {
		if s.queue[0].Kind == api.QueueKindCommand {
			initialStatus = api.StatusExecuting
		}
	}

	s.Status = initialStatus
	s.Running = true
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
		Status:    initialStatus,
	})

	s.wg.Go(func() {
		defer func() {
			var statusChanged bool
			s.Lock()
			if s.done == done {
				s.Running = false
				if s.Status != api.StatusWaitingPermission && s.Status != api.StatusWaitingQuestion && s.Status != api.StatusIdle {
					s.Status = api.StatusIdle
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

		// Apply a per-turn model override (e.g. a command's spec.models) and
		// restore the original model when the turn exits on any path.
		if hInput.ModelOverride != "" {
			origModel := s.Config.Model
			s.setConfigModel(hInput.ModelOverride)
			_ = s.configureHarness(runCtx)
			defer func() {
				s.setConfigModel(origModel)
				_ = s.configureHarness(s.bgCtx)
			}()
		}

		s.Lock()
		hErr := s.harnessErr
		s.Unlock()
		if hErr != nil {
			s.handleAsyncError(runCtx, hErr)
			return
		}

		s.Lock()
		agentName := s.Config.Agent
		s.Unlock()
		sCtx := scope.With(runCtx, scope.Scope{
			SessionID:       s.ID,
			ParentSessionID: s.ParentID,
			WorkspaceRoot:   s.workspace.RootDir(),
			Agent:           agentName,
		})
		var lastReply *message.Assistant
		reply, err := s.send(sCtx, s.Harness.Stream(sCtx, hInput))
		if err != nil {
			s.handleAsyncError(runCtx, err)
			return
		}
		if reply != nil {
			lastReply = reply
			s.finishTurn(api.TurnStatusDone)
		}

		for {
			s.Lock()
			hasMore := !s.waitingForInteractionLocked() && runCtx.Err() == nil && len(s.queue) > 0
			s.Unlock()
			if !hasMore {
				break
			}

			reply, err := s.send(sCtx, s.Harness.Stream(sCtx, harness.Input{}))
			if err != nil {
				s.handleAsyncError(runCtx, err)
				return
			}
			if reply != nil {
				lastReply = reply
				s.finishTurn(api.TurnStatusDone)
			}
		}

		s.Lock()
		waiting := s.waitingForInteractionLocked()
		s.Unlock()

		if !waiting && runCtx.Err() == nil {
			s.setStatus(api.StatusIdle)
			s.publisher.Publish(api.SessionStreamTopic(s.ID), api.CompletedEvent{
				SessionID: s.ID,
			})
			s.notifyParentUpdate()
			if s.parent != nil {
				var outText string
				if lastReply != nil {
					outText = strings.TrimSpace(lastReply.GetContent().Text())
				}
				text := fmt.Sprintf("<subagent_completed session_id=%q agent=%q title=%q status=\"completed\" work_dir=%q>\n%s\n</subagent_completed>",
					s.ID, s.Config.Agent, s.Title, s.WorkDir, outText)
				s.parent.Enqueue(message.NewUserTextMeta(text, map[string]any{harness.MetaSystemKind: harness.SystemKindSubagent}))
			}
		}
	})
	return nil
}

// Enqueue adds a message to the session's execution queue or initiates an immediate turn if idle.
func (s *Session) Enqueue(msg message.Message) {
	if msg == nil {
		return
	}
	kind := api.QueueKindMessage
	switch harness.SystemKindOf(msg) {
	case harness.SystemKindTask:
		kind = api.QueueKindTask
	case harness.SystemKindSubagent:
		kind = api.QueueKindSubagent
	}
	item := queueItem{
		ID:      msg.GetID(),
		Kind:    kind,
		Message: message.CloneMessage(msg),
	}
	s.enqueueItem(item)

	s.Lock()
	canStart := !s.Running && !s.waitingForInteractionLocked()
	s.Unlock()
	if canStart {
		_ = s.startTurn(harness.Input{})
	}
}

func (s *Session) onTaskUpdate(snap api.Task) {
	s.Lock()
	if s.Tasks != nil {
		s.State.Tasks = s.Tasks.List()
	}
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.TaskUpdatedEvent{
		SessionID: s.ID,
		Task:      snap,
	})

	if snap.Status == api.TaskStatusCompleted || snap.Status == api.TaskStatusFailed {
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

func (s *Session) resumeInteraction(ctx context.Context, record func(context.Context) (bool, error)) error {
	s.Lock()
	if s.Running {
		s.Unlock()
		return ErrSessionRunning
	}
	s.Unlock()

	ready, err := record(ctx)
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

func (s *Session) appendTimeline(ctx context.Context, entry *timeline.Entry) error {
	if err := s.Timeline.Append(ctx, entry); err != nil {
		return err
	}
	s.Lock()
	s.UpdatedAt = entry.CreatedAt
	s.Unlock()
	if err := s.checkpoint(ctx); err != nil {
		return err
	}
	publishEntryAdded(s.publisher, s.ID, entry)
	return nil
}

func (s *Session) recordDecision(ctx context.Context, decision api.PermissionDecision) (bool, error) {
	switch decision.Kind {
	case api.DecisionAllowOnce, api.DecisionAllowSession, api.DecisionReject:
	default:
		return false, fmt.Errorf("%w: unsupported decision kind %q", ErrInvalidDecision, decision.Kind)
	}

	state, err := s.Harness.LoadState(ctx)
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
	if len(remaining) == 0 && len(s.PendingQuestions) > 0 {
		s.Status = api.StatusWaitingQuestion
	}
	s.Unlock()

	matchedReq := *matched
	matchedReq.GrantPatterns = slices.Clone(matched.GrantPatterns)
	fullDec := decision
	entry := timeline.NewPermissionEntry(api.PermissionRecord{
		Request:  matchedReq,
		Decision: fullDec,
	})
	if err := s.appendTimeline(ctx, entry); err != nil {
		s.Lock()
		delete(s.decisions, matched.ID)
		s.Unlock()
		return false, err
	}

	return allResolved, nil
}

func (s *Session) recordQuestion(ctx context.Context, response api.QuestionResponse) (bool, error) {
	state, err := s.Harness.LoadState(ctx)
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
	entry := timeline.NewQuestionEntry(api.QuestionRecord{Request: *matched.Clone(), Response: response})

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
	ready := len(remaining) == 0 && len(s.PendingPermissions) == 0
	s.Unlock()

	if err := s.appendTimeline(ctx, entry); err != nil {
		s.Lock()
		delete(s.answers, matched.ID)
		s.Unlock()
		return false, err
	}
	return ready, nil
}

func isTurnCanceled(runCtx context.Context, err error) bool {
	return errors.Is(runCtx.Err(), context.Canceled) && errors.Is(err, context.Canceled)
}

func (s *Session) handleAsyncError(runCtx context.Context, err error) {
	canceled := isTurnCanceled(runCtx, err)

	s.Lock()
	s.Running = false
	s.PendingPermissions = nil
	s.PendingQuestions = nil
	s.queue = nil
	s.Status = api.StatusIdle
	s.FinishedAt = time.Now().UTC()
	s.err = err
	if !canceled {
		s.Error = err.Error()
	}
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    api.StatusIdle,
	})

	status := api.TurnStatusFailed
	if canceled {
		status = api.TurnStatusCanceled
	}
	defer s.finishTurn(status)

	if canceled {
		_ = s.appendTimeline(s.bgCtx, timeline.NewInterruptedEntry())
	} else {
		s.logger.Error("session turn failed", log.String("session_id", s.ID), log.String("error", err.Error()))
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.ErrorEvent{
			SessionID: s.ID,
			Error:     err.Error(),
		})
	}
	s.notifyParentUpdate()
	if s.parent != nil && !canceled {
		text := fmt.Sprintf("<subagent_completed session_id=%q agent=%q title=%q status=\"failed\" error=%q work_dir=%q>\n</subagent_completed>",
			s.ID, s.Config.Agent, s.Title, err.Error(), s.WorkDir)
		s.parent.Enqueue(message.NewUserTextMeta(text, map[string]any{harness.MetaSystemKind: harness.SystemKindSubagent}))
	}
}

func (s *Session) send(ctx context.Context, stream iter.Seq2[any, error]) (*message.Assistant, error) {
	var reply *message.Assistant
	var interrupted bool
	var pendingTools int
	agg := message.NewAssistantAggregator()

	for raw, err := range stream {
		if err != nil {
			if isTurnCanceled(ctx, err) {
				partialMsg, buildErr := agg.Build()
				if buildErr == nil && partialMsg != nil && len(partialMsg.Content) > 0 {
					if partialMsg.GetID() == "" || !uuid.Validate(partialMsg.GetID()) {
						partialMsg.SetID(uuid.NewString())
					}
					_ = s.appendTimeline(s.bgCtx, timeline.NewMessageEntry(partialMsg))
				}
			}
			return nil, err
		}

		switch ev := raw.(type) {
		case *message.AssistantChunk:
			if ev != nil {
				agg.Add(ev)
				status := api.StatusGenerating
				for _, b := range ev.Content {
					switch b.(type) {
					case *message.TextBlock, *message.ToolCall, *message.ToolCallChunk:
						status = api.StatusGenerating
					case *message.ThinkingBlock:
						status = api.StatusThinking
					}
				}
				s.setStatus(status)
				if status == api.StatusThinking {
					if partialMsg, buildErr := agg.Build(); buildErr == nil && partialMsg != nil && len(partialMsg.Content) > 0 {
						if blk, ok := partialMsg.Content[len(partialMsg.Content)-1].(*message.ThinkingBlock); ok && blk.Thinking != "" {
							s.updateActivityFromThought(blk.Thinking)
						}
					}
				}
				clonedChunk := message.CloneAssistantChunk(*ev)
				apiChunk := toAPIChunk(&clonedChunk)
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.ChunkEvent{SessionID: s.ID, Chunk: *apiChunk})
			}

		case message.Message:
			if ev != nil {
				agg = message.NewAssistantAggregator()
				entry := timeline.NewMessageEntry(ev)
				if err := s.appendTimeline(ctx, entry); err != nil {
					return nil, err
				}

				if user, ok := ev.(*message.User); ok && harness.SystemKindOf(user) == "" {
					s.beginTurn(user.GetID(), entry.CreatedAt)
				}

				if asst, ok := ev.(*message.Assistant); ok {
					if len(asst.ToolCalls()) > 0 {
						pendingTools = len(asst.ToolCalls())
						tc := asst.ToolCalls()[0]
						s.Lock()
						s.Activity = toolActivityLabel(*tc)
						s.Unlock()
						s.setStatus(api.StatusExecuting)
					}
				} else if _, ok := ev.(*message.Tool); ok {
					if pendingTools > 0 {
						pendingTools--
						if pendingTools == 0 {
							s.setStatus(api.StatusThinking)
						}
					} else {
						s.setStatus(api.StatusThinking)
					}
				}
			}

		case api.CommandStartedEvent:
			s.setStatus(api.StatusExecuting)
			ev.SessionID = s.ID
			s.publisher.Publish(api.SessionStreamTopic(s.ID), ev)

		case harness.CommandCompleted:
			entry := timeline.NewCommandEntry(ev.CommandRecord)
			if ev.ID != "" && uuid.Validate(ev.ID) {
				entry.ID = ev.ID
			}
			if err := s.appendTimeline(ctx, entry); err != nil {
				return nil, err
			}

		case harness.Interrupted:
			interrupted = true
			perms := make([]api.PermissionRequest, len(ev.Permissions))
			for i, p := range ev.Permissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}
			questions := make([]api.QuestionRequest, len(ev.Questions))
			for i := range ev.Questions {
				questions[i] = *ev.Questions[i].Clone()
			}
			nextStatus := api.StatusWaitingQuestion
			if len(perms) > 0 {
				nextStatus = api.StatusWaitingPermission
			}
			s.Lock()
			s.Status = nextStatus
			s.Running = false
			s.PendingPermissions = perms
			s.PendingQuestions = questions
			s.FinishedAt = time.Now().UTC()
			s.Unlock()

			s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
				SessionID: s.ID,
				Status:    nextStatus,
			})
			if len(perms) > 0 {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.PermissionRequestedEvent{SessionID: s.ID, Permissions: perms})
			}
			if len(questions) > 0 {
				s.publisher.Publish(api.SessionStreamTopic(s.ID), api.QuestionRequestedEvent{SessionID: s.ID, Questions: questions})
			}
			s.notifyParentUpdate()

		case api.TodosUpdatedEvent:
			s.Lock()
			s.Todos = slices.Clone(ev.Todos)
			s.Unlock()

			ev.SessionID = s.ID
			s.publisher.Publish(api.SessionStreamTopic(s.ID), ev)
			s.notifyParentUpdate()

		case api.CompactionStartedEvent:
			s.setStatus(api.StatusCompacting)
			ev.SessionID = s.ID
			s.publisher.Publish(api.SessionStreamTopic(s.ID), ev)

		case harness.CompactionCompleted:
			entry := timeline.NewCompactedEntry(ev.CompactedRecord)
			if ev.ID != "" && uuid.Validate(ev.ID) {
				entry.ID = ev.ID
			}
			if err := s.appendTimeline(ctx, entry); err != nil {
				return nil, err
			}

		case harness.RuleLoaded:
			entry := timeline.NewRuleEntry(api.RuleRecord{
				Rule:    ev.Rule,
				Trigger: ev.Trigger,
				Matched: ev.Matched,
				Path:    ev.Path,
			})
			if ev.ID != "" && uuid.Validate(ev.ID) {
				entry.ID = ev.ID
			}
			if err := s.appendTimeline(ctx, entry); err != nil {
				return nil, err
			}

		case api.CacheBrokenRecord:
			if err := s.appendTimeline(ctx, timeline.NewCacheBrokenEntry(ev)); err != nil {
				return nil, err
			}

		case api.ContextUpdatedEvent:
			s.Lock()
			s.Context = ev.Context
			s.Unlock()

			ev.SessionID = s.ID
			s.publisher.Publish(api.SessionStreamTopic(s.ID), ev)
			s.notifyParentUpdate()

		case harness.Completed:
			if ev.Message != nil {
				if asst, ok := message.CloneMessage(ev.Message).(*message.Assistant); ok {
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

	if err := s.appendTimeline(s.bgCtx, timeline.NewTurnCompletedEntry(rec)); err != nil {
		s.logger.Warn("failed to append turn summary", log.String("session_id", s.ID), log.String("error", err.Error()))
	}
}
