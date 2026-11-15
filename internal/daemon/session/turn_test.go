package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
)

type fakeTurnMetrics struct {
	ids   []string
	usage api.UsageGroup
}

func (f *fakeTurnMetrics) Record(context.Context, metrics.Record) error { return nil }

func (f *fakeTurnMetrics) TurnUsage(_ context.Context, ids ...string) (api.UsageGroup, error) {
	f.ids = ids
	return f.usage, nil
}

func turnEntries(t *testing.T, s *Session) []timeline.Entry {
	t.Helper()
	entries, err := s.Timeline.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []timeline.Entry
	for _, e := range entries {
		if e.Kind == api.TimelineKindTurnCompleted {
			out = append(out, e)
		}
	}
	return out
}

func TestFinishTurnWritesSummaryEntry(t *testing.T) {
	_, _, _, pub, mgr := setupTestEnv(t, nil)
	s, _, err := mgr.Create(context.Background(), api.CreateSessionRequest{Title: "turns"})
	if err != nil {
		t.Fatal(err)
	}
	fm := &fakeTurnMetrics{usage: api.UsageGroup{
		Calls: 2, UnpricedCalls: 1, CostNano: 42_000_000,
		Tokens: api.UsageTokens{Input: 1200, Output: 300},
	}}
	s.metrics = fm

	// Two user messages joined the same turn; the turn started 8s ago.
	s.beginTurn("m1", time.Now().Add(-8*time.Second))
	s.beginTurn("m2", time.Now())
	s.finishTurn(api.TurnStatusDone)

	entries := turnEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	rec := entries[0].TurnCompleted
	if rec.Status != api.TurnStatusDone || rec.TurnID != "m2" || rec.ElapsedMs < 8000 ||
		rec.InputTokens != 1200 || rec.OutputTokens != 300 || rec.CostNano != 42_000_000 || !rec.Unpriced {
		t.Fatalf("record = %+v", rec)
	}
	if len(fm.ids) != 2 || fm.ids[0] != "m1" || fm.ids[1] != "m2" {
		t.Fatalf("usage queried for %v", fm.ids)
	}
	published := false
	for _, ev := range pub.recorded() {
		if e, ok := ev.(api.EntryAddedEvent); ok && e.Entry.Kind == api.TimelineKindTurnCompleted {
			published = true
		}
	}
	if !published {
		t.Fatal("turn summary entry was not published")
	}

	// State is cleared: finishing again writes nothing.
	s.finishTurn(api.TurnStatusDone)
	if got := len(turnEntries(t, s)); got != 1 {
		t.Fatalf("entries after second finish = %d", got)
	}
}

func TestFinishTurnSkipsShortDoneTurnsButNotFailures(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	s, _, err := mgr.Create(context.Background(), api.CreateSessionRequest{Title: "short"})
	if err != nil {
		t.Fatal(err)
	}

	s.beginTurn("m1", time.Now().Add(-time.Second))
	s.finishTurn(api.TurnStatusDone)
	if got := len(turnEntries(t, s)); got != 0 {
		t.Fatalf("short done turn wrote %d entries", got)
	}

	s.beginTurn("m2", time.Now().Add(-time.Second))
	s.finishTurn(api.TurnStatusFailed)
	entries := turnEntries(t, s)
	if len(entries) != 1 || entries[0].TurnCompleted.Status != api.TurnStatusFailed {
		t.Fatalf("entries = %+v", entries)
	}

	s.finishTurn(api.TurnStatusCanceled) // no turn in progress
	if got := len(turnEntries(t, s)); got != 1 {
		t.Fatalf("entries = %d", got)
	}
}

func TestUpstreamContextCanceledReportsFailureNotUserInterrupt(t *testing.T) {
	ctx := context.Background()

	prov := &fakeProvider{
		handler: func(context.Context, *llm.Request) ([]string, string, error) {
			return nil, "", fmt.Errorf("antigravity: acquire token: %w", context.Canceled)
		},
	}
	_, _, _, pub, mgr := setupTestEnv(t, prov)

	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "upstream-canceled"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sess.Send(ctx, api.SendRequest{Content: "Start the task"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait error = %v, want wrapped context.Canceled", err)
	}

	sess.Lock()
	gotSessionErr := sess.Error
	sess.Unlock()
	if !strings.Contains(gotSessionErr, "antigravity: acquire token") {
		t.Errorf("Session.Error = %q, want substring %q", gotSessionErr, "antigravity: acquire token")
	}

	var sawErrorEvent bool
	for _, ev := range pub.recorded() {
		if errEv, ok := ev.(api.ErrorEvent); ok && strings.Contains(errEv.Error, "antigravity: acquire token") {
			sawErrorEvent = true
		}
	}
	if !sawErrorEvent {
		t.Fatal("expected api.ErrorEvent to be published for upstream context.Canceled")
	}

	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline.List: %v", err)
	}
	var (
		sawInterrupted bool
		sawFailedTurn  bool
	)
	for _, e := range entries {
		switch e.Kind {
		case api.TimelineKindInterrupted:
			sawInterrupted = true
		case api.TimelineKindTurnCompleted:
			if e.TurnCompleted != nil && e.TurnCompleted.Status == api.TurnStatusFailed {
				sawFailedTurn = true
			}
		}
	}
	if sawInterrupted {
		t.Error("unexpected TimelineKindInterrupted entry when user did not cancel")
	}
	if !sawFailedTurn {
		t.Error("expected TurnCompleted entry with TurnStatusFailed")
	}
}

func TestUserCancelRecordsInterruptedEntryWithoutErrorEvent(t *testing.T) {
	ctx := context.Background()

	streamStarted := make(chan struct{})
	prov := &fakeProvider{
		chunkHandler: func(turnCtx context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			close(streamStarted)
			<-turnCtx.Done()
			return nil, turnCtx.Err()
		},
	}
	_, _, _, pub, mgr := setupTestEnv(t, prov)

	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "user-cancel"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sess.Send(ctx, api.SendRequest{Content: "Start the task"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	<-streamStarted
	if err := sess.Cancel(ctx); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	sess.Lock()
	gotSessionErr := sess.Error
	sess.Unlock()
	if gotSessionErr != "" {
		t.Errorf("Session.Error = %q, want empty on user cancel", gotSessionErr)
	}

	for _, ev := range pub.recorded() {
		if errEv, ok := ev.(api.ErrorEvent); ok {
			t.Errorf("unexpected api.ErrorEvent on user cancel: %+v", errEv)
		}
	}

	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline.List: %v", err)
	}
	var sawInterrupted bool
	for _, e := range entries {
		if e.Kind == api.TimelineKindInterrupted {
			sawInterrupted = true
		}
	}
	if !sawInterrupted {
		t.Fatal("expected TimelineKindInterrupted entry on user cancel")
	}
}
