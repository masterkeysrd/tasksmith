package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// mcpFixture serves the daemon's MCP endpoints from memory so the CLI layer can
// be exercised without a running daemon.
type mcpFixture struct {
	t       *testing.T
	servers []api.MCPServer

	// authorizeThenConnect makes a listing report a server as connected after
	// authorization started, mirroring the OAuth callback landing.
	authorizeThenConnect bool

	// staticAuthServers maps a server name to the credential guidance the daemon
	// returns when an interactive authorization is requested for a server that has
	// no browser flow.
	staticAuthServers map[string]string

	// failPolicyAllow makes a manifest policy edit fail with this reason, mirroring
	// a daemon that refuses to rewrite a read-only manifest.
	failPolicyAllow string

	// failAuthCallback makes a pasted callback URL be rejected with this reason,
	// mirroring a daemon with no authorization in progress for the server.
	failAuthCallback string

	// failRestart makes a server restart fail with this reason, mirroring a server
	// whose handshake fails when reconnected.
	failRestart string

	// callbackConnects makes a listing report a server as connected only once a
	// callback URL has been submitted, mirroring a flow that completes solely
	// because the user pasted the redirect back.
	callbackConnects bool

	mu          sync.Mutex
	requests    []mcpRequest
	authStarted bool
	callbacks   int
}

type mcpRequest struct {
	method string
	path   string
	body   map[string]any
}

func newMCPFixture(t *testing.T, servers []api.MCPServer) *mcpFixture {
	t.Helper()
	f := &mcpFixture{t: t, servers: servers}
	c, err := client.New("/tmp/mcp-test.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: f}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	store.InitSession(context.Background(), c, store.InitSessionOptions{})
	store.SetDaemonStatus(store.DaemonConnected)
	// A pre-set session keeps a submitted turn from trying to create one.
	store.SetSession(&api.Session{ID: "sess-mcp", Config: api.SessionConfig{Mode: api.ModeDefault}})
	t.Cleanup(func() {
		// Drain background actions before tearing the store down, so a publish
		// cannot land after the reset.
		mcpBackground.Wait()
		store.ResetSession()
		store.ResetApp()
		store.ClearSlot()
		store.ResetStream()
		store.ResetTranscript()
	})
	return f
}

func (f *mcpFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet && req.URL.Path == "/v1/events" {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	body := map[string]any{}
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, mcpRequest{method: req.Method, path: req.URL.Path, body: body})
	servers := make([]api.MCPServer, len(f.servers))
	copy(servers, f.servers)
	authorized := f.authorizeThenConnect && f.authStarted
	// A flow that only a pasted callback can finish stays pending until one arrives,
	// which is what lets a test prove the paste is what connected the server.
	if f.callbackConnects && f.callbacks == 0 {
		authorized = false
	}
	f.mu.Unlock()

	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/workspace/mcps":
		if authorized {
			for i := range servers {
				servers[i].Status = api.MCPStatusConnected
				servers[i].Error = ""
			}
		}
		return mcpJSONResponse(http.StatusOK, servers)

	case strings.HasSuffix(req.URL.Path, "/auth/callback"):
		f.mu.Lock()
		f.callbacks++
		failure := f.failAuthCallback
		f.mu.Unlock()
		if failure != "" {
			return mcpJSONResponse(http.StatusBadRequest, api.APIError{Code: "bad_request", Message: failure})
		}
		// The daemon answers 204 with no body: the flow continues on its own, so
		// there is nothing to report yet beyond "the code reached the daemon".
		return mcpNoContentResponse()

	case strings.HasSuffix(req.URL.Path, "/restart"):
		name := mcpServerInPath(req.URL.Path)
		f.mu.Lock()
		failure := f.failRestart
		f.mu.Unlock()
		if failure != "" {
			return mcpJSONResponse(http.StatusBadGateway, api.APIError{Code: "bad_gateway", Message: failure})
		}
		for _, server := range servers {
			if server.Name == name {
				updated := server
				updated.Status = api.MCPStatusConnected
				updated.Error = ""
				return mcpJSONResponse(http.StatusOK, updated)
			}
		}
		return mcpJSONResponse(http.StatusNotFound, api.APIError{Code: "not_found", Message: "unknown MCP server " + name})

	case strings.HasSuffix(req.URL.Path, "/auth"):
		name := mcpServerInPath(req.URL.Path)
		f.mu.Lock()
		f.authStarted = true
		f.mu.Unlock()
		for _, server := range servers {
			if server.Name != name {
				continue
			}
			// Mirrors the daemon: only an interactive authorization scheme has a page
			// to open, and a server already authorized reports no URL.
			switch strings.ToLower(server.AuthType) {
			case "oauth2", "client_credentials", "enterprise":
				if server.Status == api.MCPStatusConnected {
					return mcpJSONResponse(http.StatusOK, api.MCPAuthResponse{Status: server.Status})
				}
				return mcpJSONResponse(http.StatusOK, api.MCPAuthResponse{
					AuthURL: "https://example.com/authorize?server=" + name,
					Status:  api.MCPStatusAuthRequired,
				})
			default:
				if instruction, ok := f.staticAuthInstruction(name); ok {
					return mcpJSONResponse(http.StatusBadRequest, api.APIError{
						Code:    api.ErrCodeMCPAuthStatic,
						Message: instruction,
					})
				}
				return mcpJSONResponse(http.StatusBadRequest, api.APIError{
					Code:    "bad_request",
					Message: fmt.Sprintf("MCP server %q does not use an interactive authorization flow", name),
				})
			}
		}
		return mcpJSONResponse(http.StatusNotFound, api.APIError{Code: "not_found", Message: "unknown MCP server " + name})

	case strings.Contains(req.URL.Path, "/prompts/"):
		prompt := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		if _, prohibited := body["allowProhibited"]; !prohibited && prompt == "danger" {
			return mcpJSONResponse(http.StatusForbidden, api.APIError{
				Code:    "mcp_policy_prohibited",
				Message: `MCP prompt "danger" is prohibited by spec.policies.prompts`,
			})
		}
		return mcpJSONResponse(http.StatusOK, api.MCPPromptResponse{
			Messages: []api.MCPPromptMessage{{Role: api.RoleUser, Text: "rendered prompt " + prompt}},
		})

	case strings.HasSuffix(req.URL.Path, "/resources"):
		uri, _ := body["uri"].(string)
		prohibited, _ := body["allowProhibited"].(bool)
		if !prohibited && uri == "notes://secret" {
			return mcpJSONResponse(http.StatusForbidden, api.APIError{
				Code:    "mcp_policy_prohibited",
				Message: `MCP resource "notes://secret" is prohibited by spec.policies.resources`,
			})
		}
		return mcpJSONResponse(http.StatusOK, api.MCPResourceResponse{
			URI:      uri,
			MIMEType: "text/markdown",
			Text:     "resource body for " + uri,
		})

	case req.Method == http.MethodPost && req.URL.Path == "/v1/workspace/reload":
		return mcpJSONResponse(http.StatusOK, map[string]any{"ok": true})

	case strings.HasSuffix(req.URL.Path, "/policies/allow"):
		name := mcpServerInPath(req.URL.Path)
		kind, _ := body["kind"].(string)
		value, _ := body["value"].(string)
		if reason := f.policyAllowFailure(); reason != "" {
			return mcpJSONResponse(http.StatusBadRequest, api.APIError{Code: "bad_request", Message: reason})
		}
		// Permitting an item rewrites the manifest, so the change has to persist in
		// the fixture: the CLI re-reads the listing afterwards and must see the item
		// allowed, exactly as it would against the daemon.
		found, updated := f.allowPolicy(name, kind, value)
		if !found {
			return mcpJSONResponse(http.StatusNotFound, api.APIError{Code: "not_found", Message: "unknown MCP server " + name})
		}
		return mcpJSONResponse(http.StatusOK, updated)
	}

	return mcpJSONResponse(http.StatusNotFound, api.APIError{Code: "not_found", Message: "unhandled path " + req.URL.Path})
}

// policyAllowFailure reports the configured failure reason for a manifest policy
// edit, or "" when the edit should succeed.
func (f *mcpFixture) policyAllowFailure() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failPolicyAllow
}

// allowPolicy marks one item allowed on the named server and reports whether that
// server exists, returning its updated listing.
func (f *mcpFixture) allowPolicy(name, kind, value string) (bool, api.MCPServer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].Name != name {
			continue
		}
		switch kind {
		case api.MCPAllowPolicyKindTool:
			for j := range f.servers[i].Tools {
				if f.servers[i].Tools[j].RawName == value || f.servers[i].Tools[j].Name == value {
					f.servers[i].Tools[j].Allowed = true
				}
			}
		case api.MCPAllowPolicyKindPrompt:
			for j := range f.servers[i].Prompts {
				if f.servers[i].Prompts[j].Name == value {
					f.servers[i].Prompts[j].Allowed = true
				}
			}
		case api.MCPAllowPolicyKindResource:
			for j := range f.servers[i].Resources {
				if f.servers[i].Resources[j].URI == value {
					f.servers[i].Resources[j].Allowed = true
				}
			}
		}
		return true, f.servers[i]
	}
	return false, api.MCPServer{}
}

// staticAuthInstruction returns the guidance configured for name, if any.
func (f *mcpFixture) staticAuthInstruction(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	instruction, ok := f.staticAuthServers[name]
	return instruction, ok
}

// mcpServerInPath extracts the server segment of /v1/workspace/mcps/<name>/...
func mcpServerInPath(path string) string {
	const prefix = "/v1/workspace/mcps/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	name, _, _ := strings.Cut(rest, "/")
	return name
}

// mcpNoContentResponse mirrors the daemon's answer to a callback submission: the
// code reached the daemon, and everything else is still in flight.
func mcpNoContentResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

func mcpJSONResponse(status int, payload any) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(raw))),
		Header:     make(http.Header),
	}, nil
}

func (f *mcpFixture) pathCalls(substring string) []mcpRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []mcpRequest
	for _, req := range f.requests {
		if strings.Contains(req.path, substring) {
			out = append(out, req)
		}
	}
	return out
}

// waitFor polls condition until it holds or the deadline passes, because the
// CLI issues daemon calls on goroutines.
func waitFor(t *testing.T, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
}

// waitForRender polls the rendered screen until it shows every want string or the
// deadline passes, for assertions about what a view shows after a background action
// changed the store.
func waitForRender(t *testing.T, env *testenv.Environment, want ...string) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var out string
	for time.Now().Before(deadline) {
		env.Flush()
		out = strings.TrimSpace(env.DumpText())
		if allContain(out, want) {
			return out, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return out, false
}

// allContain reports whether text holds every needle.
func allContain(text string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}

// mcpTestServers is one connected server with a prohibited prompt and resource,
// plus a second server awaiting authorization.
func mcpTestServers() []api.MCPServer {
	return []api.MCPServer{
		{
			Name:        "notes",
			DisplayName: "Notes",
			Description: "Project notes and specs",
			Scope:       "Workspace",
			Path:        ".agents/mcps/notes.mcp.yaml",
			Transport:   "stdio",
			Command:     []string{"npx", "-y", "@acme/notes-mcp"},
			AuthType:    "",
			Status:      api.MCPStatusConnected,
			Tools: []api.MCPToolSummary{
				{Name: "notes__echo", RawName: "echo", Description: "Echo text", Allowed: true, Annotations: api.ToolAnnotation{IsReadOnly: true}},
				{Name: "notes__purge", RawName: "purge", Description: "Delete notes", Allowed: false},
			},
			Prompts: []api.MCPPromptSummary{
				{Name: "summarize", Description: "Summarize a note", Allowed: true},
				{
					Name: "danger", Description: "Destructive action", Allowed: false,
					Arguments: []api.MCPPromptArgument{{Name: "target", Required: true, Description: "Note to purge"}},
				},
			},
			Resources: []api.MCPResourceSummary{
				{Name: "Spec", URI: "notes://specs/spec-1", Description: "Current spec", MIMEType: "text/markdown", Allowed: true},
				{Name: "Secret", URI: "notes://secret", MIMEType: "text/plain", Allowed: false},
			},
		},
		{
			Name:      "sentry",
			Scope:     "User",
			Path:      "sentry.mcp.yaml",
			Transport: "sse",
			Endpoint:  "https://mcp.example.test/sse",
			AuthType:  "oauth2",
			Status:    api.MCPStatusAuthRequired,
			AuthURL:   "https://example.com/authorize?server=sentry",
			Tools:     []api.MCPToolSummary{{Name: "sentry__search", RawName: "search", Allowed: true}},
		},
	}
}

// mcpToolDetailServer is a workspace-scoped server whose tools exercise the
// detail view: one allowed tool with a rich schema and safety annotations, and one
// prohibited tool on a manifest the CLI is allowed to rewrite.
func mcpToolDetailServer() api.MCPServer {
	return api.MCPServer{
		Name:      "middleman",
		Scope:     "Workspace",
		Path:      ".agents/mcps/middleman.mcp.yaml",
		Transport: "stdio",
		Command:   []string{"middleman", "mcp"},
		Status:    api.MCPStatusConnected,
		Tools: []api.MCPToolSummary{
			{
				Name:        "middleman__apply_rule_override",
				RawName:     "apply_rule_override",
				Description: "Create or update a mock rule, manifest, or fixture file inside an active workspace.",
				Allowed:     true,
				Annotations: api.ToolAnnotation{IsReadOnly: true},
				InputSchema: map[string]any{
					"required": []any{"workspace_id", "project", "path"},
					"properties": map[string]any{
						"workspace_id": map[string]any{"type": "string", "description": "Target workspace ID where the override is applied"},
						"project":      map[string]any{"type": "string", "description": "The mock project name"},
						"path":         map[string]any{"type": "string", "description": "Relative path of the rule or file"},
						"profile":      map[string]any{"type": "string", "description": "Optional profile name"},
					},
				},
			},
			{
				Name:        "middleman__drop_workspace",
				RawName:     "drop_workspace",
				Description: "Delete a workspace and everything under it.",
				Allowed:     false,
				Annotations: api.ToolAnnotation{IsDangerous: true},
			},
		},
	}
}

func TestMCPCommandRegistration(t *testing.T) {
	reg := NewRegistry()

	cmd, ok := reg.Lookup("mcp")
	if !ok || cmd.Name != "mcp" {
		t.Fatalf("expected /mcp to be registered, ok=%v cmd=%+v", ok, cmd)
	}
	if cmd.Usage != "/mcp [auth <name>|restart <name>]" {
		t.Errorf("unexpected usage: %q", cmd.Usage)
	}
	if cmd.Description == "" {
		t.Error("expected a non-empty description")
	}
	if cmd.Complete == nil {
		t.Error("expected non-nil Complete")
	}
	if alias, ok := reg.Lookup("/mcps"); !ok || alias.Name != "mcp" {
		t.Errorf("expected /mcps to resolve to /mcp, got %+v", alias)
	}
}

func TestMCPCommandArgs(t *testing.T) {
	store.ResetApp()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ClearSlot()

	reg := NewRegistry()

	// Bare /mcp opens the browser.
	if err := reg.Execute("/mcp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active := store.GetSlot().Active; active == nil || active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot, got %+v", active)
	}
	store.ClearSlot()

	if err := reg.Execute("/mcps"); err != nil {
		t.Fatalf("alias should work: %v", err)
	}
	store.ClearSlot()

	tests := []struct {
		line      string
		wantError string
	}{
		{line: "/mcp auth", wantError: "requires a server name"},
		{line: "/mcp restart", wantError: "requires a server name"},
		{line: "/mcp auth notes extra", wantError: "requires a server name"},
		{line: "/mcp bogus", wantError: "unknown /mcp subcommand"},
	}
	for _, tt := range tests {
		err := reg.Execute(tt.line)
		if err == nil || !strings.Contains(err.Error(), tt.wantError) {
			t.Errorf("Execute(%q) error = %v, want it to contain %q", tt.line, err, tt.wantError)
		}
	}

	// Naming a server is already the decision, so neither command asks again: auth
	// opens the browser on the authorization screen, and restart runs and reports in
	// the timeline without opening anything.
	store.ClearSlot()
	if err := reg.Execute("/mcp auth notes"); err != nil {
		t.Fatalf("Execute(\"/mcp auth notes\"): %v", err)
	}
	active := store.GetSlot().Active
	if active == nil {
		t.Fatal("Execute(\"/mcp auth notes\"): expected the browser slot")
	}
	if _, ok := active.Data.(func(string) trail.Node); !ok {
		t.Errorf("Execute(\"/mcp auth notes\"): expected the browser slot, got %T", active.Data)
	}
	if _, ok := active.Data.(components.PickerProps); ok {
		t.Error("authing a named server must not ask for confirmation first")
	}
	store.ClearSlot()

	if err := reg.Execute("/mcp restart notes"); err != nil {
		t.Fatalf("Execute(\"/mcp restart notes\"): %v", err)
	}
	if active := store.GetSlot().Active; active != nil {
		t.Errorf("Execute(\"/mcp restart notes\"): expected no slot, got %+v", active)
	}
	mcpBackground.Wait()
}

