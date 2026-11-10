package tools_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// fakeMCPCaller records invocations and replays a canned result, standing in for a
// live MCP server so tool behavior can be asserted without a transport.
type fakeMCPCaller struct {
	calls       []recordedCall
	chunks      []message.ToolChunk
	streamErr   error
	callErr     error
	resource    api.MCPResourceResponse
	resourceErr error
	resources   bool
}

type recordedCall struct {
	server string
	tool   string
	args   map[string]any
}

func (f *fakeMCPCaller) CallMCPTool(_ context.Context, serverName, rawToolName string, args map[string]any) (tool.ToolStream, error) {
	f.calls = append(f.calls, recordedCall{server: serverName, tool: rawToolName, args: args})
	if f.callErr != nil {
		return nil, f.callErr
	}
	chunks := f.chunks
	streamErr := f.streamErr
	return func(yield func(message.ToolChunk, error) bool) {
		for _, chunk := range chunks {
			if !yield(chunk, nil) {
				return
			}
		}
		if streamErr != nil {
			yield(message.ToolChunk{}, streamErr)
		}
	}, nil
}

func (f *fakeMCPCaller) ReadMCPResource(_ context.Context, serverName, uri string, allowProhibited bool) (api.MCPResourceResponse, error) {
	f.calls = append(f.calls, recordedCall{server: serverName, tool: uri, args: map[string]any{"allowProhibited": allowProhibited}})
	if f.resourceErr != nil {
		return api.MCPResourceResponse{}, f.resourceErr
	}
	resp := f.resource
	if resp.URI == "" {
		resp.URI = uri
	}
	return resp, nil
}

func (f *fakeMCPCaller) HasAllowedMCPResources() bool { return f.resources }

// textChunks builds a single-chunk text result like the ones an MCP server returns.
func textChunks(text string) []message.ToolChunk {
	return []message.ToolChunk{{Content: message.Content{&message.TextBlock{Text: text}}}}
}

// mcpFixture writes a workspace declaring one stdio MCP server, opens it, and wires
// a container whose MCP runtime is a [fakeMCPCaller].
type mcpFixture struct {
	dir    string
	ws     *workspace.Workspace
	caller *fakeMCPCaller
	sb     sandbox.Sandbox
}

