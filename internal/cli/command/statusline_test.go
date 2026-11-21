package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestStatusLineCommandArgsValidation(t *testing.T) {
	reg := NewRegistry()

	if err := reg.Execute("/statusline extra"); err == nil {
		t.Fatal("expected error when running /statusline with args, got nil")
	}

	store.ClearSlot()
	t.Cleanup(store.ClearSlot)
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

func openStatusLineEditor(t *testing.T, rows [][]store.StatusLineSegmentID) *testutils.App {
	t.Helper()
	t.Cleanup(xdg.ClearCache)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TASKSMITH_APPNAME", "tasksmith")
	xdg.ClearCache()

	original := store.GetStatusLine()
	t.Cleanup(func() {
		if _, err := store.ApplyStatusLine(original); err != nil {
			t.Errorf("restore status line: %v", err)
		}
	})
	if _, err := store.ApplyStatusLine(store.StatusLineConfig{Rows: rows}); err != nil {
		t.Fatalf("set initial status line: %v", err)
	}

	store.ClearSlot()
	t.Cleanup(store.ClearSlot)
	if err := NewRegistry().Execute("/statusline"); err != nil {
		t.Fatalf("open statusline editor: %v", err)
	}
	return testutils.NewApp(t, components.Slot(), 100, 30)
}

func assertStatusLineRows(t *testing.T, got, want [][]store.StatusLineSegmentID) {
	t.Helper()
	if !slices.EqualFunc(got, want, slices.Equal[[]store.StatusLineSegmentID]) {
		t.Fatalf("statusline rows = %v, want %v", got, want)
	}
}

func TestStatusLineEditorShowsIgnoredEntries(t *testing.T) {
	app := openStatusLineEditor(t, [][]store.StatusLineSegmentID{{store.StatusLineHint, "nope"}, {store.StatusLineHint}})
	out := app.Text()
	if !strings.Contains(out, `Ignored entries: nope, duplicate segment "hint"`) {
		t.Fatalf("expected validation details in the editor:\n%s", out)
	}
}

func TestStatusLineEditorViewAndSave(t *testing.T) {
	app := openStatusLineEditor(t, [][]store.StatusLineSegmentID{
		{store.StatusLineHint, store.StatusLineMode},
		{store.StatusLineContext, store.StatusLineCost},
	})

	text := app.Text()
	for _, want := range []string{"Status line configuration", "Row 1", "Row 2", "Disabled", "hint", "mode", "model", "effort", "warnings", "context", "cost", "tokens", "r Move to other row", "Save", "compact: off"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in:\n%s", want, text)
		}
	}

	text = app.Press(key.Key{Code: ' ', Text: " "})
	assertStatusLineRows(t, store.GetStatusLine().Rows, [][]store.StatusLineSegmentID{
		{store.StatusLineMode}, {store.StatusLineContext, store.StatusLineCost},
	})
	_, disabled, ok := strings.Cut(text, "Disabled")
	if !ok || !strings.Contains(disabled, "❯ hint") {
		t.Fatalf("expected hint selected under Disabled after toggle:\n%s", text)
	}

	app.Press(key.Key{Code: key.KeyUp})
	app.Press(key.Key{Code: 'r', Text: "r"})
	want := [][]store.StatusLineSegmentID{{store.StatusLineMode, store.StatusLineCost}, {store.StatusLineContext}}
	assertStatusLineRows(t, store.GetStatusLine().Rows, want)

	text = app.Press(key.Key{Code: 'c', Text: "c"})
	if !store.GetStatusLine().Compact || !strings.Contains(text, "compact: on") {
		t.Fatalf("expected compact mode on after c:\n%s", text)
	}

	app.Press(key.Key{Code: 's', Text: "s"})
	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var saved store.StatusLineConfig
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	assertStatusLineRows(t, saved.Rows, want)
	if !saved.Compact {
		t.Fatalf("expected compact persisted:\n%s", data)
	}
	if store.GetSlot().Active != nil {
		t.Fatalf("expected slot dismissed after save, got %+v", store.GetSlot().Active)
	}
}

