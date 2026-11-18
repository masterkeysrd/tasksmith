package session

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestSessionForkCopiesConversationAndState(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
		return []message.AssistantChunk{{
			BaseChunk: message.BaseChunk{ID: "fork-src-reply"},
			Model:     req.Model,
			Content:   []message.Block{&message.TextBlock{Text: "parent reply"}},
			Done:      true,
		}}, nil
	}}
	_, _, _, pub, mgr := setupTestEnv(t, provider)
	defer func() { _ = mgr.Close() }()

	source, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Source"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := source.Send(ctx, api.SendRequest{Content: "hello parent"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := source.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	forked, err := source.Fork(ctx, "")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	defer func() { _ = forked.Close() }()

	if forked.ID == source.ID {
		t.Fatal("fork must get a fresh session ID")
	}
	if forked.ForkedFrom != source.ID {
		t.Errorf("ForkedFrom = %q, want %q", forked.ForkedFrom, source.ID)
	}
	if forked.ParentID != "" {
		t.Errorf("ParentID = %q, want empty (forks are top-level threads)", forked.ParentID)
	}
	if forked.Title != "Source (fork)" {
		t.Errorf("Title = %q, want %q", forked.Title, "Source (fork)")
	}
	if forked.Config.Model != source.Config.Model || forked.Config.Agent != source.Config.Agent {
		t.Errorf("fork config = %+v, want source config %+v", forked.Config, source.Config)
	}
	if forked.Status != api.StatusIdle || forked.Running {
		t.Errorf("fork status = %s running=%v, want idle", forked.Status, forked.Running)
	}

	// The fork's checkpoint carries the parent's full conversation verbatim.
	sourceState := loadHarnessState(t, mgr, source.ID)
	state := loadHarnessState(t, mgr, forked.ID)
	if len(state.Messages) != len(sourceState.Messages) || len(state.Messages) == 0 {
		t.Fatalf("forked messages = %d, source = %d", len(state.Messages), len(sourceState.Messages))
	}
	var forkTexts, sourceTexts []string
	for _, m := range state.Messages {
		forkTexts = append(forkTexts, msgText(m))
	}
	for _, m := range sourceState.Messages {
		sourceTexts = append(sourceTexts, msgText(m))
	}
	if !slices.Equal(forkTexts, sourceTexts) {
		t.Fatalf("forked messages = %q, want source messages %q", forkTexts, sourceTexts)
	}
	if !slices.Contains(forkTexts, "hello parent") {
		t.Fatalf("forked messages %q lack the parent's user turn", forkTexts)
	}

	// The fork's timeline mirrors the parent's conversation entries in order.
	sourceEntries, err := source.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("source Timeline.List: %v", err)
	}
	forkEntries, err := forked.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("fork Timeline.List: %v", err)
	}
	if len(forkEntries) != len(sourceEntries) || len(forkEntries) == 0 {
		t.Fatalf("fork timeline len = %d, source len = %d", len(forkEntries), len(sourceEntries))
	}
	for i := range sourceEntries {
		if forkEntries[i].Kind != sourceEntries[i].Kind {
			t.Errorf("entry %d kind = %s, want %s", i, forkEntries[i].Kind, sourceEntries[i].Kind)
		}
		if forkEntries[i].ID == sourceEntries[i].ID {
			t.Errorf("entry %d reused source ID %q", i, forkEntries[i].ID)
		}
		if forkEntries[i].SessionID != forked.ID {
			t.Errorf("entry %d session = %q, want %q", i, forkEntries[i].SessionID, forked.ID)
		}
		if forkEntries[i].ID <= forkEntries[0].ID && i > 0 {
			t.Errorf("entry %d ID %q sorts before first entry %q", i, forkEntries[i].ID, forkEntries[0].ID)
		}
	}

	// Both timelines end with a fork marker naming the counterpart session.
	srcMarker := sourceEntries[len(sourceEntries)-1]
	if srcMarker.Kind != api.TimelineKindForked || srcMarker.Forked == nil ||
		srcMarker.Forked.Direction != api.ForkDirectionInto ||
		srcMarker.Forked.SessionID != forked.ID || srcMarker.Forked.Title != forked.Title {
		t.Errorf("source timeline marker = %+v (%s), want forked_into %q", srcMarker.Forked, srcMarker.Kind, forked.ID)
	}
	forkMarker := forkEntries[len(forkEntries)-1]
	if forkMarker.Kind != api.TimelineKindForked || forkMarker.Forked == nil ||
		forkMarker.Forked.Direction != api.ForkDirectionFrom ||
		forkMarker.Forked.SessionID != source.ID || forkMarker.Forked.Title != source.Title {
		t.Errorf("fork timeline marker = %+v (%s), want forked_from %q", forkMarker.Forked, forkMarker.Kind, source.ID)
	}
	for _, entry := range forkEntries[:len(forkEntries)-1] {
		if entry.Kind == api.TimelineKindForked {
			t.Errorf("copied conversation carries an unexpected fork marker %q", entry.ID)
		}
	}

	// The source session's stream receives an EntryAddedEvent for forked_into.
	var sawSourceForkEvent bool
	for _, ev := range pub.recorded() {
		if eae, ok := ev.(api.EntryAddedEvent); ok &&
			eae.SessionID == source.ID &&
			eae.Entry.Kind == api.TimelineKindForked &&
			eae.Entry.Forked != nil &&
			eae.Entry.Forked.Direction == api.ForkDirectionInto &&
			eae.Entry.Forked.SessionID == forked.ID {
			sawSourceForkEvent = true
		}
	}
	if !sawSourceForkEvent {
		t.Errorf("expected EntryAddedEvent with forked_into marker on source session topic")
	}

	// A forked turn works: the fork continues from its own history while the
	// source stays untouched.
	if err := forked.Send(ctx, api.SendRequest{Content: "fork follow-up"}); err != nil {
		t.Fatalf("fork Send: %v", err)
	}
	if err := forked.Wait(ctx); err != nil {
		t.Fatalf("fork Wait: %v", err)
	}
	forkState := loadHarnessState(t, mgr, forked.ID)
	if len(forkState.Messages) <= len(sourceState.Messages) {
		t.Fatalf("fork messages after turn = %d, want more than source %d", len(forkState.Messages), len(sourceState.Messages))
	}
	if srcState := loadHarnessState(t, mgr, source.ID); len(srcState.Messages) != len(sourceState.Messages) {
		t.Errorf("source messages = %d after fork turn, want %d (fork must not mutate the parent)", len(srcState.Messages), len(sourceState.Messages))
	}
}

