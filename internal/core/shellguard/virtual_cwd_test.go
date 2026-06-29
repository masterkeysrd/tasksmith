package shellguard

import (
	"os"
	"testing"
)

func TestVirtualCWDTracker(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home dir: %v", err)
	}

	tests := []struct {
		name           string
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{
			name:           "Literal Absolute Escapes",
			command:        "cd /etc && touch hosts",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "Tilde Expansion Outside Workspace",
			command:        "cd ~/some_dir && touch file",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe, // Assuming home is outside /tmp/workspace
		},
		{
			name:           "Logical Relative Navigation",
			command:        "cd subdir && cd .. && touch file.txt",
			expectedAction: ActionWrite,
			expectedSafety: SafetySafe,
		},
		{
			name:           "Dynamic Navigation Taints CWD",
			command:        "cd $TARGET && touch file.txt",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnknown,
		},
		{
			name:           "Relative Escapes from subdir",
			command:        "cd subdir && touch ../../hosts",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "Relative Write Inside subdir",
			command:        "cd subdir && touch file.txt",
			expectedAction: ActionWrite,
			expectedSafety: SafetySafe,
		},
	}

	isHomeInWorkspace := isInsideWorkspace(home, workspaceDir)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := Analyze(tt.command, workspaceDir)
			if err != nil {
				t.Fatalf("Analyze error: %v", err)
			}
			if len(ops) != 1 {
				t.Fatalf("expected 1 operation, got %d", len(ops))
			}
			op := ops[0]
			expectedSafety := tt.expectedSafety
			if tt.name == "Tilde Expansion Outside Workspace" && isHomeInWorkspace {
				expectedSafety = SafetySafe
			}
			if op.Action != tt.expectedAction {
				t.Errorf("Analyze(%q) got action %q, want %q", tt.command, op.Action, tt.expectedAction)
			}
			if op.Safety != expectedSafety {
				t.Errorf("Analyze(%q) got safety %q, want %q", tt.command, op.Safety, expectedSafety)
			}
		})
	}
}