func TestMCPStatusLabelsAndIcons(t *testing.T) {
	tests := []struct {
		status   api.MCPStatus
		wantIcon string
		wantText string
	}{
		{status: api.MCPStatusConnected, wantIcon: "✔", wantText: "connected"},
		{status: api.MCPStatusAuthRequired, wantIcon: "⚠", wantText: "needs authentication"},
		{status: api.MCPStatusConnecting, wantIcon: "⟳", wantText: "connecting…"},
		{status: api.MCPStatusError, wantIcon: "✘", wantText: "failed to connect"},
		{status: api.MCPStatusDisconnected, wantIcon: "○", wantText: "disconnected"},
		{status: api.MCPStatus("weird"), wantIcon: "○", wantText: "disconnected"},
	}
	for _, tt := range tests {
		if got := mcpStatusIcon(tt.status); got != tt.wantIcon {
			t.Errorf("mcpStatusIcon(%q) = %q, want %q", tt.status, got, tt.wantIcon)
		}
		if got := mcpStatusLabel(tt.status); got != tt.wantText {
			t.Errorf("mcpStatusLabel(%q) = %q, want %q", tt.status, got, tt.wantText)
		}
	}

	scheme := theme.Get()
	colors := []struct {
		status api.MCPStatus
		want   any
	}{
		{status: api.MCPStatusConnected, want: scheme.Color.Text.Success},
		{status: api.MCPStatusAuthRequired, want: scheme.Color.Text.Warning},
		{status: api.MCPStatusConnecting, want: scheme.Color.Text.Info},
		{status: api.MCPStatusError, want: scheme.Color.Text.Error},
		{status: api.MCPStatusDisconnected, want: scheme.Color.Text.Muted},
	}
	for _, tt := range colors {
		got, ok := mcpStatusIconColor(scheme, tt.status).ForegroundOpt().Get()
		if !ok {
			t.Errorf("mcpStatusIconColor(%q) left the foreground unset", tt.status)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("mcpStatusIconColor(%q) foreground = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestMCPServerPresentation(t *testing.T) {
	connected, auth := mcpTestServers()[0], mcpTestServers()[1]

	if got := mcpScope(connected); got != "[Workspace]" {
		t.Errorf("mcpScope = %q, want [Workspace]", got)
	}
	if got := mcpScope(auth); got != "[User]" {
		t.Errorf("mcpScope = %q, want [User]", got)
	}
	if got := mcpScope(api.MCPServer{Namespace: "plugin"}); got != "[plugin]" {
		t.Errorf("mcpScope namespace fallback = %q, want [plugin]", got)
	}
	if got := mcpScope(api.MCPServer{}); got != "[Workspace]" {
		t.Errorf("mcpScope default = %q, want [Workspace]", got)
	}

	if got := mcpCapabilityKinds(connected); got != "tools · prompts · resources" {
		t.Errorf("mcpCapabilityKinds = %q", got)
	}
	if got := mcpCapabilityKinds(api.MCPServer{Tools: auth.Tools}); got != "tools" {
		t.Errorf("mcpCapabilityKinds tool-only = %q", got)
	}
	if got := mcpCapabilityKinds(api.MCPServer{}); got != "none" {
		t.Errorf("mcpCapabilityKinds empty = %q, want none", got)
	}

	wantCounts := "2 tools (1 allowed, 1 prohibited)"
	if got := mcpCountSummary(len(connected.Tools), countAllowedTools(connected.Tools), "tool", "tools"); got != wantCounts {
		t.Errorf("mcpCountSummary = %q, want %q", got, wantCounts)
	}
	if got := mcpCountSummary(1, 1, "prompt", "prompts"); got != "1 prompt" {
		t.Errorf("mcpCountSummary singular = %q, want \"1 prompt\"", got)
	}
	if got := mcpCountSummary(3, 3, "resource", "resources"); got != "3 resources" {
		t.Errorf("mcpCountSummary all allowed = %q, want \"3 resources\"", got)
	}

	if got := mcpTransport(api.MCPServer{}); got != "—" {
		t.Errorf("mcpTransport empty = %q, want em dash", got)
	}
}

func TestMCPCardFieldsForConnectedServer(t *testing.T) {
	scheme := theme.Get()
	connected := mcpTestServers()[0]

	fields := mcpOverviewFields(scheme, connected)
	got := map[string]string{}
	for _, field := range fields {
		got[field.Label] = field.Text
	}

	if got["Status"] != "✔ connected" {
		t.Errorf("Status = %q, want \"✔ connected\"", got["Status"])
	}
	if _, ok := got["Auth"]; ok {
		t.Errorf("a server with no authentication should not show an Auth row, got %q", got["Auth"])
	}
	if got["Command"] != "npx -y @acme/notes-mcp" {
		t.Errorf("Command = %q", got["Command"])
	}
	if _, ok := got["URL"]; ok {
		t.Errorf("a stdio server should not show a URL row, got %q", got["URL"])
	}
	if got["Transport"] != "stdio" {
		t.Errorf("Transport = %q", got["Transport"])
	}
	if want := "Capabilities"; got[want] != "tools · prompts · resources" {
		t.Errorf("Capabilities = %q", got[want])
	}
	if got["Tools"] != "2 tools (1 allowed, 1 prohibited)" {
		t.Errorf("Tools = %q", got["Tools"])
	}
	if got["Prompts"] != "2 prompts (1 allowed, 1 prohibited)" {
		t.Errorf("Prompts = %q", got["Prompts"])
	}
	if _, ok := got["Error"]; ok {
		t.Errorf("a server that connected should not show an Error row, got %q", got["Error"])
	}

	// Labels appear in the order the specification lists them.
	labels := make([]string, 0, len(fields))
	for _, field := range fields {
		labels = append(labels, field.Label)
	}
	wantOrder := []string{"Status", "Command", "Transport", "Config location", "Capabilities", "Tools", "Prompts", "Resources"}
	if !reflect.DeepEqual(labels, wantOrder) {
		t.Errorf("field order = %v, want %v", labels, wantOrder)
	}
}

func TestMCPCardFieldsForAuthRequiredServer(t *testing.T) {
	scheme := theme.Get()
	auth := mcpTestServers()[1]

	got := map[string]string{}
	for _, field := range mcpOverviewFields(scheme, auth) {
		got[field.Label] = field.Text
	}

	if got["Status"] != "⚠ needs authentication" {
		t.Errorf("Status = %q", got["Status"])
	}
	if got["Auth"] != "✘ not authenticated (oauth2)" {
		t.Errorf("Auth = %q", got["Auth"])
	}
	if got["URL"] != "https://mcp.example.test/sse" {
		t.Errorf("URL = %q", got["URL"])
	}
	if got["Authorize at"] != auth.AuthURL {
		t.Errorf("Authorize at = %q, want %q", got["Authorize at"], auth.AuthURL)
	}
	if got["Tools"] != "1 tool" {
		t.Errorf("Tools = %q, want \"1 tool\"", got["Tools"])
	}
	if _, ok := got["Command"]; ok {
		t.Errorf("an endpoint server should not show a Command row, got %q", got["Command"])
	}
}

func TestMCPCardFieldOrderingForFailures(t *testing.T) {
	scheme := theme.Get()
	home := t.TempDir()
	t.Setenv("HOME", home)
	failed := api.MCPServer{
		Name:     "broken",
		Path:     home + "/.config/tasksmith/agents/mcps/broken.mcp.yaml",
		Scope:    "User",
		Status:   api.MCPStatusError,
		Error:    `connect MCP server "broken": sending "initialize": rejected by transport: connection refused`,
		AuthType: "header",
	}

	got := map[string]string{}
	for _, field := range mcpOverviewFields(scheme, failed) {
		got[field.Label] = field.Text
	}

	if got["Status"] != "✘ failed to connect" {
		t.Errorf("Status = %q", got["Status"])
	}
	if got["Auth"] != "header" {
		t.Errorf("Auth = %q, want the bare scheme name", got["Auth"])
	}
	if got["Error"] != "connection refused" {
		t.Errorf("Error = %q, want the cleaned reason", got["Error"])
	}
	if got["Capabilities"] != "none" {
		t.Errorf("Capabilities = %q, want none", got["Capabilities"])
	}
	// The home directory is shortened so the row stays readable on a narrow
	// terminal, and the scope badge tells the user which file to edit.
	wantLocation := "~/.config/tasksmith/agents/mcps/broken.mcp.yaml [User]"
	if got["Config location"] != wantLocation {
		t.Errorf("Config location = %q, want %q", got["Config location"], wantLocation)
	}
}

func TestMCPCardLabelAlignment(t *testing.T) {
	scheme := theme.Get()
	connected := mcpTestServers()[0]

	out := testutils.Render(t, trail.Box(trail.BoxProps{}, mcpCardRows(scheme, connected, 100)...), 100, 40)

	// Every value must start in the same column: the card indent plus room for the
	// longest label, so the card reads as a table.
	wantColumn := mcpCardIndent + mcpCardLabelWidth
	rows := 0
	for _, line := range trimmedIndentLines(out) {
		col, ok := mcpRenderedValueColumn(line)
		if !ok {
			continue
		}
		rows++
		if col != wantColumn {
			t.Errorf("row %q starts its value at column %d, want %d\nfull card:\n%s", line, col, wantColumn, out)
		}
	}
	if rows < 4 {
		t.Fatalf("expected at least 4 aligned card rows, measured %d in:\n%s", rows, out)
	}
}

// mcpRenderedValueColumn reports the column a rendered card row's value starts
// in, and false for any line that is not a `Key: value` row.
func mcpRenderedValueColumn(line string) (int, bool) {
	if !strings.HasPrefix(line, strings.Repeat(" ", mcpCardIndent)) {
		return 0, false
	}
	body := strings.TrimPrefix(line, strings.Repeat(" ", mcpCardIndent))
	colon := strings.Index(body, ":")
	if colon <= 0 || strings.Contains(body[:colon], "  ") {
		return 0, false
	}
	value := body[colon+1:]
	trimmed := strings.TrimLeft(value, " ")
	if trimmed == "" {
		return 0, false
	}
	return mcpCardIndent + colon + 1 + (len(value) - len(trimmed)), true
}

// trimmedIndentLines drops blank lines from a rendered dump while keeping the
// leading indentation that alignment assertions measure.
func trimmedIndentLines(out string) []string {
	var lines []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// TestMCPWrapText keeps wrapped card values readable: words move down whole
// whenever they fit a line on their own, and only a word too long for any line is
// cut, since a clipped or split failure reason is useless to the reader.
func TestMCPWrapText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		text   string
		width  int
		indent int
		want   []string
	}{
		{
			name:  "fits on one line",
			text:  "stdio server",
			width: 40,
			want:  []string{"stdio server"},
		},
		{
			name:  "wraps on spaces",
			text:  "connect refused by the transport",
			width: 18,
			want:  []string{"connect refused by", "the transport"},
		},
		{
			name:  "moves a long word down whole",
			text:  "short read-only manifest",
			width: 14,
			want:  []string{"short", "read-only", "manifest"},
		},
		{
			name:  "cuts a word too long for any line",
			text:  "supercalifragilistic",
			width: 12,
			want:  []string{"supercalifra", "gilistic"},
		},
		{
			name:   "indents continuation lines",
			text:   "alpha beta gamma",
			width:  12,
			indent: 4,
			want:   []string{"alpha beta", "    gamma"},
		},
		{
			name:  "narrow terminals still get a readable width",
			text:  "first second",
			width: 4,
			want:  []string{"first second"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := mcpWrapText(tt.text, tt.width, tt.indent)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mcpWrapText(%q, %d, %d) = %q, want %q", tt.text, tt.width, tt.indent, got, tt.want)
			}
			// A terminal narrower than the wrapper's floor is widened rather than
			// squeezed, so lines are measured against the widened width.
			effective := max(tt.width, 12)
			for _, line := range got {
				if len([]rune(line)) > effective {
					t.Errorf("line %q exceeds the width %d", line, effective)
				}
			}
		})
	}
}

// TestMCPOverviewTracksLiveListing guards the pane that shows the server card: the
// listing refreshes while the card is open, and the card must report what the daemon
// now says rather than what it said when the card opened.
func TestMCPOverviewTracksLiveListing(t *testing.T) {
	fixture := newMCPFixture(t, []api.MCPServer{mcpToolDetailServer()})

	env := testenv.Default(100, 40)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-live-overview"}), container)
	env.Flush()

	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()
	if out := env.DumpText(); !strings.Contains(out, "2 tools (1 allowed, 1 prohibited)") {
		t.Fatalf("overview did not open with the prohibited tool counted:\n%s", strings.TrimSpace(out))
	}

	fixture.mu.Lock()
	servers := append([]api.MCPServer(nil), fixture.servers...)
	for i := range servers {
		servers[i].Tools = append([]api.MCPToolSummary(nil), servers[i].Tools...)
		for j := range servers[i].Tools {
			servers[i].Tools[j].Allowed = true
		}
	}
	fixture.servers = servers
	fixture.mu.Unlock()
	if _, err := refreshMCPServers(t.Context()); err != nil {
		t.Fatalf("refreshMCPServers: %v", err)
	}

	out, ok := waitForRender(t, env, "Tools:            2 tools")
	if !ok {
		t.Errorf("overview kept reporting a prohibited tool after the listing refreshed:\n%s", out)
	}
	if strings.Contains(out, "prohibited") {
		t.Errorf("overview still reports a prohibited tool:\n%s", out)
	}
}

func TestMCPItemBadges(t *testing.T) {
	if got := mcpItemBadge(true); got != "[Allowed]" {
		t.Errorf("mcpItemBadge(true) = %q", got)
	}
	if got := mcpItemBadge(false); got != "[Prohibited]" {
		t.Errorf("mcpItemBadge(false) = %q", got)
	}
	tool := api.MCPToolSummary{Allowed: true, Annotations: api.ToolAnnotation{IsReadOnly: true, IsOpenWorld: true}}
	want := "[Allowed] [ReadOnly] [OpenWorld]"
	if got := mcpToolBadge(tool); got != want {
		t.Errorf("mcpToolBadge = %q, want %q", got, want)
	}
	if got := mcpToolBadge(api.MCPToolSummary{Allowed: false}); got != "[Prohibited]" {
		t.Errorf("mcpToolBadge prohibited = %q", got)
	}
}

func TestMCPSlotValueRoundTrip(t *testing.T) {
	servers := mcpTestServers()
	target, ok := mcpTargetFor(servers, "notes", mcpTargetPrompt, "summarize")
	if !ok {
		t.Fatal("expected to resolve prompt summarize")
	}
	if got, want := target.ref(), "/mcp:notes:summarize"; got != want {
		t.Errorf("ref = %q, want %q", got, want)
	}
	serverName, kind, item, ok := parseMCPSlotValue(target.slotValue())
	if !ok || serverName != "notes" || kind != mcpTargetPrompt || item != "summarize" {
		t.Fatalf("round trip prompt failed: %q %q %q %v", serverName, kind, item, ok)
	}

	resource, ok := mcpTargetFor(servers, "notes", mcpTargetResource, "notes://specs/spec-1")
	if !ok {
		t.Fatal("expected to resolve resource")
	}
	if got, want := resource.ref(), "@mcp:notes:notes://specs/spec-1"; got != want {
		t.Errorf("resource ref = %q, want %q", got, want)
	}
	serverName, kind, item, ok = parseMCPSlotValue(resource.slotValue())
	if !ok || serverName != "notes" || kind != mcpTargetResource || item != "notes://specs/spec-1" {
		t.Fatalf("round trip resource failed: %q %q %q %v", serverName, kind, item, ok)
	}
	if got := resource.label(); got != "Spec" {
		t.Errorf("label = %q, want Spec", got)
	}

	for _, bad := range []string{"", "prompt", "prompt:notes", "tool:notes:x", "prompt:notes:"} {
		if _, _, _, ok := parseMCPSlotValue(bad); ok {
			t.Errorf("parseMCPSlotValue(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestParseMCPRef(t *testing.T) {
	tests := []struct {
		text   string
		server string
		kind   mcpTargetKind
		item   string
		ok     bool
		rest   string
	}{
		{text: "/mcp:notes:summarize", server: "notes", kind: mcpTargetPrompt, item: "summarize", ok: true},
		{text: "@mcp:notes:notes://specs/spec-1", server: "notes", kind: mcpTargetResource, item: "notes://specs/spec-1", ok: true},
		{text: "notes:summarize", ok: false},
		{text: "/mcp:", ok: false},
		{text: "/mcp:notes", ok: false},
		{text: "/mcp:notes:", ok: false},
	}
	for _, tt := range tests {
		server, kind, item, rest, ok := parseMCPRef(tt.text)
		if ok != tt.ok {
			t.Errorf("parseMCPRef(%q) ok = %v, want %v", tt.text, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if server != tt.server || kind != tt.kind || item != tt.item {
			t.Errorf("parseMCPRef(%q) = %q/%q/%q, want %q/%q/%q", tt.text, server, kind, item, tt.server, tt.kind, tt.item)
		}
		if rest != tt.rest {
			t.Errorf("parseMCPRef(%q) remainder = %q, want %q", tt.text, rest, tt.rest)
		}
	}
}

func TestFindMCPRefs(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{text: "/mcp:notes:summarize", want: 1},
		{text: "please /mcp:notes:summarize now", want: 1},
		{text: "read @mcp:notes:notes://specs/spec-1 and summarize", want: 1},
		{text: "/mcp:notes:summarize then /mcp:notes:other", want: 2},
		{text: "no references here", want: 0},
		{text: "/mcp", want: 0},
		{text: "/mcp: ", want: 0},
		// A reference must start at a token boundary, so an embedded mention in
		// the middle of a word is prose.
		{text: "seeX/mcp:notes:summarize", want: 0},
	}
	for _, tt := range tests {
		if got := len(findMCPRefs(tt.text)); got != tt.want {
			t.Errorf("findMCPRefs(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestResolveMCPSubmission(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())

	t.Run("allowed prompt", func(t *testing.T) {
		target, remainder, referenced, err := resolveMCPSubmission("/mcp:notes:summarize")
		if err != nil || !referenced {
			t.Fatalf("referenced=%v err=%v", referenced, err)
		}
		if !target.allowed || target.name != "summarize" {
			t.Fatalf("target = %+v", target)
		}
		if remainder != "" {
			t.Errorf("remainder = %q, want empty", remainder)
		}
	})

	t.Run("prohibited resource", func(t *testing.T) {
		target, remainder, referenced, err := resolveMCPSubmission("explain @mcp:notes:notes://secret please")
		if err != nil || !referenced {
			t.Fatalf("referenced=%v err=%v", referenced, err)
		}
		if target.allowed {
			t.Fatalf("expected the secret resource to be prohibited")
		}
		if remainder != "explain please" {
			t.Errorf("remainder = %q, want \"explain please\"", remainder)
		}
	})

	t.Run("unknown item on known server is an error", func(t *testing.T) {
		_, _, referenced, err := resolveMCPSubmission("/mcp:notes:missing")
		if !referenced || err == nil {
			t.Fatalf("expected an error, referenced=%v err=%v", referenced, err)
		}
		if !strings.Contains(err.Error(), "was not found on server") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unknown server is prose", func(t *testing.T) {
		_, _, referenced, err := resolveMCPSubmission("/mcp:ghost:thing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if referenced {
			t.Error("expected an unknown server not to be treated as a reference")
		}
	})

	t.Run("two references is ambiguous", func(t *testing.T) {
		_, _, referenced, err := resolveMCPSubmission("/mcp:notes:summarize @mcp:notes:notes://secret")
		if !referenced || err == nil || !strings.Contains(err.Error(), "one MCP reference at a time") {
			t.Fatalf("expected an ambiguity error, referenced=%v err=%v", referenced, err)
		}
	})

	t.Run("ordinary message", func(t *testing.T) {
		_, _, referenced, err := resolveMCPSubmission("summarize the auth handler")
		if err != nil || referenced {
			t.Fatalf("referenced=%v err=%v", referenced, err)
		}
	})
}

func TestMCPProhibitedMessageAndEnableReason(t *testing.T) {
	servers := mcpTestServers()

	prompt, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")
	msg := mcpProhibitedMessage(prompt)
	for _, want := range []string{"MCP prompt 'danger'", "spec.policies.prompts", ".agents/mcps/notes.mcp.yaml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}

	resource, _ := mcpTargetFor(servers, "notes", mcpTargetResource, "notes://secret")
	if msg := mcpProhibitedMessage(resource); !strings.Contains(msg, "spec.policies.resources") {
		t.Errorf("resource message %q missing policy section", msg)
	}

	// A workspace manifest can be rewritten; a user-scope manifest cannot.
	if reason := mcpEnableManifestReason(prompt.server); reason != "" {
		t.Errorf("expected workspace manifest to be editable, got %q", reason)
	}
	if reason := mcpEnableManifestReason(api.MCPServer{}); reason == "" {
		t.Error("expected a reason when the manifest path is unknown")
	}
	userScope := prompt.server
	userScope.Scope = "User"
	if reason := mcpEnableManifestReason(userScope); !strings.Contains(reason, "User") {
		t.Errorf("expected the scope to be named, got %q", reason)
	}
	// A project-scoped manifest sits next to the repository, so it is editable too.
	projectScope := prompt.server
	projectScope.Scope = "Project"
	if reason := mcpEnableManifestReason(projectScope); reason != "" {
		t.Errorf("expected a project manifest to be editable, got %q", reason)
	}
}

func TestParseMCPPromptArgs(t *testing.T) {
	servers := mcpTestServers()
	single, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")
	plain, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "summarize")

	if args, err := parseMCPPromptArgs(single, ""); err != nil || args != nil {
		t.Errorf("empty args = %+v/%v, want nil/nil", args, err)
	}
	args, err := parseMCPPromptArgs(single, "draft.md")
	if err != nil {
		t.Fatalf("bare value: %v", err)
	}
	if args["target"] != "draft.md" {
		t.Errorf("bare value mapped to %v, want target=draft.md", args)
	}
	args, err = parseMCPPromptArgs(single, `{"target": "a.md"}`)
	if err != nil || args["target"] != "a.md" {
		t.Errorf("json args = %+v, err=%v", args, err)
	}
	if _, err := parseMCPPromptArgs(single, `{"target": 3}`); err == nil {
		t.Error("expected a non-string value to be rejected")
	}
	if _, err := parseMCPPromptArgs(plain, "bare value with no argument"); err == nil {
		t.Error("expected free text to be rejected when the prompt declares no arguments")
	}

	if got := mcpPromptNeedsArgs(plain); got {
		t.Error("summarize declares no arguments")
	}
	if !mcpPromptNeedsArgs(single) {
		t.Error("danger declares a required argument")
	}
	if got := mcpPromptArgsTemplate(single); got != `{"target": ""}` {
		t.Errorf("template = %q", got)
	}
	if got := mcpPromptArgsDescription(single); !strings.Contains(got, "target (required)") {
		t.Errorf("description = %q, want it to name the required argument", got)
	}
}

func TestMCPPromptCompleters(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	prompts, err := completeMCPPrompts(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("completeMCPPrompts: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompt candidates, got %d", len(prompts))
	}
	var prohibited, allowed autocomplete.Candidate
	for _, candidate := range prompts {
		switch candidate.Value {
		case "notes:summarize":
			allowed = candidate
		case "notes:danger":
			prohibited = candidate
		}
	}
	if prohibited.Label != "notes:danger [Prohibited]" {
		t.Errorf("prohibited label = %q, want it labeled", prohibited.Label)
	}
	// The trigger is consumed by the engine, so the insert text must not repeat it.
	if allowed.InsertText != "notes:summarize " {
		t.Errorf("insert text = %q, want \"notes:summarize \"", allowed.InsertText)
	}
	if _, ok := allowed.Data.(mcpTarget); !ok {
		t.Errorf("expected candidate data to carry the target, got %T", allowed.Data)
	}

	resources, err := completeMCPResources(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("completeMCPResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resource candidates, got %d", len(resources))
	}
	found := false
	for _, candidate := range resources {
		if strings.HasSuffix(candidate.Label, "notes://secret [Prohibited]") {
			found = true
			if !strings.HasPrefix(candidate.InsertText, "notes:notes://secret") {
				t.Errorf("unexpected insert text %q", candidate.InsertText)
			}
		}
	}
	if !found {
		t.Error("expected the prohibited resource to be listed and labeled")
	}
}

func TestMCPServerNameCompletion(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	subs, err := completeMCPSubcommands(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("completeMCPSubcommands: %v", err)
	}
	if len(subs) != 2 || subs[0].Value != "auth" || subs[1].Value != "restart" {
		t.Fatalf("unexpected subcommand candidates: %+v", subs)
	}

	names, err := completeMCPServerNames(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("completeMCPServerNames: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 server candidates, got %d", len(names))
	}
	for _, candidate := range names {
		if candidate.Value == "sentry" && !strings.Contains(candidate.Description, "needs authentication") {
			t.Errorf("expected the status in the description, got %q", candidate.Description)
		}
	}

	// The subcommand sources must be registered so "/mcp auth <name>" completes.
	reg := NewRegistry()
	engine := autocomplete.New()
	for _, source := range reg.Sources() {
		if err := engine.Register(source); err != nil {
			t.Fatalf("register source %q: %v", source.Name, err)
		}
	}
	result, err := engine.Complete(context.Background(), "/mcp auth ", len("/mcp auth "))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.Source != "commands:mcp-auth" {
		t.Fatalf("expected the server-name source to match, got %q", result.Source)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 server candidates, got %d", len(result.Candidates))
	}
}

func TestMCPPromptCompletionInsertsReference(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	engine := autocomplete.New()
	if err := engine.Register(autocomplete.Source{Name: "mcp-prompts", Trigger: "/mcp:", Complete: completeMCPPrompts}); err != nil {
		t.Fatalf("register: %v", err)
	}
	input := "run /mcp:"
	result, err := engine.Complete(context.Background(), input, len(input))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.Source != "mcp-prompts" {
		t.Fatalf("expected the prompt source, got %q", result.Source)
	}
	line, cursor, err := result.Apply(0)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !strings.HasPrefix(line, "run /mcp:notes:") {
		t.Fatalf("completed line = %q, want it to start with \"run /mcp:notes:\"", line)
	}
	if strings.Contains(line, "/mcp:/mcp:") {
		t.Fatalf("completed line duplicates the trigger: %q", line)
	}
	if cursor != len(line) {
		t.Errorf("cursor = %d, want %d", cursor, len(line))
	}
}

func TestMCPBrowserRendersListAndInspector(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	out := testutils.Render(t, mcpBrowser(mcpBrowserProps{slotID: "slot-list"}), 100, 40)
	for _, want := range []string{
		"Manage MCP servers",
		"2 servers",
		// Scope headings name the directory each tier's manifests live in.
		"Workspace MCPs (.agents/mcps)",
		"notes",
		"sentry",
		// Status is an icon plus a plain-language label rather than a bracketed badge.
		"✔",
		"⚠",
		"2 tools (1 allowed) · 2 prompts (1 allowed) · 2 resources (1 allowed)",
		"needs authentication",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	// The scope badge column is gone, so a row must not repeat the tier name in
	// brackets now that the heading carries it.
	for _, unwanted := range []string{"[Workspace]", "[User]", "[Connected]", "[Auth Required]"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("list output still renders the old column badge %q:\n%s", unwanted, out)
		}
	}
}

// TestMCPBrowserServerListHasNoFilterInput keeps the server list's advertised
// keys usable. A filter input swallows printable keys, so typing a server name
// would instead fire whatever letter actions the screen advertises.
func TestMCPBrowserServerListHasNoFilterInput(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	out := testutils.Render(t, mcpBrowser(mcpBrowserProps{slotID: "slot-no-filter"}), 100, 40)
	if strings.Contains(out, "Filter:") {
		t.Errorf("server list must not offer a filter input, it steals the action keys:\n%s", out)
	}
	for _, want := range []string{"a Auth", "r Restart", "R Reload"} {
		if !strings.Contains(out, want) {
			t.Errorf("list should advertise %q so the key is discoverable:\n%s", want, out)
		}
	}
}

// TestMCPBrowserBindingsFitNarrowTerminal proves every advertised key is readable
// at the narrowest width the app supports; the hint row does not wrap, so a long
// label silently truncates the last binding.
func TestMCPBrowserBindingsFitNarrowTerminal(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	const width = 72
	out := testutils.Render(t, mcpBrowser(mcpBrowserProps{slotID: "slot-hint-width"}), width, 40)
	var hint string
	for line := range strings.SplitSeq(out, "\n") {
		if trimmed := strings.TrimRight(line, " "); strings.HasPrefix(trimmed, "Keyboard:") {
			hint = trimmed
		}
	}
	if hint == "" {
		t.Fatalf("no keyboard hint row rendered:\n%s", out)
	}
	if !strings.HasSuffix(hint, "esc Close") {
		t.Errorf("keyboard hint was truncated at width %d, got %q", width, hint)
	}
	if runes := len([]rune(hint)); runes > width {
		t.Errorf("keyboard hint is %d cells wide, wants at most %d: %q", runes, width, hint)
	}
}

// TestMCPBrowserGroupsServersByScope checks the listing leads with the scope
// closest to the repository and that each server lands under its own heading.
func TestMCPBrowserGroupsServersByScope(t *testing.T) {
	newMCPFixture(t, []api.MCPServer{
		{Name: "zeta", Scope: "User", Path: "zeta.mcp.yaml", Status: api.MCPStatusConnected},
		{Name: "notes", Scope: "Workspace", Path: ".agents/mcps/notes.mcp.yaml", Status: api.MCPStatusConnected},
		{Name: "baked", Scope: "Builtin", Status: api.MCPStatusDisconnected},
		{Name: "api-local", Scope: "Project", Path: "packages/api/.agents/mcps/api-local.mcp.yaml", Status: api.MCPStatusConnected},
	})

	out := testutils.Render(t, mcpBrowser(mcpBrowserProps{slotID: "slot-groups"}), 100, 40)
	order := []string{
		"Project MCPs (packages/api/.agents/mcps)", "api-local",
		"Workspace MCPs (.agents/mcps)", "notes",
		"User MCPs", "zeta",
		"Builtin MCPs", "baked",
	}
	previous := -1
	for _, want := range order {
		at := strings.Index(out, want)
		if at < 0 {
			t.Fatalf("list output missing %q:\n%s", want, out)
		}
		if at <= previous {
			t.Errorf("%q appeared before the previous entry, so the listing is out of order:\n%s", want, out)
		}
		previous = at
	}
}

// TestMCPBrowserNoticeReplacesSubtitleTitle keeps the title clean while an in-flight
// action reports progress alongside the server count in the subtitle.
func TestMCPBrowserNoticeReplacesSubtitleTitle(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	out := testutils.Render(t, mcpBrowser(mcpBrowserProps{
		slotID:        "slot-notice",
		initialNotice: "Restarting notes…",
	}), 100, 40)
	for _, want := range []string{"Manage MCP servers", "2 servers · Restarting notes…"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Manage MCP servers —") {
		t.Errorf("title should stay clean without appending notice:\n%s", out)
	}
}

func TestMCPBrowserEmptyWorkspace(t *testing.T) {
	newMCPFixture(t, nil)

	out := renderTestNode(t, mcpBrowser(mcpBrowserProps{slotID: "slot-empty"}))
	if !strings.Contains(out, "No MCP servers configured") {
		t.Fatalf("expected the empty notice, got:\n%s", out)
	}
}

func TestMCPInspectorShowsProhibitedItems(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())

	servers := mcpTestServers()
	out := testutils.Render(t, mcpInspector(mcpInspectorProps{server: servers[0]}), 100, 40)
	for _, want := range []string{
		"notes MCP Server",
		"Status:           ✔ connected",
		".agents/mcps/notes.mcp.yaml [Workspace]",
		"Transport:        stdio",
		"Command:          npx -y @acme/notes-mcp",
		"Capabilities:     tools · prompts · resources",
		"Tools:            2 tools (1 allowed, 1 prohibited)",
		"❯ 1. View tools (2)",
		"  2. Run prompt (2)",
		"  3. Attach resource (2)",
		"  4. Reconnect",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("inspector output missing %q:\n%s", want, out)
		}
	}
	// The card explains what is discoverable; the items themselves live behind the
	// menu, so a prohibited item's name must not leak onto the overview.
	for _, unwanted := range []string{"purge", "notes://specs/spec-1", "[Prohibited]", "[ReadOnly]"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("overview should not list discovered items inline, found %q:\n%s", unwanted, out)
		}
	}
	// A server with no authorization pending must not advertise a URL.
	if strings.Contains(out, "Authorize at:") {
		t.Errorf("connected server with no pending authorization should not show an authorize URL:\n%s", out)
	}
	// The sentry server is pending authorization with an OAuth type.
	out = testutils.Render(t, mcpInspector(mcpInspectorProps{server: servers[1]}), 100, 40)
	for _, want := range []string{
		"Auth:             ✘ not authenticated (oauth2)",
		"URL:              https://mcp.example.test/sse",
		"Authorize at:     https://example.com/authorize?server=sentry",
		"❯ 1. Authenticate",
		"  2. Reconnect",
		"  3. Back to servers",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("auth-pending overview missing %q:\n%s", want, out)
		}
	}
	// A server awaiting authorization leads with the action that fixes it, so the
	// drill-down entries must not appear above it.
	for _, unwanted := range []string{"View tools", "Run prompt", "Attach resource"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("auth-pending overview should not offer %q:\n%s", unwanted, out)
		}
	}
}

func TestMCPInspectorNotConnectedNotice(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	out := renderTestNode(t, mcpInspector(mcpInspectorProps{server: api.MCPServer{
		Name:      "ghost",
		Status:    api.MCPStatusError,
		Transport: "stdio",
		Error:     "connect refused",
	}}))
	for _, want := range []string{"✘ failed to connect", "Error:            connect refused", "Capabilities:     none"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Nothing was discovered, so the only thing the user can do is try again.
	if !strings.Contains(out, "❯ 1. Reconnect") {
		t.Errorf("failed server should offer Reconnect as its only action:\n%s", out)
	}
}

// TestMCPInspectorNavigation drives every advertised overview shortcut. The
// notes server declares no OAuth scheme, so `a` must do nothing there: an
// overview never advertises or honors a key for an action the server cannot do.
func TestMCPInspectorNavigation(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())

	servers := mcpTestServers()
	var back, tools, prompts, resources, auth, restart int
	env := testenv.Default(100, 30)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpInspector(mcpInspectorProps{
		server:      servers[0],
		onBack:      func() { back++ },
		onTools:     func() { tools++ },
		onPrompts:   func() { prompts++ },
		onResources: func() { resources++ },
		onAuth:      func() { auth++ },
		onRestart:   func() { restart++ },
	}), container)
	env.Flush()

	for _, press := range []struct {
		name string
		code rune
		text string
	}{
		{name: "prompts hotkey", code: 'p', text: "p"},
		{name: "resources hotkey", code: 'u', text: "u"},
		{name: "auth hotkey on a server with no OAuth scheme", code: 'a', text: "a"},
		{name: "reconnect hotkey", code: 'r', text: "r"},
		{name: "escape", code: key.KeyEscape},
	} {
		env.SendKey(key.Key{Code: press.code, Text: press.text})
		env.Flush()
	}
	if prompts != 1 || resources != 1 || restart != 1 || back != 1 {
		t.Fatalf("hotkeys fired prompts=%d resources=%d restart=%d back=%d, want 1 each",
			prompts, resources, restart, back)
	}
	if auth != 0 {
		t.Errorf("auth fired %d times on a server with no OAuth scheme, want 0", auth)
	}
}

// TestMCPInspectorMenuSelectsByDigitAndEnter proves the numbered menu is the
// primary way to act on a server: a digit runs the entry printed beside it, and
// the arrows plus Enter reach the same place.
func TestMCPInspectorMenuSelectsByDigitAndEnter(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())

	servers := mcpTestServers()
	var back, tools, prompts, resources, restart int
	env := testenv.Default(100, 30)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpInspector(mcpInspectorProps{
		server:      servers[0],
		onBack:      func() { back++ },
		onTools:     func() { tools++ },
		onPrompts:   func() { prompts++ },
		onResources: func() { resources++ },
		onRestart:   func() { restart++ },
	}), container)
	env.Flush()

	// The menu reads: 1. View tools, 2. Run prompt, 3. Attach resource,
	// 4. Reconnect.
	env.SendKey(key.Key{Code: '3', Text: "3"})
	env.Flush()
	if resources != 1 || tools != 0 || prompts != 0 || restart != 0 {
		t.Fatalf("pressing 3 fired tools=%d prompts=%d resources=%d restart=%d, want only resources",
			tools, prompts, resources, restart)
	}

	// Selecting an entry also moves the cursor there, so one up lands on
	// "Run prompt" and two ups land on "View tools", which Enter must run.
	env.SendKey(key.Key{Code: key.KeyUp})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyUp})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()
	if tools != 1 || prompts != 0 {
		t.Fatalf("arrows plus Enter fired tools=%d prompts=%d, want tools only", tools, prompts)
	}

	// A digit past the end of the menu must be inert rather than run the last
	// entry or wrap around.
	env.SendKey(key.Key{Code: '8', Text: "8"})
	env.Flush()
	if tools != 1 || prompts != 0 || resources != 1 || restart != 0 {
		t.Errorf("out-of-range digit fired tools=%d prompts=%d resources=%d restart=%d",
			tools, prompts, resources, restart)
	}

	env.SendKey(key.Key{Code: key.KeyEscape})
	env.Flush()
	if back != 1 {
		t.Errorf("esc fired back=%d, want 1", back)
	}
}

