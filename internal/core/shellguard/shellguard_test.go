package shellguard

import (
	"reflect"
	"testing"
)

func TestBasicCommand(t *testing.T) {
	chain, err := Parse("go test -v")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	pipeline := chain.Pipelines[0]
	if len(pipeline.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(pipeline.Commands))
	}
	cmd := pipeline.Commands[0]
	if cmd.Executable != "go" {
		t.Errorf("expected executable 'go', got '%s'", cmd.Executable)
	}
	expectedArgs := []string{"test", "-v"}
	if !reflect.DeepEqual(cmd.Args, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, cmd.Args)
	}
}

func TestEnvVarsPrefixingCommand(t *testing.T) {
	chain, err := Parse("DEBUG=1 node app.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "node" {
		t.Errorf("expected executable 'node', got '%s'", cmd.Executable)
	}
	if len(cmd.Env) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(cmd.Env))
	}
	if cmd.Env[0].Name != "DEBUG" || cmd.Env[0].Value != "1" {
		t.Errorf("expected ENV {Name:DEBUG, Value:1}, got %+v", cmd.Env[0])
	}
	expectedArgs := []string{"app.js"}
	if !reflect.DeepEqual(cmd.Args, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, cmd.Args)
	}
}

func TestLogicalChaining(t *testing.T) {
	chain, err := Parse("go build && ./main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	pipeline := chain.Pipelines[0]
	if pipeline.Operator != "&&" {
		t.Errorf("expected operator '&&', got '%s'", pipeline.Operator)
	}
	if len(pipeline.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(pipeline.Commands))
	}
	if pipeline.Commands[0].Executable != "go" {
		t.Errorf("expected first command 'go', got '%s'", pipeline.Commands[0].Executable)
	}
	if pipeline.Commands[1].Executable != "./main" {
		t.Errorf("expected second command './main', got '%s'", pipeline.Commands[1].Executable)
	}
}

func TestPiping(t *testing.T) {
	chain, err := Parse("./main | grep error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	pipeline := chain.Pipelines[0]
	if pipeline.Operator != "|" {
		t.Errorf("expected operator '|', got '%s'", pipeline.Operator)
	}
	if len(pipeline.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(pipeline.Commands))
	}
	if pipeline.Commands[0].Executable != "./main" {
		t.Errorf("expected first command './main', got '%s'", pipeline.Commands[0].Executable)
	}
	if pipeline.Commands[1].Executable != "grep" {
		t.Errorf("expected second command 'grep', got '%s'", pipeline.Commands[1].Executable)
	}
}

func TestCombinedChainingAndPiping(t *testing.T) {
	chain, err := Parse("go build && ./main | grep error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	pipeline := chain.Pipelines[0]
	if pipeline.Operator != "|" {
		t.Errorf("expected operator '|', got '%s'", pipeline.Operator)
	}
	if len(pipeline.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(pipeline.Commands))
	}
}

func TestOutputRedirection(t *testing.T) {
	chain, err := Parse(`echo "hello" > test.txt`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "echo" {
		t.Errorf("expected executable 'echo', got '%s'", cmd.Executable)
	}
	if len(cmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(cmd.Redirects))
	}
	redir := cmd.Redirects[0]
	if redir.Op != ">" {
		t.Errorf("expected redirect op '>', got '%s'", redir.Op)
	}
	if redir.Target != "test.txt" {
		t.Errorf("expected target 'test.txt', got '%s'", redir.Target)
	}
}

func TestInputRedirection(t *testing.T) {
	chain, err := Parse("cat < input.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "cat" {
		t.Errorf("expected executable 'cat', got '%s'", cmd.Executable)
	}
	if len(cmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(cmd.Redirects))
	}
	redir := cmd.Redirects[0]
	if redir.Op != "<" {
		t.Errorf("expected redirect op '<', got '%s'", redir.Op)
	}
	if redir.Target != "input.json" {
		t.Errorf("expected target 'input.json', got '%s'", redir.Target)
	}
}

