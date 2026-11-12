package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// StatusLineSegmentID identifies one renderable segment of the REPL status line.
type StatusLineSegmentID string

// Supported status line segment identifiers.
const (
	StatusLineHint    StatusLineSegmentID = "hint"
	StatusLineMode    StatusLineSegmentID = "mode"
	StatusLineContext StatusLineSegmentID = "context"
	StatusLineUsage   StatusLineSegmentID = "usage"
	StatusLineModel   StatusLineSegmentID = "model"
)

// DefaultStatusLineSegments is the built-in status line composition. The first
// entry renders on the far left of the line; the rest render on the right.
var DefaultStatusLineSegments = []StatusLineSegmentID{
	StatusLineHint,
	StatusLineMode,
	StatusLineContext,
	StatusLineUsage,
	StatusLineModel,
}

// allStatusLineSegments lists every segment id the renderer knows about.
var allStatusLineSegments = []StatusLineSegmentID{
	StatusLineHint,
	StatusLineMode,
	StatusLineContext,
	StatusLineUsage,
	StatusLineModel,
}

// StatusLineConfig is the user-configurable status line composition, persisted
// as $XDG_CONFIG_HOME/tasksmith/statusline.json so both the /statusline view
// and agents can edit it directly.
type StatusLineConfig struct {
	// Segments is the ordered list of enabled segments. The first renders
	// left of the spacer, the remainder render right-aligned in order.
	Segments []StatusLineSegmentID `json:"segments"`
	// Compact forces compact variants (percentage-only context, model without
	// thinking effort) even when the terminal is wide enough for full text.
	Compact bool `json:"compact"`
}

type statusLineState struct {
	cfg     StatusLineConfig
	dropped []string
}

var statusLine = kites.Create(statusLineState{
	cfg:     StatusLineConfig{Segments: slices.Clone(DefaultStatusLineSegments)},
	dropped: nil,
})

func init() {
	LoadStatusLine()
}

// statusLineConfigPath resolves the persisted status line configuration path.
func statusLineConfigPath() (string, error) {
	return xdg.SubConfigDir("statusline.json")
}

// ValidateStatusLineConfig normalizes a raw config: unknown segment ids are
// dropped (and reported), duplicates are removed, and an empty segment list
// falls back to the default composition.
func ValidateStatusLineConfig(cfg StatusLineConfig) (StatusLineConfig, []string) {
	var dropped []string
	seen := make(map[StatusLineSegmentID]bool, len(cfg.Segments))
	segments := make([]StatusLineSegmentID, 0, len(cfg.Segments))
	for _, id := range cfg.Segments {
		id = StatusLineSegmentID(strings.ToLower(strings.TrimSpace(string(id))))
		if id == "" {
			continue
		}
		if !slices.Contains(allStatusLineSegments, id) {
			dropped = append(dropped, string(id))
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		segments = append(segments, id)
	}
	if len(segments) == 0 {
		segments = slices.Clone(DefaultStatusLineSegments)
	}
	return StatusLineConfig{Segments: segments, Compact: cfg.Compact}, dropped
}

// LoadStatusLine reads statusline.json when present and applies it to the
// live store. Missing or unreadable files keep the default composition;
// malformed JSON records the parse error as a dropped entry so /statusline
// can surface it instead of failing silently.
func LoadStatusLine() {
	path, err := statusLineConfigPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw struct {
		Segments []string `json:"segments"`
		Compact  bool     `json:"compact"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		statusLine.Set(func(s statusLineState) statusLineState {
			s.dropped = []string{fmt.Sprintf("statusline.json: %v", err)}
			return s
		})
		return
	}
	ids := make([]StatusLineSegmentID, len(raw.Segments))
	for i, s := range raw.Segments {
		ids[i] = StatusLineSegmentID(s)
	}
	cfg, dropped := ValidateStatusLineConfig(StatusLineConfig{Segments: ids, Compact: raw.Compact})
	statusLine.Set(func(s statusLineState) statusLineState {
		s.cfg = cfg
		s.dropped = dropped
		return s
	})
}

// ApplyStatusLine updates the live configuration without persisting it, so
// interactive views can preview edits before saving.
func ApplyStatusLine(cfg StatusLineConfig) ([]string, error) {
	cleaned, dropped := ValidateStatusLineConfig(cfg)
	statusLine.Set(func(s statusLineState) statusLineState {
		s.cfg = cleaned
		s.dropped = dropped
		return s
	})
	return dropped, nil
}

// SaveStatusLine applies cfg to the live store and persists it to
// statusline.json. It returns the ids that validation dropped.
func SaveStatusLine(cfg StatusLineConfig) ([]string, error) {
	cleaned, dropped := ValidateStatusLineConfig(cfg)
	statusLine.Set(func(s statusLineState) statusLineState {
		s.cfg = cleaned
		s.dropped = dropped
		return s
	})
	path, err := statusLineConfigPath()
	if err != nil {
		return dropped, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return dropped, err
	}
	raw := struct {
		Segments []string `json:"segments"`
		Compact  bool     `json:"compact"`
	}{Compact: cleaned.Compact}
	for _, id := range cleaned.Segments {
		raw.Segments = append(raw.Segments, string(id))
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return dropped, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return dropped, err
	}
	return dropped, nil
}

// GetStatusLine reads the live status line configuration without subscribing.
func GetStatusLine() StatusLineConfig {
	return statusLine.Get().cfg
}

// GetStatusLineDropped reports segment ids (or parse errors) that validation
// rejected on the last load or save.
func GetStatusLineDropped() []string {
	return statusLine.Get().dropped
}

// UseStatusLine subscribes the calling component to the status line config.
func UseStatusLine() StatusLineConfig {
	return kites.Use(statusLine, func(s statusLineState) StatusLineConfig { return s.cfg })
}
