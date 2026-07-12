package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/warp"
)

func TestResolvePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(Config{Cwd: tmpDir})

	t.Run("resolve absolute path", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), testFilePath, TypeFile, "")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if path != testFilePath {
			t.Errorf("expected path %s, got %s", testFilePath, path)
		}
	})

	t.Run("resolve relative path", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), "internal/foo/bar.go", TypeFile, "")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		expected := filepath.Join(tmpDir, "internal", "foo", "bar.go")
		if path != expected {
			t.Errorf("expected path %s, got %s", expected, path)
		}
	})

	t.Run("fuzzy find filename only", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), "bar.go", TypeFile, "")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if !strings.HasSuffix(path, "bar.go") {
			t.Errorf("expected path ending with bar.go, got %s", path)
		}
	})

	t.Run("strip line range anchor", func(t *testing.T) {
		path, err := r.ResolvePath(context.Background(), testFilePath+"#L3-L4", TypeFile, "")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if path != testFilePath {
			t.Errorf("expected path %s, got %s", testFilePath, path)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := r.ResolvePath(context.Background(), "nonexistent.go", TypeFile, "")
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})
}

func TestLoadResource(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(Config{Cwd: tmpDir})

	t.Run("load absolute path", func(t *testing.T) {
		res, err := r.LoadResource(context.Background(), testFilePath, TypeFile, "")
		if err != nil {
			t.Fatalf("LoadResource failed: %v", err)
		}
		if res.Type() != TypeFile {
			t.Errorf("expected TypeFile, got %v", res.Type())
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
		fileRes, ok := res.(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("expected content to contain function body, got: %s", fileRes.Content)
		}
	})

	t.Run("load non-existent absolute path", func(t *testing.T) {
		_, err := r.LoadResource(context.Background(), filepath.Join(tmpDir, "does_not_exist.go"), TypeFile, "")
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})
}

func TestResolveFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(Config{Cwd: tmpDir})

	t.Run("resolve absolute path", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), testFilePath)
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		if res.Type() != TypeFile {
			t.Errorf("expected TypeFile, got %v", res.Type())
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
		fileRes, ok := res.(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("expected content to contain function body, got: %s", fileRes.Content)
		}
	})

	t.Run("resolve relative path", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), "internal/foo/bar.go")
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
	})

	t.Run("fuzzy find filename only", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), "bar.go")
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		if res.Handle() != "bar.go" {
			t.Errorf("expected Handle 'bar.go', got %v", res.Handle())
		}
	})

	t.Run("resolve line range anchor", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), testFilePath+"#L3-L4")
		if err != nil {
			t.Fatalf("ResolveFile failed: %v", err)
		}
		fileRes, ok := res.(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if fileRes.StartLine != 3 || fileRes.EndLine != 4 {
			t.Errorf("expected line range 3-4, got %d-%d", fileRes.StartLine, fileRes.EndLine)
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("content missing range snippet: %s", fileRes.Content)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := r.ResolveFile(context.Background(), "nonexistent.go")
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})

	t.Run("resolve directory", func(t *testing.T) {
		res, err := r.ResolveFile(context.Background(), srcDir)
		if err != nil {
			t.Fatalf("ResolveFile on directory failed: %v", err)
		}
		if res.Type() != TypeFile {
			t.Errorf("expected TypeFile, got %v", res.Type())
		}
		if res.Handle() != "foo" {
			t.Errorf("expected Handle 'foo', got %v", res.Handle())
		}
		fileRes, ok := res.(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if !fileRes.IsDir {
			t.Error("expected IsDir to be true")
		}
		if fileRes.Content != "" {
			t.Errorf("expected empty content for directory, got: %q", fileRes.Content)
		}
	})
}

