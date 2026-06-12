package flags

import (
	"os"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

func TestFlagsLoad(t *testing.T) {
	// Since flag.Parse() uses global state, it's hard to test multiple times in one process.
	// But we can test the default values.

	opts, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.LogLevel != log.LevelInfo {
		t.Errorf("expected default log level info, got %v", opts.LogLevel)
	}

	cwd, _ := os.Getwd()
	if opts.CWD != cwd {
		t.Errorf("expected default cwd %s, got %s", cwd, opts.CWD)
	}
}