// TestMCPInspectorMenuForStatus pins the menu's contents and order per server
// state, since the entry order is what tells the user what to do next.
func TestMCPInspectorMenuForStatus(t *testing.T) {
	connected, pending := mcpTestServers()[0], mcpTestServers()[1]

	// The pending server leads with the action that unblocks it.
	labels := []string{}
	for _, item := range mcpMenuFor(pending, mcpMenuActions{}) {
		labels = append(labels, item.Label)
	}
	wantPending := []string{"Authenticate", "Reconnect", "Back to servers"}
	if !reflect.DeepEqual(labels, wantPending) {
		t.Errorf("auth-pending menu = %v, want %v", labels, wantPending)
	}

	labels = labels[:0]
	for _, item := range mcpMenuFor(connected, mcpMenuActions{}) {
		labels = append(labels, item.Label)
	}
	wantConnected := []string{"View tools (2)", "Run prompt (2)", "Attach resource (2)", "Reconnect"}
	if !reflect.DeepEqual(labels, wantConnected) {
		t.Errorf("connected menu = %v, want %v", labels, wantConnected)
	}

	// A server that discovered nothing offers only a retry.
	labels = labels[:0]
	for _, item := range mcpMenuFor(api.MCPServer{Name: "ghost", Status: api.MCPStatusError}, mcpMenuActions{}) {
		labels = append(labels, item.Label)
	}
	if !reflect.DeepEqual(labels, []string{"Reconnect"}) {
		t.Errorf("failed server menu = %v, want [Reconnect]", labels)
	}

	// An OAuth server that is already connected can still re-authorize, so the
	// entry stays.
	oauthConnected := connected
	oauthConnected.AuthType = api.MCPAuthOAuth2
	labels = labels[:0]
	for _, item := range mcpMenuFor(oauthConnected, mcpMenuActions{}) {
		labels = append(labels, item.Label)
	}
	wantOAuth := []string{"View tools (2)", "Run prompt (2)", "Attach resource (2)", "Authenticate", "Reconnect"}
	if !reflect.DeepEqual(labels, wantOAuth) {
		t.Errorf("connected OAuth menu = %v, want %v", labels, wantOAuth)
	}
}

