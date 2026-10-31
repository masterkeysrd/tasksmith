package shellguard

import (
	"fmt"
	"github.com/masterkeysrd/tasksmith/api"
	"path/filepath"
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
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{"cat file.txt", api.ActionRead, api.SafetySafe},
		{"grep pattern file.txt", api.ActionRead, api.SafetySafe},
		{"rg pattern file.txt", api.ActionRead, api.SafetySafe},
		{"find . -name test", api.ActionRead, api.SafetySafe},
		{"find . -delete", api.ActionDelete, api.SafetyUnsafe},
		{"find . -exec rm {} \\;", api.ActionExec, api.SafetySafe},
		{"fd pattern", api.ActionRead, api.SafetySafe},
		{"ls -la", api.ActionRead, api.SafetySafe},
		{"pwd", api.ActionRead, api.SafetySafe},
		{"head -n 10 file.txt", api.ActionRead, api.SafetySafe},
		{"tail -f log.txt", api.ActionRead, api.SafetySafe},
		{"file test.txt", api.ActionRead, api.SafetySafe},
		{"which go", api.ActionRead, api.SafetySafe},
		{"wc -l file.txt", api.ActionRead, api.SafetySafe},
		{"sort file.txt", api.ActionRead, api.SafetySafe},
		{"uniq file.txt", api.ActionRead, api.SafetySafe},
		{"jq '.foo' data.json", api.ActionRead, api.SafetySafe},
		{"yq '.foo' data.yaml", api.ActionRead, api.SafetySafe},
		{"stat file.txt", api.ActionRead, api.SafetySafe},
		{"tree .", api.ActionRead, api.SafetySafe},
		{"diff a.txt b.txt", api.ActionRead, api.SafetySafe},
		{"sed 's/foo/bar/g' file.txt", api.ActionRead, api.SafetySafe},
		{"sed -i 's/foo/bar/g' file.txt", api.ActionWrite, api.SafetySafe},
		{"rm -rf tmp", api.ActionDelete, api.SafetyUnsafe},
		{"rmdir empty_dir", api.ActionDelete, api.SafetyUnsafe},
		{"shred secret.txt", api.ActionDelete, api.SafetyUnsafe},
		{"unlink file.txt", api.ActionDelete, api.SafetyUnsafe},
		{"dd if=/dev/zero of=out", api.ActionDelete, api.SafetyUnsafe},
		{"mkfs.ext4 /dev/sda", api.ActionUnknown, api.SafetyUnsafe},
		{"mkfs /dev/sda", api.ActionDelete, api.SafetyUnsafe},
		{"sudo apt-get update", api.ActionDelete, api.SafetyUnsafe},
		{"mkdir newdir", api.ActionWrite, api.SafetySafe},
		{"touch file.txt", api.ActionWrite, api.SafetySafe},
		{"cp src.txt dst.txt", api.ActionWrite, api.SafetySafe},
		{"mv src.txt dst.txt", api.ActionWrite, api.SafetySafe},
		{"ln -s src.txt link.txt", api.ActionWrite, api.SafetySafe},
		{"chmod +x script.sh", api.ActionWrite, api.SafetySafe},
		{"chown user:group file.txt", api.ActionWrite, api.SafetySafe},
		{"tee output.log", api.ActionWrite, api.SafetySafe},
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
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{"git status", api.ActionRead, api.SafetySafe},
		{"git diff", api.ActionRead, api.SafetySafe},
		{"git log", api.ActionRead, api.SafetySafe},
		{"git show HEAD", api.ActionRead, api.SafetySafe},
		{"git branch", api.ActionRead, api.SafetySafe},
		{"git rev-parse HEAD", api.ActionRead, api.SafetySafe},
		{"git ls-files", api.ActionRead, api.SafetySafe},
		{"git remote -v", api.ActionRead, api.SafetySafe},
		{"git tag", api.ActionRead, api.SafetySafe},
		{"git commit -m 'hello'", api.ActionWrite, api.SafetySafe},
		{"git push origin main", api.ActionNet, api.SafetyUnknown},
		{"git add .", api.ActionWrite, api.SafetySafe},
		{"git checkout main", api.ActionWrite, api.SafetySafe},
		{"git switch main", api.ActionWrite, api.SafetySafe},
		{"git merge feature", api.ActionWrite, api.SafetySafe},
		{"git rebase main", api.ActionWrite, api.SafetySafe},
		{"git pull", api.ActionNet, api.SafetyUnknown},
		{"git fetch origin", api.ActionNet, api.SafetyUnknown},
		{"git clone https://github.com/org/repo", api.ActionNet, api.SafetyUnknown},
		{"git stash", api.ActionWrite, api.SafetySafe},
		{"git rm file.txt", api.ActionDelete, api.SafetyUnsafe},
		{"git reset --hard HEAD", api.ActionDelete, api.SafetyUnsafe},
		{"git clean -f -d", api.ActionDelete, api.SafetyUnsafe},
		{"git clean -f", api.ActionDelete, api.SafetyUnsafe},
		{"git branch -D feature", api.ActionDelete, api.SafetyUnsafe},
		{"git push --force origin main", api.ActionDelete, api.SafetyUnsafe},
		{"git push -f origin main", api.ActionDelete, api.SafetyUnsafe},
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
		if op.Action != api.ActionWrite {
			t.Errorf("expected api.ActionWrite, got %s", op.Action)
		}
		if op.Safety != api.SafetySafe {
			t.Errorf("expected api.SafetySafe, got %s", op.Safety)
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
		if op.Action != api.ActionWrite {
			t.Errorf("expected api.ActionWrite, got %s", op.Action)
		}
		if op.Safety != api.SafetyUnsafe {
			t.Errorf("expected api.SafetyUnsafe, got %s", op.Safety)
		}
	})
}

