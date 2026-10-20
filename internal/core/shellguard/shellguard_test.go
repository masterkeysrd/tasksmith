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
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})
	tests := []struct {
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{"cat file.txt", ActionRead, SafetySafe},
		{"grep pattern file.txt", ActionRead, SafetySafe},
		{"rg pattern file.txt", ActionRead, SafetySafe},
		{"find . -name test", ActionRead, SafetySafe},
		{"find . -delete", ActionDelete, SafetyUnsafe},
		{"find . -exec rm {} \\;", ActionExec, SafetySafe},
		{"fd pattern", ActionRead, SafetySafe},
		{"ls -la", ActionRead, SafetySafe},
		{"pwd", ActionRead, SafetySafe},
		{"head -n 10 file.txt", ActionRead, SafetySafe},
		{"tail -f log.txt", ActionRead, SafetySafe},
		{"file test.txt", ActionRead, SafetySafe},
		{"which go", ActionRead, SafetySafe},
		{"wc -l file.txt", ActionRead, SafetySafe},
		{"sort file.txt", ActionRead, SafetySafe},
		{"uniq file.txt", ActionRead, SafetySafe},
		{"jq '.foo' data.json", ActionRead, SafetySafe},
		{"yq '.foo' data.yaml", ActionRead, SafetySafe},
		{"stat file.txt", ActionRead, SafetySafe},
		{"tree .", ActionRead, SafetySafe},
		{"diff a.txt b.txt", ActionRead, SafetySafe},
		{"sed 's/foo/bar/g' file.txt", ActionRead, SafetySafe},
		{"sed -i 's/foo/bar/g' file.txt", ActionWrite, SafetySafe},
		{"rm -rf tmp", ActionDelete, SafetyUnsafe},
		{"rmdir empty_dir", ActionDelete, SafetyUnsafe},
		{"shred secret.txt", ActionDelete, SafetyUnsafe},
		{"unlink file.txt", ActionDelete, SafetyUnsafe},
		{"dd if=/dev/zero of=out", ActionDelete, SafetyUnsafe},
		{"mkfs.ext4 /dev/sda", ActionUnknown, SafetyUnsafe},
		{"mkfs /dev/sda", ActionDelete, SafetyUnsafe},
		{"sudo apt-get update", ActionDelete, SafetyUnsafe},
		{"mkdir newdir", ActionWrite, SafetySafe},
		{"touch file.txt", ActionWrite, SafetySafe},
		{"cp src.txt dst.txt", ActionWrite, SafetySafe},
		{"mv src.txt dst.txt", ActionWrite, SafetySafe},
		{"ln -s src.txt link.txt", ActionWrite, SafetySafe},
		{"chmod +x script.sh", ActionWrite, SafetySafe},
		{"chown user:group file.txt", ActionWrite, SafetySafe},
		{"tee output.log", ActionWrite, SafetySafe},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			ops, err := guard.Analyze(tt.command)
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
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})
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
		{"git rev-parse HEAD", ActionRead, SafetySafe},
		{"git ls-files", ActionRead, SafetySafe},
		{"git remote -v", ActionRead, SafetySafe},
		{"git tag", ActionRead, SafetySafe},
		{"git commit -m 'hello'", ActionWrite, SafetySafe},
		{"git push origin main", ActionWrite, SafetySafe},
		{"git add .", ActionWrite, SafetySafe},
		{"git checkout main", ActionWrite, SafetySafe},
		{"git switch main", ActionWrite, SafetySafe},
		{"git merge feature", ActionWrite, SafetySafe},
		{"git rebase main", ActionWrite, SafetySafe},
		{"git pull", ActionWrite, SafetySafe},
		{"git stash", ActionWrite, SafetySafe},
		{"git rm file.txt", ActionDelete, SafetyUnsafe},
		{"git reset --hard HEAD", ActionDelete, SafetyUnsafe},
		{"git clean -f -d", ActionDelete, SafetyUnsafe},
		{"git clean -f", ActionDelete, SafetyUnsafe},
		{"git branch -D feature", ActionDelete, SafetyUnsafe},
		{"git push --force origin main", ActionDelete, SafetyUnsafe},
		{"git push -f origin main", ActionDelete, SafetyUnsafe},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			ops, err := guard.Analyze(tt.command)
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
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	t.Run("redirect inside workspace", func(t *testing.T) {
		cmd := "echo 'hi' > inside.txt"
		ops, err := guard.Analyze(cmd)
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
		ops, err := guard.Analyze(cmd)
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
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})
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
			ops, err := guard.Analyze(tt.command)
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
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})
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
			got := guard.Match(tt.grantTarget, tt.matchMethod, cmd)
			if got != tt.expected {
				t.Errorf("guard.Match(%q, %q, %q) = %t, want %t", tt.grantTarget, tt.matchMethod, tt.commandStr, got, tt.expected)
			}
		})
	}
}