func TestSudoWrapper(t *testing.T) {
	chain, err := Parse("sudo rm -rf tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "sudo" {
		t.Errorf("expected executable 'sudo', got '%s'", cmd.Executable)
	}
	if cmd.SubCommand == nil {
		t.Fatal("expected SubCommand for sudo, got nil")
	}
	if cmd.SubCommand.Executable != "rm" {
		t.Errorf("expected sub-command executable 'rm', got '%s'", cmd.SubCommand.Executable)
	}
	expectedArgs := []string{"-rf", "tmp"}
	if !reflect.DeepEqual(cmd.SubCommand.Args, expectedArgs) {
		t.Errorf("expected sub-command args %v, got %v", expectedArgs, cmd.SubCommand.Args)
	}
}

func TestNpxWrapper(t *testing.T) {
	chain, err := Parse("npx eslint --fix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "npx" {
		t.Errorf("expected executable 'npx', got '%s'", cmd.Executable)
	}
	if cmd.SubCommand == nil {
		t.Fatal("expected SubCommand for npx, got nil")
	}
	if cmd.SubCommand.Executable != "eslint" {
		t.Errorf("expected sub-command executable 'eslint', got '%s'", cmd.SubCommand.Executable)
	}
	expectedArgs := []string{"--fix"}
	if !reflect.DeepEqual(cmd.SubCommand.Args, expectedArgs) {
		t.Errorf("expected sub-command args %v, got %v", expectedArgs, cmd.SubCommand.Args)
	}
}