// TestMCPToolListViewAndDetail drives the drill-down reached from the overview:
// the tool list names each tool with its policy, and selecting one opens its
// detail view including the parameters its input schema declares.
func TestMCPToolListViewAndDetail(t *testing.T) {
	newMCPFixture(t, []api.MCPServer{mcpToolDetailServer()})
	store.SetWorkspaceMCPServers([]api.MCPServer{mcpToolDetailServer()})

	server := mcpToolDetailServer()
	out := renderTestNode(t, mcpToolList(mcpToolListProps{server: server}))
	for _, want := range []string{
		"middleman — tools (2)",
		"apply_rule_override",
		"drop_workspace",
		"[Allowed] [ReadOnly]",
		"[Prohibited] [Dangerous]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tool list missing %q:\n%s", want, out)
		}
	}

	detail := testutils.Render(t, mcpToolDetailView(mcpToolDetailProps{
		server: server,
		tool:   server.Tools[0],
	}), 100, 40)
	for _, want := range []string{
		"apply_rule_override",
		"middleman",
		"Tool name:  apply_rule_override",
		"Full name:  middleman__apply_rule_override",
		"Policy:     ✔ Allowed · [ReadOnly]",
		"Description:",
		"Create or update a mock rule, manifest, or fixture file inside an active workspace.",
		"Parameters:",
		"● path (required): string - Relative path of the rule or file",
		"● profile: string - Optional profile name",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("tool detail missing %q:\n%s", want, detail)
		}
	}

	// Required parameters lead the list, so a caller sees what a call needs first.
	pathIndex := strings.Index(detail, "● path (required)")
	profileIndex := strings.Index(detail, "● profile:")
	if pathIndex < 0 || profileIndex < 0 || pathIndex > profileIndex {
		t.Errorf("required parameters should be listed before optional ones:\n%s", detail)
	}
}

// TestMCPToolPolicyLine pins how a tool's verdict and safety posture read on one
// line, since it is the row that decides whether the tool can be used at all.
func TestMCPToolPolicyLine(t *testing.T) {
	tests := []struct {
		name string
		tool api.MCPToolSummary
		want string
	}{
		{
			name: "allowed with no annotations",
			tool: api.MCPToolSummary{Allowed: true},
			want: "✔ Allowed",
		},
		{
			name: "allowed and read-only",
			tool: api.MCPToolSummary{Allowed: true, Annotations: api.ToolAnnotation{IsReadOnly: true}},
			want: "✔ Allowed · [ReadOnly]",
		},
		{
			name: "prohibited and dangerous",
			tool: api.MCPToolSummary{Allowed: false, Annotations: api.ToolAnnotation{IsDangerous: true}},
			want: "✘ Prohibited · [Dangerous]",
		},
		{
			name: "every annotation",
			tool: api.MCPToolSummary{Allowed: true, Annotations: api.ToolAnnotation{
				IsReadOnly: true, IsDangerous: true, IsOpenWorld: true,
			}},
			want: "✔ Allowed · [ReadOnly] [Dangerous] [OpenWorld]",
		},
	}
	for _, tt := range tests {
		if got := mcpToolPolicyLine(tt.tool); got != tt.want {
			t.Errorf("%s: mcpToolPolicyLine = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestMCPToolDetailEnablesProhibitedTool drives the whole point of the detail
// view for a prohibited tool: `e` rewrites the manifest, and the view then reports
// the outcome and shows the tool as allowed without the user leaving it.
func TestMCPToolDetailEnablesProhibitedTool(t *testing.T) {
	fixture := newMCPFixture(t, []api.MCPServer{mcpToolDetailServer()})

	env := testenv.Default(100, 40)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-enable-tool"}), container)
	env.Flush()

	// Screen 1 -> 2 -> 3, then open the second tool, the prohibited one.
	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()
	env.SendKey(key.Key{Code: '1', Text: "1"})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyDown})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()

	out := strings.TrimSpace(env.DumpText())
	for _, want := range []string{"drop_workspace", "Policy:     ✘ Prohibited · [Dangerous]", "e Enable in manifest"} {
		if !strings.Contains(out, want) {
			t.Errorf("prohibited tool detail missing %q:\n%s", want, out)
		}
	}

	env.SendKey(key.Key{Code: 'e', Text: "e"})
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/policies/allow")) > 0 }) {
		t.Fatalf("pressing e never reached the daemon:\n%s", out)
	}
	calls := fixture.pathCalls("/policies/allow")
	if got := calls[0].body["kind"]; got != api.MCPAllowPolicyKindTool {
		t.Errorf("allow request kind = %v, want %q", got, api.MCPAllowPolicyKindTool)
	}
	if got := calls[0].body["value"]; got != "drop_workspace" {
		t.Errorf("allow request value = %v, want the raw tool name", got)
	}

	// The view must report the outcome and then show the new verdict.
	refreshed := ""
	if !waitFor(t, func() bool {
		env.Flush()
		refreshed = strings.TrimSpace(env.DumpText())
		return strings.Contains(refreshed, "Enabled drop_workspace in .agents/mcps/middleman.mcp.yaml.") &&
			strings.Contains(refreshed, "Policy:     ✔ Allowed · [Dangerous]")
	}) {
		t.Errorf("tool detail did not refresh after enabling:\n%s", refreshed)
	}
	// It is allowed now, so the shortcut for widening it is gone.
	if strings.Contains(refreshed, "e Enable in manifest") {
		t.Errorf("an allowed tool must not advertise Enable in manifest:\n%s", refreshed)
	}
}

// TestMCPToolDetailWithholdsEnableWhenItCannotWork proves the `e` shortcut is
// offered only when pressing it would change something: an allowed tool needs no
// widening, and a manifest the CLI must not rewrite cannot be widened.
func TestMCPToolDetailWithholdsEnableWhenItCannotWork(t *testing.T) {
	newMCPFixture(t, []api.MCPServer{mcpToolDetailServer()})

	server := mcpToolDetailServer()

	allowed := testutils.Render(t, mcpToolDetailView(mcpToolDetailProps{
		server: server,
		tool:   server.Tools[0],
	}), 100, 40)
	if strings.Contains(allowed, "Enable in manifest") {
		t.Errorf("an allowed tool should not offer to widen its policy:\n%s", allowed)
	}

	// The same prohibited tool on a user-scope manifest: rewriting it would edit
	// files outside the project, so the offer is withheld.
	userScope := server
	userScope.Scope = "User"
	prohibited := testutils.Render(t, mcpToolDetailView(mcpToolDetailProps{
		server: userScope,
		tool:   userScope.Tools[1],
	}), 100, 40)
	if strings.Contains(prohibited, "Enable in manifest") {
		t.Errorf("a user-scope manifest must not be offered for editing:\n%s", prohibited)
	}
	if !strings.Contains(prohibited, "✘ Prohibited") {
		t.Errorf("expected the prohibited verdict to stay visible:\n%s", prohibited)
	}
}

// TestMCPToolDetailReportsEnableFailureInPlace checks a rejected manifest edit is
// visible where it was attempted instead of scrolling past into the transcript.
func TestMCPToolDetailReportsEnableFailureInPlace(t *testing.T) {
	fixture := newMCPFixture(t, []api.MCPServer{mcpToolDetailServer()})
	fixture.failPolicyAllow = "read-only manifest"

	env := testenv.Default(100, 40)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-enable-fails"}), container)
	env.Flush()

	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()
	env.SendKey(key.Key{Code: '1', Text: "1"})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyDown})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()
	env.SendKey(key.Key{Code: 'e', Text: "e"})

	failed := ""
	if !waitFor(t, func() bool {
		env.Flush()
		failed = strings.TrimSpace(env.DumpText())
		return strings.Contains(failed, "Failed to enable drop_workspace")
	}) {
		t.Fatalf("the failure never surfaced in the detail view:\n%s", failed)
	}
	if !strings.Contains(failed, "read-only manifest") {
		t.Errorf("expected the daemon's reason, got:\n%s", failed)
	}
	// The tool is still prohibited, so the shortcut must stay available.
	if !strings.Contains(failed, "e Enable in manifest") {
		t.Errorf("a failed enable should leave the shortcut available:\n%s", failed)
	}
}