func TestAnalyzeBubbling(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	ops, err := guard.Analyze("git status && rm -rf tmp")
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
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	t.Run("variable in output target", func(t *testing.T) {
		cmd := "echo hello > $FILE"
		ops, err := guard.Analyze(cmd)
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
		ops, err := guard.Analyze(cmd)
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
		ops, err := guard.Analyze(cmd)
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

func TestCommandAwareArgumentTargetExtraction(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	tests := []struct {
		name           string
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{
			name:           "echo with absolute path string is safe",
			command:        `echo "/etc/passwd"`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "printf with absolute path string is safe",
			command:        `printf "%s\n" "/etc/passwd"`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "git commit -m with absolute path in message is safe",
			command:        `git commit -m "fix /etc/passwd"`,
			expectedAction: ActionWrite,
			expectedSafety: SafetySafe,
		},
		{
			name:           "git commit -am with absolute path in message is safe",
			command:        `git commit -am "update /etc/hosts"`,
			expectedAction: ActionWrite,
			expectedSafety: SafetySafe,
		},
		{
			name:           "git checkout -b branch with slash is safe",
			command:        `git checkout -b /etc/passwd`,
			expectedAction: ActionWrite,
			expectedSafety: SafetySafe,
		},
		{
			name:           "git switch -c branch is safe",
			command:        `git switch -c /etc/passwd`,
			expectedAction: ActionWrite,
			expectedSafety: SafetySafe,
		},
		{
			name:           "git branch with slash name is safe",
			command:        `git branch /etc/passwd`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "git -C outside workspace is unsafe",
			command:        `git -C /etc status`,
			expectedAction: ActionRead,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "git -C inside workspace is safe",
			command:        `git -C subdir status`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "grep pattern looking like path on workspace file is safe",
			command:        `grep "/etc/passwd" file.txt`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "grep pattern on outside file is unsafe",
			command:        `grep "root" /etc/passwd`,
			expectedAction: ActionRead,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "rg pattern looking like path in workspace is safe",
			command:        `rg "/etc/shadow" src/`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "sed expression with path on workspace file is safe",
			command:        `sed 's/\/etc\/passwd/replacement/g' file.txt`,
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "sed -i on outside file is unsafe",
			command:        `sed -i 's/foo/bar/' /etc/hosts`,
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := guard.Analyze(tt.command)
			if err != nil {
				t.Fatalf("Analyze(%q) failed: %v", tt.command, err)
			}
			if len(ops) != 1 {
				t.Fatalf("Analyze(%q) expected 1 operation, got %d", tt.command, len(ops))
			}
			if ops[0].Action != tt.expectedAction {
				t.Errorf("Analyze(%q) action = %q, want %q", tt.command, ops[0].Action, tt.expectedAction)
			}
			if ops[0].Safety != tt.expectedSafety {
				t.Errorf("Analyze(%q) safety = %q, want %q", tt.command, ops[0].Safety, tt.expectedSafety)
			}
		})
	}
}

func TestSubshellsBlocksAndCmdSubst(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	tests := []struct {
		name           string
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{
			name:           "subshell destructive rm",
			command:        "(rm -rf /)",
			expectedAction: ActionDelete,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "block destructive rm",
			command:        "{ rm -rf /; }",
			expectedAction: ActionDelete,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "command substitution destructive rm",
			command:        "echo $(rm foo)",
			expectedAction: ActionDelete,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "double-quoted command substitution reading outside workspace",
			command:        `echo "$(cat /etc/passwd)"`,
			expectedAction: ActionExec,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "for loop destructive rm",
			command:        `for f in *; do rm "$f"; done`,
			expectedAction: ActionDelete,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "if clause destructive rm",
			command:        `if [ -f foo ]; then rm -rf bar; fi`,
			expectedAction: ActionDelete,
			expectedSafety: SafetyUnsafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := guard.Analyze(tt.command)
			if err != nil {
				t.Fatalf("Analyze(%q) failed: %v", tt.command, err)
			}
			if len(ops) != 1 {
				t.Fatalf("Analyze(%q) expected 1 operation, got %d", tt.command, len(ops))
			}
			if ops[0].Action != tt.expectedAction {
				t.Errorf("Analyze(%q) action = %q, want %q", tt.command, ops[0].Action, tt.expectedAction)
			}
			if ops[0].Safety != tt.expectedSafety {
				t.Errorf("Analyze(%q) safety = %q, want %q", tt.command, ops[0].Safety, tt.expectedSafety)
			}
		})
	}
}

func TestSubshellCWDIsolation(t *testing.T) {
	workspaceDir := "/tmp/workspace"
	guard := New(Config{WorkspaceDir: workspaceDir})

	ops, err := guard.Analyze("(cd /tmp && pwd); ls")
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	// First operation: pwd inside subshell after cd /tmp
	if ops[0].CWD != "/tmp" {
		t.Errorf("expected first op CWD '/tmp', got %q", ops[0].CWD)
	}
	if ops[0].Safety != SafetyUnsafe {
		t.Errorf("expected first op SafetyUnsafe (in /tmp), got %q", ops[0].Safety)
	}

	// Second operation: ls outside subshell should still resolve in workspaceDir
	if ops[1].CWD != workspaceDir {
		t.Errorf("expected second op CWD %q, got %q", workspaceDir, ops[1].CWD)
	}
	if ops[1].Path != workspaceDir {
		t.Errorf("expected second op Path %q, got %q", workspaceDir, ops[1].Path)
	}
	if ops[1].Safety != SafetySafe {
		t.Errorf("expected second op SafetySafe, got %q", ops[1].Safety)
	}
}

func TestCompoundCollapsing(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	t.Run("more than 3 operations collapses into single compound operation", func(t *testing.T) {
		cmd := "ls && pwd && cat a.txt && rm /etc/passwd"
		ops, err := guard.Analyze(cmd)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 collapsed operation, got %d", len(ops))
		}
		op := ops[0]
		if !op.IsCompound {
			t.Errorf("expected IsCompound=true, got false")
		}
		if op.Action != ActionDelete {
			t.Errorf("expected highest-severity ActionDelete, got %q", op.Action)
		}
		if op.Safety != SafetyUnsafe {
			t.Errorf("expected worst SafetyUnsafe, got %q", op.Safety)
		}
		if op.Path != "/etc/passwd" {
			t.Errorf("expected critical path '/etc/passwd', got %q", op.Path)
		}
		if op.Command == nil || op.Command.Raw != cmd {
			t.Errorf("expected summary Command.Raw=%q, got %+v", cmd, op.Command)
		}

		// AnalyzeDetailed must return all 4 individual operations
		detailed, err := guard.AnalyzeDetailed(cmd)
		if err != nil {
			t.Fatalf("AnalyzeDetailed failed: %v", err)
		}
		if len(detailed) != 4 {
			t.Fatalf("expected 4 detailed operations, got %d", len(detailed))
		}
	})

	t.Run("for loop collapses even with fewer than 4 operations", func(t *testing.T) {
		cmd := `for f in a.txt b.txt; do cat "$f"; cp "$f" "$f.bak"; done`
		ops, err := guard.Analyze(cmd)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 collapsed operation, got %d", len(ops))
		}
		op := ops[0]
		if !op.IsCompound {
			t.Errorf("expected IsCompound=true for for-loop")
		}
		if op.Action != ActionWrite {
			t.Errorf("expected ActionWrite, got %q", op.Action)
		}
		if op.Safety != SafetyUnknown {
			t.Errorf("expected SafetyUnknown due to dynamic $f, got %q", op.Safety)
		}

		detailed, err := guard.AnalyzeDetailed(cmd)
		if err != nil {
			t.Fatalf("AnalyzeDetailed failed: %v", err)
		}
		if len(detailed) != 2 {
			t.Fatalf("expected 2 detailed operations, got %d", len(detailed))
		}
	})

	t.Run("inline interpreter scripts mark IsCompound", func(t *testing.T) {
		cmd := `python3 -c "import os; print('hello')"`
		ops, err := guard.Analyze(cmd)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if !ops[0].IsCompound {
			t.Errorf("expected IsCompound=true for python3 -c")
		}
	})
}

