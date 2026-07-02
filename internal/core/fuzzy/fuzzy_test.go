package fuzzy

import (
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		target   string
		query    string
		expected bool
	}{
		{"cmd/tasksmith/main.go", "main", true},
		{"cmd/tasksmith/main.go", "cmd/main", true},
		{"cmd/tasksmith/main.go", "tasksmith", true},
		{"cmd/tasksmith/main.go", "tsm", true},  // subsequence
		{"cmd/tasksmith/main.go", "m.g", true},  // subsequence path
		{"cmd/tasksmith/main.go", "/cmd", true}, // leading slash trimmed
		{"docs/", "docs/", true},
		{"internal/app/app.go", "invalid", false},
	}

	for _, tc := range tests {
		matched, _ := Match(tc.target, tc.query)
		if matched != tc.expected {
			t.Errorf("Match(%q, %q) = %t; want %t", tc.target, tc.query, matched, tc.expected)
		}
	}
}

func TestScoring(t *testing.T) {
	// Verify that shorter match is scored higher
	_, score1 := Match("cmd/tasksmith/main.go", "main")
	_, score2 := Match("internal/app/main.go", "main")
	if score2 <= score1 {
		t.Errorf("expected score of shorter path to be higher: %d vs %d", score2, score1)
	}

	// Verify consecutive characters get higher score
	_, scoreConsec := Match("main.go", "mai")
	_, scoreNonConsec := Match("main.go", "m.g")
	if scoreConsec <= scoreNonConsec {
		t.Errorf("expected consecutive characters match score to be higher: %d vs %d", scoreConsec, scoreNonConsec)
	}
}
