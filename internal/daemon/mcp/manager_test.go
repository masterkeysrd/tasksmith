package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// testServer is a real, in-process MCP server served over streamable HTTP, so
// tests exercise the same protocol path the daemon uses in production. It also
// serves SSE for the same server, because server-initiated list-change
// notifications only flow over transports with a server-to-client stream.
type testServer struct {
	endpoint    string
	sseEndpoint string
	sdk         *sdkmcp.Server
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	type echoIn struct {
		Text string `json:"text" jsonschema:"text to echo back"`
	}
	type echoOut struct {
		Text string `json:"text"`
	}
	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "echo",
		Description: "Echo text back",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in echoIn) (*sdkmcp.CallToolResult, echoOut, error) {
		return nil, echoOut{Text: "echo: " + in.Text}, nil
	})
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "delete_everything", Description: "Destructive"},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, echoOut, error) {
			return nil, echoOut{Text: "deleted"}, nil
		})
	srv.AddPrompt(&sdkmcp.Prompt{
		Name:        "summary",
		Description: "Summarize a target",
		Arguments:   []*sdkmcp.PromptArgument{{Name: "target", Required: true}},
	}, func(_ context.Context, _ *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{
			Description: "Summarize a target",
			Messages: []*sdkmcp.PromptMessage{{
				Role:    "user",
				Content: &sdkmcp.TextContent{Text: "summarize the target"},
			}},
		}, nil
	})
	srv.AddPrompt(&sdkmcp.Prompt{Name: "danger"}, func(_ context.Context, _ *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{
			Messages: []*sdkmcp.PromptMessage{{Role: "user", Content: &sdkmcp.TextContent{Text: "danger"}}},
		}, nil
	})
	srv.AddResource(&sdkmcp.Resource{URI: "file:///notes/readme.md", Name: "readme", MIMEType: "text/markdown"},
		func(_ context.Context, _ *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{
				URI:      "file:///notes/readme.md",
				MIMEType: "text/markdown",
				Text:     "readme contents",
			}}}, nil
		})
	srv.AddResource(&sdkmcp.Resource{URI: "file:///notes/.env", Name: "env", MIMEType: "text/plain"},
		func(_ context.Context, _ *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{
				URI:  "file:///notes/.env",
				Text: "SECRET=leaked",
			}}}, nil
		})

	httpSrv := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil))
	t.Cleanup(httpSrv.Close)

	// Server-initiated list-change notifications only flow when the client keeps a
	// server-to-client stream open, which the streamable-http transport disables
	// and the sse transport keeps. This second server exists so a test can connect
	// with the stream enabled without changing how the other tests connect.
	sseSrv := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil))
	t.Cleanup(sseSrv.Close)

	return &testServer{endpoint: httpSrv.URL, sseEndpoint: sseSrv.URL, sdk: srv}
}

// recorder captures what the manager publishes into the workspace registry.
// Publishing can happen from resync goroutines, so access is guarded.
type recorder struct {
	mu    sync.Mutex
	calls []recordedSync
}

type recordedSync struct {
	def   *warp.MCP
	tools []*warp.Tool
}

func (r *recorder) SyncMCPTools(def *warp.MCP, rawTools []*warp.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedSync{def: def, tools: rawTools})
	return nil
}

// count returns how many publishes have been recorded.
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// last returns the most recent publish, or the zero value when nothing was
// published yet.
func (r *recorder) last() recordedSync {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return recordedSync{}
	}
	return r.calls[len(r.calls)-1]
}

// snapshot returns a copy of every recorded publish.
func (r *recorder) snapshot() []recordedSync {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// definer serves a fixed set of definitions.
type definer struct {
	defs []*warp.MCP
}

func (d *definer) MCPDef(name string) (*warp.MCP, bool) {
	for _, def := range d.defs {
		if def.Metadata.Name == name || def.QualifiedName() == name {
			return def, true
		}
	}
	return nil, false
}

func (d *definer) MCPDefinitions() []*warp.MCP { return d.defs }

// remoteDef builds an HTTP MCP definition pointing at a test server.
func remoteDef(name, endpoint string, mutate func(*warp.MCP)) *warp.MCP {
	def := &warp.MCP{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindMCP,
			Metadata:   warp.Metadata{Name: name},
			Namespace:  warp.NamespaceWorkspace,
		},
		Spec: warp.MCPSpec{Type: warp.MCPTransportStreamableHTTP, Endpoint: endpoint},
	}
	if mutate != nil {
		mutate(def)
	}
	return def
}