func TestBashCWrapper(t *testing.T) {
	chain, err := Parse(`bash -c "go build && rm -rf out"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "bash" {
		t.Errorf("expected executable 'bash', got '%s'", cmd.Executable)
	}
	if cmd.SubCommand == nil {
		t.Fatal("expected SubCommand for bash -c, got nil")
	}
	if cmd.SubCommand.Executable != "go" {
		t.Errorf("expected sub-command executable 'go', got '%s'", cmd.SubCommand.Executable)
	}
}

func TestStaticClassifications(t *testing.T) {
	workspaceDir := "/tmp/workspace"
	tests := []struct {
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{"cat file.txt", ActionRead, SafetySafe},
		{"grep pattern file.txt", ActionRead, SafetySafe},
		{"find . -name test", ActionRead, SafetySafe},
		{"ls -la", ActionRead, SafetySafe},
		{"pwd", ActionRead, SafetySafe},
		{"head -n 10 file.txt", ActionRead, SafetySafe},
		{"tail -f log.txt", ActionRead, SafetySafe},
		{"file test.txt", ActionRead, SafetySafe},
		{"which go", ActionRead, SafetySafe},
		{"rm -rf tmp", ActionDelete, SafetyUnsafe},
		{"rmdir empty_dir", ActionDelete, SafetyUnsafe},
		{"dd if=/dev/zero of=out", ActionDelete, SafetyUnsafe},
		{"mkfs.ext4 /dev/sda", ActionUnknown, SafetyUnsafe},
		{"mkfs /dev/sda", ActionDelete, SafetyUnsafe},
		{"sudo apt-get update", ActionDelete, SafetyUnsafe},
		{"mkdir newdir", ActionWrite, SafetySafe},
		{"touch file.txt", ActionWrite, SafetySafe},
		{"cp src.txt dst.txt", ActionWrite, SafetySafe},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			ops, err := Analyze(tt.command, workspaceDir)
			if err != nil {
				t.Fatalf("Analyze failed: %v", err)
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

func TestGitConditionals(t *testing.T) {
	workspaceDir := "/tmp/workspace"
	tests := []struct {
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{"git status", ActionRead, SafetySafe},
		{"git diff", ActionRead, SafetySafe},
		{"git log", ActionRead, SafetySafe},
		{"git show HEAD", ActionRead, SafetySafe},
		{"git branch", ActionRead, SafetySafe},
		{"git commit -m 'hello'", ActionWrite, SafetySafe},
		{"git push origin main", ActionWrite, SafetySafe},
		{"git add .", ActionWrite, SafetySafe},
		{"git reset --hard HEAD", ActionDelete, SafetyUnsafe},
		{"git clean -f -d", ActionDelete, SafetyUnsafe},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			ops, err := Analyze(tt.command, workspaceDir)
			if err != nil {
				t.Fatalf("Analyze failed: %v", err)
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

func TestOutputRedirections(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	t.Run("redirect inside workspace", func(t *testing.T) {
		cmd := "echo 'hi' > inside.txt"
		ops, err := Analyze(cmd, workspaceDir)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		op := ops[0]
		if op.Action != ActionWrite {
			t.Errorf("expected ActionWrite, got %s", op.Action)
		}
		if op.Safety != SafetySafe {
			t.Errorf("expected SafetySafe, got %s", op.Safety)
		}
	})

	t.Run("redirect outside workspace", func(t *testing.T) {
		cmd := "echo 'hi' > /etc/hosts"
		ops, err := Analyze(cmd, workspaceDir)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		op := ops[0]
		if op.Action != ActionWrite {
			t.Errorf("expected ActionWrite, got %s", op.Action)
		}
		if op.Safety != SafetyUnsafe {
			t.Errorf("expected SafetyUnsafe, got %s", op.Safety)
		}
	})
}

func TestNestedWrapperResolution(t *testing.T) {
	workspaceDir := "/tmp/workspace"
	tests := []struct {
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{"sudo rm -rf tmp", ActionDelete, SafetyUnsafe},
		{"sudo apt-get update", ActionDelete, SafetyUnsafe},
		{"npx eslint --fix", ActionUnknown, SafetySafe},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			ops, err := Analyze(tt.command, workspaceDir)
			if err != nil {
				t.Fatalf("Analyze failed: %v", err)
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

func TestMatchParsedCommand(t *testing.T) {
	tests := []struct {
		name        string
		grantTarget string
		matchMethod string
		commandStr  string
		expected    bool
	}{
		{
			name:        "wildcard match",
			grantTarget: "git *",
			matchMethod: "wildcard",
			commandStr:  "git commit -m 'hello'",
			expected:    true,
		},
		{
			name:        "subcommand match",
			grantTarget: "git commit",
			matchMethod: "prefix",
			commandStr:  "git commit -m 'hello'",
			expected:    true,
		},
		{
			name:        "env var prefix ignored",
			grantTarget: "git commit",
			matchMethod: "prefix",
			commandStr:  "DEBUG=true git commit -m 'hello'",
			expected:    true,
		},
		{
			name:        "sudo wrapper unpacking",
			grantTarget: "git status",
			matchMethod: "prefix",
			commandStr:  "sudo git status",
			expected:    true,
		},
		{
			name:        "no match different executable",
			grantTarget: "git *",
			matchMethod: "wildcard",
			commandStr:  "node app.js",
			expected:    false,
		},
		{
			name:        "no match wrong subcommand",
			grantTarget: "git commit",
			matchMethod: "prefix",
			commandStr:  "git checkout main",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, err := Parse(tt.commandStr)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			cmd := chain.Pipelines[0].Commands[0]
			got := MatchParsedCommand(tt.grantTarget, tt.matchMethod, cmd)
			if got != tt.expected {
				t.Errorf("MatchParsedCommand(%q, %q, %q) = %t, want %t", tt.grantTarget, tt.matchMethod, tt.commandStr, got, tt.expected)
			}
		})
	}
}

func TestAnalyzeBubbling(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	ops, err := Analyze("git status && rm -rf tmp", workspaceDir)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}
	if ops[1].Action != ActionDelete || ops[1].Safety != SafetyUnsafe {
		t.Errorf("expected second operation to be ActionDelete SafetyUnsafe, got action %s safety %s", ops[1].Action, ops[1].Safety)
	}
}

func TestDynamicStatePreEvaluation(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	t.Run("variable in output target", func(t *testing.T) {
		cmd := "echo hello > $FILE"
		ops, err := Analyze(cmd, workspaceDir)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		op := ops[0]
		if op.Action != ActionWrite || op.Safety != SafetyUnknown {
			t.Errorf("expected ActionWrite SafetyUnknown for variable in output target, got action %s safety %s", op.Action, op.Safety)
		}
	})

	t.Run("uses eval", func(t *testing.T) {
		cmd := "eval './script.sh'"
		ops, err := Analyze(cmd, workspaceDir)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		op := ops[0]
		if op.Action != ActionExec || op.Safety != SafetyUnknown {
			t.Errorf("expected ActionExec SafetyUnknown for eval command, got action %s safety %s", op.Action, op.Safety)
		}
	})

	t.Run("command substitution in args", func(t *testing.T) {
		cmd := "echo $(whoami)"
		ops, err := Analyze(cmd, workspaceDir)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		op := ops[0]
		if op.Action != ActionExec || op.Safety != SafetyUnknown {
			t.Errorf("expected ActionExec SafetyUnknown for command substitution, got action %s safety %s", op.Action, op.Safety)
		}
	})
}
