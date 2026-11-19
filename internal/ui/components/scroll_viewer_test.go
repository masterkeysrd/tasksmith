package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestUseScrollPageOnlyAndFollowTail(t *testing.T) {
	harness := trail.FC("ScrollHarness", func(pageOnly bool) trail.Node {
		scroll := UseScroll(ScrollOptions{
			FollowTail: true,
			PageOnly:   pageOnly,
		})
		trail.UseInput(func(input string, k event.KeyEvent) {
			scroll.HandleKey(input, k)
		})
		return trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn)},
			trail.Text(fmt.Sprintf("pos=%d auto=%v", scroll.Pos, scroll.AutoScroll)),
			trail.Box(trail.BoxProps{
				Ref: scroll.Ref,
				Style: style.S().
					MaxHeight(style.Cells(5)).
					OverflowY(style.OverflowAuto),
			}, trail.Text("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8")),
		)
	})

	env := testutils.NewEnv(t, 60, 12)
	out := env.Render(harness(true))
	if !strings.Contains(out, "pos=0 auto=true") {
		t.Fatalf("expected initial FollowTail state, got:\n%s", out)
	}

	// PageOnly ignores j/k/up/down so embedded menus keep them.
	out = env.Press(key.Key{Code: 'j', Text: "j"})
	if !strings.Contains(out, "pos=0 auto=true") {
		t.Fatalf("PageOnly should ignore 'j', got:\n%s", out)
	}

	// 'g' jumps to top and detaches autoScroll; 'G' re-attaches FollowTail.
	out = env.Press(key.Key{Code: 'g', Text: "g"})
	if !strings.Contains(out, "pos=0 auto=false") {
		t.Fatalf("expected 'g' to set auto=false, got:\n%s", out)
	}
	out = env.Press(key.Key{Code: 'g', Text: "G", Mod: key.ModShift})
	if !strings.Contains(out, "pos=9999 auto=true") {
		t.Fatalf("expected 'G' to jump to bottom and re-attach auto=true, got:\n%s", out)
	}

	// PgUp detaches autoScroll and moves toward 0.
	out = env.Press(key.Key{Code: key.KeyPgUp})
	if !strings.Contains(out, "pos=9991 auto=false") {
		t.Fatalf("expected pgup to subtract page step and detach auto, got:\n%s", out)
	}
}

func TestScrollViewerMarkdownScrollingActionsAndEmptyState(t *testing.T) {
	// Empty state renders EmptyText.
	emptyOut := testutils.Render(t, ScrollViewer(ScrollViewerProps{
		Header:    []trail.Node{trail.Text("Inspector")},
		EmptyText: "(nothing to display)",
	}), 80, 20)
	if !strings.Contains(emptyOut, "Inspector") || !strings.Contains(emptyOut, "(nothing to display)") {
		t.Fatalf("expected header and empty text, got:\n%s", emptyOut)
	}

	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("Line %02d: content", i))
	}
	var closed, actionFired int
	var scrollPositions []int

	env := testutils.NewEnv(t, 80, 20)
	env.Render(ScrollViewer(ScrollViewerProps{
		Header:             []trail.Node{trail.Text("Doc Viewer")},
		HeaderMarginBottom: 1,
		Meta:               []trail.Node{trail.Text("Meta: v1")},
		Markdown:           strings.Join(lines, "\n\n"),
		OnScroll: func(pos int) {
			scrollPositions = append(scrollPositions, pos)
		},
		OnClose: func() { closed++ },
		Actions: []ScrollViewerAction{
			{Keys: []string{"e"}, Handler: func() { actionFired++ }},
		},
		Bindings: []KeyBinding{
			{Key: "e", Label: "Edit"},
			{Key: "esc", Label: "Close"},
		},
	}))

	out := env.Text()
	for _, want := range []string{"Doc Viewer", "Meta: v1", "Line 01: content", "e Edit", "esc Close"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// Scroll down via j and PgDn; header should show "line +N".
	out = env.Press(key.Key{Code: 'j', Text: "j"})
	if !strings.Contains(out, "line +1") {
		t.Fatalf("expected 'line +1' after j, got:\n%s", out)
	}
	out = env.Press(key.Key{Code: key.KeyPgDown})
	if !strings.Contains(out, "line +9") {
		t.Fatalf("expected 'line +9' after pgdn, got:\n%s", out)
	}

	// Shift+G jumps to bottom, 'g' jumps back to top (hiding line +N).
	out = env.Press(key.Key{Code: 'G', Text: "G", Mod: key.ModShift})
	if !strings.Contains(out, "line +9999") {
		t.Fatalf("expected bottom scroll on G, got:\n%s", out)
	}
	out = env.Press(key.Key{Code: 'g', Text: "g"})
	if strings.Contains(out, "line +") {
		t.Fatalf("expected line indicator cleared at top on g, got:\n%s", out)
	}

	// Custom action fires before OnClose/scroll.
	env.Press(key.Key{Code: 'e', Text: "e"})
	if actionFired != 1 {
		t.Fatalf("expected actionFired=1, got %d", actionFired)
	}

	// Escape fires OnClose.
	env.Press(key.Key{Code: key.KeyEscape})
	if closed != 1 {
		t.Fatalf("expected closed=1, got %d", closed)
	}
	if len(scrollPositions) == 0 {
		t.Fatalf("expected OnScroll callbacks to fire")
	}
}

func TestScrollViewerDisabledIgnoresInput(t *testing.T) {
	var closed, actionFired int
	env := testutils.NewEnv(t, 80, 16)
	env.Render(ScrollViewer(ScrollViewerProps{
		Header:   []trail.Node{trail.Text("Locked")},
		Markdown: "Line 1\n\nLine 2\n\nLine 3",
		Disabled: true,
		OnClose:  func() { closed++ },
		Actions: []ScrollViewerAction{
			{Keys: []string{"p"}, Handler: func() { actionFired++ }},
		},
	}))

	out := env.Press(key.Key{Code: 'j', Text: "j"})
	env.Press(key.Key{Code: 'p', Text: "p"})
	env.Press(key.Key{Code: key.KeyEscape})
	if closed != 0 || actionFired != 0 || strings.Contains(out, "line +") {
		t.Fatalf("expected disabled ScrollViewer to ignore all input, closed=%d action=%d\n%s",
			closed, actionFired, out)
	}
}

func TestScrollViewerCustomChildren(t *testing.T) {
	out := testutils.Render(t, ScrollViewer(ScrollViewerProps{
		Header: []trail.Node{trail.Text("Custom Body")},
	},
		trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)}, trail.Text("Child row 1")),
		trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)}, trail.Text("Child row 2")),
	), 80, 16)

	for _, want := range []string{"Custom Body", "Child row 1", "Child row 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
