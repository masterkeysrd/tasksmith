package resolver

import (
	"context"
	"path/filepath"
	"testing"
)

func TestParseCoordinates(t *testing.T) {
	coord := "internal/app/app.go:42:5:function:Main"
	path, line, char, kind, name, ok := parseCoordinates(coord)
	if !ok {
		t.Fatalf("failed to parse coordinates")
	}

	if path != "internal/app/app.go" {
		t.Errorf("expected path internal/app/app.go, got %s", path)
	}
	if line != 42 {
		t.Errorf("expected line 42, got %d", line)
	}
	if char != 5 {
		t.Errorf("expected char 5, got %d", char)
	}
	if kind != "function" {
		t.Errorf("expected kind function, got %s", kind)
	}
	if name != "Main" {
		t.Errorf("expected name Main, got %s", name)
	}
}

func TestResolveSymbolAutocomplete(t *testing.T) {
	tmpDir := "/Users/masterkeysrd/Projects/tasksmith"
	r := New(Config{
		Cwd: tmpDir,
	})

	t.Run("resolve autocomplete ref to absolute coordinates", func(t *testing.T) {
		coord := "internal/app/app.go:42:5:function:Main"
		resolved, err := r.ResolveSymbol(context.Background(), coord, true)
		if err != nil {
			t.Fatalf("ResolveSymbol failed: %v", err)
		}

		expectedPath := filepath.Join(tmpDir, "internal/app/app.go")
		expectedCoord := expectedPath + ":42:5:function:Main"
		if resolved != expectedCoord {
			t.Errorf("expected %s, got %s", expectedCoord, resolved)
		}
	})
}