func newManager(t *testing.T, defs ...*warp.MCP) (*Manager, *definer, *recorder) {
	t.Helper()
	d := &definer{defs: defs}
	rec := &recorder{}
	mgr, err := New(Options{
		Definer:        d,
		Syncer:         rec,
		Env:            map[string]string{},
		RenderOptions:  warp.MCPRenderOptions{},
		ConnectTimeout: 15 * time.Second,
		Credentials:    NewCredentialStoreAt(filepath.Join(t.TempDir(), "credentials.json")),
		AutoConnect:    true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, d, rec
}

func waitForStatus(t *testing.T, mgr *Manager, name string, want api.MCPStatus) api.MCPServer {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, server := range mgr.ListMCPs(context.Background()) {
			if server.Name == name && server.Status == want {
				return server
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("MCP server %q never reached status %q: %+v", name, want, mgr.ListMCPs(context.Background()))
	return api.MCPServer{}
}

func TestSyncConnectsAndDiscovers(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Policies = &warp.MCPPolicies{
			Tools:     &warp.ResourceFilter{Exclude: []string{"delete_*"}},
			Prompts:   &warp.ResourceFilter{Exclude: []string{"danger"}},
			Resources: &warp.ResourceFilter{Exclude: []string{"file:///notes/.env"}},
		}
	})
	mgr, _, rec := newManager(t, def)

	mgr.Sync(context.Background())
	server := waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	if server.Endpoint != ts.endpoint {
		t.Errorf("endpoint = %q, want %q", server.Endpoint, ts.endpoint)
	}
	if server.Scope != "Workspace" {
		t.Errorf("scope = %q, want Workspace", server.Scope)
	}

	// Discovery retains prohibited items so they can be shown as blocked.
	if len(server.Tools) != 2 {
		t.Fatalf("expected both discovered tools, got %+v", server.Tools)
	}
	allowed := map[string]bool{}
	for _, tool := range server.Tools {
		allowed[tool.Name] = tool.Allowed
		if tool.RawName == "" {
			t.Errorf("tool %q must carry its raw wire name", tool.Name)
		}
	}
	if !allowed["notes__echo"] {
		t.Errorf("notes__echo must be allowed: %+v", server.Tools)
	}
	if allowed["notes__delete_everything"] {
		t.Errorf("delete_* must keep the tool prohibited: %+v", server.Tools)
	}

	promptAllowed := map[string]bool{}
	for _, prompt := range server.Prompts {
		promptAllowed[prompt.Name] = prompt.Allowed
	}
	if len(promptAllowed) != 2 || !promptAllowed["summary"] || promptAllowed["danger"] {
		t.Errorf("prompt policy filtering wrong: %+v", server.Prompts)
	}

	resourceAllowed := map[string]bool{}
	for _, resource := range server.Resources {
		resourceAllowed[resource.URI] = resource.Allowed
	}
	if len(resourceAllowed) != 2 || !resourceAllowed["file:///notes/readme.md"] || resourceAllowed["file:///notes/.env"] {
		t.Errorf("resource policy filtering wrong: %+v", server.Resources)
	}

	// Everything discovered is published so the workspace can register the
	// permitted subset and report the rest as blocked.
	if rec.count() == 0 {
		t.Fatal("expected tools to be published to the workspace")
	}
	last := rec.last()
	if last.def == nil || last.def.Metadata.Name != "notes" {
		t.Fatalf("published to wrong definition: %+v", last.def)
	}
	if len(last.tools) != 2 {
		t.Errorf("expected 2 published raw tools, got %d", len(last.tools))
	}
	for _, tool := range last.tools {
		if !strings.HasPrefix(tool.Metadata.Name, "notes__") {
			t.Errorf("published tool must carry the materialized name, got %q", tool.Metadata.Name)
		}
	}
}

func TestCallMCPToolAndPolicyBlock(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Policies = &warp.MCPPolicies{Tools: &warp.ResourceFilter{Exclude: []string{"delete_*"}}}
	})
	mgr, _, _ := newManager(t, def)
	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	stream, err := mgr.CallMCPTool(context.Background(), "notes", "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("CallMCPTool: %v", err)
	}
	var text strings.Builder
	for chunk, chunkErr := range stream {
		if chunkErr != nil {
			t.Fatalf("tool stream: %v", chunkErr)
		}
		text.WriteString(chunk.Content.Text())
		if structured, ok := chunk.StructuredContent.(map[string]any); ok {
			if value, ok := structured["text"].(string); ok {
				text.WriteString(" " + value)
			}
		}
	}
	if !strings.Contains(text.String(), "echo: hi") {
		t.Errorf("tool output = %q, want it to contain %q", text.String(), "echo: hi")
	}

	_, err = mgr.CallMCPTool(context.Background(), "notes", "delete_everything", nil)
	if err == nil || !strings.Contains(err.Error(), "spec.policies.tools") {
		t.Errorf("prohibited tool must be rejected by policy, got %v", err)
	}
}

func TestReadMCPResourceAndPrompt(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Policies = &warp.MCPPolicies{
			Prompts:   &warp.ResourceFilter{Exclude: []string{"danger"}},
			Resources: &warp.ResourceFilter{Exclude: []string{"file:///notes/.env"}},
		}
	})
	mgr, _, _ := newManager(t, def)
	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	resp, err := mgr.ReadMCPResource(context.Background(), "notes", api.MCPResourceRequest{URI: "file:///notes/readme.md"})
	if err != nil {
		t.Fatalf("ReadMCPResource: %v", err)
	}
	if resp.Text != "readme contents" {
		t.Errorf("resource text = %q, want %q", resp.Text, "readme contents")
	}
	if resp.MIMEType != "text/markdown" {
		t.Errorf("resource mime = %q, want text/markdown", resp.MIMEType)
	}

	prompt, err := mgr.GetMCPPrompt(context.Background(), "notes", "summary", map[string]string{"target": "the repo"}, false)
	if err != nil {
		t.Fatalf("GetMCPPrompt: %v", err)
	}
	if len(prompt.Messages) != 1 || prompt.Messages[0].Role != api.RoleUser {
		t.Errorf("prompt messages = %+v", prompt.Messages)
	}
	if !strings.Contains(prompt.Messages[0].Text, "summarize the target") {
		t.Errorf("prompt text = %q", prompt.Messages[0].Text)
	}
}

