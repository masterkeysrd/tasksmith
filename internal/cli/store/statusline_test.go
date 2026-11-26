package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestValidateStatusLineConfig(t *testing.T) {
	for _, tt := range []struct {
		name       string
		rows, want [][]StatusLineSegmentID
		dropped    []string
	}{
		{name: "defaults", want: DefaultStatusLineRows},
		{name: "one row", rows: [][]StatusLineSegmentID{{"model", "hint"}}, want: [][]StatusLineSegmentID{{"model", "hint"}, {}}},
		{name: "second row only", rows: [][]StatusLineSegmentID{nil, {"tokens"}}, want: [][]StatusLineSegmentID{{}, {"tokens"}}},
		{
			name:    "invalid entries and extra row",
			rows:    [][]StatusLineSegmentID{{" MODE ", "usage"}, {"mode", "warnings", "effort"}, {"hint"}},
			want:    [][]StatusLineSegmentID{{"mode"}, {"warnings", "effort"}},
			dropped: []string{"usage", `duplicate segment "mode"`, "row 3 exceeds the two-row limit"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped := ValidateStatusLineConfig(StatusLineConfig{Rows: tt.rows, Compact: true})
			if !reflect.DeepEqual(got.Rows, tt.want) || !got.Compact || !slices.Equal(dropped, tt.dropped) {
				t.Fatalf("got %+v, dropped %v; want %v compact, dropped %v", got, dropped, tt.want, tt.dropped)
			}
		})
	}
}

func TestSaveAndLoadStatusLine(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith")
	before := GetStatusLine()
	t.Cleanup(func() { _, _ = ApplyStatusLine(before) })

	cfg := StatusLineConfig{Rows: [][]StatusLineSegmentID{{StatusLineMode, StatusLineModel}, {StatusLineTokens}}, Compact: true}
	if _, err := SaveStatusLine(cfg); err != nil {
		t.Fatal(err)
	}
	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || raw["rows"] == nil || raw["compact"] == nil {
		t.Fatalf("unexpected persisted schema: %s", data)
	}
	_, _ = ApplyStatusLine(StatusLineConfig{})
	LoadStatusLine()
	if got := GetStatusLine(); !reflect.DeepEqual(got, cfg) {
		t.Fatalf("after reload = %+v, want %+v", got, cfg)
	}
}

func TestLoadStatusLineMalformedKeepsCurrent(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith")
	before := GetStatusLine()
	t.Cleanup(func() { _, _ = ApplyStatusLine(before) })

	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	LoadStatusLine()
	if got := GetStatusLine(); !reflect.DeepEqual(got, before) || len(GetStatusLineDropped()) == 0 {
		t.Fatalf("after malformed load = %+v, reported %v", got, GetStatusLineDropped())
	}
}
