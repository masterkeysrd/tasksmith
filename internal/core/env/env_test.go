package env_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/env"
)

func TestParse(t *testing.T) {
	t.Parallel()

	input := `
# Global configuration comment
SIMPLE=value
  SPACED_KEY  =   spaced_value  

# Export prefix variations
export EXPORTED_KEY=exported_val
export   EXPORTED_SPACED = "exported quoted"
export_not_prefix=kept_as_key

# Quotes and escapes
SINGLE_QUOTED='single quoted # not a comment \n literal'
DOUBLE_QUOTED="double \"quoted\" with\nnewline and \\ backslash # not a comment"
DOUBLE_QUOTED_COMMENT="value with spaces" # inline comment after double quotes
SINGLE_QUOTED_COMMENT='single value' # inline comment after single quotes

# Inline comments on unquoted values
INLINE_COMMENT=unquoted_val # this is an inline comment
INLINE_TIGHT=tight_val#tight comment

# Empty and special values
EMPTY_UNQUOTED=
EMPTY_SINGLE=''
EMPTY_DOUBLE=""
EQUALS_IN_VALUE=a=b=c
`

	got, err := env.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	want := map[string]string{
		"SIMPLE":                "value",
		"SPACED_KEY":            "spaced_value",
		"EXPORTED_KEY":          "exported_val",
		"EXPORTED_SPACED":       "exported quoted",
		"export_not_prefix":     "kept_as_key",
		"SINGLE_QUOTED":         `single quoted # not a comment \n literal`,
		"DOUBLE_QUOTED":         "double \"quoted\" with\nnewline and \\ backslash # not a comment",
		"DOUBLE_QUOTED_COMMENT": "value with spaces",
		"SINGLE_QUOTED_COMMENT": "single value",
		"INLINE_COMMENT":        "unquoted_val",
		"INLINE_TIGHT":          "tight_val",
		"EMPTY_UNQUOTED":        "",
		"EMPTY_SINGLE":          "",
		"EMPTY_DOUBLE":          "",
		"EQUALS_IN_VALUE":       "a=b=c",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestParseNilAndEmpty(t *testing.T) {
	t.Parallel()

	gotNil, err := env.Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) unexpected error: %v", err)
	}
	if gotNil == nil || len(gotNil) != 0 {
		t.Errorf("Parse(nil) = %#v, want empty non-nil map", gotNil)
	}

	gotEmpty, err := env.Parse(strings.NewReader("   \n# comment only\n\n"))
	if err != nil {
		t.Fatalf("Parse(empty) unexpected error: %v", err)
	}
	if gotEmpty == nil || len(gotEmpty) != 0 {
		t.Errorf("Parse(empty) = %#v, want empty non-nil map", gotEmpty)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing equals",
			input: "INVALID_LINE_WITHOUT_EQUALS",
		},
		{
			name:  "empty key",
			input: "=value",
		},
		{
			name:  "export with empty key",
			input: "export =value",
		},
		{
			name:  "unterminated double quote",
			input: `KEY="unterminated`,
		},
		{
			name:  "unterminated single quote",
			input: `KEY='unterminated`,
		},
		{
			name:  "trailing characters after double quote",
			input: `KEY="quoted" extra`,
		},
		{
			name:  "trailing characters after single quote",
			input: `KEY='quoted' extra`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := env.Parse(strings.NewReader(tc.input)); err == nil {
				t.Errorf("Parse(%q) expected error, got nil", tc.input)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent file returns empty non-nil map", func(t *testing.T) {
		t.Parallel()
		nonexistent := filepath.Join(t.TempDir(), "does-not-exist", ".env")
		got, err := env.LoadFile(nonexistent)
		if err != nil {
			t.Fatalf("LoadFile() unexpected error for nonexistent file: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("LoadFile() = %#v, want empty non-nil map", got)
		}
	})

	t.Run("valid file is parsed", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), ".env")
		content := "# comment\nexport OPENAI_API_KEY=\"sk-12345\"\nANTHROPIC_API_KEY=sk-ant-999 # inline\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile() failed: %v", err)
		}

		got, err := env.LoadFile(path)
		if err != nil {
			t.Fatalf("LoadFile() unexpected error: %v", err)
		}
		want := map[string]string{
			"OPENAI_API_KEY":    "sk-12345",
			"ANTHROPIC_API_KEY": "sk-ant-999",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LoadFile() = %#v, want %#v", got, want)
		}
	})
}

