package components

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestMascotMark(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	wantColors := [3]color.RGBA{
		{R: 42, G: 195, B: 222, A: 255},
		{R: 26, G: 188, B: 156, A: 255},
		{R: 255, G: 95, B: 95, A: 255},
	}
	for _, tt := range []struct {
		pose mascotPose
		rows [3]string
	}{
		{mascotT, [3]string{"◥██████████◣", "▐██▌", "◢████◣"}},
		{mascotMorph, [3]string{"▜████████▛", "▐██▌", "▟▛▀▀▜▙"}},
		{mascotReady, [3]string{"▜████████▛", "█▜▛█", "▟▛▀▀▜▙"}},
		{mascotBlink, [3]string{"▜████████▛", "████", "▟▛▀▀▜▙"}},
		{mascotLeft, [3]string{"▜████████▛", "▛█▜█", "▟▛▀▀▜▙"}},
		{mascotRight, [3]string{"▜████████▛", "█▛█▜", "▟▛▀▀▜▙"}},
	} {
		text := env.Render(trail.Box(trail.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow)},
			mascotMark(tt.pose), trail.Span(trail.SpanProps{}, trail.Text("TaskSmith")),
		))
		lines := testutils.TrimmedLines(text)
		if len(lines) != 3 || env.Backend.LastFrame().Surface.CellAt(16, 0).Content != "T" {
			t.Fatalf("pose %d changed banner dimensions:\n%s", tt.pose, text)
		}
		frame := env.Backend.LastFrame().Surface
		for y, row := range lines {
			if !strings.Contains(row, tt.rows[y]) {
				t.Fatalf("pose %d row %d missing %q:\n%s", tt.pose, y, tt.rows[y], text)
			}
			if got := frame.CellAt(6, y).Fg; got != wantColors[y] {
				t.Errorf("pose %d row %d foreground = %v, want %v", tt.pose, y, got, wantColors[y])
			}
		}
	}
}

func TestAnimatedMascotRespondsToPromptInput(t *testing.T) {
	app := testutils.NewApp(t, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn)},
		animatedMascot(100*time.Millisecond), kitex.Input(kitex.InputProps{ID: "prompt"}),
	), 80, 24)
	doc := app.Engine().Document()
	doc.Focus(doc.GetElementByID("prompt"))
	waitFor := func(face string) {
		t.Helper()
		app.WaitFor(func(text string) bool {
			lines := testutils.TrimmedLines(text)
			return len(lines) >= 2 && strings.TrimSpace(lines[1]) == face
		})
	}

	if text := app.Text(); !strings.Contains(text, "◥██████████◣") {
		t.Fatalf("missing startup T:\n%s", text)
	}
	waitFor("█▜▛█")
	waitFor("████")
	waitFor("▛█▜█")
	waitFor("█▛█▜")
	if text := app.Press(key.Key{Code: 'a', Text: "a"}); !strings.Contains(text, "█▜▛█") || !strings.Contains(text, "\na") {
		t.Fatalf("typing did not reset the mascot and reach the prompt:\n%s", text)
	}

	waitFor("▛█▜█")
	app.Engine().ProcessRawEvent(&backend.RawBracketedPaste{Text: "pasted"})
	if text := app.Text(); !strings.Contains(text, "█▜▛█") || !strings.Contains(text, "apasted") {
		t.Fatalf("paste did not reset the mascot and reach the prompt:\n%s", text)
	}
}