func TestStatusLineEditorEscapeRestores(t *testing.T) {
	original := [][]store.StatusLineSegmentID{{store.StatusLineHint}, {store.StatusLineContext}}
	app := openStatusLineEditor(t, original)
	app.Press(key.Key{Code: 'r', Text: "r"})
	app.Press(key.Key{Code: 'c', Text: "c"})
	assertStatusLineRows(t, store.GetStatusLine().Rows, [][]store.StatusLineSegmentID{
		nil, {store.StatusLineContext, store.StatusLineHint},
	})
	if !store.GetStatusLine().Compact {
		t.Fatal("expected live preview to enable compact mode before discarding")
	}

	app.Press(key.Key{Code: key.KeyEscape})
	assertStatusLineRows(t, store.GetStatusLine().Rows, original)
	if store.GetStatusLine().Compact {
		t.Fatal("escape should restore original compact mode")
	}
	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape must not persist the draft, stat error: %v", err)
	}
	if store.GetSlot().Active != nil {
		t.Fatalf("expected slot dismissed after escape, got %+v", store.GetSlot().Active)
	}
}

func TestStatusLineEditorReorder(t *testing.T) {
	original := [][]store.StatusLineSegmentID{
		{store.StatusLineHint, store.StatusLineMode}, {store.StatusLineContext, store.StatusLineTokens},
	}
	app := openStatusLineEditor(t, original)
	app.Press(key.Key{Code: key.KeyRight})
	swapped := [][]store.StatusLineSegmentID{
		{store.StatusLineMode, store.StatusLineHint}, {store.StatusLineContext, store.StatusLineTokens},
	}
	assertStatusLineRows(t, store.GetStatusLine().Rows, swapped)

	app.Press(key.Key{Code: key.KeyRight})
	assertStatusLineRows(t, store.GetStatusLine().Rows, swapped)
	app.Press(key.Key{Code: key.KeyDown})
	app.Press(key.Key{Code: key.KeyLeft})
	assertStatusLineRows(t, store.GetStatusLine().Rows, swapped)
	app.Press(key.Key{Code: key.KeyUp})
	app.Press(key.Key{Code: 'h', Text: "h"})
	assertStatusLineRows(t, store.GetStatusLine().Rows, original)
}

func TestStatusLineEditorReenableUsesDefaultRow(t *testing.T) {
	for row, defaults := range store.DefaultStatusLineRows {
		t.Run(fmt.Sprintf("row_%d", row+1), func(t *testing.T) {
			id := defaults[0]
			other := 1 - row
			anchor := store.DefaultStatusLineRows[other][0]
			initial := make([][]store.StatusLineSegmentID, 2)
			initial[other] = []store.StatusLineSegmentID{id, anchor}
			app := openStatusLineEditor(t, initial)

			app.Press(key.Key{Code: 't', Text: "t"})
			want := make([][]store.StatusLineSegmentID, 2)
			want[other] = []store.StatusLineSegmentID{anchor}
			assertStatusLineRows(t, store.GetStatusLine().Rows, want)
			app.Press(key.Key{Code: 'r', Text: "r"})
			assertStatusLineRows(t, store.GetStatusLine().Rows, want)
			app.Press(key.Key{Code: key.KeyEnter})
			want[row] = []store.StatusLineSegmentID{id}
			assertStatusLineRows(t, store.GetStatusLine().Rows, want)
		})
	}
}

func TestStatusLineEditorRetainsLastEnabledSegment(t *testing.T) {
	original := [][]store.StatusLineSegmentID{nil, {store.StatusLineMode}}
	app := openStatusLineEditor(t, original)
	app.Press(key.Key{Code: ' ', Text: " "})
	assertStatusLineRows(t, store.GetStatusLine().Rows, original)

	text := app.Press(key.Key{Code: 'r', Text: "r"})
	assertStatusLineRows(t, store.GetStatusLine().Rows, [][]store.StatusLineSegmentID{{store.StatusLineMode}, nil})
	if !strings.Contains(text, "❯ mode") {
		t.Fatalf("expected mode to stay selected after moving to an empty row:\n%s", text)
	}
	app.Press(key.Key{Code: 'r', Text: "r"})
	assertStatusLineRows(t, store.GetStatusLine().Rows, original)
}
