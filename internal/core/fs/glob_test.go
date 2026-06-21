package fs

import (
	"testing"
)

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		// Basic matching
		{"*.go", "main.go", true},
		{"*.go", "main.js", false},
		{"*.go", "subdir/main.go", false},

		// Question mark
		{"file?.txt", "fileA.txt", true},
		{"file?.txt", "fileAB.txt", false},
		{"file?.txt", "file/.txt", false},

		// Double star
		{"**/*.go", "main.go", true},
		{"**/*.go", "subdir/main.go", true},
		{"**/*.go", "subdir/nested/main.go", true},
		{"**/*.go", "subdir/nested/main.js", false},

		// Double star with trailing
		{"internal/**", "internal/core/fs/glob.go", true},
		{"internal/**", "cmd/tasksmith/main.go", false},

		// Wildcard directories
		{"internal/*/*.go", "internal/core/fs.go", true},
		{"internal/*/*.go", "internal/core/fs/glob.go", false},

		// Escape characters in pattern
		{"app.*.js", "app.min.js", true},
		{"app.*.js", "appXminXjs", false},
	}

	for _, tt := range tests {
		g, err := Compile(tt.pattern)
		if err != nil {
			t.Fatalf("Compile(%q) failed: %v", tt.pattern, err)
		}
		got := g.Match(tt.text)
		if got != tt.want {
			t.Errorf("Glob(%q).Match(%q) = %v; want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}
