package prompt

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/masterkeysrd/warp"
)

func TestBuildGlobals(t *testing.T) {
	os.Setenv("TERM", "vt100")
	os.Setenv("USER", "test-user")
	defer os.Unsetenv("TERM")
	defer os.Unsetenv("USER")

	globals := buildGlobals("/custom/cwd", map[string]any{"Extra": "value"})

	if globals["CWD"] != "/custom/cwd" {
		t.Errorf("expected CWD /custom/cwd, got %v", globals["CWD"])
	}
	if globals["OS"] != runtime.GOOS {
		t.Errorf("expected OS %s, got %v", runtime.GOOS, globals["OS"])
	}
	if globals["Terminal"] != "vt100" {
		t.Errorf("expected Terminal vt100, got %v", globals["Terminal"])
	}
	if globals["User"] != "test-user" {
		t.Errorf("expected User test-user, got %v", globals["User"])
	}
	if globals["Extra"] != "value" {
		t.Errorf("expected Extra value, got %v", globals["Extra"])
	}
	if _, ok := globals["Shell"]; !ok {
		t.Error("expected Shell to be set")
	}
	if _, ok := globals["Home"]; !ok {
		t.Error("expected Home to be set")
	}
	if _, ok := globals["Host"]; !ok {
		t.Error("expected Host to be set")
	}
	if globals["Arch"] != runtime.GOARCH {
		t.Errorf("expected Arch %s, got %v", runtime.GOARCH, globals["Arch"])
	}

	// Test git integration globals using current working directory
	wd, err := os.Getwd()
	if err == nil {
		gitGlobals := buildGlobals(wd, nil)
		// If git CLI is available and it is a repo, verify branch and commit
		if _, hasBranch := gitGlobals["GitBranch"]; hasBranch {
			if gitGlobals["GitBranch"] == "" {
				t.Error("expected GitBranch to be non-empty")
			}
		}
		if _, hasCommit := gitGlobals["GitCommit"]; hasCommit {
			if len(gitGlobals["GitCommit"].(string)) == 0 {
				t.Error("expected GitCommit to be non-empty")
			}
		}
	}
}

func TestRenderAgent(t *testing.T) {
	os.Setenv("USER", "test-agent-user")
	defer os.Unsetenv("USER")

	agent := &warp.Agent{
		Spec: warp.AgentSpec{
			Instructions: "Hello {{.User}} on {{.OS}}! Context: {{.Context}}",
		},
	}
	resolved := &warp.ResolvedAgent{
		Agent: agent,
	}

	res, err := RenderAgent(resolved, nil, nil, map[string]any{"Context": "some-context"})
	if err != nil {
		t.Fatalf("unexpected error rendering: %v", err)
	}

	expectedPrefix := "Hello test-agent-user on " + runtime.GOOS + "! Context: some-context"
	if !strings.HasPrefix(res, expectedPrefix) {
		t.Errorf("expected result to start with %q, got %q", expectedPrefix, res)
	}
}

func TestRenderSkill(t *testing.T) {
	skill := &warp.Skill{
		Spec: warp.SkillSpec{
			Instructions: "Skill for agent {{.Agent.Name}} in {{.CWD}}",
		},
	}
	// Give the skill a name
	skill.Metadata.Name = "git-expert"
	agent := &warp.Agent{}
	agent.Metadata.Name = "coder"

	res, err := RenderSkill(skill, agent, nil, nil, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error rendering skill: %v", err)
	}

	if !strings.Contains(res, "Skill for agent coder") {
		t.Errorf("expected rendering to contain 'Skill for agent coder', got %q", res)
	}
}