func TestHasAllowedMCPResources(t *testing.T) {
	ts := newTestServer(t)
	mgr, defs, _ := newManager(t)
	if mgr.HasAllowedMCPResources() {
		t.Error("a manager with no servers must not report resources")
	}

	def := remoteDef("notes", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Policies = &warp.MCPPolicies{Resources: &warp.ResourceFilter{Exclude: []string{"*"}}}
	})
	defs.defs = []*warp.MCP{def}
	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)
	if mgr.HasAllowedMCPResources() {
		t.Error("blocking every resource must report no allowed resources")
	}

	defs.defs = []*warp.MCP{remoteDef("notes", ts.endpoint, nil)}
	mgr.Sync(context.Background())
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !mgr.HasAllowedMCPResources() {
		time.Sleep(20 * time.Millisecond)
	}
	if !mgr.HasAllowedMCPResources() {
		t.Error("allowed resources must be reported once discovery completes")
	}
}

func TestRestartReconnectsAndReportsError(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", ts.endpoint, nil)
	mgr, _, _ := newManager(t, def)

	restarted, err := mgr.RestartMCP(context.Background(), "notes")
	if err != nil {
		t.Fatalf("RestartMCP: %v", err)
	}
	if restarted.Status != api.MCPStatusConnected {
		t.Errorf("status = %q, want connected", restarted.Status)
	}
	if len(restarted.Tools) == 0 {
		t.Error("restart must re-discover tools")
	}

	if _, err := mgr.RestartMCP(context.Background(), "missing"); err == nil {
		t.Error("expected an error for an unknown server")
	}
}

func TestUnreachableServerReportsError(t *testing.T) {
	def := remoteDef("down", "http://127.0.0.1:1/mcp", nil)
	mgr, _, _ := newManager(t, def)
	mgr.Sync(context.Background())

	server := waitForStatus(t, mgr, "down", api.MCPStatusError)
	if server.Error == "" {
		t.Error("a failed connection must report why")
	}
}

func TestBuildConfigTransportMapping(t *testing.T) {
	mgr, _, _ := newManager(t)

	cases := []struct {
		transport warp.MCPTransport
		wantSSE   bool
	}{
		{warp.MCPTransportHTTP, true},
		{warp.MCPTransportStreamableHTTP, true},
		{warp.MCPTransportSSE, false},
	}
	for _, tc := range cases {
		def := remoteDef("svc", "https://example.invalid/mcp", func(d *warp.MCP) {
			d.Spec.Type = tc.transport
		})
		cfg, provider, err := mgr.buildConfig(def, map[string]string{}, warp.MCPRenderOptions{})
		if err != nil {
			t.Fatalf("%s: buildConfig: %v", tc.transport, err)
		}
		if cfg.Transport != "http" {
			t.Errorf("%s: transport = %q, want http", tc.transport, cfg.Transport)
		}
		if cfg.DisableStandaloneSSE != tc.wantSSE {
			t.Errorf("%s: DisableStandaloneSSE = %v, want %v", tc.transport, cfg.DisableStandaloneSSE, tc.wantSSE)
		}
		if provider != nil {
			t.Errorf("%s: unexpected OAuth provider", tc.transport)
		}
	}

	stdio := &warp.MCP{
		BaseResource: warp.BaseResource{Kind: warp.KindMCP, Metadata: warp.Metadata{Name: "local"}},
		Spec: warp.MCPSpec{
			Type:    warp.MCPTransportStdio,
			Command: []string{"server", "--stdio"},
			Env:     map[string]string{"TOKEN": "$TOKEN_VALUE"},
		},
	}
	cfg, provider, err := mgr.buildConfig(stdio, map[string]string{"TOKEN_VALUE": "secret"}, warp.MCPRenderOptions{})
	if err != nil {
		t.Fatalf("stdio buildConfig: %v", err)
	}
	if provider != nil {
		t.Error("stdio must not attach an OAuth provider")
	}
	if cfg.Transport != "stdio" || cfg.Command != "server" || strings.Join(cfg.Args, " ") != "--stdio" {
		t.Errorf("stdio config = %+v", cfg)
	}
	if cfg.Env["TOKEN"] != "secret" {
		t.Errorf("stdio env expansion = %q, want secret", cfg.Env["TOKEN"])
	}
}

func TestUnsupportedTransportFails(t *testing.T) {
	mgr, _, _ := newManager(t)
	def := &warp.MCP{
		BaseResource: warp.BaseResource{Kind: warp.KindMCP, Metadata: warp.Metadata{Name: "odd"}},
		Spec:         warp.MCPSpec{Type: warp.MCPTransport("carrier-pigeon")},
	}
	if _, _, err := mgr.buildConfig(def, nil, warp.MCPRenderOptions{}); err == nil {
		t.Fatal("expected an error for an unsupported transport")
	}
}

func TestEnvironmentExpansionOnlyForReferencedNames(t *testing.T) {
	t.Setenv("TASKSMCP_TEST_REFERENCED", "from-process")

	got := expandPlaceholders("${TASKSMCP_TEST_REFERENCED}-${TASKSMCP_TEST_UNSET}", map[string]string{})
	if !strings.Contains(got, "from-process") {
		t.Errorf("referenced variable must fall back to the process environment, got %q", got)
	}
	if !strings.Contains(got, "${TASKSMCP_TEST_UNSET}") {
		t.Errorf("unset variable must stay literal, got %q", got)
	}

	isolated := expandPlaceholders("${TASKSMCP_TEST_REFERENCED}", map[string]string{"TASKSMCP_TEST_REFERENCED": "from-workspace"})
	if isolated != "from-workspace" {
		t.Errorf("workspace environment must win, got %q", isolated)
	}
}

