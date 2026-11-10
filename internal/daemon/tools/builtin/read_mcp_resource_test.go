package builtin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// stubResourceReader replays one resource read and records how it was asked,
// standing in for the MCP runtime so the built-in tool can be exercised without a
// server.
type stubResourceReader struct {
	text     string
	mimeType string
	calls    []struct {
		server string
		uri    string
		allow  bool
	}
	err error
}

func (s *stubResourceReader) CallMCPTool(context.Context, string, string, map[string]any) (tool.ToolStream, error) {
	return nil, nil
}

func (s *stubResourceReader) ReadMCPResource(_ context.Context, serverName, uri string, allowProhibited bool) (api.MCPResourceResponse, error) {
	s.calls = append(s.calls, struct {
		server string
		uri    string
		allow  bool
	}{serverName, uri, allowProhibited})
	if s.err != nil {
		return api.MCPResourceResponse{}, s.err
	}
	return api.MCPResourceResponse{URI: uri, MIMEType: s.mimeType, Text: s.text}, nil
}

func (s *stubResourceReader) HasAllowedMCPResources() bool { return true }

// readResourceEnv writes a workspace declaring one MCP server whose manifest blocks
// one resource, then wires the built-in tools with a stub MCP runtime.
func readResourceEnv(t *testing.T, reader tools.MCPCaller) (tools.Container, *workspace.Workspace) {
	t.Helper()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-read-mcp-resource-test")
	t.Cleanup(xdg.ClearCache)

	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: read-resource-test
spec:
  defaultAgent: main
---
Workspace instructions.
`
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpManifest := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: notes
spec:
  type: stdio
  command: ["notes-mcp"]
  policies:
    resources:
      exclude:
        - "file:///notes/.env"
`
	if err := os.WriteFile(filepath.Join(agentsDir, "notes.mcp.yaml"), []byte(mcpManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), dir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	stdLib, err := std.New(std.Config{Sandbox: sb, Tracker: std.NewMemoryTracker(), Logger: log.Default()})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}

	allowed := ws.Catalog().ToolNames()
	builtinTools, err := builtin.New(builtin.Config{
		Std:          stdLib,
		Sandbox:      sb,
		Guard:        shellguard.New(shellguard.Config{WorkspaceDir: dir}),
		Workspace:    ws,
		AllowedTools: allowed,
		MCPCaller:    reader,
	})
	if err != nil {
		t.Fatalf("builtin.New: %v", err)
	}

	spiller, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   filepath.Join(dir, ".spill"),
		MaxBytes:  1024,
		MaxLines:  40,
		HeadLines: 2,
		TailLines: 2,
	})
	if err != nil {
		t.Fatalf("tools.NewSpiller: %v", err)
	}

	c, err := tools.NewContainer(withExecDeps(t, tools.Config{
		Tools:        builtinTools,
		Sandbox:      sb,
		Workspace:    ws,
		Logger:       log.Default(),
		Spiller:      spiller,
		AllowedTools: allowed,
		MCPCaller:    reader,
	}))
	if err != nil {
		t.Fatalf("tools.NewContainer: %v", err)
	}
	return c, ws
}