// TestMCPToolParametersSorting proves required parameters lead the list, since
// they are what a caller has to supply for a call to succeed at all.
func TestMCPToolParametersSorting(t *testing.T) {
	tool := api.MCPToolSummary{
		Name:    "middleman__apply_rule_override",
		RawName: "apply_rule_override",
		Allowed: true,
		InputSchema: map[string]any{
			"required": []any{"workspace_id", "path", "project"},
			"properties": map[string]any{
				"workspace_id": map[string]any{"type": "string", "description": "Target workspace ID"},
				"project":      map[string]any{"type": "string", "description": "The mock project name"},
				"path":         map[string]any{"type": "string"},
				"profile":      map[string]any{"type": "string", "description": "Optional profile name"},
				"retries":      map[string]any{"description": "No declared type"},
			},
		},
	}

	got := mcpToolParameters(tool)
	want := []string{
		"  ● path (required): string",
		"  ● project (required): string - The mock project name",
		"  ● workspace_id (required): string - Target workspace ID",
		"  ● profile: string - Optional profile name",
		"  ● retries: unknown - No declared type",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mcpToolParameters =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if params := mcpToolParameters(api.MCPToolSummary{}); params != nil {
		t.Errorf("a tool with no schema should have no parameter rows, got %v", params)
	}
}

// TestMCPBrowserOpensToolDetailView walks the full drill-down from the server
// list through the overview, the tool list, and a single tool's detail, then back
// out again, which is the path a user takes to learn what a server can do.
func TestMCPBrowserOpensToolDetailView(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	env := testenv.Default(100, 40)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-tools-drill"}), container)
	env.Flush()
	// Keys arriving in the same tick as the mount effects can be dropped, so let
	// the browser settle before driving it.
	mcpSettle(env)

	// Screen 1 -> Screen 2: enter opens the first listed server (notes).
	env.SendKey(key.Key{Code: key.KeyEnter})
	mcpSettle(env)
	if out := strings.TrimSpace(env.DumpText()); !strings.Contains(out, "notes MCP Server") {
		t.Fatalf("expected the overview after enter, got:\n%s", out)
	}

	// Screen 2 -> Screen 3: the first menu entry is "View tools".
	env.SendKey(key.Key{Code: '1', Text: "1"})
	mcpSettle(env)
	out := strings.TrimSpace(env.DumpText())
	for _, want := range []string{"notes — tools (2)", "echo", "purge"} {
		if !strings.Contains(out, want) {
			t.Errorf("tool list missing %q after choosing View tools:\n%s", want, out)
		}
	}

	// Screen 3 -> tool detail: enter opens the highlighted tool.
	env.SendKey(key.Key{Code: key.KeyEnter})
	mcpSettle(env)
	out = strings.TrimSpace(env.DumpText())
	for _, want := range []string{"Tool name:  echo", "Full name:  notes__echo"} {
		if !strings.Contains(out, want) {
			t.Errorf("tool detail missing %q:\n%s", want, out)
		}
	}

	// Esc steps back one screen at a time rather than jumping to the list.
	env.SendKey(key.Key{Code: key.KeyEscape})
	mcpSettle(env)
	if out = strings.TrimSpace(env.DumpText()); !strings.Contains(out, "notes — tools (2)") {
		t.Errorf("expected the tool list after esc from the detail, got:\n%s", out)
	}
	env.SendKey(key.Key{Code: key.KeyEscape})
	mcpSettle(env)
	if out = strings.TrimSpace(env.DumpText()); !strings.Contains(out, "notes MCP Server") {
		t.Errorf("expected the overview after esc from the tool list, got:\n%s", out)
	}
	env.SendKey(key.Key{Code: key.KeyEscape})
	mcpSettle(env)
	if out = strings.TrimSpace(env.DumpText()); !strings.Contains(out, "Manage MCP servers") {
		t.Errorf("expected the server list after esc from the overview, got:\n%s", out)
	}
}

// TestMCPMenuDigit pins which digit keys mean anything on the overview.
func TestMCPMenuDigit(t *testing.T) {
	tests := []struct {
		name string
		key  key.Key
		want int
	}{
		{name: "first entry", key: key.Key{Text: "1"}, want: 1},
		{name: "last entry", key: key.Key{Text: "9"}, want: 9},
		{name: "zero means nothing", key: key.Key{Text: "0"}, want: 0},
		{name: "letter", key: key.Key{Text: "p"}, want: 0},
		{name: "modified digit", key: key.Key{Text: "2", Mod: key.ModCtrl}, want: 0},
		{name: "no text", key: key.Key{}, want: 0},
	}
	for _, tt := range tests {
		if got := mcpMenuDigit(tt.key); got != tt.want {
			t.Errorf("%s: mcpMenuDigit = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// TestMCPInspectorHintRowFitsNarrowTerminal keeps every advertised overview key
// readable, because the hint row clips rather than wraps.
func TestMCPInspectorHintRowFitsNarrowTerminal(t *testing.T) {
	for _, width := range []int{100, 80, 72, 60} {
		out := testutils.Render(t, mcpInspector(mcpInspectorProps{server: mcpTestServers()[0]}), width, 40)
		var hint string
		for line := range strings.SplitSeq(out, "\n") {
			if trimmed := strings.TrimRight(line, " "); strings.HasPrefix(trimmed, "Keyboard:") {
				hint = trimmed
			}
		}
		if hint == "" {
			t.Fatalf("width %d rendered no keyboard hint:\n%s", width, out)
		}
		if !strings.HasSuffix(hint, "esc Back") {
			t.Errorf("keyboard hint truncated at width %d, got %q", width, hint)
		}
		if runes := len([]rune(hint)); runes > width {
			t.Errorf("keyboard hint is %d cells wide at width %d: %q", runes, width, hint)
		}
	}
}

func TestMCPItemPickerListsPromptsAndResources(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	servers := mcpTestServers()
	out := renderTestNode(t, mcpItemPicker(mcpItemPickerProps{server: servers[0], kind: mcpTargetPrompt}))
	for _, want := range []string{"notes — prompts (2)", "summarize", "danger", "[Prohibited]"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt picker missing %q:\n%s", want, out)
		}
	}

	out = renderTestNode(t, mcpItemPicker(mcpItemPickerProps{server: servers[0], kind: mcpTargetResource}))
	for _, want := range []string{"notes — resources (2)", "Spec", "notes://secret"} {
		if !strings.Contains(out, want) {
			t.Errorf("resource picker missing %q:\n%s", want, out)
		}
	}

	out = renderTestNode(t, mcpItemPicker(mcpItemPickerProps{
		server: servers[1],
		kind:   mcpTargetResource,
	}))
	if !strings.Contains(out, "No resources discovered") {
		t.Errorf("expected the empty notice, got:\n%s", out)
	}
}

func TestMCPArgsPromptRendersTemplate(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	servers := mcpTestServers()
	target, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")

	out := renderTestNode(t, mcpArgsPrompt(mcpArgsPromptProps{target: target}))
	for _, want := range []string{"Arguments for /mcp:notes:danger", `{"target": ""}`, "target (required)"} {
		if !strings.Contains(out, want) {
			t.Errorf("args prompt missing %q:\n%s", want, out)
		}
	}
}

func TestMCPOptionBPromptOptions(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	servers := mcpTestServers()

	prohibited, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")
	props := mcpOptionBPrompt(prohibited, "")
	if props.Title != "Prohibited MCP item" {
		t.Errorf("title = %q", props.Title)
	}
	if len(props.Options) != 3 {
		t.Fatalf("expected Cancel / Use once / Enable in manifest, got %+v", props.Options)
	}
	if props.Options[0].Value != "cancel" || !props.Options[0].Current {
		t.Errorf("expected Cancel to be the default choice, got %+v", props.Options[0])
	}
	if props.Options[1].Value != "once" || props.Options[2].Value != "manifest" {
		t.Errorf("unexpected choices: %+v", props.Options)
	}
	if props.Header == nil {
		t.Error("expected the prohibition message in the header")
	}
	out := renderTestNode(t, components.Picker(props))
	for _, want := range []string{"MCP prompt 'danger'", "spec.policies.prompts", "Use once", "Enable in manifest"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt output missing %q:\n%s", want, out)
		}
	}

	// A user-scoped manifest cannot be rewritten, so that choice is withheld.
	userScope := prohibited
	userScope.server.Scope = "User"
	props = mcpOptionBPrompt(userScope, "")
	if len(props.Options) != 2 {
		t.Fatalf("expected the manifest choice to be withheld, got %+v", props.Options)
	}
}

func TestMCPOptionBUseOnceSendsAllowProhibited(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	servers := mcpTestServers()
	target, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")

	// The daemon rejects the prohibited prompt unless AllowProhibited is set, so
	// a successful render proves the flag reached the wire.
	confirmMCPSubmission(target, "and list the fallout")
	active := store.GetSlot().Active
	if active == nil {
		t.Fatal("expected the Option B confirmation to be enqueued")
	}
	props, ok := active.Data.(components.PickerProps)
	if !ok {
		t.Fatalf("expected PickerProps, got %T", active.Data)
	}
	store.ClearSlot()

	for _, opt := range props.Options {
		if opt.Value == "once" {
			props.OnSelect(opt)
		}
	}
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/prompts/danger")) > 0 }) {
		t.Fatal("expected the prompt to be rendered")
	}
	calls := fixture.pathCalls("/prompts/danger")
	if v, ok := calls[0].body["allowProhibited"].(bool); !ok || !v {
		t.Fatalf("expected allowProhibited=true, got body %+v", calls[0].body)
	}
}

func TestMCPOptionBCancelChangesNothing(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	servers := mcpTestServers()
	target, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")
	confirmMCPSubmission(target, "")
	props, ok := store.GetSlot().Active.Data.(components.PickerProps)
	if !ok {
		t.Fatalf("expected PickerProps, got %T", store.GetSlot().Active.Data)
	}
	store.ClearSlot()

	props.OnSelect(props.Options[0])
	props.OnCancel()

	if got := len(fixture.pathCalls("/prompts/")); got != 0 {
		t.Fatalf("cancel must not call the daemon, saw %d prompt calls", got)
	}
}

func TestMCPOptionBEnableInManifest(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	servers := mcpTestServers()
	target, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "danger")
	confirmMCPSubmission(target, "")
	props, ok := store.GetSlot().Active.Data.(components.PickerProps)
	if !ok {
		t.Fatalf("expected PickerProps, got %T", store.GetSlot().Active.Data)
	}
	store.ClearSlot()

	chosen := false
	for _, opt := range props.Options {
		if opt.Value == "manifest" {
			props.OnSelect(opt)
			chosen = true
		}
	}
	if !chosen {
		t.Fatal("expected an Enable in manifest choice")
	}

	if !waitFor(t, func() bool { return len(fixture.pathCalls("/policies/allow")) > 0 }) {
		t.Fatal("expected the manifest to be updated")
	}
	calls := fixture.pathCalls("/policies/allow")
	if calls[0].body["kind"] != api.MCPAllowPolicyKindPrompt || calls[0].body["value"] != "danger" {
		t.Fatalf("unexpected allow request body: %+v", calls[0].body)
	}
	// Once permitted, the prompt runs through the normal path.
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/prompts/danger")) > 0 }) {
		t.Fatal("expected the prompt to run after enabling")
	}
	// The item is permitted by the manifest now, so no override flag is needed.
	promptCalls := fixture.pathCalls("/prompts/danger")
	if _, set := promptCalls[0].body["allowProhibited"]; set {
		t.Errorf("did not expect allowProhibited after enabling in the manifest: %+v", promptCalls[0].body)
	}
}

func TestMCPAllowedItemRunsImmediately(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	servers := mcpTestServers()
	target, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "summarize")
	confirmMCPSubmission(target, "")

	if active := store.GetSlot().Active; active != nil {
		t.Fatalf("an allowed item must not prompt, got slot %+v", active)
	}
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/prompts/summarize")) > 0 }) {
		t.Fatal("expected the prompt to render immediately")
	}
	if _, set := fixture.pathCalls("/prompts/summarize")[0].body["allowProhibited"]; set {
		t.Error("an allowed item must not set allowProhibited")
	}
}

func TestMCPResourceSubmissionAttachesBody(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())

	if !SubmitMCPReference("summarize @mcp:notes:notes://specs/spec-1") {
		t.Fatal("expected the reference to be handled")
	}
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/resources")) > 0 }) {
		t.Fatal("expected the resource to be read")
	}
	calls := fixture.pathCalls("/resources")
	if calls[0].body["uri"] != "notes://specs/spec-1" {
		t.Fatalf("unexpected uri: %+v", calls[0].body)
	}
}

func TestMCPSubmissionReportsUnknownItem(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())
	store.ResetTranscript()
	defer store.ResetTranscript()

	if !SubmitMCPReference("/mcp:notes:missing") {
		t.Fatal("expected the line to be handled as a reference")
	}
	out := strings.ToLower(transcriptText())
	if !strings.Contains(out, "was not found on server") {
		t.Fatalf("expected an error notice, got:\n%s", out)
	}
}

func TestMCPSubmissionIgnoresOrdinaryMessage(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.SetWorkspaceMCPServers(mcpTestServers())

	if SubmitMCPReference("what does the auth handler do?") {
		t.Fatal("an ordinary message must not be handled as a reference")
	}
}

func TestMCPServerActions(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	notes := mcpTestServers()[0]
	notes.Status = api.MCPStatusDisconnected
	store.SetWorkspaceMCPServers([]api.MCPServer{notes})

	var notices []string
	message, ok := restartMCPServer(ctx, "notes", func(msg string) { notices = append(notices, msg) })
	if !ok {
		t.Fatalf("expected a successful restart, got %q", message)
	}

	calls := fixture.pathCalls("/restart")
	if len(calls) != 1 {
		t.Fatalf("expected one restart call, got %d", len(calls))
	}
	if message != "Restarted notes — connected." {
		t.Fatalf("completion message = %q, want it to name the new state", message)
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "Restarted notes — connected.") {
		t.Fatalf("expected a completion notice naming the new state, got:\n%s", joined)
	}
	// The listing is re-read so the browser shows the new state.
	if len(fixture.pathCalls("/v1/workspace/mcps")) == 0 {
		t.Error("expected the listing to be refreshed after a restart")
	}
}

// TestMCPServerActionWithoutClient keeps a restart attempted with no daemon
// connection reportable: the caller has a message to log rather than an empty one.
func TestMCPServerActionWithoutClient(t *testing.T) {
	store.ResetSession()
	var notices []string
	message, ok := restartMCPServer(context.Background(), "notes", func(msg string) { notices = append(notices, msg) })
	if ok {
		t.Fatal("expected a restart to fail without a daemon connection")
	}
	if !strings.Contains(message, "No daemon connection") || !strings.Contains(message, "notes") {
		t.Fatalf("message = %q, want it to name the missing connection and the server", message)
	}
	if len(notices) == 0 || notices[0] != message {
		t.Fatalf("notices = %v, want the same message reported as progress", notices)
	}
}

// TestMCPRestartCommandWritesTheTimeline proves /mcp restart reports in the
// conversation timeline rather than inside a modal that closes: the outcome has to
// still be readable after the command finishes.
func TestMCPRestartCommandWritesTheTimeline(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	if err := handleMCP([]string{"restart", "notes"}); err != nil {
		t.Fatalf("handleMCP: %v", err)
	}
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/restart")) > 0 }) {
		t.Fatalf("expected a restart call, saw %v", reloadPaths(fixture))
	}
	if !waitFor(t, func() bool { return strings.Contains(transcriptText(), "Restarted notes") }) {
		t.Fatalf("expected the outcome in the timeline, got:\n%s", transcriptText())
	}
	// A restart opens no slot: the composer command does the work and reports.
	if len(fixture.pathCalls("/auth")) != 0 {
		t.Errorf("a restart must not start an authorization, saw %v", fixture.pathCalls("/auth"))
	}
}

// TestMCPRestartCommandReportsFailure keeps a failed reconnect visible instead of
// letting it disappear with a transient notice.
func TestMCPRestartCommandReportsFailure(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	if err := handleMCP([]string{"restart", "does-not-exist"}); err != nil {
		t.Fatalf("handleMCP: %v", err)
	}
	if !waitFor(t, func() bool { return strings.Contains(transcriptText(), "Failed to restart does-not-exist") }) {
		t.Fatalf("expected the failure in the timeline, got:\n%s", transcriptText())
	}
}

// TestMCPBrowserAnnouncesUnhealthyServersInTimeline pins where an unhealthy server
// is reported: the conversation timeline, never the startup banner. The banner is
// frozen once the conversation starts, so a server that authenticates or breaks
// later would either never be seen or leave a stale warning behind.
func TestMCPBrowserAnnouncesUnhealthyServersInTimeline(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()
	store.SetWorkspaceWarnings(nil)
	defer store.SetWorkspaceWarnings(nil)
	store.ClearTranscript()
	store.ClearMCPWarningAnnouncements()

	if err := handleMCP(nil); err != nil {
		t.Fatalf("handleMCP: %v", err)
	}
	// The listing is read when the browser renders, so mounting it is part of
	// running the command the way the app does.
	mcpMountActiveSlot(t)

	warning := "MCP server \"sentry\" requires authentication"
	if !waitFor(t, func() bool { return strings.Contains(transcriptText(), warning) }) {
		t.Fatalf("expected the auth requirement in the timeline, got:\n%s", transcriptText())
	}

	var announced []store.NoticeData
	for _, item := range *store.GetTranscript().Items {
		if notice, ok := item.Data.(store.NoticeData); ok && strings.Contains(notice.Text, "MCP server") {
			announced = append(announced, notice)
		}
	}
	if len(announced) != 1 || announced[0].Severity != store.NoticeSeverityWarning {
		t.Fatalf("expected one warning-severity announcement, got %+v", announced)
	}
	if warnings := store.CurrentHeaderData().Warnings; len(warnings) != 0 {
		t.Fatalf("MCP health must never reach the header, got %v", warnings)
	}
}