func TestProtectedDirectories(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	tests := []struct {
		name           string
		command        string
		expectedAction ActionType
		expectedSafety SafetyLevel
	}{
		{
			name:           "read inside .git is safe",
			command:        "cat .git/config",
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "read inside .agents is safe",
			command:        "cat .agents/rules.md",
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "read inside .bare is safe",
			command:        "ls .bare/refs",
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
		{
			name:           "write inside .git is unsafe",
			command:        "touch .git/hooks/pre-commit",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "redirect write inside .git is unsafe",
			command:        "echo 'malicious' > .git/config",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "write inside .bare is unsafe",
			command:        "mkdir .bare/hooks",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "write inside .agents is unsafe",
			command:        "touch .agents/malicious.md",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "delete inside .agents is unsafe",
			command:        "rm .agents/rules.md",
			expectedAction: ActionDelete,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "cd into .agents then write is unsafe",
			command:        "cd .agents && touch rules.md",
			expectedAction: ActionWrite,
			expectedSafety: SafetyUnsafe,
		},
		{
			name:           "cd into .agents then read is safe",
			command:        "cd .agents && cat rules.md",
			expectedAction: ActionRead,
			expectedSafety: SafetySafe,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := guard.Analyze(tt.command)
			if err != nil {
				t.Fatalf("Analyze(%q) failed: %v", tt.command, err)
			}
			if len(ops) != 1 {
				t.Fatalf("Analyze(%q) expected 1 operation, got %d", tt.command, len(ops))
			}
			if ops[0].Action != tt.expectedAction {
				t.Errorf("Analyze(%q) action = %q, want %q", tt.command, ops[0].Action, tt.expectedAction)
			}
			if ops[0].Safety != tt.expectedSafety {
				t.Errorf("Analyze(%q) safety = %q, want %q", tt.command, ops[0].Safety, tt.expectedSafety)
			}
		})
	}
}

