package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestValidateStatusLineConfig(t *testing.T) {
	tests := []struct {
		name        string
		in          StatusLineConfig
		wantSegs    []StatusLineSegmentID
		wantDropped []string
		wantCompact bool
	}{
		{
			name:     "valid order kept",
			in:       StatusLineConfig{Segments: []StatusLineSegmentID{StatusLineModel, StatusLineHint}},
			wantSegs: []StatusLineSegmentID{"model", "hint"},
		},
		{
			name:        "unknown dropped, case normalized",
			in:          StatusLineConfig{Segments: []StatusLineSegmentID{"MODE", "bogus", "usage"}},
			wantSegs:    []StatusLineSegmentID{"mode", "usage"},
			wantDropped: []string{"bogus"},
		},
		{
			name:     "duplicates removed",
			in:       StatusLineConfig{Segments: []StatusLineSegmentID{"hint", "hint", "mode"}},
			wantSegs: []StatusLineSegmentID{"hint", "mode"},
		},
		{
			name:     "empty falls back to defaults",
			in:       StatusLineConfig{Compact: true},
			wantSegs: DefaultStatusLineSegments,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped := ValidateStatusLineConfig(tt.in)
			if !slices.Equal(got.Segments, tt.wantSegs) {
				t.Errorf("segments = %v, want %v", got.Segments, tt.wantSegs)
			}
			if !slices.Equal(dropped, tt.wantDropped) {
				t.Errorf("dropped = %v, want %v", dropped, tt.wantDropped)
			}
		})
	}
}

func TestSaveAndLoadStatusLine(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	cfg := StatusLineConfig{Segments: []StatusLineSegmentID{StatusLineModel, StatusLineMode}, Compact: true}
	if _, err := SaveStatusLine(cfg); err != nil {
		t.Fatalf("SaveStatusLine: %v", err)
	}

	path, err := xdg.SubConfigDir("statusline.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	var raw struct {
		Segments []string `json:"segments"`
		Compact  bool     `json:"compact"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("persisted JSON invalid: %v", err)
	}
	if !slices.Equal(raw.Segments, []string{"model", "mode"}) || !raw.Compact {
		t.Fatalf("persisted = %+v", raw)
	}

	// Reset live state, then reload from disk.
	if _, err := ApplyStatusLine(StatusLineConfig{Segments: slices.Clone(DefaultStatusLineSegments)}); err != nil {
		t.Fatalf("reset live config: %v", err)
	}
	LoadStatusLine()
	got := GetStatusLine()
	if !slices.Equal(got.Segments, cfg.Segments) || !got.Compact {
		t.Fatalf("after reload = %+v, want %+v", got, cfg)
	}
}

func TestLoadStatusLineMalformedKeepsCurrent(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

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

	before := GetStatusLine()
	LoadStatusLine()
	if got := GetStatusLine(); !slices.Equal(got.Segments, before.Segments) {
		t.Fatalf("malformed file changed config: %v -> %v", before.Segments, got.Segments)
	}
	if len(GetStatusLineDropped()) == 0 {
		t.Fatal("expected parse error surfaced in dropped list")
	}
}