func TestMerge(t *testing.T) {
	t.Parallel()

	t.Run("empty arguments returns non-nil map", func(t *testing.T) {
		t.Parallel()
		got := env.Merge()
		if got == nil || len(got) != 0 {
			t.Errorf("Merge() = %#v, want empty non-nil map", got)
		}
	})

	t.Run("precedence and defensive copy", func(t *testing.T) {
		t.Parallel()
		global := map[string]string{
			"SHARED":     "from_global",
			"GLOBAL_KEY": "global_only",
		}
		workspace := map[string]string{
			"SHARED":        "from_workspace",
			"WORKSPACE_KEY": "workspace_only",
		}

		merged := env.Merge(nil, global, workspace)
		want := map[string]string{
			"SHARED":        "from_workspace",
			"GLOBAL_KEY":    "global_only",
			"WORKSPACE_KEY": "workspace_only",
		}
		if !reflect.DeepEqual(merged, want) {
			t.Errorf("Merge() = %#v, want %#v", merged, want)
		}

		// Mutating inputs or result must not affect each other.
		global["GLOBAL_KEY"] = "mutated"
		merged["WORKSPACE_KEY"] = "mutated_merged"

		if merged["GLOBAL_KEY"] != "global_only" {
			t.Errorf("Merge() did not defensively copy global map")
		}
		if workspace["WORKSPACE_KEY"] != "workspace_only" {
			t.Errorf("Merge() mutated source workspace map")
		}
	})
}

func TestSetInFile(t *testing.T) {
	t.Parallel()

	t.Run("creates new file and parent directories with 0600 permissions", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "nested", "dir", ".env")

		if err := env.SetInFile(path, "API_KEY", "secret-123"); err != nil {
			t.Fatalf("SetInFile() unexpected error: %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() failed: %v", err)
		}
		if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
			t.Errorf("file permissions = %o, want 0600", gotPerm)
		}

		loaded, err := env.LoadFile(path)
		if err != nil {
			t.Fatalf("LoadFile() unexpected error: %v", err)
		}
		if loaded["API_KEY"] != "secret-123" {
			t.Errorf("loaded[API_KEY] = %q, want %q", loaded["API_KEY"], "secret-123")
		}
	})

	t.Run("updates existing key in-place preserving comments blank lines and ordering", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), ".env")
		initial := strings.Join([]string{
			"# Header comment",
			"",
			"FIRST_KEY=first",
			"export SECOND_KEY=old_second",
			"# Mid comment",
			"THIRD_KEY=third",
			"",
		}, "\n")

		if err := os.WriteFile(path, []byte(initial), 0o640); err != nil {
			t.Fatalf("WriteFile() failed: %v", err)
		}

		if err := env.SetInFile(path, "SECOND_KEY", "new_second"); err != nil {
			t.Fatalf("SetInFile() unexpected error: %v", err)
		}

		if err := env.SetInFile(path, "FOURTH_KEY", "fourth value # with hash"); err != nil {
			t.Fatalf("SetInFile() unexpected error on append: %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() failed: %v", err)
		}
		if gotPerm := info.Mode().Perm(); gotPerm != 0o640 {
			t.Errorf("file permissions = %o, want preserved 0640", gotPerm)
		}

		rawBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() failed: %v", err)
		}
		wantRaw := strings.Join([]string{
			"# Header comment",
			"",
			"FIRST_KEY=first",
			"SECOND_KEY=new_second",
			"# Mid comment",
			"THIRD_KEY=third",
			`FOURTH_KEY="fourth value # with hash"`,
			"",
		}, "\n")

		if string(rawBytes) != wantRaw {
			t.Errorf("file content mismatch:\ngot:\n%s\nwant:\n%s", string(rawBytes), wantRaw)
		}
	})

	t.Run("auto-quotes special characters and round-trips cleanly", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), ".env")

		cases := map[string]string{
			"PLAIN":       "simple_token_123",
			"WITH_SPACE":  "hello world",
			"WITH_HASH":   "token#comment_like",
			"WITH_QUOTES": `he said "hello" and 'world'`,
			"WITH_LINES":  "line1\nline2\r\nline3\\end",
		}

		for k, v := range cases {
			if err := env.SetInFile(path, k, v); err != nil {
				t.Fatalf("SetInFile(%q) failed: %v", k, err)
			}
		}

		loaded, err := env.LoadFile(path)
		if err != nil {
			t.Fatalf("LoadFile() failed: %v", err)
		}
		if !reflect.DeepEqual(loaded, cases) {
			t.Errorf("round-trip mismatch:\ngot:  %#v\nwant: %#v", loaded, cases)
		}
	})

	t.Run("rejects invalid keys", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), ".env")

		if err := env.SetInFile(path, "", "val"); err == nil {
			t.Error("SetInFile with empty key expected error, got nil")
		}
		if err := env.SetInFile(path, "BAD=KEY", "val"); err == nil {
			t.Error("SetInFile with '=' in key expected error, got nil")
		}
		if err := env.SetInFile(path, "BAD\nKEY", "val"); err == nil {
			t.Error("SetInFile with newline in key expected error, got nil")
		}
	})
}
