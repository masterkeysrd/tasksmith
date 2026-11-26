package shellguard

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestCdEffects(t *testing.T) {
	root := t.TempDir()
	g := New(Config{WorkspaceDir: root})
	for _, tc := range []struct {
		cd         string
		action     api.ActionType
		path       string
		nextSafety api.SafetyLevel
	}{
		{"cd subdir", "", "", api.SafetySafe},
		{"cd /tmp > out.txt", api.ActionWrite, root + "/out.txt", api.SafetyUnsafe},
		{"cd subdir < input.txt", api.ActionRead, root + "/input.txt", api.SafetySafe},
		{"cd $(rm -rf /outside)", api.ActionDelete, "/outside", api.SafetyUnknown},
		{"cd $TARGET", api.ActionUnknown, "", api.SafetyUnknown},
	} {
		t.Run(tc.cd, func(t *testing.T) {
			ops, err := g.AnalyzeDetailed(tc.cd + " && touch file.txt")
			if err != nil {
				t.Fatal(err)
			}
			if tc.action != "" {
				if len(ops) != 2 || ops[0].Action != tc.action || ops[0].CWD != root ||
					(tc.path != "" && ops[0].Path != tc.path) {
					t.Fatalf("unexpected cd effects: %+v", ops)
				}
				ops = ops[1:]
			}
			if len(ops) != 1 || ops[0].Action != api.ActionWrite || ops[0].Safety != tc.nextSafety {
				t.Fatalf("unexpected following operation: %+v", ops)
			}
		})
	}
}

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