// TestMCPAuthCommandOpensTheAuthScreen pins that naming a server in the composer is
// the whole decision: the browser opens on the authorization screen instead of
// asking whether the user really meant it.
func TestMCPAuthCommandOpensTheAuthScreen(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()
	mcpStubAuthFlow(t)

	if err := handleMCP([]string{"auth", "sentry"}); err != nil {
		t.Fatalf("handleMCP: %v", err)
	}
	slot := store.GetSlot()
	if slot.Queue == nil || len(*slot.Queue) == 0 {
		t.Fatal("expected /mcp auth to open the browser slot")
	}
	// The flow starts when its screen renders, so mounting the slot is part of
	// running the command the way the app does.
	env := mcpMountActiveSlot(t)
	if _, ok := waitForRender(t, env, "Authenticating with sentry…"); !ok {
		t.Fatalf("expected the authorization screen, got:\n%s", env.DumpText())
	}
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/auth")) > 0 }) {
		t.Fatalf("expected authorization to start without a confirmation, saw %v", reloadPaths(fixture))
	}
}

func TestMCPAuthCommandRequiresAServerName(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	if err := handleMCP([]string{"auth"}); err == nil {
		t.Error("expected an error for a missing server name")
	}
	if err := handleMCP([]string{"restart", "  "}); err == nil {
		t.Error("expected an error for a blank server name")
	}
	if err := handleMCP([]string{"bogus"}); err == nil {
		t.Error("expected an error for an unknown subcommand")
	}
}

// mcpStubAuthFlow keeps the authorization flow in-process: no browser is
// launched and the OAuth callback poll resolves quickly. It returns the URLs the
// code asked to open, safe to read while an action goroutine is still running.
func mcpStubAuthFlow(t *testing.T) *mcpOpenedURLs {
	t.Helper()
	opened := &mcpOpenedURLs{}
	original := mcpLaunchAuthURL
	originalInterval, originalTimeout := mcpAuthPollInterval, mcpAuthPollTimeout
	mcpLaunchAuthURL = func(url string) bool {
		opened.add(url)
		return true
	}
	mcpAuthPollInterval = time.Millisecond
	mcpAuthPollTimeout = 2 * time.Second
	t.Cleanup(func() {
		// Drain while the short budget is still in effect, so an in-flight poll
		// finishes quickly instead of resuming against the restored 90s wait.
		mcpBackground.Wait()
		mcpLaunchAuthURL = original
		mcpAuthPollInterval = originalInterval
		mcpAuthPollTimeout = originalTimeout
	})
	return opened
}

// mcpOpenedURLs records requested browser URLs across goroutines.
type mcpOpenedURLs struct {
	mu   sync.Mutex
	urls []string
}

func (o *mcpOpenedURLs) add(url string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.urls = append(o.urls, url)
}

func (o *mcpOpenedURLs) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.urls...)
}

func (o *mcpOpenedURLs) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.urls)
}

// TestMCPStartAuthOpensTheBrowser covers the first half of the flow: the daemon's
// URL is published and the desktop is asked to open it.
func TestMCPStartAuthOpensTheBrowser(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	opened := mcpStubAuthFlow(t)

	outcome := mcpStartAuth(context.Background(), "sentry")
	if outcome.Settled {
		t.Fatalf("expected a flow still waiting on the browser, got %+v", outcome)
	}
	if urls := opened.all(); len(urls) != 1 || !strings.Contains(urls[0], "server=sentry") {
		t.Fatalf("expected the sentry authorization URL to be opened, saw %v", urls)
	}
	if !strings.Contains(outcome.Message, "waiting for it to finish") {
		t.Errorf("expected a waiting message, got %q", outcome.Message)
	}
}

// TestMCPStartAuthKeepsTheURLWhenNoBrowserExists proves the URL still reaches the
// screen when the desktop refuses to open it, which is the case the copy shortcut
// and the manual paste exist for.
func TestMCPStartAuthKeepsTheURLWhenNoBrowserExists(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	original := mcpLaunchAuthURL
	mcpLaunchAuthURL = func(string) bool { return false }
	t.Cleanup(func() { mcpLaunchAuthURL = original })

	outcome := mcpStartAuth(context.Background(), "sentry")
	if outcome.Settled {
		t.Fatalf("expected a flow still waiting on the browser, got %+v", outcome)
	}
	if !strings.Contains(outcome.AuthURL, "server=sentry") {
		t.Errorf("expected the URL on the outcome for a manual visit, got %q", outcome.AuthURL)
	}
}

func TestMCPAuthorizeReportsServersWithoutAnInteractiveFlow(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	opened := mcpStubAuthFlow(t)

	outcome := mcpStartAuth(context.Background(), "notes")
	if opened.count() != 0 {
		t.Errorf("expected no browser launch, saw %v", opened.all())
	}
	if !outcome.Settled || !outcome.Failed {
		t.Errorf("expected a settled failure, got %+v", outcome)
	}
	if !strings.Contains(outcome.Message, "does not use an interactive authorization flow") {
		t.Errorf("expected the daemon explanation to reach the user, got:\n%s", outcome.Message)
	}
	if got := len(fixture.pathCalls("/auth")); got != 1 {
		t.Errorf("expected one auth call, got %d", got)
	}
	if outcome.LeavesBrowser {
		t.Error("a server whose credentials must be edited should stay on screen for the guidance")
	}
}

func TestMCPAuthorizeReportsAlreadyAuthorizedServers(t *testing.T) {
	servers := mcpTestServers()
	servers[1].Status = api.MCPStatusConnected
	servers[1].AuthURL = ""
	fixture := newMCPFixture(t, servers)
	opened := mcpStubAuthFlow(t)

	outcome := mcpStartAuth(context.Background(), "sentry")
	if opened.count() != 0 {
		t.Errorf("expected no browser launch, saw %v", opened.all())
	}
	if !outcome.Settled || outcome.Failed {
		t.Errorf("expected a settled success, got %+v", outcome)
	}
	if !strings.Contains(outcome.Message, "needs no interactive authorization") {
		t.Errorf("unexpected message: %q", outcome.Message)
	}
	if got := len(fixture.pathCalls("/auth")); got != 1 {
		t.Errorf("expected one auth call, got %d", got)
	}
	// Nothing is left to do here, so the browser has no reason to stay open.
	if !outcome.LeavesBrowser {
		t.Error("expected the browser to close once there is nothing to authorize")
	}
}

// TestMCPStartAuthWithoutDaemonConnection keeps the failure legible when the CLI has
// no daemon to ask.
func TestMCPStartAuthWithoutDaemonConnection(t *testing.T) {
	store.ResetSession()
	outcome := mcpStartAuth(context.Background(), "notes")
	if !outcome.Settled || !outcome.Failed {
		t.Fatalf("expected a settled failure, got %+v", outcome)
	}
	if !strings.Contains(outcome.Message, "No daemon connection") {
		t.Errorf("unexpected message: %q", outcome.Message)
	}
}

func TestMCPOpenAuthURLRejectsEmpty(t *testing.T) {
	if openMCPAuthURL("") || openMCPAuthURL("   ") {
		t.Error("an empty URL must not launch a browser")
	}
}

func TestMCPListColumns(t *testing.T) {
	servers := mcpTestServers()
	name, status := mcpListColumns(servers, 120)
	if name <= 0 || status <= 0 {
		t.Fatalf("column widths = (%d, %d), want positive widths", name, status)
	}
	// The name column has to hold the cursor gutter, the status glyph, a space, and
	// the longest server name without clipping it.
	longest := 0
	for _, server := range servers {
		if n := len(mcpServerName(server)); n > longest {
			longest = n
		}
	}
	if name < longest+5 {
		t.Errorf("name column %d cannot fit %q plus its gutter and icon", name, strings.Repeat("n", longest))
	}
	if name+status > 120+8 {
		t.Errorf("columns %d+%d overflow a 120 cell terminal", name, status)
	}

	// An unknown terminal width leaves the status column unmeasured so the text is
	// never squeezed into an ellipsis.
	if name, status := mcpListColumns(servers, 0); status != 0 {
		t.Errorf("status width with no terminal = %d, want 0", status)
	} else if name <= 0 {
		t.Errorf("name width with no terminal = %d, want a positive width", name)
	}

	// A terminal too narrow to fit a useful status column leaves it unmeasured too.
	if _, status := mcpListColumns(servers, 12); status != 0 {
		t.Errorf("status width on a 12 cell terminal = %d, want 0", status)
	}

	if name, status := mcpListColumns(nil, 80); name <= 0 || status <= 0 {
		t.Errorf("empty listing produced widths (%d, %d)", name, status)
	}
}

// TestMCPScopeGrouping pins the scope-grouped ordering and headings of the server
// list: project manifests first, then workspace, user, and builtin, each named by
// the directory its manifests live in.
func TestMCPScopeGrouping(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory available to shorten")
	}
	userManifest := filepath.Join(home, ".config", "tasksmith", "agents", "mcps", "zeta.mcp.yaml")
	servers := []api.MCPServer{
		{Name: "zeta", Scope: "User", Path: userManifest},
		{Name: "alpha", Scope: "Workspace", Path: ".agents/mcps/alpha.mcp.yaml"},
		{Name: "mid", Scope: "Project", Path: "packages/api/.agents/mcps/mid.mcp.yaml"},
		{Name: "baked", Scope: "Builtin"},
		{Name: "beta", Scope: "Workspace", Path: ".agents/mcps/beta.mcp.yaml"},
	}

	ordered := mcpGroupServers(servers)
	wantOrder := []string{"mid", "alpha", "beta", "zeta", "baked"}
	if len(ordered) != len(wantOrder) {
		t.Fatalf("grouped listing has %d servers, want %d", len(ordered), len(wantOrder))
	}
	for i, want := range wantOrder {
		if ordered[i].Name != want {
			t.Errorf("position %d = %q, want %q", i, ordered[i].Name, want)
		}
	}
	// The input slice must survive untouched for the caller's own bookkeeping.
	if servers[0].Name != "zeta" {
		t.Errorf("mcpGroupServers reordered the caller's slice: %q", servers[0].Name)
	}

	tests := []struct {
		server api.MCPServer
		want   string
	}{
		{server: api.MCPServer{Scope: "Workspace", Path: ".agents/mcps/alpha.mcp.yaml"}, want: "Workspace MCPs (.agents/mcps)"},
		{server: api.MCPServer{Scope: "Project", Path: "packages/api/.agents/mcps/mid.mcp.yaml"}, want: "Project MCPs (packages/api/.agents/mcps)"},
		{server: api.MCPServer{Scope: "User", Path: userManifest}, want: "User MCPs (~/.config/tasksmith/agents/mcps)"},
		// Without a manifest path there is nothing to name, so no empty parentheses.
		{server: api.MCPServer{Scope: "Builtin"}, want: "Builtin MCPs"},
		// A manifest sitting at the root has no directory to name.
		{server: api.MCPServer{Scope: "Workspace", Path: "orphan.mcp.yaml"}, want: "Workspace MCPs"},
		// An unknown scope falls back to the namespace, then to Workspace.
		{server: api.MCPServer{Namespace: "plugin", Path: ".agents/mcps/p.mcp.yaml"}, want: "plugin MCPs (.agents/mcps)"},
		{server: api.MCPServer{}, want: "Workspace MCPs"},
	}
	for _, tt := range tests {
		if got := mcpScopeHeading(tt.server); got != tt.want {
			t.Errorf("mcpScopeHeading(%+v) = %q, want %q", tt.server, got, tt.want)
		}
	}
}

// TestMCPStatusIcons pins the glyph and color of every connection state.
func TestMCPStatusIcons(t *testing.T) {
	scheme := theme.Get()
	tests := []struct {
		status     api.MCPStatus
		icon       string
		label      string
		foreground any
	}{
		{status: api.MCPStatusConnected, icon: "✔", label: "connected", foreground: scheme.Color.Text.Success},
		{status: api.MCPStatusAuthRequired, icon: "⚠", label: "needs authentication", foreground: scheme.Color.Text.Warning},
		{status: api.MCPStatusError, icon: "✘", label: "failed to connect", foreground: scheme.Color.Text.Error},
		{status: api.MCPStatusConnecting, icon: "⟳", label: "connecting…", foreground: scheme.Color.Text.Info},
		{status: api.MCPStatusDisconnected, icon: "○", label: "disconnected", foreground: scheme.Color.Text.Muted},
		{status: api.MCPStatus("weird"), icon: "○", label: "disconnected", foreground: scheme.Color.Text.Muted},
	}
	for _, tt := range tests {
		if got := mcpStatusIcon(tt.status); got != tt.icon {
			t.Errorf("mcpStatusIcon(%q) = %q, want %q", tt.status, got, tt.icon)
		}
		// The plain-language label is shared with the notices and completions, so
		// the browser never shows two names for one state.
		if got := mcpStatusLabel(tt.status); got != tt.label {
			t.Errorf("mcpStatusLabel(%q) = %q, want %q", tt.status, got, tt.label)
		}
		got, ok := mcpStatusIconColor(scheme, tt.status).ForegroundOpt().Get()
		if !ok {
			t.Errorf("mcpStatusIconColor(%q) left the foreground unset", tt.status)
			continue
		}
		if !reflect.DeepEqual(got, tt.foreground) {
			t.Errorf("mcpStatusIconColor(%q) foreground = %v, want %v", tt.status, got, tt.foreground)
		}
	}

	// The status column stays muted for healthy and neutral servers so the colors
	// only draw the eye where something needs attention.
	for _, status := range []api.MCPStatus{api.MCPStatusConnected, api.MCPStatusDisconnected} {
		got, ok := mcpRowStatusColor(scheme, status).ForegroundOpt().Get()
		if !ok || !reflect.DeepEqual(got, scheme.Color.Text.Muted) {
			t.Errorf("mcpRowStatusColor(%q) = %v, want muted", status, got)
		}
	}
	for _, status := range []api.MCPStatus{api.MCPStatusAuthRequired, api.MCPStatusError, api.MCPStatusConnecting} {
		if got, want := mcpRowStatusColor(scheme, status), mcpStatusIconColor(scheme, status); !reflect.DeepEqual(got, want) {
			t.Errorf("mcpRowStatusColor(%q) = %v, want it to match the icon color %v", status, got, want)
		}
	}
}

func TestMCPServerCountSubtitle(t *testing.T) {
	if got := mcpServerCountSubtitle(1); got != "1 server" {
		t.Errorf("single server = %q", got)
	}
	if got := mcpServerCountSubtitle(3); got != "3 servers" {
		t.Errorf("three servers = %q", got)
	}
	if got := mcpServerCountSubtitle(0); got != "0 servers" {
		t.Errorf("no servers = %q", got)
	}
}

// transcriptText renders every transcript notice so assertions can check what
// the user would have seen.
func transcriptText() string {
	var builder strings.Builder
	state := store.GetTranscript()
	if state.Items == nil {
		return ""
	}
	for _, item := range *state.Items {
		switch data := item.Data.(type) {
		case store.ErrorData:
			builder.WriteString(data.Title + ": " + data.Message + "\n")
		case store.NoticeData:
			builder.WriteString(data.Text + "\n")
		case store.CommandData:
			builder.WriteString(data.Command + " " + data.Output + "\n")
		}
	}
	return builder.String()
}

func TestMCPInspectorHeight(t *testing.T) {
	tests := []struct {
		name     string
		termRows int
		want     int
	}{
		{name: "unknown terminal size falls back", termRows: 0, want: 22},
		{name: "tiny terminal keeps a usable window", termRows: 4, want: 8},
		{name: "typical terminal", termRows: 32, want: 22},
		{name: "tall terminal is capped", termRows: 200, want: 28},
	}
	for _, tt := range tests {
		if got := mcpInspectorHeight(tt.termRows); got != tt.want {
			t.Errorf("%s: mcpInspectorHeight(%d) = %d, want %d", tt.name, tt.termRows, got, tt.want)
		}
	}
}

