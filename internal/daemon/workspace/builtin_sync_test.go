package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	builtinDir, err := EnsureBuiltinDir()
	if err != nil {
		t.Fatalf("EnsureBuiltinDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(builtinDir, "skills", "tasksmith-customizations", "references", "agents-md.md")); err != nil {
		t.Fatalf("agents-md reference missing: %v", err)
	}
}