func TestExpandPlaceholdersEdgeCases(t *testing.T) {
	t.Setenv("TASKSMCP_TEST_EMPTY", "")

	cases := []struct {
		name string
		in   string
		env  map[string]string
		want string
	}{
		{"no placeholder", "plain value", nil, "plain value"},
		{"empty string", "", nil, ""},
		{"trailing dollar", "token$", map[string]string{}, "token$"},
		{"lone braced dollar", "a${b", nil, "a${b"},
		{"empty braced name", "a${}b", nil, "a${}b"},
		{"dollar escape", "cost $5", map[string]string{}, "cost $5"},
		{"bare name", "prefix-$TASKSMCP_TEST_BARE", map[string]string{"TASKSMCP_TEST_BARE": "x"}, "prefix-x"},
		{"bare name stops at punctuation", "$TASKSMCP_TEST_BARE.host", map[string]string{"TASKSMCP_TEST_BARE": "x"}, "x.host"},
		{"adjacent references", "${A}${B}", map[string]string{"A": "1", "B": "2"}, "12"},
		{"missing closing brace stays verbatim", "${TASKSMCP_TEST_UNSET", map[string]string{}, "${TASKSMCP_TEST_UNSET"},
		{"explicitly empty in workspace env", "v=${TASKSMCP_TEST_BLANK}", map[string]string{"TASKSMCP_TEST_BLANK": ""}, "v="},
		{"explicitly empty in process env", "v=${TASKSMCP_TEST_EMPTY}", map[string]string{}, "v="},
		{"hyphenated braced name", "${TASKSMCP-TEST-NAME}", map[string]string{"TASKSMCP-TEST-NAME": "y"}, "y"},
		{"value containing a dollar is not rescanned", "${A}", map[string]string{"A": "${B}"}, "${B}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandPlaceholders(tc.in, tc.env); got != tc.want {
				t.Errorf("expandPlaceholders(%q, %v) = %q, want %q", tc.in, tc.env, got, tc.want)
			}
		})
	}
}

func TestStaticAuthHeaders(t *testing.T) {
	t.Setenv("TASKSMCP_TEST_TOKEN", "tok-123")

	def := remoteDef("svc", "https://example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "bearer", Env: "TASKSMCP_TEST_TOKEN"}
		d.Spec.Headers = map[string]string{"X-Workspace": "${TASKSMCP_TEST_TOKEN}"}
	})
	headers, err := staticAuthHeaders(def, map[string]string{})
	if err != nil {
		t.Fatalf("staticAuthHeaders: %v", err)
	}
	if headers["Authorization"] != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want %q", headers["Authorization"], "Bearer tok-123")
	}
	if headers["X-Workspace"] != "tok-123" {
		t.Errorf("custom header = %q, want the expanded credential", headers["X-Workspace"])
	}

	def.Spec.Auth = &warp.ProviderAuth{Type: "api-key", Header: "X-Api-Key", Env: "TASKSMCP_TEST_TOKEN"}
	headers, err = staticAuthHeaders(def, map[string]string{})
	if err != nil {
		t.Fatalf("staticAuthHeaders (api-key): %v", err)
	}
	if headers["X-Api-Key"] != "tok-123" {
		t.Errorf("X-Api-Key = %q, want tok-123", headers["X-Api-Key"])
	}
}

func TestCredentialStorePersistsWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.json")
	store := NewCredentialStoreAt(path)
	key := serverKey("notes", "https://mcp.example.invalid/mcp")

	if _, ok := store.Get(key); ok {
		t.Error("an empty store must not report credentials")
	}

	entry := storedServer{ClientID: "client-1", ClientSecret: "secret-1", Issuer: "https://auth.example.invalid"}
	if err := store.Put(key, entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credential file mode = %o, want 600", info.Mode().Perm())
	}

	// A fresh store reads the same file, proving the token survived a daemon restart.
	reloaded := NewCredentialStoreAt(path)
	got, ok := reloaded.Get(key)
	if !ok {
		t.Fatal("credentials must survive a reload")
	}
	if got.ClientID != entry.ClientID || got.ClientSecret != entry.ClientSecret {
		t.Errorf("reloaded credentials = %+v, want %+v", got, entry)
	}

	if err := reloaded.Forget(key); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, ok := NewCredentialStoreAt(path).Get(key); ok {
		t.Error("Forget must remove the stored entry")
	}
}

func TestServerKeySeparatesEndpoints(t *testing.T) {
	first := serverKey("notes", "https://a.example.invalid/mcp")
	second := serverKey("notes", "https://b.example.invalid/mcp")
	if first == second {
		t.Error("different endpoints must not share credentials")
	}
	if first != serverKey("notes", "https://a.example.invalid/mcp") {
		t.Error("the same server must resolve to a stable key")
	}
}

// A workspace reload that changes the manifest (for example pointing the server at
// a different endpoint) must reconnect rather than keep the stale connection.
func TestSyncReconnectsWhenDefinitionChanges(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", "http://127.0.0.1:1/mcp", nil)
	mgr, defs, rec := newManager(t, def)

	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusError)

	defs.defs = []*warp.MCP{remoteDef("notes", ts.endpoint, nil)}
	mgr.Sync(context.Background())
	connected := waitForStatus(t, mgr, "notes", api.MCPStatusConnected)
	if len(connected.Tools) == 0 {
		t.Error("reconnecting after a definition change must re-discover tools")
	}
	if rec.count() == 0 {
		t.Error("the reconnected server must publish its tools")
	}
}