func TestMCPItemCellShowsResourceURI(t *testing.T) {
	servers := mcpTestServers()
	named, _ := mcpTargetFor(servers, "notes", mcpTargetResource, "notes://secret")
	if got := mcpItemCell(named); got != "Secret (notes://secret)" {
		t.Errorf("mcpItemCell = %q, want the name and URI", got)
	}
	prompt, _ := mcpTargetFor(servers, "notes", mcpTargetPrompt, "summarize")
	if got := mcpItemCell(prompt); got != "summarize" {
		t.Errorf("mcpItemCell for a prompt = %q, want the bare name", got)
	}
}

// TestMCPBrowserSurvivesPaneSwitches guards the hook order inside the browser:
// an empty listing renders a different view than a populated one, and inspecting
// a server swaps the view again, so a hook called only on one path would crash
// the whole CLI the moment a server appeared or a user pressed enter.
func TestMCPBrowserSurvivesPaneSwitches(t *testing.T) {
	fixture := newMCPFixture(t, nil)

	env := testenv.Default(100, 40)
	// Close only after background actions finish, so an action cannot report
	// progress into an environment that is already gone.
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-switch"}), container)
	env.Flush()
	mcpSettle(env)

	if out := strings.TrimSpace(env.DumpText()); !strings.Contains(out, "No MCP servers configured") {
		t.Fatalf("expected the empty notice before servers arrive, got:\n%s", out)
	}

	// The browser owns the listing, so servers appear through its own refresh.
	fixture.mu.Lock()
	fixture.servers = mcpTestServers()
	fixture.mu.Unlock()
	if _, err := refreshMCPServers(context.Background()); err != nil {
		t.Fatalf("refreshMCPServers: %v", err)
	}
	mcpSettle(env)
	out := strings.TrimSpace(env.DumpText())
	for _, want := range []string{"Manage MCP servers", "2 servers", "notes", "sentry"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q after servers arrived:\n%s", want, out)
		}
	}

	env.SendKey(key.Key{Code: key.KeyEnter})
	mcpSettle(env)
	out = strings.TrimSpace(env.DumpText())
	if !strings.Contains(out, "notes MCP Server") {
		t.Fatalf("expected the inspector after pressing enter, got:\n%s", out)
	}

	env.SendKey(key.Key{Code: key.KeyEscape})
	mcpSettle(env)
	out = strings.TrimSpace(env.DumpText())
	if !strings.Contains(out, "Manage MCP servers") {
		t.Fatalf("expected the list again after esc, got:\n%s", out)
	}
}

// TestMCPReloadWorkspaceKey drives the reload from the browser's own keybinding
// so the advertised key is proven to reach the daemon.
func TestMCPReloadWorkspaceKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  key.Key
	}{
		{name: "capital R", key: key.Key{Code: 'R', Text: "R"}},
		{name: "ctrl+r chord", key: key.Key{Code: 'r', Text: "r", Mod: key.ModCtrl}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newMCPFixture(t, mcpTestServers())
			env := testenv.Default(100, 40)
			defer func() {
				mcpBackground.Wait()
				env.Close()
			}()
			container := element.NewBox(env.Document())
			env.Mount(container)
			kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-reload"}), container)
			env.Flush()

			if got := len(fixture.pathCalls("/workspace/reload")); got != 0 {
				t.Fatalf("mounting the browser must not reload the workspace, saw %d", got)
			}

			env.SendKey(tc.key)
			if !waitFor(t, func() bool { return len(fixture.pathCalls("/workspace/reload")) > 0 }) {
				t.Fatalf("expected a workspace reload, saw calls: %v", reloadPaths(fixture))
			}
		})
	}
}

func reloadPaths(fixture *mcpFixture) []string {
	var paths []string
	for _, req := range fixture.pathCalls("") {
		paths = append(paths, req.method+" "+req.path)
	}
	return paths
}

func TestMCPReloadFromEmptyNotice(t *testing.T) {
	fixture := newMCPFixture(t, nil)
	store.ClearSlot()
	defer store.ClearSlot()

	env := testenv.Default(100, 30)
	defer func() {
		mcpBackground.Wait()
		env.Close()
	}()
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-empty-reload"}), container)
	env.Flush()

	if out := strings.TrimSpace(env.DumpText()); !strings.Contains(out, "No MCP servers configured") {
		t.Fatalf("expected the empty notice, got:\n%s", out)
	}
	if out := strings.TrimSpace(env.DumpText()); !strings.Contains(out, "Reload workspace") {
		t.Fatalf("expected the empty notice to advertise a reload, got:\n%s", out)
	}

	if got := len(fixture.pathCalls("/workspace/reload")); got != 0 {
		t.Fatalf("mounting the browser must not reload the workspace, saw %d", got)
	}

	env.SendKey(key.Key{Code: 'R', Text: "R"})
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/workspace/reload")) > 0 }) {
		t.Fatalf("expected a workspace reload from the empty notice, saw %v", reloadPaths(fixture))
	}
}

// TestMCPBrowserActionKeys drives authorize and restart from the server list so
// the picker's per-option keybindings are proven to reach the named server.
func TestMCPBrowserActionKeys(t *testing.T) {
	for _, tc := range []struct {
		name     string
		key      rune
		char     string
		pathPart string
	}{
		{name: "authorize", key: 'a', char: "a", pathPart: "/auth"},
		{name: "restart", key: 'r', char: "r", pathPart: "/restart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newMCPFixture(t, mcpTestServers())
			fixture.authorizeThenConnect = true
			mcpStubAuthFlow(t)
			store.ClearSlot()
			defer store.ClearSlot()

			env := testenv.Default(100, 40)
			defer func() {
				mcpBackground.Wait()
				env.Close()
			}()
			container := element.NewBox(env.Document())
			env.Mount(container)
			kitex.Render(mcpBrowser(mcpBrowserProps{slotID: "slot-actions"}), container)
			env.Flush()

			env.SendKey(key.Key{Code: tc.key, Text: tc.char})
			if !waitFor(t, func() bool { return len(fixture.pathCalls(tc.pathPart)) > 0 }) {
				t.Fatalf("expected %s on the highlighted server, saw %v", tc.name, reloadPaths(fixture))
			}
			calls := fixture.pathCalls(tc.pathPart)
			if !strings.Contains(calls[0].path, "/mcps/notes"+tc.pathPart) {
				t.Errorf("%s targeted %q, want the highlighted notes server", tc.name, calls[0].path)
			}
		})
	}
}

// TestMCPRowStatusPrioritizesStatusDiagnostics pins the status column of a list
// row: an unhealthy server names why it is unhealthy, with the transport wrappers
// stripped, while a connected server reports what it offers instead.
func TestMCPRowStatusPrioritizesStatusDiagnostics(t *testing.T) {
	failing := api.MCPServer{
		Name:        "github",
		Description: "GitHub repositories, issues, and pull requests",
		Status:      api.MCPStatusError,
		Error:       `connect MCP server "github": sending "initialize": rejected by transport: 401 Unauthorized`,
	}
	got := mcpRowStatus(failing, 0)
	if got != "failed to connect (401 Unauthorized)" {
		t.Errorf("error status = %q, want the cleaned reason", got)
	}

	// A long reason is shortened so one row stays one line; the full reason stays
	// in the server overview.
	shortened := mcpRowStatus(failing, 30)
	if len([]rune(shortened)) > 30 {
		t.Errorf("status %q exceeds the 30 cell column", shortened)
	}
	if !strings.Contains(shortened, "failed to connect") {
		t.Errorf("shortened status %q should still name the state", shortened)
	}

	bareFailure := api.MCPServer{Name: "broken", Status: api.MCPStatusError}
	if got := mcpRowStatus(bareFailure, 0); got != "failed to connect" {
		t.Errorf("error without a reason = %q", got)
	}

	pending := api.MCPServer{Name: "linear", Description: "whatever", Status: api.MCPStatusAuthRequired}
	if got := mcpRowStatus(pending, 0); got != "needs authentication" {
		t.Errorf("pending authorization = %q, want needs authentication", got)
	}

	if got := mcpRowStatus(api.MCPServer{Status: api.MCPStatusConnecting}, 0); got != "connecting…" {
		t.Errorf("connecting = %q", got)
	}
	if got := mcpRowStatus(api.MCPServer{Status: api.MCPStatusDisconnected, Error: "dial refused"}, 0); got != "disconnected" {
		t.Errorf("disconnected = %q, want the neutral label", got)
	}
}

// TestMCPCapabilitySummary pins the right-hand counts of a connected server,
// including the policy caveat shown only when something is prohibited.
func TestMCPCapabilitySummary(t *testing.T) {
	connected := mcpTestServers()[0]
	want := "2 tools (1 allowed) · 2 prompts (1 allowed) · 2 resources (1 allowed)"
	if got := mcpCapabilitySummary(connected); got != want {
		t.Errorf("mcpCapabilitySummary = %q, want %q", got, want)
	}

	// A server whose policy blocks nothing reads as a plain count, and a singular
	// kind uses the singular noun.
	plain := api.MCPServer{
		Status:  api.MCPStatusConnected,
		Tools:   []api.MCPToolSummary{{Name: "x__one", RawName: "one", Allowed: true}},
		Prompts: []api.MCPPromptSummary{{Name: "p1", Allowed: true}, {Name: "p2", Allowed: true}},
	}
	if got := mcpCapabilitySummary(plain); got != "1 tool · 2 prompts" {
		t.Errorf("plain counts = %q, want \"1 tool · 2 prompts\"", got)
	}

	if got := mcpCapabilitySummary(api.MCPServer{Status: api.MCPStatusConnected}); got != "connected" {
		t.Errorf("server with nothing discovered = %q, want connected", got)
	}
}

// TestMCPServerSummaryDiagnosticsReachTheInspector checks the inspector shows the
// cleaned reason rather than the raw SDK chain.
func TestMCPServerSummaryDiagnosticsReachTheInspector(t *testing.T) {
	newMCPFixture(t, mcpTestServers())

	out := renderTestNode(t, mcpInspector(mcpInspectorProps{server: api.MCPServer{
		Name:   "github",
		Status: api.MCPStatusError,
		Error:  `connect MCP server "github": calling "initialize": connection refused`,
	}}))
	if !strings.Contains(out, "Error:            connection refused") {
		t.Errorf("inspector should show the cleaned reason, got:\n%s", out)
	}
	if strings.Contains(out, "calling ") {
		t.Errorf("inspector leaked transport wrappers:\n%s", out)
	}
}

// TestMCPStaticAuthInstructionNamesTheCredential verifies the CLI presents the
// daemon's credential guidance as advice rather than wrapping it in a failure
// message, so pressing `a` on a bearer server tells the user what to set.
func TestMCPStaticAuthInstructionNamesTheCredential(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	fixture.staticAuthServers = map[string]string{
		"notes": `MCP server "notes" authenticates via environment variable NOTES_TOKEN (bearer); set it and run /mcp restart notes`,
	}
	opened := mcpStubAuthFlow(t)

	outcome := mcpStartAuth(context.Background(), "notes")

	if opened.count() != 0 {
		t.Errorf("expected no browser launch, saw %v", opened.all())
	}
	if !strings.Contains(outcome.Message, "NOTES_TOKEN") || !strings.Contains(outcome.Message, "/mcp restart notes") {
		t.Errorf("expected actionable credential guidance, got:\n%s", outcome.Message)
	}
	if strings.Contains(outcome.Message, "Failed to start authorization") {
		t.Errorf("credential guidance should not read as a failure:\n%s", outcome.Message)
	}
}

// mcpStubClipboard records what the authorization screen asked the terminal to copy
// and reports success, since the test terminal has no clipboard to write to.
func mcpStubClipboard(t *testing.T) *mcpOpenedURLs {
	t.Helper()
	opened := &mcpOpenedURLs{}
	original := mcpCopyToClipboard
	mcpCopyToClipboard = func(_ dom.Document, text string) bool {
		if strings.TrimSpace(text) == "" {
			return false
		}
		opened.add(text)
		return true
	}
	t.Cleanup(func() { mcpCopyToClipboard = original })
	return opened
}

// waitForTranscript pumps the render loop until the conversation timeline shows
// every needle. Timeline writes happen in a render effect, so a test that only
// sleeps would keep reading a transcript that has had no chance to update.
func waitForTranscript(t *testing.T, env *testenv.Environment, want ...string) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var out string
	for time.Now().Before(deadline) {
		env.Flush()
		out = transcriptText()
		if allContain(out, want) {
			return out, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return out, false
}

// mcpMountBrowser renders the MCP browser in a test terminal and registers the slot
// it would occupy in production, so a view that closes itself can be observed doing
// it. It returns the environment so a test can send keys and read frames.
func mcpMountBrowser(t *testing.T, props mcpBrowserProps) *testenv.Environment {
	t.Helper()
	if props.slotID == "" {
		t.Fatal("a mounted browser needs the slot id it renders into")
	}
	store.EnqueueSlot(store.SlotItem{
		ID:   props.slotID,
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node { return mcpBrowser(props) },
	})
	return mcpMountActiveSlot(t)
}

// mcpMountActiveSlot renders whichever browser the slot queue is holding, which is
// how the app shows it. An authorization only starts once its screen renders, so a
// test of a composer command has to mount what the command enqueued.
func mcpMountActiveSlot(t *testing.T) *testenv.Environment {
	t.Helper()
	item := store.GetSlot().Active
	if item == nil {
		t.Fatal("expected an active slot to mount")
	}
	render, ok := item.Data.(func(string) trail.Node)
	if !ok {
		t.Fatalf("slot data = %T, want a renderer function", item.Data)
	}

	env := testenv.Default(100, 40)
	t.Cleanup(func() {
		mcpBackground.Wait()
		env.Close()
	})
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(render(item.ID), container)
	env.Flush()
	mcpSettle(env)
	return env
}

// mcpSettle gives a mounted pane the moment it needs to finish its mount effects,
// including focusing a field and settling its first listing. A person cannot press
// a key in the same tick a screen appears, and the test terminal drops a key sent
// that early, so waiting for a fixed beat is not enough on a loaded machine: the
// frame is only safe to drive once it stops changing.
func mcpSettle(env *testenv.Environment) {
	const (
		beat    = 10 * time.Millisecond
		steady  = 3
		maxWait = 2 * time.Second
	)
	deadline := time.Now().Add(maxWait)
	previous := env.DumpText()
	unchanged := 0
	for time.Now().Before(deadline) && unchanged < steady {
		time.Sleep(beat)
		env.Flush()
		current := env.DumpText()
		if current == previous {
			unchanged++
			continue
		}
		unchanged = 0
		previous = current
	}
	env.Flush()
}

// TestMCPAuthScreenShowsTheURLAndThePasteField pins the authorization screen's
// contents: what is happening, the page to open, how to copy it, and how to feed
// the redirect back when the browser cannot reach the daemon.
func TestMCPAuthScreenShowsTheURLAndThePasteField(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	mcpStubAuthFlow(t)
	store.ClearSlot()
	defer store.ClearSlot()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:            "slot-auth-screen",
		authStore:         newMCPAuthStore(),
		initialAuthServer: "sentry",
	})

	out, ok := waitForRender(t, env,
		"Authenticating with sentry…",
		"A browser window will open for authentication",
		"(c to copy)",
		"https://example.com/authorize?server=sentry",
		"paste the URL from your browser's address bar:",
		"URL >",
		"Return here after authenticating in your browser. Press Esc to go back.",
	)
	if !ok {
		t.Fatalf("authorization screen missing expected content, got:\n%s", out)
	}
	if !strings.Contains(out, "Copy URL") || !strings.Contains(out, "Submit pasted URL") {
		t.Errorf("authorization screen must advertise its keys, got:\n%s", out)
	}
	// The confirmation only appears once the user actually copies.
	if strings.Contains(out, mcpAuthCopiedNotice) {
		t.Errorf("the copy confirmation showed before a copy, got:\n%s", out)
	}
}

// TestMCPAuthScreenWaitsForTheURLBeforeItArrives keeps the screen honest while the
// daemon has not reported a page yet: it says what it is waiting for rather than
// rendering an empty line where a URL belongs.
func TestMCPAuthScreenWaitsForTheURLBeforeItArrives(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.ClearSlot()
	defer store.ClearSlot()

	store.ClearTranscript()
	node := mcpAuthPane(mcpAuthPaneProps{
		server: "sentry",
		state:  kites.Create(mcpAuthState{server: "sentry"}),
	})
	out := testutils.Render(t, node, 100, 30)
	if !strings.Contains(out, "Waiting for the authorization URL…") {
		t.Fatalf("expected the waiting notice, got:\n%s", out)
	}
}

