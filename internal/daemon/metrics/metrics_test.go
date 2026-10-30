package metrics_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

func newStore(t *testing.T) *metrics.Store {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "daemon.db"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := metrics.NewStore(ctx, db, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		_ = db.Close()
	})
	return s
}

func scoped(session, parent string) context.Context {
	return scope.With(context.Background(), scope.Scope{
		SessionID: session, ParentSessionID: parent, WorkspaceRoot: "/ws", Agent: "build",
	})
}

func llmCall(model string, cost int64, priced bool, tier string) metrics.LLMCall {
	return metrics.LLMCall{
		Provider: "p", Model: model, Purpose: metrics.PurposeChat, Status: metrics.StatusOK,
		Tokens:   metrics.Tokens{Input: 100, Output: 10, CacheRead: 60, CacheWrite: 20},
		Cost:     metrics.Cost{Total: cost},
		CostTier: tier, Priced: priced, Duration: 250 * time.Millisecond,
	}
}

func TestHelpersWriteNonZeroMeasurementsAndAlwaysDuration(t *testing.T) {
	s := newStore(t)
	ctx := scoped("s1", "")
	if err := metrics.RecordLLMCall(ctx, s, metrics.LLMCall{Provider: "p", Model: "m", Status: metrics.StatusError}); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := s.LoadEntries(ctx, "s1")
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	e := entries[0]
	if len(e.Measurements) != 1 || e.Measurements[0].Metric != metrics.LatencyDuration {
		t.Fatalf("measurements = %+v", e.Measurements)
	}
	if e.Attr(metrics.AttrPriced) != "false" || e.Attr(metrics.AttrCostTier) != "" || e.Agent != "build" || e.WorkspaceRoot != "/ws" {
		t.Fatalf("unexpected entry %+v", e)
	}
}

