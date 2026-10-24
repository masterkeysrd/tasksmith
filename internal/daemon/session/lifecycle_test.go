package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestStateAndSessionClone(t *testing.T) {
	st := NewState()
	if st.Status != StatusIdle {
		t.Fatalf("NewState().Status = %q, want %q", st.Status, StatusIdle)
	}
	if st.Running || st.WaitingPermission {
		t.Fatalf("expected NewState not running, got running=%v waiting=%v", st.Running, st.WaitingPermission)
	}

	st.Status = StatusRunning
	st.Running = true
	st.StartedAt = time.Now().UTC()
	st.Error = "some error"

	clonedSt := st.Clone()
	if clonedSt.Status != StatusRunning || !clonedSt.Running || clonedSt.Error != "some error" {
		t.Fatalf("clonedSt mismatch: %+v", clonedSt)
	}

	// Verify empty status defaults to StatusIdle
	emptySt := &State{}
	if emptySt.Clone().Status != StatusIdle {
		t.Errorf("emptySt.Clone().Status = %q, want %q", emptySt.Clone().Status, StatusIdle)
	}

	now := time.Now().UTC()
	sess := &Session{
		State:     st,
		ID:        "01931f7a-8b83-7182-8924-a7192ba87123",
		Title:     "Test Session",
		Agent:     "test-agent",
		Model:     "test-model",
		CreatedAt: now,
		UpdatedAt: now,
		Root:      "/tmp/session-dir",
	}

	clonedSess := sess.Clone()
	if clonedSess.ID != sess.ID || clonedSess.Title != sess.Title || clonedSess.Status != StatusRunning {
		t.Fatalf("clonedSess mismatch: %+v", clonedSess)
	}

	// Mutate cloned session; verify original is untouched
	clonedSess.Lock()
	clonedSess.Status = StatusIdle
	clonedSess.Running = false
	clonedSess.Title = "Mutated"
	clonedSess.Unlock()

	sess.Lock()
	defer sess.Unlock()
	if sess.Status != StatusRunning || !sess.Running || sess.Title != "Test Session" {
		t.Errorf("original sess modified after mutating clone: %+v", sess)
	}
}

