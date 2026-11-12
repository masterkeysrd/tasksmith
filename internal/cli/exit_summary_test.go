package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

func sampleUsage() api.SessionUsage {
	return api.SessionUsage{
		SessionID: "019a75f0-7bf3-712c-8ea9-b865c2645b22",
		Totals: api.UsageGroup{
			Calls:      12,
			CostNano:   180_000_000,
			DurationMS: 62_000,
			Tokens: api.UsageTokens{
				Input:      25_100,
				Output:     1_200,
				CacheRead:  21_400,
				CacheWrite: 300,
			},
		},
		Changes: api.UsageChanges{LinesAdded: 42, LinesRemoved: 11, Files: 5},
	}
}

func TestFormatExitSummary(t *testing.T) {
	out := FormatExitSummary(ExitSummaryInput{
		SessionID: "sess-123",
		Usage:     sampleUsage(),
	})

	if strings.Contains(out, "\x1b[") {
		t.Errorf("expected plain text without ANSI codes, got:\n%s", out)
	}
	for _, want := range []string{
		"Changes", "+42", "-11", "(5 files)",
		"Cost", "$0.18", "1m 02s",
		"Tokens", "25.1k", "21.4k cached", "300 written", "1.2k",
		"Resume", "tasksmith --session sess-123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected exit summary to contain %q, got:\n%s", want, out)
		}
	}

	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 rows (3 logo + 1 blank gutter), got %d:\n%s", len(lines), out)
	}
	for i, line := range lines {
		if !strings.Contains(line, "   ") {
			t.Errorf("row %d missing logo gutter: %q", i, line)
		}
	}
}

func TestFormatExitSummaryQuotaRow(t *testing.T) {
	out := FormatExitSummary(ExitSummaryInput{
		SessionID: "sess-123",
		Usage:     sampleUsage(),
		Quota: &api.ModelQuota{
			Model:             "antigravity/gemini",
			RemainingFraction: 0.94,
			ResetAt:           time.Now().Add(3*time.Hour + 40*time.Minute),
			Account:           "user@example.com",
		},
	})

	if !strings.Contains(out, "Quota") {
		t.Errorf("expected Quota label, got:\n%s", out)
	}
	if strings.Contains(out, "Cost ") {
		t.Errorf("expected Cost label replaced by Quota, got:\n%s", out)
	}
	if !strings.Contains(out, "94%") || !strings.Contains(out, "↻") {
		t.Errorf("expected quota percentage and reset countdown, got:\n%s", out)
	}
	if strings.Contains(out, "$0.18") {
		t.Errorf("quota mode must not render dollar cost, got:\n%s", out)
	}
}

func TestFormatExitSummaryUnpricedAndEmpty(t *testing.T) {
	u := sampleUsage()
	u.Totals.CostNano = 0
	u.Totals.UnpricedCalls = u.Totals.Calls
	out := FormatExitSummary(ExitSummaryInput{SessionID: "s", Usage: u})
	if !strings.Contains(out, "— (") {
		t.Errorf("expected em-dash degradation for zero-call session, got:\n%s", out)
	}

	if got := FormatExitSummary(ExitSummaryInput{}); got != "" {
		t.Errorf("expected empty output for draft session, got %q", got)
	}
}

func TestFormatExitSummaryColors(t *testing.T) {
	out := FormatExitSummary(ExitSummaryInput{
		SessionID: "sess-123",
		Usage:     sampleUsage(),
		Color:     true,
	})
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Errorf("expected true-color ANSI sequences when Color is enabled, got:\n%s", out)
	}
}
