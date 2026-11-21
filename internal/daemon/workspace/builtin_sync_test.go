package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

func TestBuiltinSync(t *testing.T) {
	dir := t.TempDir()
	source := fstest.MapFS{
		"skills/demo/SKILL.md":    {Data: []byte("first")},
		"skills/demo/scripts/run": {Data: []byte("script")},
		"obsolete/old.md":         {Data: []byte("old")},
	}
	manifest, err := makeBuiltinManifest(source)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files["skills/demo/scripts/run"].Mode != 0755 {
		t.Fatal("script mode")
	}
	if err := syncBuiltinDir(dir, source, manifest); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, ".manifest.json")
	stamp := time.Unix(1000000000, 0)
	if err := os.Chtimes(manifestPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := syncBuiltinDir(dir, source, manifest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manifestPath)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("unchanged manifest was rewritten: %v", err)
	}
	file := filepath.Join(dir, "skills", "demo", "SKILL.md")
	if err := os.WriteFile(file, []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}
	delete(source, "obsolete/old.md")
	next, err := makeBuiltinManifest(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncBuiltinDir(dir, source, next); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "first" {
		t.Fatalf("tampering not repaired: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "obsolete")); !os.IsNotExist(err) {
		t.Fatalf("stale directory remains: %v", err)
	}
	if err := os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syncBuiltinDir(dir, source, next); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(file)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("mode not repaired: %v %v", info, err)
	}
}

func TestBuiltinSyncRejectsStaleTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside-marker")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })
	previous := builtinManifest{Version: 1, Files: map[string]builtinFile{"../outside-marker": {}}}
	data, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".manifest.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{"example": {Data: []byte("example")}}
	manifest, err := makeBuiltinManifest(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncBuiltinDir(dir, source, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file removed: %v", err)
	}
}

func TestBuiltinSkillDiscoveryAndFallback(t *testing.T) {
	root := setupTestXDG(t)
	ws, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := ws.Catalog().GetSkill("tasksmith-customizations")
	if !ok {
		t.Fatal("built-in skill missing")
	}
	dir, err := xdg.BuiltinDir("skills", "tasksmith-customizations")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(sk.Def.Directory) != filepath.Clean(dir) {
		t.Fatalf("skill directory %q, want %q", sk.Def.Directory, dir)
	}
	if _, err := os.Stat(filepath.Join(sk.Def.Directory, "references", "rules.md")); err != nil {
		t.Fatal(err)
	}
	resolved, err := ws.Catalog().ResolveAgent("main")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, skill := range resolved.Skills {
		if skill.Metadata.Name == sk.Name {
			found = true
		}
	}
	if !found {
		t.Fatal("main agent cannot load customization skill")
	}
	for _, name := range []string{"agent-creator", "skill-creator", "tool-creator", "project-initializer", "provider-manager"} {
		if _, ok := ws.Catalog().GetAgent(name); ok {
			t.Fatalf("obsolete agent %q still registered", name)
		}
	}
	dataHome := filepath.Join(root, "blocked")
	if err := os.WriteFile(dataHome, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	xdg.ClearCache()
	fallback, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fbSk, ok := fallback.Catalog().GetSkill("tasksmith-customizations")
	if !ok {
		t.Fatal("embedded fallback missing skill")
	}
	text, err := fbSk.Def.Render(&warp.SkillRenderOptions{
		Workspace: fallback.Catalog().WorkspaceSpec(),
		Project:   fallback.Catalog().Project(),
	})
	if err != nil || !strings.Contains(text, "references/rules.md") {
		t.Fatalf("fallback render: %v", err)
	}
}

// TestBuiltinInitCommandResolves guards the /init slash command end-to-end on
// the daemon side: the built-in command must be listed for the CLI registry,
// resolve for the main agent, and carry the AGENTS.md authoring skill.
func TestBuiltinInitCommandResolves(t *testing.T) {
	setupTestXDG(t)
	ws, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat := ws.Catalog()

	cmd, ok := cat.GetCommand("init")
	if !ok {
		t.Fatal("built-in init command missing")
	}
	if cmd.Namespace != warp.NamespaceSystem {
		t.Fatalf("init namespace = %q, want %q", cmd.Namespace, warp.NamespaceSystem)
	}
	found := false
	for _, listed := range cat.Commands() {
		if listed.Name == "init" {
			found = true
		}
	}
	if !found {
		t.Fatal("init command not listed for CLI registration")
	}

	ref := cmd.Def.QualifiedName()
	if ref == "" {
		ref = cmd.Def.Metadata.Name
	}
	resolved, err := cat.ResolveCommand("main", ref)
	if err != nil {
		t.Fatalf("resolve init for main agent: %v", err)
	}
	if resolved.Agent == nil {
		t.Fatal("init resolved without an agent")
	}
	skillFound := false
	for _, sk := range resolved.Skills {
		if sk.Metadata.Name == "tasksmith-customizations" {
			skillFound = true
		}
	}
	if !skillFound {
		t.Fatal("init command does not carry the tasksmith-customizations skill")
	}
	hasLS := slices.Contains(cmd.Def.Spec.Tools, "ls")
	if !hasLS {
		t.Fatalf("init command tools = %v, want ls included", cmd.Def.Spec.Tools)
	}
	if !strings.Contains(cmd.Def.Spec.Instructions, "empty or contains only bare configuration") {
		t.Fatal("init command missing empty/config-only pre-flight guard")
	}
	builtinDir, err := EnsureBuiltinDir()
	if err != nil {
		t.Fatalf("EnsureBuiltinDir: %v", err)
	}
	agentsMDRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "tasksmith-customizations", "references", "agents-md.md"))
	if err != nil {
		t.Fatalf("agents-md reference missing: %v", err)
	}
	if !strings.Contains(string(agentsMDRef), "Progressive Disclosure") {
		t.Fatal("agents-md reference missing progressive disclosure guidance")
	}
}

