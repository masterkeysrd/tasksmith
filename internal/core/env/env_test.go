package env_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/env"
)

func TestLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith_env_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	envContent := `
# A comment line
KEY_SIMPLE=val1
KEY_QUOTED="val2"
KEY_SINGLE_QUOTED='val3'
export KEY_EXPORTED=val4
# Another comment
  KEY_SPACED  =   val5  
`

	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write test .env file: %v", err)
	}

	// Preset an existing environment variable to verify we don't overwrite it
	if err := os.Setenv("KEY_SIMPLE", "preset_val"); err != nil {
		t.Fatalf("failed to set preset env var: %v", err)
	}
	defer os.Unsetenv("KEY_SIMPLE")
	defer os.Unsetenv("KEY_QUOTED")
	defer os.Unsetenv("KEY_SINGLE_QUOTED")
	defer os.Unsetenv("KEY_EXPORTED")
	defer os.Unsetenv("KEY_SPACED")

	if err := env.Load(envPath); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify preset variable was NOT overwritten
	if got := os.Getenv("KEY_SIMPLE"); got != "preset_val" {
		t.Errorf("expected KEY_SIMPLE to remain preset_val, got %q", got)
	}

	// Verify other variables were loaded correctly
	if got := os.Getenv("KEY_QUOTED"); got != "val2" {
		t.Errorf("expected KEY_QUOTED = val2, got %q", got)
	}
	if got := os.Getenv("KEY_SINGLE_QUOTED"); got != "val3" {
		t.Errorf("expected KEY_SINGLE_QUOTED = val3, got %q", got)
	}
	if got := os.Getenv("KEY_EXPORTED"); got != "val4" {
		t.Errorf("expected KEY_EXPORTED = val4, got %q", got)
	}
	if got := os.Getenv("KEY_SPACED"); got != "val5" {
		t.Errorf("expected KEY_SPACED = val5, got %q", got)
	}
}

func TestLoad_NotExists(t *testing.T) {
	// Should not error if the file doesn't exist
	if err := env.Load("/nonexistent/file/path/.env"); err != nil {
		t.Errorf("expected no error for nonexistent file, got %v", err)
	}
}