func TestManagerStartTurnAndCancel(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{}
	_, _, _, pub, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, err := mgr.Create(ctx, CreateParams{Title: "Async Turn Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1. StartTurn nil message returns ErrNilMessage
	if err := mgr.StartTurn(ctx, sess.ID, nil, false); !errors.Is(err, ErrNilMessage) {
		t.Errorf("StartTurn nil message err = %v, want %v", err, ErrNilMessage)
	}

	// 2. StartTurn with wait=true executes turn to completion using default fakeProvider
	err = mgr.StartTurn(ctx, sess.ID, message.NewUserText("Hi"), true)
	if err != nil {
		t.Fatalf("StartTurn with wait=true error: %v", err)
	}

	// Verify session returned by Get is StatusIdle
	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusIdle || got.Running {
		t.Errorf("got.Status = %q, Running = %v; want StatusIdle and not running", got.Status, got.Running)
	}

	// Verify completed event was published
	var foundCompleted bool
	for _, ev := range pub.recorded() {
		if cev, ok := ev.(CompletedEvent); ok && cev.SessionID == sess.ID {
			foundCompleted = true
			break
		}
	}
	if !foundCompleted {
		t.Errorf("expected CompletedEvent to be published")
	}
}

func TestManagerConcurrentTurnRejection(t *testing.T) {
	ctx := context.Background()
	releaseTurn := make(chan struct{})
	prov := &fakeProvider{
		handler: func(ctx context.Context, req *llm.Request) ([]string, string, error) {
			select {
			case <-releaseTurn:
				return []string{"Done"}, uuid.NewString(), nil
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		},
	}
	_, _, _, _, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, err := mgr.Create(ctx, CreateParams{Title: "Concurrency Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Start asynchronous turn with wait=false
	err = mgr.StartTurn(ctx, sess.ID, message.NewUserText("First"), false)
	if err != nil {
		t.Fatalf("StartTurn error: %v", err)
	}

	// Verify session is now Running
	time.Sleep(10 * time.Millisecond)
	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusRunning || !got.Running {
		t.Errorf("got.Status = %q, Running = %v; want StatusRunning and Running=true", got.Status, got.Running)
	}

	// Second StartTurn should fail with ErrSessionRunning
	err = mgr.StartTurn(ctx, sess.ID, message.NewUserText("Second"), false)
	if !errors.Is(err, ErrSessionRunning) {
		t.Errorf("concurrent StartTurn err = %v, want %v", err, ErrSessionRunning)
	}

	// Unblock first turn
	close(releaseTurn)

	// Wait for turn to finish
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err = mgr.Get(ctx, sess.ID)
		if err == nil && got.Status == StatusIdle && !got.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != StatusIdle || got.Running {
		t.Fatalf("session failed to return to StatusIdle: %+v", got)
	}
}

func TestManagerTurnErrorPublishing(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return nil, "", errors.New("simulated llm failure")
		},
	}
	_, _, _, pub, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, err := mgr.Create(ctx, CreateParams{Title: "Error Turn Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// StartTurn with wait=true should return the error
	err = mgr.StartTurn(ctx, sess.ID, message.NewUserText("Fail please"), true)
	if err == nil {
		t.Fatalf("expected error from StartTurn")
	}

	// Verify session has error recorded and is StatusIdle
	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusIdle || got.Running {
		t.Errorf("got.Status = %q, Running = %v; want StatusIdle and not running", got.Status, got.Running)
	}
	if got.Error == "" {
		t.Errorf("expected non-empty got.Error")
	}

	// Verify ErrorEvent was published
	var foundErrorEvent bool
	for _, ev := range pub.recorded() {
		if eev, ok := ev.(ErrorEvent); ok && eev.SessionID == sess.ID {
			foundErrorEvent = true
			break
		}
	}
	if !foundErrorEvent {
		t.Errorf("expected ErrorEvent to be published: %+v", pub.recorded())
	}
}

func TestManagerCancelTurn(t *testing.T) {
	ctx := context.Background()
	hanging := make(chan struct{})
	prov := &fakeProvider{
		handler: func(ctx context.Context, _ *llm.Request) ([]string, string, error) {
			select {
			case <-hanging:
				return []string{"Finished"}, uuid.NewString(), nil
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		},
	}
	_, _, _, _, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, err := mgr.Create(ctx, CreateParams{Title: "Cancel Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = mgr.StartTurn(ctx, sess.ID, message.NewUserText("Hang"), false)
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Cancel the turn
	cancelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := mgr.Cancel(cancelCtx, sess.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	close(hanging)

	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusIdle || got.Running {
		t.Errorf("after Cancel: got.Status = %q, Running = %v", got.Status, got.Running)
	}
}

func TestManagerUpdateInvalidatesHarness(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{}
	_, _, _, _, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, err := mgr.Create(ctx, CreateParams{Title: "Harness Invalidation"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	liveSess, err := mgr.getLiveSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("getLiveSession: %v", err)
	}

	// Ensure harness is created
	h1, err := mgr.harness(ctx, liveSess)
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	if liveSess.Harness == nil || liveSess.Harness != h1 {
		t.Fatalf("expected liveSess.Harness to be cached")
	}

	// Update with same model/agent -> Harness remains cached
	_, err = mgr.Update(ctx, sess.ID, UpdateParams{Title: "New Title"})
	if err != nil {
		t.Fatalf("Update title: %v", err)
	}
	if liveSess.Harness == nil {
		t.Fatalf("expected liveSess.Harness to remain non-nil after title update")
	}

	// Update with new model -> Harness cleared
	_, err = mgr.Update(ctx, sess.ID, UpdateParams{Model: "custom-model"})
	if err != nil {
		t.Fatalf("Update model: %v", err)
	}
	if liveSess.Harness != nil {
		t.Errorf("expected liveSess.Harness to be nil after model change, got %v", liveSess.Harness)
	}
}

func TestManagerRespondPermissionAsync(t *testing.T) {
	ctx := context.Background()
	step := 0
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			step++
			if step == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.ToolCall{
								ID:   "call-write-1",
								Name: "write",
								Args: map[string]any{"path": "foo.txt", "content": "bar"},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "All done!"},
					},
					Done: true,
				},
			}, nil
		},
	}
	_, _, _, pub, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, err := mgr.Create(ctx, CreateParams{Title: "Async Perm Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// StartTurn with wait=true stops when interrupted
	err = mgr.StartTurn(ctx, sess.ID, message.NewUserText("Write foo.txt"), true)
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}

	// Check status is StatusWaitingPermission
	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusWaitingPermission || !got.WaitingPermission || got.Running {
		t.Errorf("got.Status = %q, WaitingPermission = %v, Running = %v; want StatusWaitingPermission",
			got.Status, got.WaitingPermission, got.Running)
	}

	pending, err := mgr.PendingPermissions(ctx, sess.ID)
	if err != nil {
		t.Fatalf("PendingPermissions: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending permission, got %d", len(pending))
	}

	// RespondPermissionAsync with wait=true
	err = mgr.RespondPermissionAsync(ctx, sess.ID, tools.PermissionDecision{
		RequestID: pending[0].ID,
		Kind:      tools.DecisionAllowOnce,
	}, true)
	if err != nil {
		t.Fatalf("RespondPermissionAsync error: %v", err)
	}

	// Status should now be StatusIdle
	gotAfter, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after resume: %v", err)
	}
	if gotAfter.Status != StatusIdle || gotAfter.Running || gotAfter.WaitingPermission {
		t.Errorf("gotAfter = %+v; want StatusIdle", gotAfter)
	}

	// Verify CompletedEvent published
	var completedCount int
	for _, ev := range pub.recorded() {
		if _, ok := ev.(CompletedEvent); ok {
			completedCount++
		}
	}
	if completedCount != 1 {
		t.Errorf("expected 1 CompletedEvent, got %d", completedCount)
	}
}
