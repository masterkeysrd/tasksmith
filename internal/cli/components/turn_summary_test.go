package components

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestFormatTurnSummary(t *testing.T) {
	at := time.Date(2026, 10, 30, 14, 15, 0, 0, time.Local)
	done := FormatTurnSummary(api.TurnCompletedRecord{
		Status: api.TurnStatusDone, ElapsedMs: 42_000, InputTokens: 12_400, OutputTokens: 1_800, CostNano: 30_000_000,
	}, "entry-1", at)
	for _, want := range []string{" for 42s", "↑12.4k ↓1.8k", "$0.03", "done 2:15 PM"} {
		if !strings.Contains(done, want) {
			t.Errorf("missing %q in %q", want, done)
		}
	}
	if verb := strings.Fields(done)[0]; !strings.Contains(strings.Join(turnVerbs, " "), verb) {
		t.Errorf("verb %q not in the rotating set", verb)
	}
	if again := FormatTurnSummary(api.TurnCompletedRecord{Status: api.TurnStatusDone, ElapsedMs: 42_000}, "entry-1", at); strings.Fields(again)[0] != strings.Fields(done)[0] {
		t.Error("verb must be stable for the same entry")
	}

	if got := FormatTurnSummary(api.TurnCompletedRecord{Status: api.TurnStatusCanceled, ElapsedMs: 3_100}, "x", time.Time{}); got != "Stopped after 3.1s" {
		t.Errorf("canceled = %q", got)
	}
	if got := FormatTurnSummary(api.TurnCompletedRecord{Status: api.TurnStatusFailed, ElapsedMs: 9_000, CostNano: 1, Unpriced: true}, "x", time.Time{}); !strings.HasPrefix(got, "Failed after 9.0s") || !strings.Contains(got, "≥") {
		t.Errorf("failed = %q", got)
	}

	zeroCost := FormatTurnSummary(api.TurnCompletedRecord{
		Status: api.TurnStatusDone, ElapsedMs: 5_000, InputTokens: 100, OutputTokens: 50, CostNano: 0,
	}, "entry-zero", at)
	if !strings.Contains(zeroCost, "$0.00") {
		t.Errorf("zero cost turn with tokens must show $0.00, got %q", zeroCost)
	}
}

func TestTurnVerbsRotate(t *testing.T) {
	seen := map[string]bool{}
	for i := range 200 {
		seen[turnVerb(string(rune('a'+i%26))+strings.Repeat("z", i))] = true
	}
	if len(seen) < 3 {
		t.Fatalf("only %d distinct verbs: %v", len(seen), seen)
	}
}

func TestSpinnerStats(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 12, 0, time.UTC)
	start := now.Add(-12 * time.Second)
	if got := SpinnerStats(start, 12_400, 1_800, now); got != "(12s · ↑12.4k ↓1.8k)" {
		t.Errorf("stats = %q", got)
	}
	if got := SpinnerStats(start, 0, 0, now); got != "(12s · ↑0 ↓0)" {
		t.Errorf("turn start = %q", got)
	}
	if got := SpinnerStats(time.Time{}, 5, 0, now); got != "(↑5 ↓0)" {
		t.Errorf("tokens only = %q", got)
	}
	if got := SpinnerStats(time.Time{}, 0, 0, now); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestTurnStatsTokens(t *testing.T) {
	ts := turnStats{BaseInput: 1000, BaseOutput: 100, CallInput: 50, CallOutput: 10}
	totals := storeTotals(1500, 180)
	if ts.input(totals) != 550 || ts.output(totals) != 90 {
		t.Fatalf("input=%d output=%d", ts.input(totals), ts.output(totals))
	}
}

func storeTotals(in, out int64) store.UsageTotals {
	return store.UsageTotals{InputTokens: in, OutputTokens: out}
}