func TestGuardDependencyInjection(t *testing.T) {
	t.Run("custom HomeDir inside workspace makes cd ~ safe", func(t *testing.T) {
		guard := New(Config{
			WorkspaceDir: "/tmp/workspace",
			HomeDir:      "/tmp/workspace/home",
		})
		ops, err := guard.Analyze("cd ~/project && touch file.txt")
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Safety != SafetySafe || ops[0].Path != "/tmp/workspace/home/project/file.txt" {
			t.Errorf("expected SafetySafe at /tmp/workspace/home/project/file.txt, got safety=%s path=%q", ops[0].Safety, ops[0].Path)
		}
	})

	t.Run("custom ProtectedDirs", func(t *testing.T) {
		guard := New(Config{
			WorkspaceDir:  "/tmp/workspace",
			ProtectedDirs: []string{".vault"},
		})
		ops, err := guard.Analyze("touch .vault/secret.key")
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 || ops[0].Safety != SafetyUnsafe {
			t.Errorf("expected write to custom protected dir .vault to be SafetyUnsafe, got %+v", ops)
		}
	})

	t.Run("custom MaxOperations threshold", func(t *testing.T) {
		guard := New(Config{
			WorkspaceDir:  "/tmp/workspace",
			MaxOperations: 1,
		})
		ops, err := guard.Analyze("ls && pwd")
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 || !ops[0].IsCompound {
			t.Errorf("expected 2 operations to collapse when MaxOperations=1, got %+v", ops)
		}
	})

	t.Run("custom Policy with declarative CommandRule", func(t *testing.T) {
		policy := DefaultPolicy()
		policy.Rules["mytool"] = CommandRule{
			DefaultCategory: ActionWrite,
			TargetMode:      TargetsSkipFirst,
			ValueFlags:      []string{"--tag"},
		}
		guard := New(Config{
			WorkspaceDir: "/tmp/workspace",
			Policy:       &policy,
		})
		ops, err := guard.Analyze(`mytool --tag "/etc/passwd" "action_name" output.txt`)
		if err != nil {
			t.Fatalf("Analyze failed: %v", err)
		}
		if len(ops) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(ops))
		}
		if ops[0].Action != ActionWrite || ops[0].Safety != SafetySafe || ops[0].Path != "/tmp/workspace/output.txt" {
			t.Errorf("unexpected operation for custom rule: %+v", ops[0])
		}
	})
}