// A reload that only changes the workspace environment must still reach the next
// connection, since credentials and headers are expanded from it.
func TestSetRuntimeContextFeedsHeaderExpansion(t *testing.T) {
	def := remoteDef("svc", "https://example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Headers = map[string]string{"X-Api-Key": "${SVC_TOKEN}"}
	})
	mgr, _, _ := newManager(t, def)

	cfg, _, err := mgr.buildConfig(def, map[string]string{}, warp.MCPRenderOptions{})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Headers["X-Api-Key"] != "${SVC_TOKEN}" {
		t.Errorf("unresolved header = %q", cfg.Headers["X-Api-Key"])
	}

	mgr.SetRuntimeContext(map[string]string{"SVC_TOKEN": "tok-9"}, warp.MCPRenderOptions{})
	env, _ := mgr.runtimeContext()
	cfg, _, err = mgr.buildConfig(def, env, warp.MCPRenderOptions{})
	if err != nil {
		t.Fatalf("buildConfig after reload: %v", err)
	}
	if cfg.Headers["X-Api-Key"] != "tok-9" {
		t.Errorf("header after reload = %q, want tok-9", cfg.Headers["X-Api-Key"])
	}
}

func TestSyncUnregistersRemovedServer(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", ts.endpoint, nil)
	mgr, defs, rec := newManager(t, def)
	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	defs.defs = nil
	mgr.Sync(context.Background())

	if servers := mgr.ListMCPs(context.Background()); len(servers) != 0 {
		t.Errorf("a removed server must disappear, got %+v", servers)
	}
	cleared := false
	for _, call := range rec.snapshot() {
		if call.def != nil && call.def.Metadata.Name == "notes" && len(call.tools) == 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("removing a server must clear its registered tools")
	}
}

// The manager must ask the workspace to re-read manifests after editing a policy,
// otherwise the daemon keeps enforcing the old policy in memory.
func TestAllowPolicyItemReloadsWorkspace(t *testing.T) {
	ts := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.mcp.yaml")
	content := "---\napiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: notes\nspec:\n  type: stdio\n  command: [\"server\"]\n  policies:\n    tools:\n      exclude:\n        - delete_everything\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	def := remoteDef("notes", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Policies = &warp.MCPPolicies{Tools: &warp.ResourceFilter{Exclude: []string{"delete_everything"}}}
	})
	def.Path = path

	d := &definer{defs: []*warp.MCP{def}}
	reloads := 0
	mgr, err := New(Options{
		Definer: d,
		// A real reload re-reads manifests from disk, so the in-memory definition
		// loses the exclude entry the edit just removed.
		ReloadWorkspace: func(context.Context) error {
			reloads++
			def.Spec.Policies.Tools.Exclude = nil
			return nil
		},
		Credentials: NewCredentialStoreAt(filepath.Join(t.TempDir(), "credentials.json")),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	connected, err := mgr.AllowPolicyItem(context.Background(), "notes", api.MCPAllowPolicyRequest{
		Kind:  api.MCPAllowPolicyKindTool,
		Value: "delete_everything",
	})
	if err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	if reloads != 1 {
		t.Errorf("workspace reloads = %d, want 1", reloads)
	}
	if strings.Contains(readFile(t, path), "delete_everything") {
		t.Error("the exclude entry must be removed from the manifest")
	}
	// The returned state must reflect the reconnect, so callers see the newly
	// permitted tool as available.
	if connected.Status != api.MCPStatusConnected {
		t.Errorf("status = %q, want connected", connected.Status)
	}
	for _, tool := range connected.Tools {
		if tool.RawName == "delete_everything" && !tool.Allowed {
			t.Errorf("the permitted tool must be reported allowed: %+v", connected.Tools)
		}
	}
}

func TestPromptDescriptionAndResourceMIMEFromDiscovery(t *testing.T) {
	ts := newTestServer(t)
	def := remoteDef("notes", ts.endpoint, nil)
	mgr, _, _ := newManager(t, def)
	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	if got := mgr.promptDescription(def, "summary"); got != "Summarize a target" {
		t.Errorf("prompt description = %q, want %q", got, "Summarize a target")
	}
	if got := mgr.resourceMIMEType(def, "file:///notes/readme.md"); got != "text/markdown" {
		t.Errorf("resource mime = %q, want text/markdown", got)
	}
}

func TestCloseRejectsFurtherUse(t *testing.T) {
	ts := newTestServer(t)
	mgr, _, _ := newManager(t, remoteDef("notes", ts.endpoint, nil))
	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Errorf("Close must be idempotent, got %v", err)
	}
	if servers := mgr.ListMCPs(context.Background()); len(servers) != 0 {
		t.Errorf("a closed manager lists nothing, got %+v", servers)
	}
	// Sync after Close must be a no-op rather than reviving connections.
	mgr.Sync(context.Background())
	if servers := mgr.ListMCPs(context.Background()); len(servers) != 0 {
		t.Errorf("Sync after Close must not reconnect, got %+v", servers)
	}
}