func newMCPFixture(t *testing.T, policies string) *mcpFixture {
	t.Helper()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-mcp-tool-test")
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
  name: mcp-exec-ws
spec:
  defaultAgent: main
---
Workspace instructions.
`
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpManifest := "apiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: notes\n  description: Notes MCP server\nspec:\n  type: stdio\n  command: [\"notes-mcp\"]\n" + policies
	if err := os.WriteFile(filepath.Join(agentsDir, "notes.mcp.yaml"), []byte(mcpManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	return &mcpFixture{dir: dir, ws: ws, caller: &fakeMCPCaller{}, sb: sb}
}

// publish registers discovered tools with the workspace, mirroring what the MCP
// manager does once a server connects.
func (f *mcpFixture) publish(t *testing.T, discovered ...*warp.Tool) {
	t.Helper()
	def, ok := f.ws.Catalog().GetMCP("notes")
	if !ok {
		t.Fatal("MCP server notes missing from catalog")
	}
	if err := f.ws.SyncMCPTools(def, discovered); err != nil {
		t.Fatalf("SyncMCPTools: %v", err)
	}
}

// container builds a tool container granting the named tools.
func (f *mcpFixture) container(t *testing.T, allowed ...string) tools.Container {
	t.Helper()
	c, err := tools.NewContainer(withExecDeps(t, tools.Config{
		Sandbox:      f.sb,
		Workspace:    f.ws,
		Logger:       log.Default(),
		AllowedTools: allowed,
		MCPCaller:    f.caller,
	}))
	if err != nil {
		t.Fatalf("tools.NewContainer: %v", err)
	}
	return c
}

// discovered builds a tool as the manager reports it after discovery, carrying the
// wire annotation the remote server advertised.
func discovered(name, description string, ann *warp.ToolAnnotation) *warp.Tool {
	return &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Metadata:   warp.Metadata{Name: name, Description: description},
		},
		Spec: warp.ToolSpec{
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
				"required": []any{"message"},
			},
			Annotations: ann,
		},
	}
}

// TestMCPToolPlanFromAnnotation verifies a discovered tool's plan is derived
// strictly from its effective annotations, matching the mapping command-backed
// custom tools use, and that the permission target is scoped to the server.
func TestMCPToolPlanFromAnnotation(t *testing.T) {
	cases := []struct {
		name       string
		ann        *warp.ToolAnnotation
		wantAction api.ActionType
		wantSafety api.SafetyLevel
		wantRead   bool
		wantSum    string
	}{
		{
			name:       "unannotated",
			wantAction: api.ActionExec,
			wantSafety: api.SafetyUnknown,
			wantSum:    "Run MCP tool: notes__echo",
		},
		{
			name:       "read only",
			ann:        &warp.ToolAnnotation{IsReadOnly: true},
			wantAction: api.ActionRead,
			wantSafety: api.SafetySafe,
			wantRead:   true,
			wantSum:    "Run MCP tool: notes__echo",
		},
		{
			name:       "read only and open world",
			ann:        &warp.ToolAnnotation{IsReadOnly: true, IsOpenWorld: true},
			wantAction: api.ActionRead,
			wantSafety: api.SafetyUnknown,
			wantRead:   true,
			wantSum:    "Run MCP tool: notes__echo",
		},
		{
			name:       "open world",
			ann:        &warp.ToolAnnotation{IsOpenWorld: true},
			wantAction: api.ActionNet,
			wantSafety: api.SafetyUnknown,
			wantSum:    "Run MCP tool: notes__echo",
		},
		{
			name:       "dangerous wins",
			ann:        &warp.ToolAnnotation{IsDangerous: true, IsReadOnly: true, IsOpenWorld: true},
			wantAction: api.ActionExec,
			wantSafety: api.SafetyUnsafe,
			wantSum:    "Run MCP tool: notes__echo",
		},
		{
			name:       "user hint without placeholder",
			ann:        &warp.ToolAnnotation{IsReadOnly: true, UserHint: "Echo text"},
			wantAction: api.ActionRead,
			wantSafety: api.SafetySafe,
			wantRead:   true,
			wantSum:    "Echo text",
		},
		{
			name:       "user hint with placeholder",
			ann:        &warp.ToolAnnotation{IsReadOnly: true, UserHint: "Echo: %s"},
			wantAction: api.ActionRead,
			wantSafety: api.SafetySafe,
			wantRead:   true,
			wantSum:    "Echo: notes/echo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMCPFixture(t, "")
			f.publish(t, discovered("notes__echo", "Echo text back", tc.ann))
			c := f.container(t, "notes__echo")

			plan, err := c.Prepare(context.Background(), &message.ToolCall{
				ID:   "p1",
				Name: "notes__echo",
				Args: map[string]any{"message": "hi"},
			})
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if plan.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", plan.Action, tc.wantAction)
			}
			if plan.Safety != tc.wantSafety {
				t.Errorf("Safety = %q, want %q", plan.Safety, tc.wantSafety)
			}
			if plan.ReadOnly != tc.wantRead {
				t.Errorf("ReadOnly = %v, want %v", plan.ReadOnly, tc.wantRead)
			}
			if plan.Summary != tc.wantSum {
				t.Errorf("Summary = %q, want %q", plan.Summary, tc.wantSum)
			}
			if want := "notes/echo"; plan.Target != want {
				t.Errorf("Target = %q, want %q", plan.Target, want)
			}
		})
	}
}

// TestMCPToolDefinitionReflectsDiscovery verifies a registered MCP tool keeps the
// server's schema, description, and annotation so the model sees what the server
// actually advertises.
func TestMCPToolDefinitionReflectsDiscovery(t *testing.T) {
	f := newMCPFixture(t, "")
	f.publish(t, discovered("notes__echo", "Echo text back", &warp.ToolAnnotation{IsReadOnly: true, IsIdempotent: true}))
	c := f.container(t, "notes__echo")

	got, ok := c.Get("notes__echo")
	if !ok {
		t.Fatalf("MCP tool not registered: %v", toolNames(c))
	}
	def := got.Definition()
	if def.Description != "Echo text back" {
		t.Errorf("Description = %q, want the server's description", def.Description)
	}
	if !def.Annotation.IsReadOnly || !def.Annotation.IsIdempotent {
		t.Errorf("Annotation = %+v, want the wire annotation preserved", def.Annotation)
	}
	if def.InputSchema == nil || def.InputSchema.Properties["message"] == nil {
		t.Errorf("InputSchema = %v, want the server's message property", def.InputSchema)
	}
	if def.OutputSchema == nil {
		t.Error("OutputSchema = nil, want the string fallback so the tool is well-formed")
	}
}

// TestMCPToolValidatesArgsAgainstServerSchema verifies arguments are checked against
// the schema the server advertised before anything is sent to it.
func TestMCPToolValidatesArgsAgainstServerSchema(t *testing.T) {
	f := newMCPFixture(t, "")
	f.publish(t, discovered("notes__echo", "Echo text back", nil))
	c := f.container(t, "notes__echo")

	if _, err := c.Prepare(context.Background(), &message.ToolCall{ID: "p1", Name: "notes__echo", Args: map[string]any{}}); err == nil {
		t.Error("Prepare with a missing required argument = nil error, want a validation error")
	}
	res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Errorf("Execute with a missing required argument: IsError = false, want true")
	}
	if len(f.caller.calls) != 0 {
		t.Errorf("rejected call reached the server: %+v", f.caller.calls)
	}
}

// TestMCPToolSendsRawToolName verifies the server receives its own tool name rather
// than the materialized "<server>__<tool>" name the agent calls, which no server
// would recognize.
func TestMCPToolSendsRawToolName(t *testing.T) {
	f := newMCPFixture(t, "")
	f.publish(t, discovered("notes__echo", "Echo text back", nil))
	f.caller.chunks = textChunks("echo: hi")
	c := f.container(t, "notes__echo")

	res, err := c.Execute(context.Background(), &message.ToolCall{
		ID:   "e1",
		Name: "notes__echo",
		Args: map[string]any{"message": "hi"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned an error: %s", res.Content.Text())
	}
	if len(f.caller.calls) != 1 {
		t.Fatalf("caller calls = %+v, want exactly one", f.caller.calls)
	}
	call := f.caller.calls[0]
	if call.server != "notes" || call.tool != "echo" {
		t.Errorf("call = %s/%s, want notes/echo", call.server, call.tool)
	}
	if call.args["message"] != "hi" {
		t.Errorf("call args = %v, want message=hi", call.args)
	}
}

// TestMCPToolAggregatesContentAndErrors verifies text and media blocks both survive,
// a structured-only result is still surfaced to the model, and a server-reported tool
// error marks the result as an error instead of reading as success.
func TestMCPToolAggregatesContentAndErrors(t *testing.T) {
	t.Run("text and media", func(t *testing.T) {
		f := newMCPFixture(t, "")
		f.publish(t, discovered("notes__echo", "Echo text back", nil))
		f.caller.chunks = []message.ToolChunk{
			{Content: message.Content{&message.TextBlock{Text: "part one "}}},
			{Content: message.Content{&message.TextBlock{Text: "part two"}}},
			{Content: message.Content{&message.ImageBlock{Data: []byte("png"), MIMEType: "image/png"}}},
			{StructuredContent: map[string]any{"text": "echo: hi"}},
		}
		c := f.container(t, "notes__echo")

		res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.IsError {
			t.Fatalf("Execute reported an error: %s", res.Content.Text())
		}
		if got := res.Content.Text(); got != "part one part two" {
			t.Errorf("text = %q, want the chunks concatenated", got)
		}
		var images int
		for _, block := range res.Content {
			if _, ok := block.(*message.ImageBlock); ok {
				images++
			}
		}
		if images != 1 {
			t.Errorf("image blocks = %d, want 1: media must not be dropped", images)
		}
		if res.StructuredContent == nil {
			t.Error("StructuredContent = nil, want the server's structured payload")
		}
	})

	t.Run("structured only", func(t *testing.T) {
		f := newMCPFixture(t, "")
		f.publish(t, discovered("notes__echo", "Echo text back", nil))
		f.caller.chunks = []message.ToolChunk{{StructuredContent: map[string]any{"count": 2}}}
		c := f.container(t, "notes__echo")

		res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(res.Content.Text(), `"count":2`) {
			t.Errorf("text = %q, want the structured payload rendered as JSON so the model sees it", res.Content.Text())
		}
	})

	t.Run("server reported error", func(t *testing.T) {
		f := newMCPFixture(t, "")
		f.publish(t, discovered("notes__echo", "Echo text back", nil))
		f.caller.chunks = []message.ToolChunk{{
			Content: message.Content{&message.TextBlock{Text: "no such note"}},
			IsError: true,
		}}
		c := f.container(t, "notes__echo")

		res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !res.IsError {
			t.Errorf("IsError = false, want true for a server-reported tool error")
		}
		if got := res.Content.Text(); got != "no such note" {
			t.Errorf("text = %q, want the server's error text preserved for the model", got)
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		f := newMCPFixture(t, "")
		f.publish(t, discovered("notes__echo", "Echo text back", nil))
		f.caller.streamErr = errors.New("connection reset")
		c := f.container(t, "notes__echo")

		res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content.Text(), "connection reset") {
			t.Errorf("result = %+v, want IsError naming the transport failure", res)
		}
	})
}

// TestMCPToolOutputSpilling verifies a large result becomes a truncated preview
// pointing at the spilled file instead of flooding the model's context.
func TestMCPToolOutputSpilling(t *testing.T) {
	f := newMCPFixture(t, "")
	f.publish(t, discovered("notes__echo", "Echo text back", nil))

	var big strings.Builder
	for i := 1; i <= 900; i++ {
		fmt.Fprintf(&big, "line %d of a very large MCP result\n", i)
	}
	f.caller.chunks = textChunks(big.String())

	spiller, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir:   t.TempDir(),
		MaxBytes:  1024,
		MaxLines:  50,
		HeadLines: 3,
		TailLines: 3,
	})
	if err != nil {
		t.Fatalf("NewSpiller: %v", err)
	}

	c, err := tools.NewContainer(withExecDeps(t, tools.Config{
		Sandbox:      f.sb,
		Workspace:    f.ws,
		Logger:       log.Default(),
		Spiller:      spiller,
		AllowedTools: []string{"notes__echo"},
		MCPCaller:    f.caller,
	}))
	if err != nil {
		t.Fatalf("tools.NewContainer: %v", err)
	}

	res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned an error: %s", res.Content.Text())
	}
	text := res.Content.Text()
	if strings.Contains(text, "line 450 of") {
		t.Errorf("result carried the middle of the payload, want it truncated:\n%s", text)
	}
	if !strings.Contains(text, "line 1 of") || !strings.Contains(text, "line 900 of") {
		t.Errorf("result = %.200s, want both the head and the tail of the payload", text)
	}
	if !strings.Contains(text, "full output saved to") {
		t.Errorf("result = %.300s, want a pointer to the spilled file", text)
	}
}

// TestMCPToolProhibitedAfterTightenedPolicy verifies the container refuses a tool
// the agent could previously reach once the server manifest's policy tightens,
// naming the section and manifest so the user can resolve it.
func TestMCPToolProhibitedAfterTightenedPolicy(t *testing.T) {
	f := newMCPFixture(t, "")
	f.publish(t, discovered("notes__echo", "Echo text back", nil))
	f.caller.chunks = textChunks("echo: hi")
	c := f.container(t, "notes__echo")

	if _, err := c.Prepare(context.Background(), &message.ToolCall{ID: "p1", Name: "notes__echo", Args: map[string]any{"message": "hi"}}); err != nil {
		t.Fatalf("Prepare before the policy change: %v", err)
	}

	// A reload is what tightens policy: the container keeps its tool instance, so
	// the check must consult the definition now on disk rather than a snapshot.
	manifest := filepath.Join(f.dir, ".agents", "notes.mcp.yaml")
	content, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	tightened := strings.Replace(string(content), "  command: [\"notes-mcp\"]\n",
		"  command: [\"notes-mcp\"]\n  policies:\n    tools:\n      exclude:\n        - delete_everything\n        - echo\n", 1)
	if err := os.WriteFile(manifest, []byte(tightened), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.Reload(context.Background()); err != nil {
		t.Fatalf("workspace reload: %v", err)
	}

	_, err = c.Prepare(context.Background(), &message.ToolCall{ID: "p2", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
	if err == nil {
		t.Fatal("Prepare after the policy change = nil error, want a hard block")
	}
	if _, ok := errors.AsType[*tools.MCPProhibitedError](err); !ok {
		t.Fatalf("Prepare error = %v, want *tools.MCPProhibitedError", err)
	}
	if !strings.Contains(err.Error(), "spec.policies.tools") {
		t.Errorf("Prepare error = %q, want it to name spec.policies.tools", err)
	}
	if !strings.Contains(err.Error(), "notes.mcp.yaml") {
		t.Errorf("Prepare error = %q, want it to name the manifest", err)
	}
	if strings.Contains(err.Error(), f.dir) {
		t.Errorf("Prepare error = %q, want no absolute machine path", err)
	}

	res, err := c.Execute(context.Background(), &message.ToolCall{ID: "e1", Name: "notes__echo", Args: map[string]any{"message": "hi"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content.Text(), "spec.policies.tools") {
		t.Errorf("Execute result = %+v, want an error result naming the policy", res)
	}
	if len(f.caller.calls) != 0 {
		t.Errorf("prohibited call reached the server: %+v", f.caller.calls)
	}
}

// TestRegisterMCPToolsGating verifies MCP tools are registered only when an MCP
// runtime is attached and only for tools the agent's resolved list grants, while
// command-backed custom tools keep going through their own path.
func TestRegisterMCPToolsGating(t *testing.T) {
	f := newMCPFixture(t, "")
	f.publish(t, discovered("notes__echo", "Echo text back", nil), discovered("notes__other", "Another tool", nil))

	granted := f.container(t, "notes__echo")
	if _, ok := granted.Get("notes__echo"); !ok {
		t.Errorf("granted MCP tool missing: %v", toolNames(granted))
	}
	if _, ok := granted.Get("notes__other"); ok {
		t.Errorf("MCP tool outside the agent's tool list was registered: %v", toolNames(granted))
	}

	// Without a runtime, nothing can invoke the server's tools, so they must not be
	// offered at all rather than failing at call time.
	noRuntime, err := tools.NewContainer(withExecDeps(t, tools.Config{
		Sandbox:      f.sb,
		Workspace:    f.ws,
		Logger:       log.Default(),
		AllowedTools: []string{"notes__echo"},
	}))
	if err != nil {
		t.Fatalf("tools.NewContainer without a caller: %v", err)
	}
	if _, ok := noRuntime.Get("notes__echo"); ok {
		t.Errorf("MCP tool registered without an MCP runtime: %v", toolNames(noRuntime))
	}
}

// TestReadMCPResourcePolicy verifies reading a resource enforces the server
// manifest's resource policy unless the caller is a user authorizing one prohibited
// read, and that a policy rejection names the manifest without an absolute path.
func TestReadMCPResourcePolicy(t *testing.T) {
	f := newMCPFixture(t, `  policies:
    resources:
      exclude:
        - "file:///notes/.env"
`)
	f.caller.resource = api.MCPResourceResponse{Text: "readme contents", MIMEType: "text/markdown"}
	ctx := context.Background()

	t.Run("permitted", func(t *testing.T) {
		resp, err := tools.ReadMCPResource(ctx, f.ws, f.caller, "notes", "file:///notes/readme.md", false)
		if err != nil {
			t.Fatalf("ReadMCPResource: %v", err)
		}
		if resp.Text != "readme contents" || resp.MIMEType != "text/markdown" {
			t.Errorf("resp = %+v, want the server's text and MIME type", resp)
		}
		if len(f.caller.calls) != 1 || f.caller.calls[0].server != "notes" {
			t.Errorf("caller calls = %+v, want one call to notes", f.caller.calls)
		}
	})

	t.Run("prohibited", func(t *testing.T) {
		before := len(f.caller.calls)
		_, err := tools.ReadMCPResource(ctx, f.ws, f.caller, "notes", "file:///notes/.env", false)
		if err == nil {
			t.Fatal("ReadMCPResource of a prohibited resource = nil error, want a hard block")
		}
		if _, ok := errors.AsType[*tools.MCPProhibitedError](err); !ok {
			t.Fatalf("error = %v, want *tools.MCPProhibitedError", err)
		}
		if !strings.Contains(err.Error(), "spec.policies.resources") || !strings.Contains(err.Error(), "notes.mcp.yaml") {
			t.Errorf("error = %q, want it to name spec.policies.resources and the manifest", err)
		}
		if strings.Contains(err.Error(), f.dir) {
			t.Errorf("error = %q, want no absolute machine path", err)
		}
		if len(f.caller.calls) != before {
			t.Errorf("prohibited read reached the server: %+v", f.caller.calls[before:])
		}
	})

	t.Run("user authorized once", func(t *testing.T) {
		if _, err := tools.ReadMCPResource(ctx, f.ws, f.caller, "notes", "file:///notes/.env", true); err != nil {
			t.Fatalf("ReadMCPResource with AllowProhibited: %v", err)
		}
		last := f.caller.calls[len(f.caller.calls)-1]
		if allowed, _ := last.args["allowProhibited"].(bool); !allowed {
			t.Errorf("allowProhibited = %v, want the runtime told a user authorized this read", last.args["allowProhibited"])
		}
	})

	t.Run("unknown server", func(t *testing.T) {
		_, err := tools.ReadMCPResource(ctx, f.ws, f.caller, "absent", "file:///x", false)
		if !errors.Is(err, tools.ErrToolNotFound) {
			t.Errorf("error = %v, want ErrToolNotFound", err)
		}
	})

	t.Run("blank arguments", func(t *testing.T) {
		if _, err := tools.ReadMCPResource(ctx, f.ws, f.caller, "  ", "file:///x", false); !errors.Is(err, tools.ErrInvalidConfig) {
			t.Errorf("blank server error = %v, want ErrInvalidConfig", err)
		}
		if _, err := tools.ReadMCPResource(ctx, f.ws, f.caller, "notes", "  ", false); !errors.Is(err, tools.ErrInvalidConfig) {
			t.Errorf("blank uri error = %v, want ErrInvalidConfig", err)
		}
	})

	t.Run("no runtime attached", func(t *testing.T) {
		if _, err := tools.ReadMCPResource(ctx, f.ws, nil, "notes", "file:///notes/readme.md", false); err == nil {
			t.Error("ReadMCPResource without a runtime = nil error, want a descriptive error")
		}
	})
}

func toolNames(c tools.Container) []string {
	var names []string
	for _, t := range c.List() {
		names = append(names, t.Name())
	}
	return names
}
