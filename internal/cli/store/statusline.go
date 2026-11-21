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

type StatusLineSegmentID string

const (
	StatusLineHint     StatusLineSegmentID = "hint"
	StatusLineMode     StatusLineSegmentID = "mode"
	StatusLineModel    StatusLineSegmentID = "model"
	StatusLineEffort   StatusLineSegmentID = "effort"
	StatusLineWarnings StatusLineSegmentID = "warnings"
	StatusLineContext  StatusLineSegmentID = "context"
	StatusLineCost     StatusLineSegmentID = "cost"
	StatusLineTokens   StatusLineSegmentID = "tokens"
)

var DefaultStatusLineRows = [][]StatusLineSegmentID{
	{StatusLineHint, StatusLineMode, StatusLineModel, StatusLineEffort, StatusLineWarnings},
	{StatusLineContext, StatusLineCost, StatusLineTokens},
}

type StatusLineConfig struct {
	Rows    [][]StatusLineSegmentID `json:"rows"`
	Compact bool                    `json:"compact"`
}

type statusLineState struct {
	cfg     StatusLineConfig
	dropped []string
}

var statusLine = kites.Create(statusLineState{cfg: defaultStatusLine(false)})

func init() {
	LoadStatusLine()
}

func defaultStatusLine(compact bool) StatusLineConfig {
	return StatusLineConfig{
		Rows:    [][]StatusLineSegmentID{slices.Clone(DefaultStatusLineRows[0]), slices.Clone(DefaultStatusLineRows[1])},
		Compact: compact,
	}
}

func statusLineConfigPath() (string, error) {
	return xdg.SubConfigDir("statusline.json")
}

func ValidateStatusLineConfig(cfg StatusLineConfig) (StatusLineConfig, []string) {
	cleaned := StatusLineConfig{Rows: [][]StatusLineSegmentID{{}, {}}, Compact: cfg.Compact}
	seen := make(map[StatusLineSegmentID]bool)
	var dropped []string
	for i, row := range cfg.Rows {
		if i >= len(cleaned.Rows) {
			dropped = append(dropped, fmt.Sprintf("row %d exceeds the two-row limit", i+1))
			continue
		}
		for _, raw := range row {
			id := StatusLineSegmentID(strings.ToLower(strings.TrimSpace(string(raw))))
			switch {
			case id == "":
			case !slices.Contains(DefaultStatusLineRows[0], id) && !slices.Contains(DefaultStatusLineRows[1], id):
				dropped = append(dropped, string(id))
			case seen[id]:
				dropped = append(dropped, fmt.Sprintf("duplicate segment %q", id))
			default:
				seen[id] = true
				cleaned.Rows[i] = append(cleaned.Rows[i], id)
			}
		}
	}
	if len(seen) == 0 {
		return defaultStatusLine(cfg.Compact), dropped
	}
	return cleaned, dropped
}

func LoadStatusLine() {
	path, err := statusLineConfigPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw StatusLineConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		statusLine.Set(func(s statusLineState) statusLineState {
			s.dropped = []string{fmt.Sprintf("statusline.json: %v", err)}
			return s
		})
		return
	}
	_, _ = ApplyStatusLine(raw)
}

func ApplyStatusLine(cfg StatusLineConfig) ([]string, error) {
	cleaned, dropped := ValidateStatusLineConfig(cfg)
	statusLine.Set(func(statusLineState) statusLineState {
		return statusLineState{cfg: cleaned, dropped: dropped}
	})
	return dropped, nil
}

func SaveStatusLine(cfg StatusLineConfig) ([]string, error) {
	cleaned, dropped := ValidateStatusLineConfig(cfg)
	path, err := statusLineConfigPath()
	if err != nil {
		return dropped, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return dropped, err
	}
	data, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		return dropped, err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return dropped, err
	}
	statusLine.Set(func(statusLineState) statusLineState {
		return statusLineState{cfg: cleaned, dropped: dropped}
	})
	return dropped, nil
}

func GetStatusLine() StatusLineConfig {
	return statusLine.Get().cfg
}

func GetStatusLineDropped() []string {
	return statusLine.Get().dropped
}

func UseStatusLine() StatusLineConfig {
	return kites.Use(statusLine, func(s statusLineState) StatusLineConfig { return s.cfg })
}