func TestRawToolsCarryMaterializedNames(t *testing.T) {
	summaries := []api.MCPToolSummary{
		{Name: "notes__echo", RawName: "echo", Description: "Echo text back", InputSchema: map[string]any{"type": "object"}},
	}
	raw := rawTools(summaries)
	if len(raw) != 1 {
		t.Fatalf("expected 1 raw tool, got %d", len(raw))
	}
	if raw[0].Metadata.Name != "notes__echo" {
		t.Errorf("raw name = %q, want notes__echo", raw[0].Metadata.Name)
	}
	if raw[0].Spec.InputSchema["type"] != "object" {
		t.Errorf("input schema not carried: %+v", raw[0].Spec.InputSchema)
	}
}

func TestEffectiveAnnotationPrecedence(t *testing.T) {
	def := remoteDef("notes", "https://example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Annotations = &warp.ToolAnnotation{IsReadOnly: true, UserHint: "manifest default"}
		d.Spec.Overrides = map[string]warp.ToolAnnotation{
			"echo": {IsDangerous: true, UserHint: "override"},
		}
	})
	wire := &warp.ToolAnnotation{UserHint: "from server"}

	got := def.EffectiveAnnotation("echo", wire)
	if got == nil || got.UserHint != "override" || !got.IsDangerous {
		t.Errorf("override must win, got %+v", got)
	}

	got = def.EffectiveAnnotation("other", wire)
	if got == nil || got.UserHint != "manifest default" || !got.IsReadOnly {
		t.Errorf("manifest-wide annotation must beat the wire annotation, got %+v", got)
	}
}

// TestListChangeNotificationResyncs verifies a server that advertises listChanged
// re-syncs the workspace on its own: a tool the server removes mid-session stops
// being offered and stops being registered with the workspace, without a restart.
// It connects over SSE, the transport that carries server-to-client notifications.
func TestListChangeNotificationResyncs(t *testing.T) {
	ts := newTestServer(t)
	mgr, _, rec := newManager(t, remoteDef("notes", ts.sseEndpoint, func(d *warp.MCP) {
		d.Spec.Type = warp.MCPTransportSSE
	}))
	mgr.Sync(context.Background())
	connected := waitForStatus(t, mgr, "notes", api.MCPStatusConnected)
	if !toolNamed(connected.Tools, "notes__echo") {
		t.Fatalf("initial discovery missing echo: %+v", connected.Tools)
	}
	before := rec.count()

	ts.sdk.RemoveTools("echo")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		server := statusOf(mgr, "notes")
		if server != nil && !toolNamed(server.Tools, "notes__echo") && rec.count() > before {
			if toolNamedPublished(rec.last().tools, "notes__echo") {
				t.Fatalf("re-sync must re-register the reduced tool set: %+v", rec.last().tools)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	after := statusOf(mgr, "notes")
	if after == nil {
		t.Fatal("server disappeared from listings")
	}
	t.Errorf("removing a tool on the server must re-sync the workspace; tools still %v, publishes went from %d to %d",
		after.Tools, before, rec.count())
}

// toolNamed reports whether a discovered tool list carries the given name.
func toolNamed(tools []api.MCPToolSummary, name string) bool {
	return slices.ContainsFunc(tools, func(tool api.MCPToolSummary) bool { return tool.Name == name })
}

// toolNamedPublished reports whether a published raw tool list carries the name.
func toolNamedPublished(tools []*warp.Tool, name string) bool {
	return slices.ContainsFunc(tools, func(t *warp.Tool) bool { return t != nil && t.Metadata.Name == name })
}

// statusOf returns the current listing for one server, or nil when it is absent.
func statusOf(mgr *Manager, name string) *api.MCPServer {
	for _, server := range mgr.ListMCPs(context.Background()) {
		if server.Name == name {
			return &server
		}
	}
	return nil
}

// TestAuthorizeURLParksServerInAuthRequired verifies the callback wired into an
// OAuth provider parks the server in auth_required with its authorization URL,
// and that a connection failing while authorization is pending reports
// auth_required rather than a plain error, so callers can offer the login link.
func TestAuthorizeURLParksServerInAuthRequired(t *testing.T) {
	def := remoteDef("secure", "https://mcp.example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "oauth2"}
	})
	mgr, _, _ := newManager(t, def)

	cfg, provider, err := mgr.buildConfig(def, map[string]string{}, warp.MCPRenderOptions{})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if provider == nil {
		t.Fatal("an oauth2 server must attach an OAuth provider")
	}
	if cfg.Auth == nil {
		t.Error("the OAuth provider must be handed to the transport")
	}
	if provider.events.onAuthorize == nil {
		t.Fatal("the provider must report the authorization URL back to the manager")
	}

	provider.events.onAuthorize("https://auth.example.invalid/authorize?state=abc")

	state := mgr.state(def)
	status, authorizeURL := state.view()
	if status != api.MCPStatusAuthRequired {
		t.Errorf("status = %q, want %q", status, api.MCPStatusAuthRequired)
	}
	if authorizeURL != "https://auth.example.invalid/authorize?state=abc" {
		t.Errorf("authURL = %q, want the pending authorization URL", authorizeURL)
	}
	if got := mgr.failureStatus(state, errors.New("connect failed")); got != api.MCPStatusAuthRequired {
		t.Errorf("failure status with pending authorization = %q, want %q", got, api.MCPStatusAuthRequired)
	}
	// With no authorization pending, the same failure is a plain error.
	state.mu.Lock()
	state.authURL = ""
	state.mu.Unlock()
	if got := mgr.failureStatus(state, errors.New("connect failed")); got != api.MCPStatusError {
		t.Errorf("failure status without pending authorization = %q, want %q", got, api.MCPStatusError)
	}
}

// TestAuthenticateMCPRejectsNonOAuthServers verifies an interactive authorization
// request for a server that does not use one fails with an explanation instead of
// returning an empty authorization URL.
func TestAuthenticateMCPRejectsNonOAuthServers(t *testing.T) {
	mgr, _, _ := newManager(t, remoteDef("plain", "https://mcp.example.invalid/mcp", nil))

	_, err := mgr.AuthenticateMCP(context.Background(), "plain")
	if err == nil || !strings.Contains(err.Error(), "interactive authorization") {
		t.Errorf("expected an explanatory error for a non-OAuth server, got %v", err)
	}

	if _, err := mgr.AuthenticateMCP(context.Background(), "missing"); err == nil {
		t.Error("expected an error for an unknown server")
	}
}

func TestAllowPolicyItemOnConnectedServerWithAutoConnectAndSync(t *testing.T) {
	ts := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.mcp.yaml")
	content := "---\napiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: notes\nspec:\n  type: streamable-http\n  endpoint: " + ts.endpoint + "\n  policies:\n    tools:\n      exclude:\n        - delete_everything\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	def := remoteDef("notes", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Policies = &warp.MCPPolicies{Tools: &warp.ResourceFilter{Exclude: []string{"delete_everything"}}}
	})
	def.Path = path

	d := &definer{defs: []*warp.MCP{def}}
	rec := &recorder{}
	var mgr *Manager
	var err error
	mgr, err = New(Options{
		Definer: d,
		Syncer:  rec,
		ReloadWorkspace: func(ctx context.Context) error {
			def.Spec.Policies.Tools.Exclude = nil
			mgr.Sync(ctx)
			return nil
		},
		Credentials: NewCredentialStoreAt(filepath.Join(t.TempDir(), "credentials.json")),
		AutoConnect: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.Sync(context.Background())
	waitForStatus(t, mgr, "notes", api.MCPStatusConnected)

	updated, err := mgr.AllowPolicyItem(context.Background(), "notes", api.MCPAllowPolicyRequest{
		Kind:  api.MCPAllowPolicyKindTool,
		Value: "delete_everything",
	})
	if err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	if updated.Status != api.MCPStatusConnected {
		t.Errorf("status = %q, want connected", updated.Status)
	}
	for _, tool := range updated.Tools {
		if tool.RawName == "delete_everything" && !tool.Allowed {
			t.Errorf("delete_everything must be allowed after policy update: %+v", updated.Tools)
		}
	}
}

// TestStaticCredentialFailureReportsAuthRequired verifies a bearer server whose
// credential variable is unset parks in auth_required rather than error, with a
// message naming the variable, so both the /mcp listing and the CLI header can
// tell the user exactly what to supply.
func TestStaticCredentialFailureReportsAuthRequired(t *testing.T) {
	const envName = "TASKSMITH_MCP_TEST_UNSET_BEARER_TOKEN"
	t.Setenv(envName, "")

	ts := newTestServer(t)
	def := remoteDef("github", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "bearer", Env: envName}
	})
	mgr, _, _ := newManager(t, def)
	mgr.Sync(context.Background())

	server := waitForStatus(t, mgr, "github", api.MCPStatusAuthRequired)
	if !strings.Contains(server.Error, envName) {
		t.Errorf("error must name the credential variable: %q", server.Error)
	}
}

