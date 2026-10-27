package fsutils

import (
	"testing"
)

func TestGlobCompileAndMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		match    []string
		mismatch []string
	}{
		{
			pattern:  "*.go",
			match:    []string{"main.go", "foo.go", "./bar.go"},
			mismatch: []string{"pkg/main.go", "foo.txt", "main.go.bak"},
		},
		{
			pattern:  "file?.txt",
			match:    []string{"file1.txt", "fileA.txt", "file_.txt"},
			mismatch: []string{"file.txt", "file10.txt", "file/a.txt"},
		},
		{
			pattern:  "[abc].txt",
			match:    []string{"a.txt", "b.txt", "c.txt"},
			mismatch: []string{"d.txt", "x.txt", "ab.txt"},
		},
		{
			pattern:  "[!abc].txt",
			match:    []string{"d.txt", "x.txt", "1.txt"},
			mismatch: []string{"a.txt", "b.txt", "c.txt"},
		},
		{
			pattern:  "**/main.go",
			match:    []string{"main.go", "pkg/main.go", "a/b/c/main.go", "./a/b/main.go"},
			mismatch: []string{"other.go", "pkg/other.go", "main.go.bak"},
		},
		{
			pattern:  "src/**",
			match:    []string{"src/main.go", "src/pkg/a.go", "src/deep/nested/file.txt"},
			mismatch: []string{"other/file.txt", "main.go"},
		},
		{
			pattern:  "**",
			match:    []string{"main.go", "pkg/main.go", "a/b/c/d.txt"},
			mismatch: []string{},
		},
		{
			pattern:  `pkg\**\*.go`, // Windows-style pattern
			match:    []string{"pkg/main.go", `pkg\main.go`, "pkg/sub/test.go", `pkg\sub\test.go`},
			mismatch: []string{"other.go", "main.go"},
		},
		{
			pattern:  "**/toolgen*/**",
			match:    []string{"tools/toolgen/main.go", "a/b/toolgen-v2/sub/file.go"},
			mismatch: []string{"tools/other/main.go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			g, err := Compile(tc.pattern)
			if err != nil {
				t.Fatalf("Compile(%q) error: %v", tc.pattern, err)
			}
			for _, m := range tc.match {
				if !g.Match(m) {
					t.Errorf("expected pattern %q to match %q", tc.pattern, m)
				}
			}
			for _, mm := range tc.mismatch {
				if g.Match(mm) {
					t.Errorf("expected pattern %q to NOT match %q", tc.pattern, mm)
				}
			}
		})
	}
}

func TestGlobInvalidPatterns(t *testing.T) {
	invalids := []string{
		"[invalid",
		"[bad",
		"pkg/[unclosed",
		`trailing\`,
		`foo/bar\`,
	}

	for _, p := range invalids {
		t.Run(p, func(t *testing.T) {
			_, err := Compile(p)
			if err == nil {
				t.Fatalf("expected Compile(%q) to fail, got nil error", p)
			}
		})
	}
}
