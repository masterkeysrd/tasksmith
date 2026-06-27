package cmdparse

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
	// The entire chain should be in one pipeline with the last operator.
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	pipeline := chain.Pipelines[0]
	if pipeline.Operator != "|" {
		t.Errorf("expected operator '|', got '%s'", pipeline.Operator)
	}
	// Should have 3 commands: go build, ./main, grep error
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
	// The subcommand should parse the inner chain.
	if cmd.SubCommand.Executable != "go" {
		t.Errorf("expected sub-command executable 'go', got '%s'", cmd.SubCommand.Executable)
	}
}

func TestAppOutputRedirection(t *testing.T) {
	chain, err := Parse("echo hello >> out.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if len(cmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(cmd.Redirects))
	}
	if cmd.Redirects[0].Op != ">>" {
		t.Errorf("expected redirect op '>>', got '%s'", cmd.Redirects[0].Op)
	}
	if cmd.Redirects[0].Target != "out.log" {
		t.Errorf("expected target 'out.log', got '%s'", cmd.Redirects[0].Target)
	}
}

func TestBackgroundOperator(t *testing.T) {
	chain, err := Parse("go build &")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pipeline := chain.Pipelines[0]
	if pipeline.Operator != "&" {
		t.Errorf("expected operator '&', got '%s'", pipeline.Operator)
	}
}

func TestMultipleRedirects(t *testing.T) {
	chain, err := Parse("cmd > out.txt 2> err.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if len(cmd.Redirects) != 2 {
		t.Fatalf("expected 2 redirects, got %d", len(cmd.Redirects))
	}
}

func TestEmptyCommand(t *testing.T) {
	chain, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 0 {
		t.Errorf("expected 0 pipelines for empty command, got %d", len(chain.Pipelines))
	}
}

func TestEnvVarMultiple(t *testing.T) {
	chain, err := Parse("PORT=8080 HOST=localhost go run main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "go" {
		t.Errorf("expected executable 'go', got '%s'", cmd.Executable)
	}
	if len(cmd.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(cmd.Env))
	}
	if cmd.Env[0].Name != "PORT" || cmd.Env[0].Value != "8080" {
		t.Errorf("expected ENV {Name:PORT, Value:8080}, got %+v", cmd.Env[0])
	}
	if cmd.Env[1].Name != "HOST" || cmd.Env[1].Value != "localhost" {
		t.Errorf("expected ENV {Name:HOST, Value:localhost}, got %+v", cmd.Env[1])
	}
}

func TestComplexChain(t *testing.T) {
	chain, err := Parse("go build && ./main | grep error > out.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(chain.Pipelines))
	}
	pipeline := chain.Pipelines[0]
	// Should have 3 commands in the pipeline.
	if len(pipeline.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(pipeline.Commands))
	}
	// The last command (grep) should have the redirect.
	grepCmd := pipeline.Commands[2]
	if len(grepCmd.Redirects) != 1 {
		t.Errorf("expected 1 redirect on grep, got %d", len(grepCmd.Redirects))
	}
}

func TestSudoComplex(t *testing.T) {
	chain, err := Parse("sudo rm -rf tmp && echo done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pipeline := chain.Pipelines[0]
	if pipeline.Operator != "&&" {
		t.Errorf("expected operator '&&', got '%s'", pipeline.Operator)
	}
	// First command should be sudo with sub-command.
	sudoCmd := pipeline.Commands[0]
	if sudoCmd.Executable != "sudo" {
		t.Errorf("expected first command 'sudo', got '%s'", sudoCmd.Executable)
	}
	if sudoCmd.SubCommand == nil {
		t.Fatal("expected SubCommand for sudo")
	}
	if sudoCmd.SubCommand.Executable != "rm" {
		t.Errorf("expected sub-command 'rm', got '%s'", sudoCmd.SubCommand.Executable)
	}
	// Second command should be echo.
	echoCmd := pipeline.Commands[1]
	if echoCmd.Executable != "echo" {
		t.Errorf("expected second command 'echo', got '%s'", echoCmd.Executable)
	}
}

func TestShCWrapper(t *testing.T) {
	chain, err := Parse(`sh -c "echo hello"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd := chain.Pipelines[0].Commands[0]
	if cmd.Executable != "sh" {
		t.Errorf("expected executable 'sh', got '%s'", cmd.Executable)
	}
	if cmd.SubCommand == nil {
		t.Fatal("expected SubCommand for sh -c")
	}
	if cmd.SubCommand.Executable != "echo" {
		t.Errorf("expected sub-command 'echo', got '%s'", cmd.SubCommand.Executable)
	}
}