// TestAPIKeyCredentialFailureReportsAuthRequired covers the api-key scheme, which
// resolves its credential the same way but sends it in a custom header.
func TestAPIKeyCredentialFailureReportsAuthRequired(t *testing.T) {
	const envName = "TASKSMITH_MCP_TEST_UNSET_API_KEY"
	t.Setenv(envName, "")

	ts := newTestServer(t)
	def := remoteDef("search", ts.endpoint, func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "api-key", Header: "X-Api-Key", Env: envName}
	})
	mgr, _, _ := newManager(t, def)

	if _, err := mgr.RestartMCP(context.Background(), "search"); err == nil {
		t.Fatal("expected the connection to fail without a credential")
	}
	server := statusOf(mgr, "search")
	if server == nil {
		t.Fatal("server missing from listing")
	}
	if server.Status != api.MCPStatusAuthRequired {
		t.Errorf("status = %q, want %q", server.Status, api.MCPStatusAuthRequired)
	}
}

// TestUnauthorizedRemoteReportsAuthRequired verifies an HTTP 401 from a remote
// server is classified as needing authentication. The MCP SDK surfaces status
// codes as text, so this pins the reason recognition that makes it work.
func TestUnauthorizedRemoteReportsAuthRequired(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", code)
		}))

		def := remoteDef("secure", srv.URL, nil)
		mgr, _, _ := newManager(t, def)
		mgr.Sync(context.Background())

		want := api.MCPStatusAuthRequired
		server := waitForStatus(t, mgr, "secure", want)
		if server.Error == "" {
			t.Errorf("status %d: a rejected request must report why", code)
		}
		srv.Close()
	}
}

// TestStoredTokenRejectionReportsAuthRequired verifies a stored OAuth token the
// server rejects counts as needing authentication rather than a broken server.
func TestStoredTokenRejectionReportsAuthRequired(t *testing.T) {
	def := remoteDef("secure", "https://mcp.example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "oauth2"}
	})
	mgr, _, _ := newManager(t, def)
	state := mgr.state(def)

	handler := &staticTokenHandler{token: &oauth2.Token{AccessToken: "expired"}}
	err := handler.Authorize(context.Background(), httptest.NewRequest(http.MethodPost, "https://mcp.example.invalid/mcp", nil), nil)
	if err == nil {
		t.Fatal("a rejected stored credential must fail")
	}
	if got := mgr.failureStatus(state, err); got != api.MCPStatusAuthRequired {
		t.Errorf("failure status = %q, want %q", got, api.MCPStatusAuthRequired)
	}
}

