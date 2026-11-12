package command

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

// resetStatusLine restores the default composition so view tests start from a
// known state regardless of test ordering within the package.
func resetStatusLine(t *testing.T) {
	t.Helper()
	if _, err := store.ApplyStatusLine(store.StatusLineConfig{Segments: store.DefaultStatusLineSegments}); err != nil {
		t.Fatalf("reset status line: %v", err)
	}
}

func TestStatusLineCommandArgsValidation(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	resetStatusLine(t)

	reg := NewRegistry()

	if err := reg.Execute("/statusline extra"); err == nil {
		t.Fatal("expected error when running /statusline with args, got nil")
	}

	store.ClearSlot()
	defer store.ClearSlot()
	if err := reg.Execute("/statusline"); err != nil {
		t.Fatalf("unexpected error running /statusline: %v", err)
	}
	st := store.GetSlot()
	if st.Active == nil || st.Active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot to be enqueued, got %+v", st.Active)
	}

	// The /status alias resolves to the same command.
	if err := reg.Execute("/status"); err != nil {
		t.Fatalf("unexpected error running /status alias: %v", err)
	}
}

func TestStatusLineEditorView(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	resetStatusLine(t)

	store.ClearSlot()
	defer store.ClearSlot()

	app := testutils.NewApp(t, StatusLineEditor("slot-statusline"), 100, 30)

	text := app.Text()
	if !strings.Contains(text, "Status line configuration") {
		t.Fatalf("expected editor title, got:\n%s", text)
	}
	for _, want := range []string{"hint", "mode", "context", "usage", "model", "Save", "compact: off"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected row %q in:\n%s", want, text)
		}
	}

	// Space toggles the row under the cursor (first row: hint) off live.
	app.Press(key.Key{Code: ' ', Text: " "})
	if segments := store.GetStatusLine().Segments; len(segments) != 4 || slices.Contains(segments, store.StatusLineHint) {
		t.Fatalf("expected hint toggled off, got %v", segments)
	}
	if !strings.Contains(app.Text(), "hint shortcut hint / cancel prompt (disabled)") {
		t.Fatalf("expected hint row marked disabled after toggle:\n%s", app.Text())
	}

	// 'c' toggles compact mode.
	app.Press(key.Key{Code: 'c', Text: "c"})
	if !store.GetStatusLine().Compact {
		t.Fatal("expected compact enabled after 'c'")
	}
	if !strings.Contains(app.Text(), "compact: on") {
		t.Fatalf("expected compact row to report on:\n%s", app.Text())
	}

	// 's' persists the draft and dismisses the editor.
	app.Press(key.Key{Code: 's', Text: "s"})
	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(data)
	if !strings.Contains(saved, `"mode"`) || strings.Contains(saved, `"hint"`) {
		t.Fatalf("unexpected saved config:\n%s", saved)
	}
	if !strings.Contains(saved, `"compact": true`) {
		t.Fatalf("expected compact persisted:\n%s", saved)
	}
	if store.GetSlot().Active != nil {
		t.Fatalf("expected slot dismissed after save, got %+v", store.GetSlot().Active)
	}
}

func TestStatusLineEditorEscapeRestores(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	resetStatusLine(t)

	store.ClearSlot()
	defer store.ClearSlot()

	original := joinSegments(store.GetStatusLine().Segments)

	app := testutils.NewApp(t, StatusLineEditor("slot-statusline"), 100, 30)
	// Toggle first segment (hint)
	app.Press(key.Key{Code: ' ', Text: " "})
	// Move cursor down to second segment (triggers re-render)
	app.Press(key.Key{Code: 'j', Text: "j"})
	// Toggle second segment
	app.Press(key.Key{Code: ' ', Text: " "})
	if joinSegments(store.GetStatusLine().Segments) == original {
		t.Fatal("expected draft toggle to change the live config")
	}

	app.Press(key.Key{Code: key.KeyEscape})

	if got := joinSegments(store.GetStatusLine().Segments); got != original {
		t.Fatalf("escape should restore original config, got %q want %q", got, original)
	}
	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("escape must not persist the draft")
	}
	if store.GetSlot().Active != nil {
		t.Fatalf("expected slot dismissed after escape, got %+v", store.GetSlot().Active)
	}
}

func TestStatusLineEditorReorder(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	resetStatusLine(t)

	store.ClearSlot()
	defer store.ClearSlot()

	app := testutils.NewApp(t, StatusLineEditor("slot-statusline"), 100, 30)

	// Cursor starts on the first enabled segment (hint); right swaps it with mode.
	app.Press(key.Key{Code: 'l', Text: "l"})
	segments := store.GetStatusLine().Segments
	if len(segments) < 2 || segments[0] != store.StatusLineMode || segments[1] != store.StatusLineHint {
		t.Fatalf("expected hint/mode swapped, got %v", segments)
	}
	// Left moves the cursor back and swaps them again.
	app.Press(key.Key{Code: 'h', Text: "h"})
	if got := store.GetStatusLine().Segments; got[0] != store.StatusLineHint {
		t.Fatalf("expected hint restored to first position, got %v", got)
	}
}

func joinSegments(segments []store.StatusLineSegmentID) string {
	parts := make([]string, len(segments))
	for i, seg := range segments {
		parts[i] = string(seg)
	}
	return strings.Join(parts, ",")
}