func TestResolveReferences(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "internal", "foo")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create internal/foo: %v", err)
	}

	testFilePath := filepath.Join(srcDir, "bar.go")
	testContent := "package foo\n\nfunc Hello() string {\n\treturn \"Hello World\"\n}"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := New(Config{Cwd: tmpDir})

	t.Run("extract and resolve manual reference", func(t *testing.T) {
		text := "Check @file:bar.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		fileRes, ok := resources[0].(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resolved resource to be *ResolvedFile")
		}
		if !strings.Contains(fileRes.Content, "func Hello() string") {
			t.Errorf("expected content to contain function body")
		}
	})

	t.Run("dedup same file referenced twice", func(t *testing.T) {
		text := "Check @file:bar.go and also @file:bar.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 1 {
			t.Errorf("expected 1 deduplicated resource, got %d", len(resources))
		}
	})

	t.Run("multiple different files", func(t *testing.T) {
		fooDir := filepath.Join(tmpDir, "internal", "bar")
		if err := os.MkdirAll(fooDir, 0755); err != nil {
			t.Fatalf("failed to create internal/bar: %v", err)
		}
		fooFile := filepath.Join(fooDir, "baz.go")
		if err := os.WriteFile(fooFile, []byte("package bar\n"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		text := "Check @file:bar.go and @file:baz.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 2 {
			t.Errorf("expected 2 resources, got %d", len(resources))
		}
	})

	t.Run("skip unresolvable reference", func(t *testing.T) {
		text := "Check @file:nonexistent.go"
		resources, err := r.ResolveReferences(context.Background(), text, nil, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 0 {
			t.Errorf("expected 0 resources (file not found), got %d", len(resources))
		}
	})

	t.Run("skip already tracked reference", func(t *testing.T) {
		trackedRefs := []Reference{
			{Type: TypeFile, Value: testFilePath, InsertText: "@file:bar.go", FromTracker: true},
		}
		text := "Also check @file:bar.go"
		resources, err := r.ResolveReferences(context.Background(), text, trackedRefs, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 1 {
			t.Errorf("expected 1 resource (deduplicated), got %d", len(resources))
		}
	})

	t.Run("different line ranges for same file", func(t *testing.T) {
		text := "Check lines 3-4 @file:bar.go#L3-L4 and lines 1-2 @file:bar.go#L1-L2"
		resources, err := r.ResolveReferences(context.Background(), text, nil, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		if len(resources) != 2 {
			t.Errorf("expected 2 distinct resources for different ranges, got %d", len(resources))
		}

		// Verify first resource (sorted by insert text "@file:bar.go#L3-L4")
		res0, ok0 := resources[0].(*ResolvedFile)
		if !ok0 {
			t.Fatalf("expected resource to be *ResolvedFile")
		}
		if res0.StartLine != 3 || res0.EndLine != 4 {
			t.Errorf("expected range L3-L4, got L%d-L%d", res0.StartLine, res0.EndLine)
		}

		// Verify second resource ("@file:bar.go#L1-L2")
		res1, ok1 := resources[1].(*ResolvedFile)
		if !ok1 {
			t.Fatalf("expected resource to be *ResolvedFile")
		}
		if res1.StartLine != 1 || res1.EndLine != 2 {
			t.Errorf("expected range L1-L2, got L%d-L%d", res1.StartLine, res1.EndLine)
		}
	})

	t.Run("whole file suppresses ranges", func(t *testing.T) {
		text := "Check all @file:bar.go and also slice @file:bar.go#L3-L4"
		resources, err := r.ResolveReferences(context.Background(), text, nil, "")
		if err != nil {
			t.Fatalf("ResolveReferences failed: %v", err)
		}
		// The range reference L3-L4 should be optimized out because the whole file is also referenced.
		if len(resources) != 1 {
			t.Errorf("expected 1 resource (whole file suppresses range), got %d", len(resources))
		}
		res, ok := resources[0].(*ResolvedFile)
		if !ok {
			t.Fatalf("expected resource to be *ResolvedFile")
		}
		if res.StartLine != 1 || res.EndLine != 5 { // 5 is actualEndLine for whole file bar.go
			t.Errorf("expected whole file range, got L%d-L%d", res.StartLine, res.EndLine)
		}
	})
}

type mockWorkspace struct {
	agents    map[string]*warp.ResolvedAgent
	resources []warp.Resource
}

func (m *mockWorkspace) ResolveAgent(ctx context.Context, ref string) (*warp.ResolvedAgent, error) {
	if a, ok := m.agents[ref]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("agent not found")
}

func (m *mockWorkspace) Resources() []warp.Resource {
	return m.resources
}

func (m *mockWorkspace) CWD() string {
	return ""
}

func (m *mockWorkspace) Providers() []*warp.ModelProvider {
	return nil
}

func (m *mockWorkspace) ResolveDefaults(ctx context.Context) (agentName, providerName, modelName string, err error) {
	return "", "", "", nil
}

func (m *mockWorkspace) Contexts() []*warp.Context {
	return nil
}

func (m *mockWorkspace) WorkspaceSpec() *warp.Workspace {
	return nil
}

func (m *mockWorkspace) Project() *warp.Project {
	return nil
}

func (m *mockWorkspace) Agents() []*warp.Agent {
	return nil
}

func TestResolveSkill(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-resolver-skill-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock skill directory and SKILL.md file
	skillDir := filepath.Join(tmpDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	diskInstructions := "Instructions from disk"
	if err := os.WriteFile(skillPath, []byte(diskInstructions), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	wsInstructions := "Instructions from workspace"
	skillObj := &warp.Skill{}
	skillObj.Directory = skillDir
	skillObj.Metadata.Name = "test-skill"
	skillObj.Spec.Instructions = wsInstructions

	wsAgent := &warp.ResolvedAgent{
		Skills: []warp.Skill{*skillObj},
	}

	mockWS := &mockWorkspace{
		agents: map[string]*warp.ResolvedAgent{
			"test-agent": wsAgent,
		},
		resources: []warp.Resource{skillObj},
	}

	t.Run("resolve from workspace agent", func(t *testing.T) {
		r := New(Config{
			Cwd:       tmpDir,
			Workspace: mockWS,
		})

		path, err := r.ResolvePath(context.Background(), "test-skill", TypeSkill, "test-agent")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}

		res, err := r.LoadResource(context.Background(), path, TypeSkill, "test-agent")
		if err != nil {
			t.Fatalf("LoadResource failed: %v", err)
		}

		skillRes, ok := res.(*ResolvedSkill)
		if !ok {
			t.Fatalf("expected *ResolvedSkill, got %T", res)
		}

		if skillRes.Instructions != wsInstructions {
			t.Errorf("expected instructions %q, got %q", wsInstructions, skillRes.Instructions)
		}

		if skillRes.Name != "test-skill" {
			t.Errorf("expected name %q, got %q", "test-skill", skillRes.Name)
		}
	})

	t.Run("returns error when workspace is nil", func(t *testing.T) {
		r := New(Config{
			Cwd: tmpDir,
		})

		_, err := r.loadResourceSkill(context.Background(), skillDir, "test-agent")
		if err == nil {
			t.Error("expected error when workspace is nil, got nil")
		}
	})

	t.Run("returns error when skill is not assigned to agent", func(t *testing.T) {
		r := New(Config{
			Cwd:       tmpDir,
			Workspace: mockWS,
		})

		_, err := r.loadResourceSkill(context.Background(), "/some/other/path", "test-agent")
		if err == nil {
			t.Error("expected error when skill is not assigned to agent, got nil")
		}
	})
}