// TestUnexpandedPlaceholderFailureReportsAuthRequired verifies a header whose
// variable resolves nowhere is reported as needing authentication once the
// connection fails, while the same server connecting successfully is unaffected.
func TestUnexpandedPlaceholderFailureReportsAuthRequired(t *testing.T) {
	def := remoteDef("headers", "http://127.0.0.1:1/mcp", func(d *warp.MCP) {
		d.Spec.Headers = map[string]string{"Authorization": "Bearer ${TASKSMITH_MCP_TEST_UNSET_HEADER_VAR}"}
	})
	mgr, _, _ := newManager(t, def)
	mgr.Sync(context.Background())

	waitForStatus(t, mgr, "headers", api.MCPStatusAuthRequired)
}

// TestPlainConnectionFailureStaysError guards against over-classifying: a server
// with no authentication whose endpoint is unreachable must stay an error, so the
// user is not sent looking for a credential that would not help.
func TestPlainConnectionFailureStaysError(t *testing.T) {
	def := remoteDef("down", "http://127.0.0.1:1/mcp", nil)
	mgr, _, _ := newManager(t, def)
	state := mgr.state(def)

	if got := mgr.failureStatus(state, errors.New(`connect MCP server "down": sending "initialize": dial tcp 127.0.0.1:1: connect: connection refused`)); got != api.MCPStatusError {
		t.Errorf("failure status = %q, want %q", got, api.MCPStatusError)
	}
}

// TestAuthenticateMCPExplainsStaticCredentials verifies asking a static-credential
// server to authorize interactively answers with the credential to supply and the
// restart to run, instead of only saying no browser flow exists.
func TestAuthenticateMCPExplainsStaticCredentials(t *testing.T) {
	def := remoteDef("github", "https://mcp.example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "bearer", Env: "GITHUB_TOKEN"}
	})
	mgr, _, _ := newManager(t, def)

	_, err := mgr.AuthenticateMCP(context.Background(), "github")
	if err == nil {
		t.Fatal("expected guidance for a server with no interactive flow")
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected a structured API error, got %T: %v", err, err)
	}
	if apiErr.Code != api.ErrCodeMCPAuthStatic {
		t.Errorf("code = %q, want %q", apiErr.Code, api.ErrCodeMCPAuthStatic)
	}
	for _, want := range []string{"GITHUB_TOKEN", "bearer", "/mcp restart github"} {
		if !strings.Contains(apiErr.Message, want) {
			t.Errorf("message %q must mention %q", apiErr.Message, want)
		}
	}
	if strings.Contains(strings.ToLower(apiErr.Message), "does not use") {
		t.Errorf("message should be actionable rather than a dead end: %q", apiErr.Message)
	}
}

// TestAuthenticateMCPFallsBackWithoutAuthConfig verifies a server with no
// authentication configured still explains that no interactive flow applies, since
// there is no credential the manifest could be waiting on.
func TestAuthenticateMCPFallsBackWithoutAuthConfig(t *testing.T) {
	mgr, _, _ := newManager(t, remoteDef("plain", "https://mcp.example.invalid/mcp", nil))

	_, err := mgr.AuthenticateMCP(context.Background(), "plain")
	if err == nil || !strings.Contains(err.Error(), "interactive authorization") {
		t.Errorf("expected an explanatory error for an unauthenticated server, got %v", err)
	}
}

// TestStaticAuthInstructionCoversCredentialShapes checks the guidance names the
// right source for each way a manifest can point at a credential.
func TestStaticAuthInstructionCoversCredentialShapes(t *testing.T) {
	cases := []struct {
		name string
		auth *warp.ProviderAuth
		want []string
	}{
		{
			name: "bearer env",
			auth: &warp.ProviderAuth{Type: "bearer", Env: "GITHUB_TOKEN"},
			want: []string{"GITHUB_TOKEN", "bearer", "/mcp restart github"},
		},
		{
			name: "basic file",
			auth: &warp.ProviderAuth{Type: "basic", File: "~/.config/creds"},
			want: []string{"credential file", "/mcp restart github"},
		},
		{
			name: "client credentials",
			auth: &warp.ProviderAuth{Type: "client-credentials", Env: "CLIENT_ID"},
			want: []string{"client credentials", "CLIENT_ID"},
		},
		{
			name: "no source configured",
			auth: &warp.ProviderAuth{Type: "bearer"},
			want: []string{"spec.auth.env", "spec.auth.file"},
		},
		{
			name: "enterprise",
			auth: &warp.ProviderAuth{Type: "enterprise"},
			want: []string{"enterprise", "oauth2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := remoteDef("github", "https://mcp.example.invalid/mcp", func(d *warp.MCP) {
				d.Spec.Auth = tc.auth
			})
			message, ok := staticAuthInstruction(def)
			if !ok {
				t.Fatal("expected actionable guidance")
			}
			for _, want := range tc.want {
				if !strings.Contains(message, want) {
					t.Errorf("message %q must mention %q", message, want)
				}
			}
		})
	}

	if _, ok := staticAuthInstruction(remoteDef("github", "https://x.invalid", nil)); ok {
		t.Error("a server with no spec.auth has no credential to name")
	}
}
