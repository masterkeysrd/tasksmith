package session

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
)

// forkTitleMaxBytes bounds the auto-generated fork title derived from the
// parent's title before the " (fork)" suffix is appended.
const forkTitleMaxBytes = 60

// runningStatuses are the live turn statuses. A session in one of them has a
// turn in flight whose harness checkpoint is mid-graph, so it cannot be forked
// without risking a dangling tool call in the fork's conversation.
var runningStatuses = []api.SessionStatus{
	api.StatusThinking,
	api.StatusGenerating,
	api.StatusExecuting,
	api.StatusCompacting,
}

func isRunningStatus(status api.SessionStatus) bool {
	return slices.Contains(runningStatuses, status)
}

// Fork creates a new top-level session branching this session's conversation.
// The fork receives the parent's latest harness checkpoint (all state
// channels), a copy of the parent's timeline, and the same session config and
// sandbox backend. Pending permissions, pending questions, subagents,
// background tasks, queued input, and the file-change journal are deliberately
// not carried over: the fork is an independent branch that never mutates the
// parent, and file rollback stays owned by the thread that made the changes.
//
// Forking requires an idle session: a mid-turn or waiting checkpoint could
// leave the fork's conversation ending on an unanswered tool call.
func (s *Session) Fork(ctx context.Context, title string) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.Lock()
	if isRunningStatus(s.Status) {
		s.Unlock()
		return nil, fmt.Errorf("%w: cannot fork while a turn is running", ErrSessionRunning)
	}
	if s.Status == api.StatusWaitingPermission || s.Status == api.StatusWaitingQuestion || s.Status == api.StatusWaitingPlanReview ||
		len(s.PendingPermissions) > 0 || len(s.PendingQuestions) > 0 || len(s.PendingPlanReviews) > 0 {
		s.Unlock()
		return nil, fmt.Errorf("%w: cannot fork while the session awaits a permission, answer, or plan review", ErrSessionRunning)
	}
	if s.ParentID != "" {
		s.Unlock()
		return nil, fmt.Errorf("%w: cannot fork a subagent session", ErrInvalidConfig)
	}
	if s.harnessErr != nil {
		s.Unlock()
		return nil, fmt.Errorf("%w: session harness is unavailable: %v", ErrInvalidConfig, s.harnessErr)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = forkTitle(s.Title)
	}
	// The fork keeps the parent's effective sandbox backend rather than
	// re-resolving a (possibly different) host default, and inherits the
	// parent's session-scoped permission grants.
	sessionCfg := s.Config
	sessionCfg.Sandbox = s.Sandbox.Mode()
	cfg := Config{
		Title:              title,
		WorkDir:            s.WorkDir,
		Session:            sessionCfg,
		InitialPermissions: s.Permissions.Clone(),
		DB:                 s.db,
		Workspace:          s.workspace,
		Gateway:            s.gateway,
		Catalog:            s.catalog,
		// The fork attaches to the root permissions manager (not the parent's
		// session manager) because it is an independent top-level thread,
		// mirroring how fork-context commands spawn children.
		Permissions:     s.permRoot,
		SandboxSettings: s.sandboxCfg,
		Publisher:       s.publisher,
		Logger:          s.logger,
		Metrics:         s.metrics,
		ReloadLock:      s.reloadLock,
		Context:         s.bgCtx,
		WaitGroup:       s.wg,
		MCPCaller:       s.mcpCaller,
		OnUpdate:        s.onUpdate,
	}
	sourceID := s.ID
	sourceTimeline := s.Timeline
	sourceTitle := s.Title
	s.Unlock()

	child, err := New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	child.ForkedFrom = sourceID

	if err := child.Harness.ForkFrom(ctx, sourceID); err != nil {
		cleanupFailedFork(ctx, child)
		return nil, err
	}
	if err := child.mediaStore.LinkAllFrom(s.mediaDir()); err != nil {
		cleanupFailedFork(ctx, child)
		return nil, err
	}
	copyBase := time.Now().UTC()
	copied, err := copyForkTimeline(ctx, sourceTimeline, child.Timeline, copyBase)
	if err != nil {
		cleanupFailedFork(ctx, child)
		return nil, err
	}
	if err := child.adoptForkedState(ctx); err != nil {
		cleanupFailedFork(ctx, child)
		return nil, err
	}
	if err := child.checkpoint(ctx); err != nil {
		cleanupFailedFork(ctx, child)
		return nil, err
	}

	srcMarker, err := recordForkMarkers(ctx, sourceTimeline, child.Timeline, copyBase, copied, sourceID, sourceTitle, child.ID, child.Title)
	if err != nil {
		cleanupFailedFork(ctx, child)
		return nil, err
	}

	publishEntryAdded(s.publisher, sourceID, srcMarker)
	s.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(child),
	})

	return child, nil
}

