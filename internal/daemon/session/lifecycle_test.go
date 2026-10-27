package session

import (
	"context"
	"errors"
	"sync"
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

	st.Status = StatusThinking
	st.Running = true
	st.StartedAt = time.Now().UTC()
	st.Error = "some error"

	clonedSt := st.Clone()
	if clonedSt.Status != StatusThinking || !clonedSt.Running || clonedSt.Error != "some error" {
		t.Fatalf("clonedSt mismatch: %+v", clonedSt)
	}

	// Verify empty status defaults to StatusIdle
	emptySt := &State{}
	if emptySt.Clone().Status != StatusIdle {
		t.Errorf("emptySt.Clone().Status = %q, want %q", emptySt.Clone().Status, StatusIdle)
	}

	now := time.Now().UTC()
	sess := &Session{
		State: st,
		ID:    "01931f7a-8b83-7182-8924-a7192ba87123",
		Title: "Test Session",
		Config: SessionConfig{
			Agent: "test-agent",
			Model: "test-model",
		},
		CreatedAt: now,
		UpdatedAt: now,
		Root:      "/tmp/session-dir",
		wg:        &sync.WaitGroup{},
	}

	clonedSess := sess.Clone()
	if clonedSess.ID != sess.ID || clonedSess.Title != sess.Title || clonedSess.Status != StatusThinking {
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
	if sess.Status != StatusThinking || !sess.Running || sess.Title != "Test Session" {
		t.Errorf("original sess modified after mutating clone: %+v", sess)
	}
}

func TestManagerStartTurnAndCancel(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{}
	_, _, _, pub, mgr := setupTestEnv(t, prov)
	defer mgr.Close()

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Async Turn Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1. Send nil message returns ErrNilMessage
	if err := sess.Send(ctx, SendInput{}); !errors.Is(err, ErrNilMessage) {
		t.Errorf("Send nil message err = %v, want %v", err, ErrNilMessage)
	}

	// 2. Send executes turn in background using default fakeProvider
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Hi")})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Wait for turn to complete
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Concurrency Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Start asynchronous turn
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("First")})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Verify session is now Running
	time.Sleep(10 * time.Millisecond)
	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusThinking || !got.Running {
		t.Errorf("got.Status = %q, Running = %v; want StatusThinking and Running=true", got.Status, got.Running)
	}

	// Decision while session is running should fail with ErrSessionRunning
	err = sess.Send(ctx, SendInput{Decision: &tools.PermissionDecision{Kind: tools.DecisionAllowOnce}})
	if !errors.Is(err, ErrSessionRunning) {
		t.Errorf("concurrent Send decision err = %v, want %v", err, ErrSessionRunning)
	}

	// User message while running should be enqueued without error
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Second")})
	if err != nil {
		t.Errorf("concurrent Send message err = %v, want nil", err)
	}
	if len(sess.VisibleQueue()) != 1 {
		t.Errorf("expected 1 queued message in VisibleQueue, got %d", len(sess.VisibleQueue()))
	}

	// Unblock first turn
	close(releaseTurn)

	// Wait for turn to finish
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait error: %v", err)
	}

	got, err = mgr.Get(ctx, sess.ID)
	if err != nil || got.Status != StatusIdle || got.Running {
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Error Turn Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Send should launch turn and Wait should return the error
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Fail please")})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	err = sess.Wait(ctx)
	if err == nil {
		t.Fatalf("expected error from sess.Wait")
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Cancel Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Hang")})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Cancel the turn
	cancelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sess.Cancel(cancelCtx); err != nil {
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Harness Invalidation"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Ensure harness is created
	h1, err := sess.harness(ctx)
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	if sess.Harness == nil || sess.Harness != h1 {
		t.Fatalf("expected sess.Harness to be cached")
	}

	// Update with same model/agent -> Harness remains cached
	_, err = mgr.Update(ctx, sess.ID, UpdateParams{Title: "New Title"})
	if err != nil {
		t.Fatalf("Update title: %v", err)
	}
	if sess.Harness == nil {
		t.Fatalf("expected sess.Harness to remain non-nil after title update")
	}

	// Update with new model -> Harness cleared
	_, err = mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Model: "custom-model"}})
	if err != nil {
		t.Fatalf("Update model: %v", err)
	}
	if sess.Harness != nil {
		t.Errorf("expected sess.Harness to be nil after model change, got %v", sess.Harness)
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Async Perm Test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Send stops when interrupted
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Write foo.txt")})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	_ = sess.Wait(ctx)

	// Check status is StatusWaitingPermission
	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusWaitingPermission || !got.WaitingPermission || got.Running {
		t.Errorf("got.Status = %q, WaitingPermission = %v, Running = %v; want StatusWaitingPermission",
			got.Status, got.WaitingPermission, got.Running)
	}

	if len(got.PendingPermissions) != 1 {
		t.Fatalf("expected 1 pending permission, got %d", len(got.PendingPermissions))
	}

	// Respond to permission via sess.Send with Decision
	err = sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: got.PendingPermissions[0].ID,
			Kind:      tools.DecisionAllowOnce,
		},
	})
	if err != nil {
		t.Fatalf("Send decision error: %v", err)
	}
	_ = sess.Wait(ctx)

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
