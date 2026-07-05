package autocomplete

import (
	"testing"
)

func TestControllerParse(t *testing.T) {
	c := New(Config{
		Triggers: map[string][]string{
			"@": {"file", "symbol", "skill"},
			"/": {"command"},
		},
		Prefixes: map[string]string{
			"@file:":  "file",
			"@sym:":   "symbol",
			"@skill:": "skill",
		},
	})

	tests := []struct {
		query        string
		wantStripped string
		wantSources  []string
		wantMatched  bool
	}{
		{"@", "", []string{"file", "symbol", "skill"}, true},
		{"@file:", "", []string{"file"}, true},
		{"@file:main", "main", []string{"file"}, true},
		{"@sym:", "", []string{"symbol"}, true},
		{"@sym:Main", "Main", []string{"symbol"}, true},
		{"@skill:", "", []string{"skill"}, true},
		{"@skill:agent", "agent", []string{"skill"}, true},
		{"/co", "co", []string{"command"}, true},
		{"hello", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			stripped, sources, matched := c.Parse(tt.query)
			if matched != tt.wantMatched {
				t.Fatalf("Parse(%q) matched = %t, want %t", tt.query, matched, tt.wantMatched)
			}
			if !matched {
				return
			}
			if stripped != tt.wantStripped {
				t.Errorf("Parse(%q) strippedQuery = %q, want %q", tt.query, stripped, tt.wantStripped)
			}
			if len(sources) != len(tt.wantSources) {
				t.Errorf("Parse(%q) sources count = %d, want %d", tt.query, len(sources), len(tt.wantSources))
				return
			}
			for i, s := range sources {
				if s != tt.wantSources[i] {
					t.Errorf("Parse(%q) sources[%d] = %q, want %q", tt.query, i, s, tt.wantSources[i])
				}
			}
		})
	}
}
