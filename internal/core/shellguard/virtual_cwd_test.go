package shellguard

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestVirtualCWDTracker(t *testing.T) {
	guard := New(Config{
		WorkspaceDir: "/tmp/workspace",
		HomeDir:      "/Users/tester",
	})

	tests := []struct {
		name           string
		command        string
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{
			name:           "Literal Absolute Escapes",
			command:        "cd /etc && touch hosts",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "Tilde Expansion Outside Workspace",
			command:        "cd ~/some_dir && touch file",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "Logical Relative Navigation",
			command:        "cd subdir && cd .. && touch file.txt",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "Dynamic Navigation Taints CWD",
			command:        "cd $TARGET && touch file.txt",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnknown,
		},
		{
			name:           "Relative Escapes from subdir",
			command:        "cd subdir && touch ../../hosts",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "Relative Write Inside subdir",
			command:        "cd subdir && touch file.txt",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetySafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := guard.Analyze(tt.command)
			if err != nil {
				t.Fatalf("Analyze error: %v", err)
			}
			if len(ops) != 1 {
				t.Fatalf("expected 1 operation, got %d", len(ops))
			}
			op := ops[0]
			if op.Action != tt.expectedAction {
				t.Errorf("Analyze(%q) got action %q, want %q", tt.command, op.Action, tt.expectedAction)
			}
			if op.Safety != tt.expectedSafety {
				t.Errorf("Analyze(%q) got safety %q, want %q", tt.command, op.Safety, tt.expectedSafety)
			}
		})
	}
}