func TestNestedWrapperResolution(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})
	tests := []struct {
		command        string
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{"sudo rm -rf tmp", api.ActionDelete, api.SafetyUnsafe},
		{"sudo apt-get update", api.ActionDelete, api.SafetyUnsafe},
		{"npx eslint --fix", api.ActionUnknown, api.SafetySafe},
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
	if ops[1].Action != api.ActionDelete || ops[1].Safety != api.SafetyUnsafe {
		t.Errorf("expected second operation to be api.ActionDelete api.SafetyUnsafe, got action %s safety %s", ops[1].Action, ops[1].Safety)
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
		if op.Action != api.ActionWrite || op.Safety != api.SafetyUnknown {
			t.Errorf("expected api.ActionWrite api.SafetyUnknown for variable in output target, got action %s safety %s", op.Action, op.Safety)
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
		if op.Action != api.ActionExec || op.Safety != api.SafetyUnknown {
			t.Errorf("expected api.ActionExec api.SafetyUnknown for eval command, got action %s safety %s", op.Action, op.Safety)
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
		if op.Action != api.ActionExec || op.Safety != api.SafetyUnknown {
			t.Errorf("expected api.ActionExec api.SafetyUnknown for command substitution, got action %s safety %s", op.Action, op.Safety)
		}
	})
}

func TestCommandAwareArgumentTargetExtraction(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	tests := []struct {
		name           string
		command        string
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{
			name:           "echo with absolute path string is safe",
			command:        `echo "/etc/passwd"`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "printf with absolute path string is safe",
			command:        `printf "%s\n" "/etc/passwd"`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "git commit -m with absolute path in message is safe",
			command:        `git commit -m "fix /etc/passwd"`,
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "git commit -am with absolute path in message is safe",
			command:        `git commit -am "update /etc/hosts"`,
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "git checkout -b branch with slash is safe",
			command:        `git checkout -b /etc/passwd`,
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "git switch -c branch is safe",
			command:        `git switch -c /etc/passwd`,
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "git branch with slash name is safe",
			command:        `git branch /etc/passwd`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "git -C outside workspace is unsafe",
			command:        `git -C /etc status`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "git -C inside workspace is safe",
			command:        `git -C subdir status`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "grep pattern looking like path on workspace file is safe",
			command:        `grep "/etc/passwd" file.txt`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "grep pattern on outside file is unsafe",
			command:        `grep "root" /etc/passwd`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "rg pattern looking like path in workspace is safe",
			command:        `rg "/etc/shadow" src/`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "sed expression with path on workspace file is safe",
			command:        `sed 's/\/etc\/passwd/replacement/g' file.txt`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "sed -i on outside file is unsafe",
			command:        `sed -i 's/foo/bar/' /etc/hosts`,
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "cat outside workspace non-sensitive file is api.SafetyOutside",
			command:        `cat ../somefile.txt`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetyOutside,
		},
		{
			name:           "cat sensitive /etc/hosts is api.SafetyUnsafe",
			command:        `cat /etc/hosts`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "cat sensitive ~/.ssh/id_rsa is api.SafetyUnsafe",
			command:        `cat ~/.ssh/id_rsa`,
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "touch outside workspace non-sensitive file is api.SafetyUnsafe",
			command:        `touch ../somefile.txt`,
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
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
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{
			name:           "subshell destructive rm",
			command:        "(rm -rf /)",
			expectedAction: api.ActionDelete,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "block destructive rm",
			command:        "{ rm -rf /; }",
			expectedAction: api.ActionDelete,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "command substitution destructive rm",
			command:        "echo $(rm foo)",
			expectedAction: api.ActionDelete,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "double-quoted command substitution reading outside workspace",
			command:        `echo "$(cat /etc/passwd)"`,
			expectedAction: api.ActionExec,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "for loop destructive rm",
			command:        `for f in *; do rm "$f"; done`,
			expectedAction: api.ActionDelete,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "if clause destructive rm",
			command:        `if [ -f foo ]; then rm -rf bar; fi`,
			expectedAction: api.ActionDelete,
			expectedSafety: api.SafetyUnsafe,
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

	// First operation: pwd inside subshell after cd /tmp (read-only outside workspace -> api.SafetyOutside)
	if ops[0].CWD != "/tmp" {
		t.Errorf("expected first op CWD '/tmp', got %q", ops[0].CWD)
	}
	if ops[0].Safety != api.SafetyOutside {
		t.Errorf("expected first op api.SafetyOutside (in /tmp), got %q", ops[0].Safety)
	}

	// Second operation: ls outside subshell should still resolve in workspaceDir
	if ops[1].CWD != workspaceDir {
		t.Errorf("expected second op CWD %q, got %q", workspaceDir, ops[1].CWD)
	}
	if ops[1].Path != workspaceDir {
		t.Errorf("expected second op Path %q, got %q", workspaceDir, ops[1].Path)
	}
	if ops[1].Safety != api.SafetySafe {
		t.Errorf("expected second op api.SafetySafe, got %q", ops[1].Safety)
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
		if op.Action != api.ActionDelete {
			t.Errorf("expected highest-severity api.ActionDelete, got %q", op.Action)
		}
		if op.Safety != api.SafetyUnsafe {
			t.Errorf("expected worst api.SafetyUnsafe, got %q", op.Safety)
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
		if op.Action != api.ActionWrite {
			t.Errorf("expected api.ActionWrite, got %q", op.Action)
		}
		if op.Safety != api.SafetyUnknown {
			t.Errorf("expected api.SafetyUnknown due to dynamic $f, got %q", op.Safety)
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
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{
			name:           "read inside .git is safe",
			command:        "cat .git/config",
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "read inside .agents is safe",
			command:        "cat .agents/rules.md",
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "read inside .bare is safe",
			command:        "ls .bare/refs",
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
		},
		{
			name:           "write inside .git is unsafe",
			command:        "touch .git/hooks/pre-commit",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "redirect write inside .git is unsafe",
			command:        "echo 'malicious' > .git/config",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "write inside .bare is unsafe",
			command:        "mkdir .bare/hooks",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "write inside .agents is unsafe",
			command:        "touch .agents/malicious.md",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "delete inside .agents is unsafe",
			command:        "rm .agents/rules.md",
			expectedAction: api.ActionDelete,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "cd into .agents then write is unsafe",
			command:        "cd .agents && touch rules.md",
			expectedAction: api.ActionWrite,
			expectedSafety: api.SafetyUnsafe,
		},
		{
			name:           "cd into .agents then read is safe",
			command:        "cd .agents && cat rules.md",
			expectedAction: api.ActionRead,
			expectedSafety: api.SafetySafe,
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
		if ops[0].Safety != api.SafetySafe || ops[0].Path != "/tmp/workspace/home/project/file.txt" {
			t.Errorf("expected api.SafetySafe at /tmp/workspace/home/project/file.txt, got safety=%s path=%q", ops[0].Safety, ops[0].Path)
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
		if len(ops) != 1 || ops[0].Safety != api.SafetyUnsafe {
			t.Errorf("expected write to custom protected dir .vault to be api.SafetyUnsafe, got %+v", ops)
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
			DefaultCategory: api.ActionWrite,
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
		if ops[0].Action != api.ActionWrite || ops[0].Safety != api.SafetySafe || ops[0].Path != "/tmp/workspace/output.txt" {
			t.Errorf("unexpected operation for custom rule: %+v", ops[0])
		}
	})
}

func TestStringMethods(t *testing.T) {
	actionCases := []struct {
		action api.ActionType
		want   string
	}{
		{api.ActionRead, "read"},
		{api.ActionUnknown, "unknown"},
		{api.ActionWrite, "write"},
		{api.ActionExec, "exec"},
		{api.ActionDelete, "delete"},
		{api.ActionType("invalid"), "unknown"},
	}
	for _, tc := range actionCases {
		if got := tc.action.String(); got != tc.want {
			t.Errorf("api.ActionType(%q).String() = %q, want %q", tc.action, got, tc.want)
		}
	}

	safetyCases := []struct {
		safety api.SafetyLevel
		want   string
	}{
		{api.SafetySafe, "safe"},
		{api.SafetyUnknown, "unknown"},
		{api.SafetyUnsafe, "unsafe"},
		{api.SafetyLevel("invalid"), "unknown"},
	}
	for _, tc := range safetyCases {
		if got := tc.safety.String(); got != tc.want {
			t.Errorf("api.SafetyLevel(%q).String() = %q, want %q", tc.safety, got, tc.want)
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
		wantAction api.ActionType
		wantSafety api.SafetyLevel
		wantComp   bool
	}{
		{
			name:       "while clause with destructive body",
			command:    "while true; do rm /etc/passwd; done",
			wantAction: api.ActionDelete,
			wantSafety: api.SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "if-elif-else clause",
			command:    "if true; then ls; elif false; then pwd; else touch out.txt; fi",
			wantAction: api.ActionWrite,
			wantSafety: api.SafetySafe,
			wantComp:   true,
		},
		{
			name:       "case clause with subst in word and pattern",
			command:    "case $(pwd) in $(echo foo)) rm /etc/passwd ;; esac",
			wantAction: api.ActionDelete,
			wantSafety: api.SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "function declaration",
			command:    "cleanup() { rm -rf build; }; cleanup",
			wantAction: api.ActionDelete,
			wantSafety: api.SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "time and coproc clauses",
			command:    "time ls && coproc pwd",
			wantAction: api.ActionRead,
			wantSafety: api.SafetySafe,
			wantComp:   false,
		},
		{
			name:       "export declaration with command substitution",
			command:    "export SECRET=$(cat /etc/passwd)",
			wantAction: api.ActionRead,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "test clause with command substitution",
			command:    `[[ $(rm /tmp/outside) == "x" ]]`,
			wantAction: api.ActionDelete,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "process substitution <(...)",
			command:    "diff <(cat a.txt) <(cat /etc/passwd)",
			wantAction: api.ActionExec,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "heredoc with command substitution",
			command:    "cat <<EOF\n$(rm /etc/passwd)\nEOF",
			wantAction: api.ActionDelete,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "bare file truncation redirect",
			command:    "> truncated.txt",
			wantAction: api.ActionWrite,
			wantSafety: api.SafetySafe,
			wantComp:   false,
		},
		{
			name:       "multi-command bash -c wrapper",
			command:    `bash -c "cd /tmp && touch escaped.txt"`,
			wantAction: api.ActionWrite,
			wantSafety: api.SafetyUnsafe,
			wantComp:   true,
		},
		{
			name:       "node -e inline script",
			command:    `node -e "console.log(1)"`,
			wantAction: api.ActionUnknown,
			wantSafety: api.SafetySafe,
			wantComp:   true,
		},
		{
			name:       "ruby -e inline script",
			command:    `ruby -e "puts 1"`,
			wantAction: api.ActionUnknown,
			wantSafety: api.SafetySafe,
			wantComp:   true,
		},
		{
			name:       "find with -f path flag and predicate stop",
			command:    `find -f src -name "*.go" -delete`,
			wantAction: api.ActionDelete,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "grep with --file= outside workspace",
			command:    `grep --file=/etc/passwd src/main.go`,
			wantAction: api.ActionRead,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "git with attached -C outside workspace",
			command:    `git -C/etc status`,
			wantAction: api.ActionRead,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "git checkout branch -- outside pathspec",
			command:    `git checkout main -- ../outside.txt`,
			wantAction: api.ActionWrite,
			wantSafety: api.SafetyUnsafe,
			wantComp:   false,
		},
		{
			name:       "BSD sed -i '' in-place edit inside workspace",
			command:    `sed -i '' 's/foo/bar/' file.txt`,
			wantAction: api.ActionWrite,
			wantSafety: api.SafetySafe,
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

func TestIsProtectedPath(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	tests := []struct {
		path string
		want bool
	}{
		{".git", true},
		{".git/config", true},
		{"sub/.git/HEAD", true},
		{".bare", true},
		{".bare/refs/heads/main", true},
		{".agents", true},
		{".agents/skills/test/SKILL.md", true},
		{"/tmp/workspace/.git/config", true},
		{"/tmp/workspace/.agents/rules.md", true},
		{"/tmp/workspace/.bare/HEAD", true},
		{".github/workflows/ci.yml", false},
		{".gitignore", false},
		{"src/main.go", false},
		{"/tmp/workspace/src/main.go", false},
	}

	for _, tt := range tests {
		if got := guard.IsProtectedPath(tt.path); got != tt.want {
			t.Errorf("guard.IsProtectedPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}

	customGuard := New(Config{
		WorkspaceDir:  "/tmp/workspace",
		ProtectedDirs: []string{".secrets", ".private"},
	})
	if !customGuard.IsProtectedPath(".secrets/api.key") {
		t.Error("expected .secrets/api.key to be protected with custom ProtectedDirs")
	}
	if !customGuard.IsProtectedPath("/tmp/workspace/.private/cert.pem") {
		t.Error("expected /tmp/workspace/.private/cert.pem to be protected with custom ProtectedDirs")
	}
	if customGuard.IsProtectedPath(".git/config") {
		t.Error("expected .git/config not to be protected when overridden by custom ProtectedDirs")
	}
}

func TestNetworkCommands(t *testing.T) {
	guard := New(Config{WorkspaceDir: "/tmp/workspace"})

	tests := []struct {
		command        string
		expectedAction api.ActionType
		expectedSafety api.SafetyLevel
	}{
		{"curl https://example.com", api.ActionNet, api.SafetyUnknown},
		{"wget https://example.com/file.tar.gz", api.ActionNet, api.SafetyUnknown},
		{"http GET https://example.com", api.ActionNet, api.SafetyUnknown},
		{"ssh user@example.com", api.ActionNet, api.SafetyUnknown},
		{"scp file.txt user@example.com:/tmp", api.ActionNet, api.SafetyUnknown},
		{"nc localhost 8080", api.ActionNet, api.SafetyUnknown},
		{"dig example.com", api.ActionNet, api.SafetyUnknown},
		{"ping -c 1 8.8.8.8", api.ActionNet, api.SafetyUnknown},
		{"git fetch", api.ActionNet, api.SafetyUnknown},
		{"git pull", api.ActionNet, api.SafetyUnknown},
		{"git push", api.ActionNet, api.SafetyUnknown},
		{"git clone https://github.com/org/repo", api.ActionNet, api.SafetyUnknown},
		{"git clone https://github.com/org/repo subrepo", api.ActionNet, api.SafetyUnknown},
		{"curl -o out.txt https://example.com", api.ActionNet, api.SafetyUnknown},
		{"curl -o ../outside.txt https://example.com", api.ActionNet, api.SafetyUnsafe},
		{"wget -O /etc/passwd https://example.com", api.ActionNet, api.SafetyUnsafe},
		{"curl https://example.com > ../outside.txt", api.ActionNet, api.SafetyUnsafe},
		{"git clone https://github.com/org/repo ../outside", api.ActionNet, api.SafetyUnsafe},
		{"git push --force origin main", api.ActionDelete, api.SafetyUnsafe},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
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

func TestGuardReadDirsAndWriteDirs(t *testing.T) {
	wsDir := "/tmp/test-ws"
	readDir := "/tmp/test-readonly-outputs"
	writeDir := "/tmp/test-scratch-writable"

	g := New(Config{
		WorkspaceDir: wsDir,
		ReadDirs:     []string{readDir},
		WriteDirs:    []string{writeDir},
	})

	// 1. EvaluatePath checks
	// Workspace
	if p, s := g.EvaluatePath(filepath.Join(wsDir, "file.txt"), api.ActionRead); s != api.SafetySafe {
		t.Errorf("expected api.SafetySafe for reading in workspace, got %s (path=%s)", s, p)
	}
	if p, s := g.EvaluatePath(filepath.Join(wsDir, "file.txt"), api.ActionWrite); s != api.SafetySafe {
		t.Errorf("expected api.SafetySafe for writing in workspace, got %s (path=%s)", s, p)
	}

	// ReadDirs: reads are api.SafetySafe, writes/deletes are api.SafetyUnsafe
	readTarget := filepath.Join(readDir, "session-output.txt")
	if p, s := g.EvaluatePath(readTarget, api.ActionRead); s != api.SafetySafe {
		t.Errorf("expected api.SafetySafe for reading in ReadDirs, got %s (path=%s)", s, p)
	}
	if p, s := g.EvaluatePath(readTarget, api.ActionWrite); s != api.SafetyUnsafe {
		t.Errorf("expected api.SafetyUnsafe for writing in ReadDirs, got %s (path=%s)", s, p)
	}
	if p, s := g.EvaluatePath(readTarget, api.ActionDelete); s != api.SafetyUnsafe {
		t.Errorf("expected api.SafetyUnsafe for deleting in ReadDirs, got %s (path=%s)", s, p)
	}

	overlap := filepath.Join(wsDir, "builtin")
	overlapGuard := New(Config{WorkspaceDir: wsDir, ReadDirs: []string{overlap}})
	for _, action := range []api.ActionType{api.ActionWrite, api.ActionDelete} {
		if _, safety := overlapGuard.EvaluatePath(filepath.Join(overlap, "file"), action); safety != api.SafetyUnsafe {
			t.Errorf("read-only workspace overlap: action %s got %s", action, safety)
		}
	}

	// WriteDirs: reads, writes, and deletes are api.SafetySafe
	writeTarget := filepath.Join(writeDir, "temp.txt")
	if p, s := g.EvaluatePath(writeTarget, api.ActionRead); s != api.SafetySafe {
		t.Errorf("expected api.SafetySafe for reading in WriteDirs, got %s (path=%s)", s, p)
	}
	if p, s := g.EvaluatePath(writeTarget, api.ActionWrite); s != api.SafetySafe {
		t.Errorf("expected api.SafetySafe for writing in WriteDirs, got %s (path=%s)", s, p)
	}
	if p, s := g.EvaluatePath(writeTarget, api.ActionDelete); s != api.SafetySafe {
		t.Errorf("expected api.SafetySafe for deleting in WriteDirs, got %s (path=%s)", s, p)
	}

	// Outside path (not in workspace, readDirs, or writeDirs)
	outsideTarget := "/var/log/other.log"
	if p, s := g.EvaluatePath(outsideTarget, api.ActionRead); s != api.SafetyOutside {
		t.Errorf("expected api.SafetyOutside for reading outside path, got %s (path=%s)", s, p)
	}
	if p, s := g.EvaluatePath(outsideTarget, api.ActionWrite); s != api.SafetyUnsafe {
		t.Errorf("expected api.SafetyUnsafe for writing outside path, got %s (path=%s)", s, p)
	}

	// 2. Analyze command checks (cat vs rm)
	readOps, err := g.Analyze(fmt.Sprintf("cat %s", readTarget))
	if err != nil {
		t.Fatalf("Analyze cat readTarget: %v", err)
	}
	if len(readOps) != 1 || readOps[0].Safety != api.SafetySafe || readOps[0].Action != api.ActionRead {
		t.Errorf("expected cat on readTarget to be api.ActionRead api.SafetySafe, got %+v", readOps)
	}

	rmOps, err := g.Analyze(fmt.Sprintf("rm %s", readTarget))
	if err != nil {
		t.Fatalf("Analyze rm readTarget: %v", err)
	}
	if len(rmOps) != 1 || rmOps[0].Safety != api.SafetyUnsafe || rmOps[0].Action != api.ActionDelete {
		t.Errorf("expected rm on readTarget to be api.ActionDelete api.SafetyUnsafe, got %+v", rmOps)
	}

	writeOps, err := g.Analyze(fmt.Sprintf("touch %s && rm %s", writeTarget, writeTarget))
	if err != nil {
		t.Fatalf("Analyze touch && rm writeTarget: %v", err)
	}
	if len(writeOps) != 2 || writeOps[0].Safety != api.SafetySafe || writeOps[1].Safety != api.SafetySafe {
		t.Errorf("expected touch and rm in writeTarget to both be api.SafetySafe, got %+v", writeOps)
	}
}
