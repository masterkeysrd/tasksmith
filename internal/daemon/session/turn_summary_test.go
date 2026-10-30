package session

import (
	"context"
	"testing"
	"time"

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
	entries, err := s.Timeline.ListByKind(context.Background(), api.TimelineKindTurnCompleted)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestFinishTurnWritesSummaryEntry(t *testing.T) {
	_, _, _, pub, mgr := setupTestEnv(t, nil)
	s, _, err := mgr.Create(context.Background(), CreateParams{Title: "turns"})
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
	s, _, err := mgr.Create(context.Background(), CreateParams{Title: "short"})
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