func TestLiveUsageAndPublish(t *testing.T) {
	s := newStore(t)
	var got []api.SessionUsage
	live := metrics.NewLive(s, func(_ string, u api.SessionUsage) { got = append(got, u) })
	ctx := scoped("s1", "")

	_ = metrics.RecordLLMCall(ctx, live, llmCall("m1", 1000, true, ""))
	_ = metrics.RecordLLMCall(ctx, live, llmCall("m2", 2000, false, "long"))
	_ = metrics.RecordToolCall(ctx, live, metrics.ToolCall{Tool: "bash", Status: metrics.StatusOK, Duration: time.Second})
	_ = metrics.RecordToolCall(ctx, live, metrics.ToolCall{Tool: "bash", Status: metrics.StatusDenied})

	if len(got) != 4 {
		t.Fatalf("published %d updates", len(got))
	}
	u := got[3]
	if u.Totals.Calls != 2 || u.Totals.UnpricedCalls != 1 || u.Totals.CostNano != 3000 {
		t.Fatalf("totals = %+v", u.Totals)
	}
	if u.Totals.Tokens.Uncached() != 40 {
		t.Fatalf("uncached = %d", u.Totals.Tokens.Uncached())
	}
	if len(u.Tools) != 1 || u.Tools[0].Calls != 2 || u.Tools[0].Denied != 1 || u.Tools[0].Errors != 0 || u.Tools[0].MaxDurationMS != 1000 {
		t.Fatalf("tools = %+v", u.Tools)
	}

	// A fresh Live over the same DB loads identical totals from storage.
	fresh := metrics.NewLive(s, nil)
	loaded, err := fresh.SessionUsage(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Totals != u.Totals {
		t.Fatalf("loaded %+v != live %+v", loaded.Totals, u.Totals)
	}
}

func TestParentRollup(t *testing.T) {
	s := newStore(t)
	live := metrics.NewLive(s, nil)
	_ = metrics.RecordLLMCall(scoped("child", "parent"), live, llmCall("m", 500, true, ""))
	u, err := live.SessionUsage(context.Background(), "parent")
	if err != nil {
		t.Fatal(err)
	}
	if u.Totals.CostNano != 500 {
		t.Fatalf("parent totals = %+v", u.Totals)
	}
}

func TestQueryGroupingAndFilters(t *testing.T) {
	s := newStore(t)
	_ = metrics.RecordLLMCall(scoped("s1", ""), s, llmCall("m1", 100, true, ""))
	_ = metrics.RecordLLMCall(scoped("s1", ""), s, llmCall("m1", 300, true, ""))
	_ = metrics.RecordLLMCall(scoped("s2", ""), s, llmCall("m2", 50, true, ""))
	ctx := context.Background()
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	res, err := s.Query(ctx, metrics.Query{
		Kind:       metrics.KindLLMCall,
		Aggregates: []metrics.Aggregate{{Metric: metrics.CostTotal, Func: metrics.AggSum}, {Metric: metrics.CostTotal, Func: metrics.AggMax}},
		GroupBy:    []metrics.Dimension{"attr.model"},
		Bucket:     metrics.BucketDay,
		Filter:     metrics.Filter{WorkspaceRoot: "/ws", Attrs: map[string]string{"provider": "p"}},
		Since:      time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %+v", res.Rows)
	}
	first := res.Rows[0]
	if first.Group["attr.model"] != "m1" || first.Count != 2 || first.Values[metrics.CostTotal] != 300 || first.Bucket.IsZero() {
		t.Fatalf("first row = %+v", first)
	}

	one, err := s.Query(ctx, metrics.Query{
		Aggregates: []metrics.Aggregate{{Metric: metrics.CostTotal, Func: metrics.AggSum}},
		Filter:     metrics.Filter{SessionID: "s2", IncludeChildren: true},
	})
	if err != nil || len(one.Rows) != 1 || one.Rows[0].Values[metrics.CostTotal] != 50 {
		t.Fatalf("session filter: %+v err=%v", one, err)
	}
}

func TestQueryRejectsUnsafeIdentifiers(t *testing.T) {
	for _, q := range []metrics.Query{
		{GroupBy: []metrics.Dimension{"attr.model'); DROP TABLE metrics_records;--"}},
		{GroupBy: []metrics.Dimension{"not_a_column"}},
		{Filter: metrics.Filter{Attrs: map[string]string{"a'b": "x"}}},
		{Aggregates: []metrics.Aggregate{{Metric: "x'; --"}}},
		{Bucket: "year"},
	} {
		if _, _, err := metrics.BuildSQL(q); err == nil {
			t.Fatalf("expected error for %+v", q)
		}
	}
}

func TestSummaryAndFlushOnClose(t *testing.T) {
	s := newStore(t)
	ws := scoped("s1", "")
	_ = metrics.RecordLLMCall(ws, s, llmCall("m1", 100, true, ""))
	_ = metrics.RecordLLMCall(ws, s, llmCall("m1", 200, false, "long"))
	_ = metrics.RecordToolCall(ws, s, metrics.ToolCall{Tool: "read", Status: metrics.StatusError})

	sum, err := s.Summary(context.Background(), metrics.SummaryRequest{
		Scope: api.UsageScopeWorkspace, WorkspaceRoot: "/ws", GroupBy: api.UsageGroupByModel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Totals.Calls != 2 || sum.Totals.UnpricedCalls != 1 || sum.Totals.CostNano != 300 {
		t.Fatalf("totals = %+v", sum.Totals)
	}
	if len(sum.Groups) != 1 || sum.Groups[0].Key != "m1" {
		t.Fatalf("groups = %+v", sum.Groups)
	}
	if len(sum.Tools) != 1 || sum.Tools[0].Errors != 1 || sum.Tools[0].Calls != 1 {
		t.Fatalf("tools = %+v", sum.Tools)
	}

	other, err := s.Summary(context.Background(), metrics.SummaryRequest{
		Scope: api.UsageScopeWorkspace, WorkspaceRoot: "/other", GroupBy: api.UsageGroupByTier,
	})
	if err != nil || other.Totals.Calls != 0 {
		t.Fatalf("other workspace: %+v err=%v", other, err)
	}

	_ = metrics.RecordToolCall(ws, s, metrics.ToolCall{Tool: "late", Status: metrics.StatusOK})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ws, metrics.Record{Kind: metrics.KindToolCall}); err != metrics.ErrClosed {
		t.Fatalf("Record after close = %v", err)
	}
}

func chatCall(read, write int64) metrics.LLMCall {
	return metrics.LLMCall{
		Provider: "p", Model: "m", Purpose: metrics.PurposeChat, Status: metrics.StatusOK, Priced: true,
		Tokens: metrics.Tokens{Input: 1000, CacheRead: read, CacheWrite: write},
	}
}

func TestCacheMissAnalysis(t *testing.T) {
	s := newStore(t)
	live := metrics.NewLive(s, nil)
	ctx := scoped("s1", "")
	_ = metrics.RecordLLMCall(ctx, live, chatCall(0, 500))   // first call: no previous
	_ = metrics.RecordLLMCall(ctx, live, chatCall(500, 100)) // warm
	_ = metrics.RecordLLMCall(ctx, live, chatCall(0, 600))   // miss
	_ = metrics.RecordLLMCall(ctx, live, chatCall(600, 0))   // warm again
	_ = metrics.RecordLLMCall(ctx, live, metrics.LLMCall{Provider: "p", Model: "m", Purpose: metrics.PurposeCompaction, Status: metrics.StatusOK})
	_ = metrics.RecordLLMCall(ctx, live, chatCall(0, 200)) // drop after compaction: rebuild, not a miss

	u, err := live.SessionUsage(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	c := u.Cache
	if c.Requests != 5 || c.Misses != 1 || c.Rebuilds != 1 || c.LastMissRecachedTokens != 600 || c.LastMissAt.IsZero() {
		t.Fatalf("cache = %+v", c)
	}
	if got := c.HitRate(); got != 1100.0/5000.0 {
		t.Fatalf("hit rate = %v", got)
	}
	if u.FirstAt.IsZero() || u.LastAt.Before(u.FirstAt) {
		t.Fatalf("bounds = %v..%v", u.FirstAt, u.LastAt)
	}
}

func TestFileChangesExcludeReverts(t *testing.T) {
	s := newStore(t)
	live := metrics.NewLive(s, nil)
	ctx := scoped("s1", "")
	_ = metrics.RecordFileChange(ctx, live, metrics.FileChange{Tool: "edit", Path: "a.go", Added: 10, Removed: 2})
	_ = metrics.RecordFileChange(ctx, live, metrics.FileChange{Tool: "write", Path: "b.go", Added: 5})
	_ = metrics.RecordFileChange(ctx, live, metrics.FileChange{Tool: "revert", Path: "a.go", Added: 2, Removed: 10, Revert: true})

	u, err := live.SessionUsage(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if u.Changes != (api.UsageChanges{LinesAdded: 15, LinesRemoved: 2, Files: 2}) {
		t.Fatalf("changes = %+v", u.Changes)
	}

	sum, err := s.Summary(context.Background(), metrics.SummaryRequest{
		Scope: api.UsageScopeWorkspace, WorkspaceRoot: "/ws", GroupBy: api.UsageGroupByDay,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Changes != u.Changes {
		t.Fatalf("summary changes = %+v", sum.Changes)
	}
	var edit api.ToolUsage
	for _, tool := range sum.Tools {
		if tool.Tool == "edit" {
			edit = tool
		}
	}
	if edit.LinesAdded != 10 || edit.LinesRemoved != 2 {
		t.Fatalf("edit tool = %+v", edit)
	}
}

func TestSummaryGlobalSessionsPreviousAndErrors(t *testing.T) {
	s := newStore(t)
	other := scope.With(context.Background(), scope.Scope{SessionID: "s9", WorkspaceRoot: "/other"})
	_ = metrics.RecordLLMCall(scoped("s1", ""), s, llmCall("m", 700, true, ""))
	_ = metrics.RecordLLMCall(scoped("s1", ""), s, metrics.LLMCall{Provider: "p", Model: "m", Status: metrics.StatusError})
	_ = metrics.RecordLLMCall(other, s, llmCall("m", 100, true, ""))

	sum, err := s.Summary(context.Background(), metrics.SummaryRequest{
		Scope: api.UsageScopeGlobal, GroupBy: api.UsageGroupByProvider, Since: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Sessions != 2 || len(sum.TopSessions) != 2 || sum.TopSessions[0].SessionID != "s1" {
		t.Fatalf("sessions = %+v", sum.TopSessions)
	}
	if len(sum.Workspaces) != 2 || sum.Totals.Errors != 1 {
		t.Fatalf("workspaces=%+v totals=%+v", sum.Workspaces, sum.Totals)
	}
	if sum.Previous == nil || sum.Previous.Calls != 0 {
		t.Fatalf("previous = %+v", sum.Previous)
	}
}

func TestTurnUsageSumsTurns(t *testing.T) {
	s := newStore(t)
	turn := func(id string) context.Context {
		return scope.With(scoped("s1", ""), scope.Scope{TurnID: id})
	}
	_ = metrics.RecordLLMCall(turn("t1"), s, llmCall("m", 100, true, ""))
	_ = metrics.RecordLLMCall(turn("t1"), s, llmCall("m", 200, false, ""))
	_ = metrics.RecordLLMCall(turn("t2"), s, llmCall("m", 400, true, ""))
	_ = metrics.RecordLLMCall(turn("t3"), s, llmCall("m", 800, true, ""))

	got, err := s.TurnUsage(context.Background(), "t1", "t2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Calls != 3 || got.CostNano != 700 || got.UnpricedCalls != 1 || got.Tokens.Input != 300 || got.Tokens.Output != 30 {
		t.Fatalf("usage = %+v", got)
	}
	none, err := s.TurnUsage(context.Background(), "missing")
	if err != nil || none.Calls != 0 {
		t.Fatalf("missing turn: %+v err=%v", none, err)
	}
}