func TestStringMethods(t *testing.T) {
	actionCases := []struct {
		action ActionType
		want   string
	}{
		{ActionRead, "read"},
		{ActionUnknown, "unknown"},
		{ActionWrite, "write"},
		{ActionExec, "exec"},
		{ActionDelete, "delete"},
		{ActionType(99), "unknown"},
	}
	for _, tc := range actionCases {
		if got := tc.action.String(); got != tc.want {
			t.Errorf("ActionType(%d).String() = %q, want %q", tc.action, got, tc.want)
		}
	}

	safetyCases := []struct {
		safety SafetyLevel
		want   string
	}{
		{SafetySafe, "safe"},
		{SafetyUnknown, "unknown"},
		{SafetyUnsafe, "unsafe"},
		{SafetyLevel(99), "unknown"},
	}
	for _, tc := range safetyCases {
		if got := tc.safety.String(); got != tc.want {
			t.Errorf("SafetyLevel(%d).String() = %q, want %q", tc.safety, got, tc.want)
		}
	}
}

func TestCompoundASTConstructsAndEdgeCases(t *testing.T) {
	guard := New(Config{
		WorkspaceDir: "/tmp/workspace",
		HomeDir:      "/Users/tester",
	})

	cases := []struct {
		name       string
		command    string
		wantAction ActionType
		wantSafety SafetyLevel
		wantComp   bool
	}{
		{
			name:       "while clause with destructive body",
			command:    "while true; do rm /etc/passwd; done",
			wantAction: ActionDelete,
			wantSafety: SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "if-elif-else clause",
			command:    "if true; then ls; elif false; then pwd; else touch out.txt; fi",
			wantAction: ActionWrite,
			wantSafety: SafetySafe,
			wantComp:   true,
		},
		{
			name:       "case clause with subst in word and pattern",
			command:    "case $(pwd) in $(echo foo)) rm /etc/passwd ;; esac",
			wantAction: ActionDelete,
			wantSafety: SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "function declaration",
			command:    "cleanup() { rm -rf build; }; cleanup",
			wantAction: ActionDelete,
			wantSafety: SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "time and coproc clauses",
			command:    "time ls && coproc pwd",
			wantAction: ActionRead,
			wantSafety: SafetySafe,
			wantComp:   false,
		},
		{
			name:       "export declaration with command substitution",
			command:    "export SECRET=$(cat /etc/passwd)",
			wantAction: ActionRead,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "test clause with command substitution",
			command:    `[[ $(rm /tmp/outside) == "x" ]]`,
			wantAction: ActionDelete,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "process substitution <(...)",
			command:    "diff <(cat a.txt) <(cat /etc/passwd)",
			wantAction: ActionExec,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "heredoc with command substitution",
			command:    "cat <<EOF\n$(rm /etc/passwd)\nEOF",
			wantAction: ActionDelete,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "bare file truncation redirect",
			command:    "> truncated.txt",
			wantAction: ActionWrite,
			wantSafety: SafetySafe,
			wantComp:   false,
		},
		{
			name:       "multi-command bash -c wrapper",
			command:    `bash -c "cd /tmp && touch escaped.txt"`,
			wantAction: ActionWrite,
			wantSafety: SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "node -e inline script",
			command:    `node -e "console.log(1)"`,
			wantAction: ActionUnknown,
			wantSafety: SafetySafe,
			wantComp:   true,
		},
		{
			name:       "ruby -e inline script",
			command:    `ruby -e "puts 1"`,
			wantAction: ActionUnknown,
			wantSafety: SafetySafe,
			wantComp:   true,
		},
		{
			name:       "find with -f path flag and predicate stop",
			command:    `find -f src -name "*.go" -delete`,
			wantAction: ActionDelete,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "grep with --file= outside workspace",
			command:    `grep --file=/etc/passwd src/main.go`,
			wantAction: ActionRead,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "git with attached -C outside workspace",
			command:    `git -C/etc status`,
			wantAction: ActionRead,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "git checkout branch -- outside pathspec",
			command:    `git checkout main -- ../outside.txt`,
			wantAction: ActionWrite,
			wantSafety: SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "BSD sed -i '' in-place edit inside workspace",
			command:    `sed -i '' 's/foo/bar/' file.txt`,
			wantAction: ActionWrite,
			wantSafety: SafetySafe,
			wantComp:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops, err := guard.Analyze(tc.command)
			if err != nil {
				t.Fatalf("Analyze(%q) unexpected error: %v", tc.command, err)
			}
			if len(ops) == 0 {
				t.Fatalf("Analyze(%q) returned 0 operations", tc.command)
			}
			// Pick worst op if not collapsed
			op := ops[0]
			if len(ops) > 1 {
				op = collapseOperations(ops, tc.command)
				op.IsCompound = false
			}
			if op.Action != tc.wantAction {
				t.Errorf("Analyze(%q) action = %s, want %s", tc.command, op.Action, tc.wantAction)
			}
			if op.Safety != tc.wantSafety {
				t.Errorf("Analyze(%q) safety = %s, want %s", tc.command, op.Safety, tc.wantSafety)
			}
			if ops[0].IsCompound != tc.wantComp {
				t.Errorf("Analyze(%q) IsCompound = %v, want %v", tc.command, ops[0].IsCompound, tc.wantComp)
			}
		})
	}

	t.Run("Match edge cases: wildcard, empty, exact compound Raw, git -C subcommand", func(t *testing.T) {
		if guard.Match("", "prefix", ParsedCommand{Executable: "ls"}) {
			t.Error("expected empty grant target not to match")
		}
		if !guard.Match("*", "prefix", ParsedCommand{Executable: "rm"}) {
			t.Error("expected '*' wildcard to match any command")
		}
		compoundCmd := ParsedCommand{Executable: "python3", Raw: `python3 -c "print(1)"`}
		if !guard.Match(`python3 -c "print(1)"`, "exact", compoundCmd) {
			t.Error("expected exact match on compound Raw command")
		}
		gitWithGlobalFlag := ParsedCommand{Executable: "git", Args: []string{"-C", "subpkg", "commit", "-m", "init"}}
		if !guard.Match("git commit", "prefix", gitWithGlobalFlag) {
			t.Error("expected 'git commit' grant to match 'git -C subpkg commit'")
		}
	})
}
