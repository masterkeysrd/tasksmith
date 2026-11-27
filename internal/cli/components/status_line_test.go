package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func initStatusLineTest(t *testing.T) {
	t.Helper()
	store.ResetApp()
	store.SetWorkspaceWarnings(nil)
	store.SetWorkspaceMCPServers(nil)
	store.ResetSession()
	store.ClearSlot()
	previous := store.GetStatusLine()
	_, _ = store.ApplyStatusLine(store.StatusLineConfig{})
	t.Cleanup(func() {
		store.ResetApp()
		store.SetWorkspaceWarnings(nil)
		store.SetWorkspaceMCPServers(nil)
		store.ResetSession()
		store.ClearSlot()
		_, _ = store.ApplyStatusLine(previous)
	})
}

func TestDefaultStatusLine(t *testing.T) {
	initStatusLineTest(t)
	store.SetSession(&api.Session{ID: "status", Context: api.ContextState{UsedTokens: 42800, MaxTokens: 200000}})
	store.SetMode(api.ModeAcceptEdits)
	store.SetModelInfo("Claude Sonnet", "high")
	store.SetUsage(api.SessionUsage{
		SessionID: "status",
		Totals:    api.UsageGroup{Calls: 2, CostNano: 40_000_000, Tokens: api.UsageTokens{Input: 18400, Output: 1200}},
	})
	store.SetWorkspaceWarnings([]string{"one", "two", "two", "three"})

	out := testutils.Render(t, DefaultStatusLine(), 100, 6)
	lines := testutils.TrimmedLines(out)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], store.DefaultHint) ||
		!strings.HasSuffix(lines[0], "[accept edits]  Claude Sonnet  high  ⚠ 3 warnings") {
		t.Fatalf("bad top row:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "Context 42.8k/200.0k (21%)") ||
		!strings.HasSuffix(lines[1], "Cost $0.04 · Tokens ↑18.4k ↓1.2k") ||
		ansi.StringWidth(lines[1]) != 100 {
		t.Fatalf("bad bottom row:\n%s", out)
	}

	out = testutils.Render(t, DefaultStatusLine(), 24, 6)
	lines = testutils.TrimmedLines(out)
	if len(lines) != 2 || !strings.Contains(lines[0], "[edits]") || !strings.HasSuffix(lines[0], "⚠ 3") {
		t.Fatalf("mode and warnings must survive at a narrow width:\n%s", out)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 24 {
			t.Fatalf("wrapped or clipped statusline: %q", line)
		}
	}
}

func TestRestrictedStatusLine(t *testing.T) {
	initStatusLineTest(t)
	store.SetWorkspaceRestricted(true)
	for _, width := range []int{80, 18} {
		out := testutils.Render(t, DefaultStatusLine(), width, 6)
		row := testutils.TrimmedLines(out)[0]
		if !strings.Contains(row, "⚠ restricted") || (width == 80 && !strings.Contains(row, "/setup")) {
			t.Fatal(out)
		}
	}
	store.SetWorkspaceRestricted(false)
	if out := testutils.Render(t, DefaultStatusLine(), 80, 6); strings.Contains(out, "restricted") {
		t.Fatal(out)
	}
}

func TestStatusLineModelProvider(t *testing.T) {
	for _, tc := range []struct {
		name, provider, id, config, label, want string
		width                                   int
		compact                                 bool
	}{
		{"resolved", "antigravity", "other/neon", "", "Gemini 4 Neon", "Gemini 4 Neon (antigravity)", 100, false},
		{"id", "", "google-genai/neon", "", "Gemini 4 Neon", "Gemini 4 Neon (google-genai)", 100, false},
		{"configured", "", "neon", "codex/neon", "Neon", "Neon (codex)", 100, false},
		{"qualified", "", "codex/neon", "", "", "codex/neon", 100, false},
		{"no provider", "", "mock", "@default", "Mock", "Mock", 100, false},
		{"compact", "antigravity", "neon", "", "Gemini 4 Neon", "Gemini 4 Neon", 100, true},
		{"narrow", "antigravity", "neon", "", "Gemini 4 Neon", "Gemini 4 Neon", 35, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initStatusLineTest(t)
			store.SetSession(&api.Session{Config: api.SessionConfig{Model: tc.config, Mode: api.ModeAuto}})
			store.SetSessionModelInfo(&api.Model{ID: tc.id, Provider: tc.provider, Label: tc.label}, nil)
			_, _ = store.ApplyStatusLine(store.StatusLineConfig{Rows: [][]store.StatusLineSegmentID{{store.StatusLineMode, store.StatusLineModel, store.StatusLineEffort}}, Compact: tc.compact})
			out := testutils.Render(t, DefaultStatusLine(), tc.width, 5)
			lines := testutils.TrimmedLines(out)
			if len(lines) != 1 || strings.Join(strings.Fields(lines[0]), " ") != "[auto] "+tc.want+" default effort" {
				t.Fatalf("unexpected status line:\n%s", out)
			}
		})
	}
}