func TestBuiltinGrillMeCommandResolves(t *testing.T) {
	setupTestXDG(t)
	ctx := t.Context()
	root := t.TempDir()
	const name = "grill-me"
	const path = "commands/grill-me.command.md"
	builtinDir, err := EnsureBuiltinDir()
	if err != nil {
		t.Fatal(err)
	}
	mirrored, err := os.ReadFile(filepath.Join(builtinDir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := builtinFS.ReadFile("builtin/" + path)
	if err != nil || string(mirrored) != string(embedded) {
		t.Fatalf("mirrored command differs from embedded source: %v", err)
	}

	for _, source := range []string{"mirror", "embedded fallback"} {
		t.Run(source, func(t *testing.T) {
			if source == "embedded fallback" {
				blocked := filepath.Join(t.TempDir(), "blocked")
				if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("XDG_DATA_HOME", blocked)
				xdg.ClearCache()
				t.Cleanup(xdg.ClearCache)
			}
			ws, err := Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			cat := ws.Catalog()
			cmd, ok := cat.GetCommand(name)
			if !ok || cmd.Namespace != warp.NamespaceSystem {
				t.Fatalf("%s command missing from system catalog: %+v", name, cmd)
			}
			if cmd.Def.Metadata.DisplayName != "Grill Me" || cmd.Def.Spec.Context != warp.CommandContextInline || cmd.Def.Spec.ArgumentHint != "[idea, plan, or problem]" {
				t.Fatalf("unexpected command metadata or spec: %+v / %+v", cmd.Def.Metadata, cmd.Def.Spec)
			}
			wantTools := []string{"ls", "view", "glob", "grep", "web_fetch", "web_search", "invoke_agent", "manage_agents", "ask_question"}
			if !slices.Equal(cmd.Def.Spec.Tools, wantTools) {
				t.Fatalf("tools = %v, want %v", cmd.Def.Spec.Tools, wantTools)
			}
			if !slices.ContainsFunc(cat.Commands(), func(c CommandSnapshot) bool { return c.Name == name }) {
				t.Fatalf("%s command not listed for client registration", name)
			}
			resolved, err := cat.ResolveCommand("main", cmd.Def.QualifiedName())
			if err != nil {
				t.Fatal(err)
			}
			const topic = "How should task retries work?"
			text, err := cmd.Def.Render(&warp.CommandRenderOptions{
				Workspace: cat.WorkspaceSpec(),
				Project:   cat.Project(),
				Resolved:  resolved,
				Globals:   map[string]any{"RawArguments": topic},
			})
			if err != nil || !strings.Contains(text, topic) || !strings.Contains(text, "Confirm shared understanding") {
				t.Fatalf("rendered %s: %v\n%s", name, err, text)
			}
		})
	}
}

// TestBuiltinSandboxPermissionReferences guards Task 7 of the sandbox plan:
// both customization skills must sync their sandbox-and-permissions reference
// through EnsureBuiltinDir, resolve on the main agent, and stay readable under
// the embedded fallback when the mirror is unavailable.
func TestBuiltinSandboxPermissionReferences(t *testing.T) {
	setupTestXDG(t)
	ws, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat := ws.Catalog()

	resolved, err := cat.ResolveAgent("main")
	if err != nil {
		t.Fatal(err)
	}
	carrying := map[string]bool{}
	for _, sk := range resolved.Skills {
		carrying[sk.Metadata.Name] = true
	}
	for _, name := range []string{"tasksmith-customizations", "cli-customizations"} {
		if !carrying[name] {
			t.Fatalf("main agent does not carry skill %q", name)
		}
	}

	builtinDir, err := EnsureBuiltinDir()
	if err != nil {
		t.Fatalf("EnsureBuiltinDir: %v", err)
	}
	for _, skill := range []string{"tasksmith-customizations", "cli-customizations"} {
		path := filepath.Join(builtinDir, "skills", skill, "references", "sandbox-and-permissions.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("synced %s reference missing: %v", skill, err)
		}
		text := string(data)
		if !strings.Contains(text, "sandbox.json") || !strings.Contains(text, "permissions.json") {
			t.Fatalf("%s reference lacks sandbox.json/permissions.json guidance", skill)
		}
		if strings.Contains(text, "docs/guides/") {
			t.Fatalf("%s reference contains repo-only docs/guides/ path unavailable in user workspaces", skill)
		}
	}
	tsRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "tasksmith-customizations", "references", "sandbox-and-permissions.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, replOnly := range []string{"`/sandbox`", "`/permissions`", "`/workspace reload`"} {
		if strings.Contains(string(tsRef), replOnly) {
			t.Fatalf("tasksmith-customizations reference must stay client-agnostic; found %s", replOnly)
		}
	}
	headlessRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "cli-customizations", "references", "headless-exec.md"))
	if err != nil {
		t.Fatalf("synced cli-customizations headless-exec reference missing: %v", err)
	}
	for _, want := range []string{"--input-format", "--output-format", "needs_input", "ConnectOrStart"} {
		if !strings.Contains(string(headlessRef), want) {
			t.Fatalf("headless-exec reference lacks %q guidance", want)
		}
	}
	modesRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "cli-customizations", "references", "modes-and-planning.md"))
	if err != nil {
		t.Fatalf("synced cli-customizations modes-and-planning reference missing: %v", err)
	}
	for _, want := range []string{"/plan", "Shift+Tab", "exit_plan_mode", "accept_edits"} {
		if !strings.Contains(string(modesRef), want) {
			t.Fatalf("modes-and-planning reference lacks %q guidance", want)
		}
	}
	quotasAuthRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "cli-customizations", "references", "models-quotas-and-auth.md"))
	if err != nil {
		t.Fatalf("synced cli-customizations models-quotas-and-auth reference missing: %v", err)
	}
	for _, want := range []string{"Quotas", "tasksmith auth", "/login", "--paste"} {
		if !strings.Contains(string(quotasAuthRef), want) {
			t.Fatalf("models-quotas-and-auth reference lacks %q guidance", want)
		}
	}
	sessionsMediaRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "cli-customizations", "references", "sessions-and-media.md"))
	if err != nil {
		t.Fatalf("synced cli-customizations sessions-and-media reference missing: %v", err)
	}
	for _, want := range []string{"--continue", "/fork", "capability_mismatch", "Ctrl+V"} {
		if !strings.Contains(string(sessionsMediaRef), want) {
			t.Fatalf("sessions-and-media reference lacks %q guidance", want)
		}
	}
	appearanceRef, err := os.ReadFile(filepath.Join(builtinDir, "skills", "cli-customizations", "references", "appearance.md"))
	if err != nil {
		t.Fatalf("synced cli-customizations appearance reference missing: %v", err)
	}
	for _, want := range []string{"statusline.json", "theme.json", "/theme", "extends"} {
		if !strings.Contains(string(appearanceRef), want) {
			t.Fatalf("appearance reference lacks %q guidance", want)
		}
	}

	mainPrompt, err := resolved.Agent.Render(&warp.AgentRenderOptions{
		Workspace: cat.WorkspaceSpec(),
		Project:   cat.Project(),
		Resolved:  resolved,
		Globals:   cat.AgentRenderGlobals(resolved),
	})
	if err != nil {
		t.Fatalf("render main agent: %v", err)
	}
	for _, unwanted := range []string{"<keywords>", "<globs>", "No specific skills required."} {
		if strings.Contains(mainPrompt, unwanted) {
			t.Fatalf("main agent prompt still renders redundant %q", unwanted)
		}
	}

	sk, ok := cat.GetSkill("tasksmith-customizations")
	if !ok {
		t.Fatal("tasksmith-customizations skill missing")
	}
	text, err := sk.Def.Render(&warp.SkillRenderOptions{
		Workspace: cat.WorkspaceSpec(),
		Project:   cat.Project(),
	})
	if err != nil || !strings.Contains(text, "references/sandbox-and-permissions.md") {
		t.Fatalf("tasksmith-customizations render: %v", err)
	}

	// Embedded fallback: block the XDG data mirror and confirm both skills
	// still resolve and render the new reference from embedded resources.
	root := t.TempDir()
	dataHome := filepath.Join(root, "blocked")
	if err := os.WriteFile(dataHome, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	xdg.ClearCache()
	fallback, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tasksmith-customizations", "cli-customizations"} {
		fbSk, ok := fallback.Catalog().GetSkill(name)
		if !ok {
			t.Fatalf("embedded fallback missing skill %q", name)
		}
		fbText, err := fbSk.Def.Render(&warp.SkillRenderOptions{
			Workspace: fallback.Catalog().WorkspaceSpec(),
			Project:   fallback.Catalog().Project(),
		})
		if err != nil || !strings.Contains(fbText, "references/sandbox-and-permissions.md") {
			t.Fatalf("fallback render for %s: %v", name, err)
		}
		if name == "cli-customizations" {
			for _, ref := range []string{
				"references/appearance.md",
				"references/headless-exec.md",
				"references/models-quotas-and-auth.md",
				"references/sessions-and-media.md",
			} {
				if !strings.Contains(fbText, ref) {
					t.Fatalf("fallback render for %s lacks %s reference", name, ref)
				}
			}
		}
	}
}