// TestReadMCPResourceTool verifies the built-in reads a permitted resource with its
// MIME type, never authorizes a prohibited one on the agent's behalf, and spills an
// oversized resource instead of flooding the context.
func TestReadMCPResourceTool(t *testing.T) {
	reader := &stubResourceReader{text: "readme contents", mimeType: "text/markdown"}
	c, _ := readResourceEnv(t, reader)

	if _, ok := c.Get(builtin.ToolReadMCPResource); !ok {
		t.Fatalf("read_mcp_resource not registered with an MCP runtime attached")
	}

	plan, err := c.Prepare(context.Background(), &message.ToolCall{
		ID:   "p1",
		Name: builtin.ToolReadMCPResource,
		Args: map[string]any{"server": "notes", "uri": "file:///notes/readme.md"},
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Action != api.ActionRead || !plan.ReadOnly {
		t.Errorf("plan = %+v, want a read-only read action from the manifest annotations", plan)
	}
	if plan.Target != "notes/file:///notes/readme.md" {
		t.Errorf("Target = %q, want it scoped to the server and URI", plan.Target)
	}
	if !strings.Contains(plan.Summary, "file:///notes/readme.md") {
		t.Errorf("Summary = %q, want the userHint to name the resource", plan.Summary)
	}

	res, err := c.Execute(context.Background(), &message.ToolCall{
		ID:   "e1",
		Name: builtin.ToolReadMCPResource,
		Args: map[string]any{"server": "notes", "uri": "file:///notes/readme.md"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned an error: %s", res.Content.Text())
	}
	out, ok := res.StructuredContent.(api.ReadMCPResourceOutput)
	if !ok {
		t.Fatalf("StructuredContent = %T, want api.ReadMCPResourceOutput", res.StructuredContent)
	}
	if out.Text != "readme contents" || out.MimeType != "text/markdown" || out.Server != "notes" {
		t.Errorf("output = %+v, want the server's text and MIME type", out)
	}
	if out.Truncated {
		t.Errorf("Truncated = true for a small resource")
	}
	if len(reader.calls) == 0 {
		t.Fatal("the MCP runtime was never called")
	}
	last := reader.calls[len(reader.calls)-1]
	if last.allow {
		t.Errorf("allowProhibited = true, want the agent never to authorize a prohibited read")
	}

	// A prohibited resource is refused in Prepare (before any permission prompt)
	// and in Execute (before anything reaches the server).
	if _, err := c.Prepare(context.Background(), &message.ToolCall{
		ID:   "p2",
		Name: builtin.ToolReadMCPResource,
		Args: map[string]any{"server": "notes", "uri": "file:///notes/.env"},
	}); err == nil || !strings.Contains(err.Error(), "spec.policies.resources") {
		t.Errorf("Prepare(prohibited) error = %v, want hard block naming spec.policies.resources", err)
	}
	before := len(reader.calls)
	prohibited, err := c.Execute(context.Background(), &message.ToolCall{
		ID:   "e2",
		Name: builtin.ToolReadMCPResource,
		Args: map[string]any{"server": "notes", "uri": "file:///notes/.env"},
	})
	if err != nil {
		t.Fatalf("Execute(prohibited): %v", err)
	}
	if !prohibited.IsError {
		t.Errorf("Execute(prohibited) IsError = false, want a hard block")
	}
	if !strings.Contains(prohibited.Content.Text(), "spec.policies.resources") {
		t.Errorf("prohibited result = %q, want it to name the policy section", prohibited.Content.Text())
	}
	if !strings.Contains(prohibited.Content.Text(), "notes.mcp.yaml") {
		t.Errorf("prohibited result = %q, want it to name the manifest", prohibited.Content.Text())
	}
	if len(reader.calls) != before {
		t.Errorf("prohibited read reached the server: %+v", reader.calls[before:])
	}
}

// TestReadMCPResourceToolSpillsLargeResource verifies a bulky resource becomes a
// preview flagged as truncated, so reading documentation cannot blow the context.
func TestReadMCPResourceToolSpillsLargeResource(t *testing.T) {
	var big strings.Builder
	for range 600 {
		big.WriteString("resource line\n")
	}
	reader := &stubResourceReader{text: big.String()}
	c, _ := readResourceEnv(t, reader)

	res, err := c.Execute(context.Background(), &message.ToolCall{
		ID:   "e1",
		Name: builtin.ToolReadMCPResource,
		Args: map[string]any{"server": "notes", "uri": "file:///notes/big.md"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned an error: %s", res.Content.Text())
	}
	out, ok := res.StructuredContent.(api.ReadMCPResourceOutput)
	if !ok {
		t.Fatalf("StructuredContent = %T, want api.ReadMCPResourceOutput", res.StructuredContent)
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true for a spilled resource")
	}
	if !strings.Contains(out.Text, "full output saved to") {
		t.Errorf("text = %.300s, want a pointer to the spilled file", out.Text)
	}
	if len(out.Text) >= len(big.String()) {
		t.Errorf("text length = %d, want it truncated from %d", len(out.Text), len(big.String()))
	}
}

// TestReadMCPResourceToolRequiresRuntime verifies the tool is not offered when no MCP
// runtime is attached, so an agent is never invited to read resources nothing can
// serve.
func TestReadMCPResourceToolRequiresRuntime(t *testing.T) {
	c, _ := readResourceEnv(t, nil)
	if _, ok := c.Get(builtin.ToolReadMCPResource); ok {
		t.Error("read_mcp_resource registered without an MCP runtime")
	}
}