func TestStatusLineHintAndCustomRows(t *testing.T) {
	initStatusLineTest(t)
	store.SetMode(api.ModePlan)
	store.SetModelInfo("TestModel", "high")
	store.SetHint("custom picker hint")
	store.EnqueueSlot(store.SlotItem{ID: "test", Kind: store.SlotKindPicker})
	out := testutils.Render(t, DefaultStatusLine(), 80, 5)
	if strings.Contains(out, "custom picker hint") || !strings.HasSuffix(strings.TrimSpace(out), "[plan]  TestModel  high") {
		t.Fatalf("expected hint hidden while a slot is active:\n%s", out)
	}
	store.SetCtrlCPending(true)
	out = testutils.Render(t, DefaultStatusLine(), 80, 5)
	if !strings.Contains(out, store.CtrlCHint) {
		t.Fatalf("exit confirmation must override slot hint hiding:\n%s", out)
	}

	_, _ = store.ApplyStatusLine(store.StatusLineConfig{Rows: [][]store.StatusLineSegmentID{nil, {store.StatusLineEffort, store.StatusLineMode}}})
	out = testutils.Render(t, DefaultStatusLine(), 80, 5)
	lines := testutils.TrimmedLines(out)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "high") || !strings.HasSuffix(lines[0], "[plan]") || strings.Contains(out, "TestModel") {
		t.Fatalf("expected one visible custom row in configured order:\n%s", out)
	}
}

func TestStatusLineBadgeOpensWarnings(t *testing.T) {
	initStatusLineTest(t)
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)
	const routine = "provider not configured"
	const security = "sandbox enforcement unavailable"
	const pickerTitle = "Warnings · 2 unresolved"
	store.SetWorkspaceWarnings([]string{routine, security}, security)
	_, _ = store.ApplyStatusLine(store.StatusLineConfig{Rows: [][]store.StatusLineSegmentID{{store.StatusLineWarnings}}})
	app := testutils.NewApp(t, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn)}, DefaultStatusLine(), Transcript(), Slot()), 100, 24)
	if text := app.Text(); !strings.Contains(text, "⚠ 2 warnings") || !strings.Contains(text, security) || strings.Contains(text, routine) {
		t.Fatalf("expected both warnings counted but only the security warning in the transcript:\n%s", text)
	}
	app.Engine().ProcessRawEvent(&backend.RawMouseEvent{X: 2, Y: 0, Button: event.ButtonLeft})
	app.Engine().ProcessRawEvent(&backend.RawMouseEvent{X: 2, Y: 0, Button: event.ButtonLeft, Up: true})
	app.WaitFor(func(text string) bool {
		_, picker, found := strings.Cut(text, pickerTitle)
		return found && strings.Contains(picker, routine) && strings.Contains(picker, security)
	})
}

func TestStatusLineQuota(t *testing.T) {
	initStatusLineTest(t)
	store.SetSession(&api.Session{ID: "quota"})
	store.SetSessionModelInfo(&api.Model{ID: "pool/gemini", Name: "Gemini", QuotaBilled: true}, nil)
	store.SetUsage(api.SessionUsage{
		SessionID: "quota",
		Totals:    api.UsageGroup{Calls: 4, CostNano: 100, QuotaCalls: 4, Tokens: api.UsageTokens{Input: 2_000_000, Output: 4_900}},
	})
	out := testutils.Render(t, DefaultStatusLine(), 100, 5)
	lines := testutils.TrimmedLines(out)
	if len(lines) != 2 || strings.TrimSpace(lines[1]) != "Tokens ↑2.0M ↓4.9k" {
		t.Fatalf("expected only token usage before a quota reading:\n%s", out)
	}
	reset := time.Now().Add(3*time.Hour + 40*time.Minute)
	store.SetQuota(api.ModelQuota{
		Model:   "pool/gemini",
		Buckets: []api.QuotaBucket{{ID: "5h", RemainingFraction: 0.94, ResetAt: &reset}},
	})
	out = testutils.Render(t, DefaultStatusLine(), 100, 5)
	if strings.Contains(out, "$") || !strings.Contains(out, "Quota 94% (↻ 3h") || !strings.Contains(out, ") · Tokens ↑2.0M ↓4.9k") {
		t.Fatalf("expected quota and reset countdown, not dollars:\n%s", out)
	}
}

func TestPlanStatusRowMeasuresTerminalCells(t *testing.T) {
	row := []store.StatusLineSegmentID{store.StatusLineHint, store.StatusLineMode, store.StatusLineWarnings}
	fields := map[store.StatusLineSegmentID]statusField{
		store.StatusLineHint:     {full: "開く開く開く開く"},
		store.StatusLineMode:     {full: "[plan]"},
		store.StatusLineWarnings: {full: "⚠ 1 warning", compact: "⚠ 1"},
	}
	parts, gap := planStatusRow(20, false, row, fields, "")
	if len(parts) != 3 || parts[0].text == fields[store.StatusLineHint].full || parts[2].text != "⚠ 1" {
		t.Fatalf("expected compact warning and truncated wide hint, got %+v", parts)
	}
	width := (len(parts) - 1) * gap
	for _, p := range parts {
		width += ansi.StringWidth(p.text)
	}
	if width > 20 {
		t.Fatalf("planned row uses %d cells", width)
	}
}
