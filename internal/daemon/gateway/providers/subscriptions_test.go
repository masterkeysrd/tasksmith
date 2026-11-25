package providers

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fakeSubscriptionBinary(t *testing.T, binary, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, binary), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestClaudeRuntimeIsolatesEnvironment(t *testing.T) {
	fakeSubscriptionBinary(t, "claude", `printf '%s\n' "$PWD" "$5" "$ANTHROPIC_API_KEY" "$GH_TOKEN" "$TASKSMITH_PRIVATE" "$CLAUDE_CONFIG_DIR" "$LANG"`)
	t.Setenv("ANTHROPIC_API_KEY", "api-key-not-for-cli")
	t.Setenv("GH_TOKEN", "github-token-not-for-cli")
	t.Setenv("TASKSMITH_PRIVATE", "daemon-only")
	t.Setenv("CLAUDE_CONFIG_DIR", "host-profile-not-for-cli")
	t.Setenv("LANG", "test'locale")
	profileDir := filepath.Join(t.TempDir(), "profile with 'quotes'")
	cli, err := newClaudeCLI(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	runtime := claudeRuntime(cli)
	if runtime.Path != cli.Executable || runtime.ConfigDir != profileDir || runtime.SourceAuthDir != "" || runtime.Env == nil {
		t.Fatal("Claude runtime does not use the isolated executable, profile, and environment")
	}
	if want := []string{"--setting-sources", "", "--strict-mcp-config", "--no-chrome"}; !slices.Equal(runtime.Args, want) {
		t.Fatalf("Claude runtime args = %q, want %q", runtime.Args, want)
	}
	argument := "literal argument with 'quotes'"
	cmd := exec.CommandContext(t.Context(), runtime.Path, append(runtime.Args, argument)...)
	cmd.Dir = runtime.WorkingDirectory
	cmd.Env = runtime.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("launch Claude fixture: %v: %s", err, out)
	}
	workingDir, err := filepath.EvalSymlinks(cli.WorkDir)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{workingDir, argument, "", "", "", profileDir, "test'locale", ""}, "\n")
	if string(out) != want {
		t.Fatalf("Claude fixture output = %q, want %q", out, want)
	}
}

func TestCopilotLaunchDisablesHostKeyring(t *testing.T) {
	fakeSubscriptionBinary(t, "copilot", `[ "$COPILOT_DISABLE_KEYTAR" = 1 ] && [ -z "$GH_TOKEN" ]`)
	t.Setenv("COPILOT_DISABLE_KEYTAR", "0")
	t.Setenv("GH_TOKEN", "fixture-host-token")
	cli, err := newCopilotCLI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := copilotLogin(t.Context(), cli); err != nil {
		t.Fatalf("login inherited the host keyring or token: %v", err)
	}
	runtime := copilotRuntime(cli)
	cmd := exec.CommandContext(t.Context(), runtime.Path, runtime.Args...)
	cmd.Env = runtime.Env
	cmd.Dir = runtime.WorkingDirectory
	if err := cmd.Run(); err != nil {
		t.Fatalf("inference inherited the host keyring or token: %v", err)
	}
}