// TestMCPAuthScreenCopyPutsTheURLOnTheClipboard proves the copy key hands the
// daemon's authorization URL to the terminal and confirms it on screen.
func TestMCPAuthScreenCopyPutsTheURLOnTheClipboard(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	mcpStubAuthFlow(t)
	copied := mcpStubClipboard(t)
	store.ClearSlot()
	defer store.ClearSlot()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:            "slot-auth-copy",
		authStore:         newMCPAuthStore(),
		initialAuthServer: "sentry",
	})
	if _, ok := waitForRender(t, env, "https://example.com/authorize?server=sentry"); !ok {
		t.Fatalf("the authorization URL never reached the screen:\n%s", env.DumpText())
	}

	env.SendKey(key.Key{Code: 'y', Text: "y", Mod: key.ModCtrl})
	out, ok := waitForRender(t, env, mcpAuthCopiedNotice)
	if !ok {
		t.Fatalf("expected the copy confirmation, got:\n%s", out)
	}
	if !slices.Contains(copied.all(), "https://example.com/authorize?server=sentry") {
		t.Errorf("copied URLs = %v, want the authorization URL", copied.all())
	}
}

// TestMCPAuthScreenCopyKeyCopiesAnEmptyField covers the bare `c` shortcut. The paste
// field has focus on this screen, so the keystroke must not leave a stray letter
// behind once the copy is done.
func TestMCPAuthScreenCopyKeyCopiesAnEmptyField(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	mcpStubAuthFlow(t)
	copied := mcpStubClipboard(t)
	store.ClearSlot()
	defer store.ClearSlot()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:            "slot-auth-copy-key",
		authStore:         newMCPAuthStore(),
		initialAuthServer: "sentry",
	})
	if _, ok := waitForRender(t, env, "https://example.com/authorize?server=sentry"); !ok {
		t.Fatalf("the authorization URL never reached the screen:\n%s", env.DumpText())
	}

	env.SendKey(key.Key{Code: 'c', Text: "c"})
	out, ok := waitForRender(t, env, mcpAuthCopiedNotice)
	if !ok {
		t.Fatalf("expected the copy confirmation, got:\n%s", out)
	}
	if !slices.Contains(copied.all(), "https://example.com/authorize?server=sentry") {
		t.Errorf("copied URLs = %v, want the authorization URL", copied.all())
	}
	if strings.Contains(out, "URL > c") {
		t.Errorf("the copy keystroke left a stray letter in the paste field:\n%s", out)
	}
}

// TestMCPAuthScreenPasteCompletesTheAuthorization is the heart of the feature: a
// browser that cannot reach the loopback port still finishes the flow, because the
// address bar can be pasted and submitted.
func TestMCPAuthScreenPasteCompletesTheAuthorization(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	fixture.authorizeThenConnect = true
	fixture.callbackConnects = true
	mcpStubAuthFlow(t)
	store.ClearSlot()
	defer store.ClearSlot()
	store.ClearTranscript()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:            "slot-auth-paste",
		authStore:         newMCPAuthStore(),
		initialAuthServer: "sentry",
	})
	if _, ok := waitForRender(t, env, "URL >"); !ok {
		t.Fatalf("the paste field never appeared:\n%s", env.DumpText())
	}
	mcpSettle(env)

	pasted := "http://localhost:56014/callback?code=abc123&state=xyz"
	env.Type(pasted)
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEnter, Text: "\r"})

	if !waitFor(t, func() bool { return len(fixture.pathCalls("/auth/callback")) > 0 }) {
		t.Fatalf("expected the pasted URL to reach the daemon, saw %v", reloadPaths(fixture))
	}
	calls := fixture.pathCalls("/auth/callback")
	if !strings.Contains(calls[0].path, "/mcps/sentry/auth/callback") {
		t.Errorf("callback targeted %q, want the sentry server", calls[0].path)
	}
	if got, _ := calls[0].body["callbackUrl"].(string); got != pasted {
		t.Errorf("callbackUrl = %q, want the pasted URL %q", got, pasted)
	}

	// The daemon's flow completes on its own, the server connects, and the browser
	// closes with the outcome left in the timeline.
	if _, ok := waitForTranscript(t, env, "Authentication successful. Connected to sentry."); !ok {
		t.Fatalf("expected the success in the timeline, got:\n%s", transcriptText())
	}
	if !waitFor(t, func() bool {
		slot := store.GetSlot()
		return slot.Queue == nil || len(*slot.Queue) == 0
	}) {
		t.Fatal("expected the browser to close once authorization succeeded")
	}
}

// TestMCPAuthScreenReportsARejectedCallback keeps a paste the daemon cannot use
// readable: the reason stays in the timeline rather than vanishing with a notice.
func TestMCPAuthScreenReportsARejectedCallback(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	fixture.failAuthCallback = "no authorization is in progress for sentry"
	mcpStubAuthFlow(t)
	store.ClearSlot()
	defer store.ClearSlot()
	store.ClearTranscript()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:            "slot-auth-rejected",
		authStore:         newMCPAuthStore(),
		initialAuthServer: "sentry",
	})
	if _, ok := waitForRender(t, env, "URL >"); !ok {
		t.Fatalf("the paste field never appeared:\n%s", env.DumpText())
	}
	mcpSettle(env)

	env.Type("?code=expired")
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEnter, Text: "\r"})

	if _, ok := waitForTranscript(t, env, "no authorization is in progress for sentry"); !ok {
		t.Fatalf("expected the rejection in the timeline, got:\n%s", transcriptText())
	}
	if !strings.Contains(transcriptText(), "Could not use that callback URL") {
		t.Errorf("the rejection should say what was wrong with the paste:\n%s", transcriptText())
	}
	if strings.Contains(transcriptText(), "Authentication successful") {
		t.Errorf("a rejected callback must not report success:\n%s", transcriptText())
	}
	// A paste the daemon refused settles the flow, reports the failure in the
	// timeline, and closes the browser slot so the error is immediately visible.
	if !waitFor(t, func() bool {
		slot := store.GetSlot()
		return slot.Queue == nil || len(*slot.Queue) == 0
	}) {
		t.Fatal("expected the browser slot to close after a settled callback failure")
	}
}

// TestMCPAuthScreenEnterWithoutAPasteDoesNothing keeps an accidental Enter from
// sending an empty callback or reporting a spurious failure.
func TestMCPAuthScreenEnterWithoutAPasteDoesNothing(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	mcpStubAuthFlow(t)
	store.ClearSlot()
	defer store.ClearSlot()
	store.ClearTranscript()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:            "slot-auth-empty-enter",
		authStore:         newMCPAuthStore(),
		initialAuthServer: "sentry",
	})
	if _, ok := waitForRender(t, env, "URL >"); !ok {
		t.Fatalf("the paste field never appeared:\n%s", env.DumpText())
	}
	mcpSettle(env)

	env.SendKey(key.Key{Code: key.KeyEnter, Text: "\r"})
	time.Sleep(50 * time.Millisecond)
	env.Flush()
	if calls := fixture.pathCalls("/auth/callback"); len(calls) != 0 {
		t.Fatalf("an empty paste must not reach the daemon, saw %v", calls)
	}
	if out := transcriptText(); out != "" {
		t.Errorf("an empty paste must not report anything, got:\n%s", out)
	}
}

// TestMCPAuthScreenEscapeReturnsToTheServerList covers leaving a flow the user
// changed their mind about: the browser stays open, back where they came from.
func TestMCPAuthScreenEscapeReturnsToTheServerList(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	mcpStubAuthFlow(t)
	store.ClearSlot()
	defer store.ClearSlot()

	env := mcpMountBrowser(t, mcpBrowserProps{slotID: "slot-auth-esc", authStore: newMCPAuthStore()})
	// The sentry server is the one with an interactive flow, and it sits below the
	// connected server in the list.
	env.SendKey(key.Key{Code: key.KeyDown})
	env.Flush()
	env.SendKey(key.Key{Code: 'a', Text: "a"})
	if !waitFor(t, func() bool { return len(fixture.pathCalls("/auth")) > 0 }) {
		t.Fatalf("expected authorization to start from the list, saw %v", reloadPaths(fixture))
	}
	if _, ok := waitForRender(t, env, "URL >"); !ok {
		t.Fatalf("the authorization screen never opened:\n%s", env.DumpText())
	}
	mcpSettle(env)

	env.SendKey(key.Key{Code: key.KeyEscape, Text: "\x1b"})
	out, ok := waitForRender(t, env, "MCP servers")
	if !ok {
		t.Fatalf("expected the server list after Esc, got:\n%s", out)
	}
	slot := store.GetSlot()
	if slot.Queue == nil || len(*slot.Queue) == 0 {
		t.Error("Esc from the server list must keep the browser open")
	}
}

// TestMCPAuthScreenEscapeFromTheComposerClosesTheBrowser matches the screen to the
// command that opened it: /mcp auth <name> has nothing behind it, so Esc leaves.
func TestMCPAuthScreenEscapeFromTheComposerClosesTheBrowser(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	mcpStubAuthFlow(t)
	store.ClearSlot()
	defer store.ClearSlot()

	if err := handleMCP([]string{"auth", "sentry"}); err != nil {
		t.Fatalf("handleMCP: %v", err)
	}
	slot := store.GetSlot()
	if slot.Queue == nil || len(*slot.Queue) == 0 {
		t.Fatal("expected /mcp auth to open the browser slot")
	}
	env := mcpMountActiveSlot(t)

	if _, ok := waitForRender(t, env, "URL >"); !ok {
		t.Fatalf("the authorization screen never opened:\n%s", env.DumpText())
	}
	mcpSettle(env)
	env.SendKey(key.Key{Code: key.KeyEscape, Text: "\x1b"})
	if !waitFor(t, func() bool {
		remaining := store.GetSlot()
		return remaining.Queue == nil || len(*remaining.Queue) == 0
	}) {
		t.Fatal("expected Esc to close the browser opened by /mcp auth")
	}
	if len(fixture.pathCalls("/auth")) == 0 {
		t.Error("expected /mcp auth to start the authorization before Esc")
	}
}

// TestMCPAuthScreenReportsAFailedAuthorization proves a flow that ends in failure is
// recorded in the timeline as an error, where the reason can still be read.
func TestMCPAuthScreenReportsAFailedAuthorization(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.ClearTranscript()

	mcpAuthTimelineReport(mcpAuthState{
		server:  "sentry",
		notice:  "Authorization for sentry failed: token endpoint returned 400",
		settled: true,
		failed:  true,
	})
	out := transcriptText()
	if !strings.Contains(out, "Authorization for sentry failed: token endpoint returned 400") {
		t.Fatalf("expected the failure in the timeline, got:\n%s", out)
	}
	if strings.Contains(out, "Authentication successful") {
		t.Errorf("a failure must not read as a success:\n%s", out)
	}
}

// TestMCPAuthScreenReportsSuccessAsCommandOutput matches the plan's shape: the
// success line hangs under the /mcp command rather than appearing as a warning.
func TestMCPAuthScreenReportsSuccessAsCommandOutput(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.ClearTranscript()

	mcpAuthTimelineReport(mcpAuthState{
		server:  "sentry",
		notice:  "Authentication successful. Connected to sentry.",
		settled: true,
	})
	out := transcriptText()
	if !strings.Contains(out, "/mcp") || !strings.Contains(out, "Authentication successful. Connected to sentry.") {
		t.Fatalf("expected a /mcp output line, got:\n%s", out)
	}
	if strings.Contains(out, "MCP:") {
		t.Errorf("a success must not be recorded as an error:\n%s", out)
	}
}

// TestMCPAuthScreenStaysQuietWhileFlowing proves the screen reports nothing until
// the authorization actually settles.
func TestMCPAuthScreenStaysQuietWhileFlowing(t *testing.T) {
	newMCPFixture(t, mcpTestServers())
	store.ClearTranscript()

	mcpAuthTimelineReport(mcpAuthState{server: "sentry", notice: "Opened the authorization page for sentry"})
	if out := transcriptText(); out != "" {
		t.Fatalf("an in-flight authorization must not report to the timeline, got:\n%s", out)
	}
}

// TestMCPAuthCopyKeyClaimsOnlyTheCopyChords pins which keystrokes copy. A bare `c`
// copies only while nothing has been typed, so a URL being entered keeps its letter;
// ctrl+y always copies.
func TestMCPAuthCopyKeyClaimsOnlyTheCopyChords(t *testing.T) {
	tests := []struct {
		name   string
		key    key.Key
		pasted string
		want   bool
	}{
		{name: "bare c on an empty field", key: key.Key{Code: 'c', Text: "c"}, want: true},
		{name: "bare c while typing", key: key.Key{Code: 'c', Text: "c"}, pasted: "http://localhost:1/c", want: false},
		{name: "ctrl+y with a paste", key: key.Key{Code: 'y', Text: "y", Mod: key.ModCtrl}, pasted: "code=abc", want: true},
		{name: "ctrl+y on an empty field", key: key.Key{Code: 'y', Text: "y", Mod: key.ModCtrl}, want: true},
		{name: "ctrl+c is left alone", key: key.Key{Code: 'c', Text: "c", Mod: key.ModCtrl}},
		{name: "capital C is left alone", key: key.Key{Code: 'C', Text: "C"}},
		{name: "enter is left alone", key: key.Key{Code: key.KeyEnter, Text: "\r"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpAuthCopyKey(event.KeyEvent{Key: tc.key}, tc.pasted); got != tc.want {
				t.Errorf("mcpAuthCopyKey = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMCPAuthFlowSubmitReportsWithoutADaemonConnection covers the paste path when
// there is nothing to paste to, which has to settle rather than poll forever.
func TestMCPAuthFlowSubmitReportsWithoutADaemonConnection(t *testing.T) {
	store.ResetSession()
	outcome := mcpSubmitAuthCallback(context.Background(), "sentry", "code=abc")
	if !outcome.Settled || !outcome.Failed {
		t.Fatalf("submit without a daemon connection = %+v, want a settled failure", outcome)
	}
	if !strings.Contains(outcome.Message, "No daemon connection") {
		t.Errorf("message = %q, want it to name the missing connection", outcome.Message)
	}
}

// TestMCPAuthFlowSubmitKeepsWaitingAfterAcceptance proves an accepted paste is not
// itself the end of the flow: the terminal keeps watching for the server to connect.
func TestMCPAuthFlowSubmitKeepsWaitingAfterAcceptance(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	fixture.callbackConnects = true

	outcome := mcpSubmitAuthCallback(context.Background(), "sentry", "code=abc")
	if outcome.Settled || outcome.Failed {
		t.Fatalf("an accepted paste = %+v, want the flow to keep waiting", outcome)
	}
	if !strings.Contains(outcome.Message, "finishing authorization for sentry") {
		t.Errorf("message = %q, want it to say what happens next", outcome.Message)
	}
	if calls := fixture.pathCalls("/auth/callback"); len(calls) != 1 {
		t.Fatalf("callback calls = %d, want one", len(calls))
	}
}

// TestMCPBrowserRestartPreservesFailureReason ensures a failed restart triggered
// from the browser closes the browser and surfaces the daemon's cleaned failure
// message once in the timeline rather than in the picker title.
func TestMCPBrowserRestartPreservesFailureReason(t *testing.T) {
	fixture := newMCPFixture(t, mcpTestServers())
	fixture.failRestart = `connect MCP server "notes": sending "initialize": connection refused`
	store.ClearSlot()
	defer store.ClearSlot()
	store.ClearTranscript()

	env := mcpMountBrowser(t, mcpBrowserProps{
		slotID:    "slot-restart-failure",
		authStore: newMCPAuthStore(),
	})
	mcpSettle(env)

	env.SendKey(key.Key{Code: 'r', Text: "r"})
	out, ok := waitForTranscript(t, env, "Failed to restart notes: connection refused")
	if !ok {
		t.Fatalf("expected the restart failure reason in the timeline, got:\n%s", out)
	}
	if strings.Contains(out, "No daemon connection") {
		t.Errorf("a daemon restart error must not be overwritten with a missing-connection message:\n%s", out)
	}
	if !waitFor(t, func() bool {
		slot := store.GetSlot()
		return slot.Queue == nil || len(*slot.Queue) == 0
	}) {
		t.Fatal("expected the browser slot to close after restart completes")
	}
}