func TestSessionForkCustomTitle(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, mgr := setupTestEnv(t, nil)
	defer func() { _ = mgr.Close() }()

	source, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Source"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	forked, err := source.Fork(ctx, "  Exploration branch  ")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	defer func() { _ = forked.Close() }()

	if forked.Title != "Exploration branch" {
		t.Errorf("Title = %q, want %q", forked.Title, "Exploration branch")
	}
}

func TestSessionForkRejectsRunningSession(t *testing.T) {
	ctx := context.Background()
	blocked := make(chan struct{})
	release := make(chan struct{})
	provider := &fakeProvider{chunkHandler: func(ctx context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
		close(blocked)
		<-release
		return []message.AssistantChunk{{
			BaseChunk: message.BaseChunk{ID: "late"},
			Model:     req.Model,
			Content:   []message.Block{&message.TextBlock{Text: "done"}},
			Done:      true,
		}}, nil
	}}
	_, _, _, _, mgr := setupTestEnv(t, provider)
	defer func() { _ = mgr.Close() }()
	defer close(release)

	source, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Busy"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := source.Send(ctx, api.SendRequest{Content: "start"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-blocked

	if _, err := source.Fork(ctx, ""); !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("Fork while running = %v, want ErrSessionRunning", err)
	}
}

func TestSessionForkRejectsWaitingSession(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, mgr := setupTestEnv(t, nil)
	defer func() { _ = mgr.Close() }()

	source, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Waiting"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, status := range []api.SessionStatus{api.StatusWaitingPermission, api.StatusWaitingQuestion} {
		source.Lock()
		source.Status = status
		source.Unlock()
		if _, err := source.Fork(ctx, ""); !errors.Is(err, ErrSessionRunning) {
			t.Fatalf("Fork in status %s = %v, want ErrSessionRunning", status, err)
		}
	}
}

func TestSessionForkInheritsPermissionGrants(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, mgr := setupTestEnv(t, nil)
	defer func() { _ = mgr.Close() }()

	source, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Grants"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	source.Lock()
	source.Permissions.Allow = []string{"bash:git status"}
	source.Unlock()

	forked, err := source.Fork(ctx, "")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	defer func() { _ = forked.Close() }()

	if len(forked.Permissions.Allow) != 1 || forked.Permissions.Allow[0] != "bash:git status" {
		t.Errorf("fork grants = %+v, want the source allow rule", forked.Permissions.Allow)
	}
}

func TestSessionForkSurvivesRestore(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, mgr := setupTestEnv(t, nil)
	defer func() { _ = mgr.Close() }()

	source, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Persist"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	forked, err := source.Fork(ctx, "Persist branch")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if err := forked.CheckpointTo(ctx, mgr.store); err != nil {
		t.Fatalf("CheckpointTo: %v", err)
	}

	dbSess, err := mgr.store.Get(ctx, forked.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if dbSess.ForkedFrom != source.ID {
		t.Errorf("persisted ForkedFrom = %q, want %q", dbSess.ForkedFrom, source.ID)
	}

	latest, err := mgr.store.Latest(ctx)
	if err != nil {
		t.Fatalf("store.Latest: %v", err)
	}
	if latest.ID != forked.ID {
		t.Errorf("Latest = %q, want fork %q", latest.ID, forked.ID)
	}
}

func TestStoreLatestSkipsSubagentsAndOrdersByUpdate(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, mgr := setupTestEnv(t, nil)
	defer func() { _ = mgr.Close() }()

	store := mgr.store

	if _, err := store.Latest(ctx); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Latest on empty store = %v, want ErrSessionNotFound", err)
	}

	old, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Old"})
	if err != nil {
		t.Fatalf("Create old: %v", err)
	}
	recent, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Recent"})
	if err != nil {
		t.Fatalf("Create recent: %v", err)
	}
	// Backdate the older session so updated_at ordering is deterministic.
	oldDB, err := store.Get(ctx, old.ID)
	if err != nil {
		t.Fatalf("Get old: %v", err)
	}
	oldDB.UpdatedAt = oldDB.UpdatedAt.Add(-time.Hour)
	if err := store.Save(ctx, oldDB); err != nil {
		t.Fatalf("Save old: %v", err)
	}

	child := &Session{
		ID:        newV7(t),
		Title:     "child",
		ParentID:  recent.ID,
		CreatedAt: time.Now().UTC().Add(time.Hour),
		UpdatedAt: time.Now().UTC().Add(time.Hour),
	}
	if err := store.Save(ctx, child); err != nil {
		t.Fatalf("Save child: %v", err)
	}

	got, err := store.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.ID != recent.ID {
		t.Errorf("Latest = %q, want most recently updated top-level session %q", got.ID, recent.ID)
	}
	if got.ForkedFrom != "" {
		t.Errorf("ForkedFrom = %q, want empty", got.ForkedFrom)
	}
}