func cleanupFailedFork(ctx context.Context, child *Session) {
	_ = child.Timeline.Delete(ctx)
	_ = child.Close()
	if child.Root != "" {
		_ = os.RemoveAll(child.Root)
	}
}

// adoptForkedState projects the forked harness checkpoint onto the child
// session's live state. A fork always starts idle: the parent's unfinished
// turn is intentionally left behind rather than resumed, so a checkpoint
// still carrying a suspended interaction is rejected instead of silently
// duplicated into the fork.
func (s *Session) adoptForkedState(ctx context.Context) error {
	state, err := s.Harness.LoadState(ctx)
	if err != nil {
		return fmt.Errorf("session: load forked state: %w", err)
	}
	if len(state.PendingPermissions) > 0 || len(state.PendingQuestions) > 0 {
		return fmt.Errorf("%w: cannot fork while the session awaits a permission or answer", ErrSessionRunning)
	}

	s.Lock()
	defer s.Unlock()
	s.Context = state.Context
	s.Todos = slices.Clone(state.Todos)
	return nil
}

// copyForkTimeline copies every timeline entry from the source into the
// destination and reports how many were copied. Entry IDs are re-identified
// with fresh clock-ordered UUIDs (the timeline ID column is a global primary
// key), while message payloads keep their original IDs so the fork's timeline
// stays consistent with its forked checkpoint.
func copyForkTimeline(ctx context.Context, src, dst timeline.Timeline, base time.Time) (int, error) {
	entries, err := src.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("session: list source timeline: %w", err)
	}

	for i, entry := range entries {
		next := entry.Clone()
		next.ID = uuid.Monotonic(base, i)
		next.SessionID = ""
		if err := dst.Append(ctx, &next); err != nil {
			return 0, fmt.Errorf("session: copy forked timeline entry: %w", err)
		}
	}
	return len(entries), nil
}

// recordForkMarkers appends a fork marker to both sides of the fork event: a
// forked_from entry on the fork (naming the source) and a forked_into entry on
// the source (naming the new fork), so each timeline shows where the branch
// happened when reopened. The fork-side marker takes the sequence slot right
// after the copied conversation so it renders at the fork point, while the
// source-side marker uses a fresh time-ordered ID.
func recordForkMarkers(ctx context.Context, src, dst timeline.Timeline, copyBase time.Time, copied int, sourceID, sourceTitle, forkID, forkName string) (*timeline.Entry, error) {
	marker := timeline.NewForkedEntry(api.ForkRecord{
		Direction: api.ForkDirectionFrom,
		SessionID: sourceID,
		Title:     sourceTitle,
	})
	marker.ID = uuid.Monotonic(copyBase, copied)
	if err := dst.Append(ctx, marker); err != nil {
		return nil, fmt.Errorf("session: append fork marker to fork timeline: %w", err)
	}
	srcMarker := timeline.NewForkedEntry(api.ForkRecord{
		Direction: api.ForkDirectionInto,
		SessionID: forkID,
		Title:     forkName,
	})
	if err := src.Append(ctx, srcMarker); err != nil {
		return nil, fmt.Errorf("session: append fork marker to source timeline: %w", err)
	}
	return srcMarker, nil
}

func forkTitle(parent string) string {
	if parent = strings.TrimSpace(parent); parent != "" {
		title, _ := strutils.TruncateLine(parent, forkTitleMaxBytes, "")
		return title + " (fork)"
	}
	return "Forked session"
}
