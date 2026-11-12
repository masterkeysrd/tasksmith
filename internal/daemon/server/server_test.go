package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/server"
)

type mockBackend struct {
	server.Backend

	healthResp        *api.HealthResponse
	healthErr         error
	shutdownErr       error
	connectSub        *broker.Subscriber
	connectTopics     []string
	connectErr        error
	subTopics         []string
	subErr            error
	unsubTopics       []string
	unsubErr          error
	doneCh            chan struct{}
	wsResp            *api.Workspace
	wsErr             error
	reloadWsCalled    bool
	reloadWsErr       error
	agentsResp        []api.Agent
	agentsErr         error
	agentResp         *api.Agent
	agentErr          error
	commandsResp      []api.Command
	commandsErr       error
	commandResp       *api.Command
	commandErr        error
	skillsResp        []api.Skill
	skillsErr         error
	skillResp         *api.Skill
	skillErr          error
	rulesResp         []api.Rule
	rulesErr          error
	toolsResp         []api.Tool
	toolsErr          error
	toolsAgent        string
	mcpsResp          []api.MCPServer
	mcpsErr           error
	restartMCPServer  api.MCPServer
	restartMCPErr     error
	restartMCPName    string
	authMCPResp       api.MCPAuthResponse
	authMCPErr        error
	authMCPName       string
	authCallbackReq   api.MCPAuthCallbackRequest
	authCallbackErr   error
	authCallbackName  string
	mcpPromptResp     api.MCPPromptResponse
	mcpPromptErr      error
	mcpPromptServer   string
	mcpPromptName     string
	mcpPromptReq      api.MCPPromptRequest
	mcpResourceResp   api.MCPResourceResponse
	mcpResourceErr    error
	mcpResourceServer string
	mcpResourceReq    api.MCPResourceRequest
	allowPolicyServer api.MCPServer
	allowPolicyErr    error
	allowPolicyName   string
	allowPolicyReq    api.MCPAllowPolicyRequest
	modelsResp        []api.Model
	modelsErr         error
	modelResp         *api.Model
	modelErr          error
	modelQuotaResp    *api.ModelQuota
	modelQuotaErr     error
	permsResp         *api.PermissionsResponse
	permsErr          error
	createSessResp    *api.Session
	createSessCreated bool
	createSessErr     error
	previewSessResp   *api.SessionPreview
	previewSessErr    error
	listSessResp      []api.Session
	listSessErr       error
	getSessResp       *api.Session
	getSessErr        error
	updateSessResp    *api.Session
	updateSessErr     error
	deleteSessErr     error
	timelineResp      []api.TimelineEntry
	timelineErr       error
	sendErr           error
	cancelErr         error
	changesResp       []api.FileChange
	changesErr        error
	diffResp          *api.DiffResponse
	diffErr           error
	revertErr         error
	filesResp         []api.WorkspaceFile
	filesErr          error
	filesReq          api.ListFilesRequest
}

func (m *mockBackend) Health(ctx context.Context) (*api.HealthResponse, error) {
	return m.healthResp, m.healthErr
}

func (m *mockBackend) Shutdown(ctx context.Context) error {
	return m.shutdownErr
}

func (m *mockBackend) AddSubscriber(ctx context.Context, subscriberID, since string, topics ...string) (*broker.Subscriber, []string, error) {
	return m.connectSub, m.connectTopics, m.connectErr
}

func (m *mockBackend) RemoveSubscriber(sub *broker.Subscriber) {}

func (m *mockBackend) FormatEvent(msg broker.Message) (api.Event, error) {
	if payload, ok := msg.Event.(api.EventPayload); ok {
		return api.Event{
			ID:      msg.ID,
			Type:    api.EventType(payload.EventType()),
			Topic:   msg.Topic,
			Payload: payload,
		}, nil
	}
	return api.Event{ID: msg.ID}, nil
}

func (m *mockBackend) Subscribe(ctx context.Context, subscriberID, since string, topics ...string) ([]string, error) {
	return m.subTopics, m.subErr
}

func (m *mockBackend) Unsubscribe(ctx context.Context, subscriberID string, topics ...string) ([]string, error) {
	return m.unsubTopics, m.unsubErr
}

func (m *mockBackend) Done() <-chan struct{} {
	if m.doneCh == nil {
		ch := make(chan struct{})
		return ch
	}
	return m.doneCh
}

func (m *mockBackend) GetWorkspace(ctx context.Context, workspace string) (*api.Workspace, error) {
	return m.wsResp, m.wsErr
}

func (m *mockBackend) PreviewSession(ctx context.Context, workspace string, req api.PreviewSessionRequest) (*api.SessionPreview, error) {
	return m.previewSessResp, m.previewSessErr
}

func (m *mockBackend) ReloadWorkspace(ctx context.Context, workspace string) error {
	m.reloadWsCalled = true
	return m.reloadWsErr
}

func (m *mockBackend) ListAgents(ctx context.Context, workspace string) ([]api.Agent, error) {
	return m.agentsResp, m.agentsErr
}

func (m *mockBackend) GetAgent(ctx context.Context, workspace, name string) (*api.Agent, error) {
	return m.agentResp, m.agentErr
}

func (m *mockBackend) ListCommands(ctx context.Context, workspace string) ([]api.Command, error) {
	return m.commandsResp, m.commandsErr
}

func (m *mockBackend) GetCommand(ctx context.Context, workspace, name string) (*api.Command, error) {
	return m.commandResp, m.commandErr
}

func (m *mockBackend) ListSkills(ctx context.Context, workspace string) ([]api.Skill, error) {
	return m.skillsResp, m.skillsErr
}

func (m *mockBackend) GetSkill(ctx context.Context, workspace, name string) (*api.Skill, error) {
	return m.skillResp, m.skillErr
}

func (m *mockBackend) ListRules(ctx context.Context, workspace string) ([]api.Rule, error) {
	return m.rulesResp, m.rulesErr
}

func (m *mockBackend) ListTools(ctx context.Context, workspace, agent string) ([]api.Tool, error) {
	m.toolsAgent = agent
	return m.toolsResp, m.toolsErr
}

func (m *mockBackend) ListMCPs(ctx context.Context, workspace string) ([]api.MCPServer, error) {
	return m.mcpsResp, m.mcpsErr
}

func (m *mockBackend) RestartMCP(ctx context.Context, workspace, name string) (api.MCPServer, error) {
	m.restartMCPName = name
	return m.restartMCPServer, m.restartMCPErr
}

func (m *mockBackend) AuthenticateMCP(ctx context.Context, workspace, name string) (api.MCPAuthResponse, error) {
	m.authMCPName = name
	return m.authMCPResp, m.authMCPErr
}

func (m *mockBackend) CompleteMCPAuth(ctx context.Context, workspace, name string, req api.MCPAuthCallbackRequest) error {
	m.authCallbackName = name
	m.authCallbackReq = req
	return m.authCallbackErr
}

func (m *mockBackend) GetMCPPrompt(ctx context.Context, workspace, name, prompt string, req api.MCPPromptRequest) (api.MCPPromptResponse, error) {
	m.mcpPromptServer = name
	m.mcpPromptName = prompt
	m.mcpPromptReq = req
	return m.mcpPromptResp, m.mcpPromptErr
}

func (m *mockBackend) ReadMCPResource(ctx context.Context, workspace, name string, req api.MCPResourceRequest) (api.MCPResourceResponse, error) {
	m.mcpResourceServer = name
	m.mcpResourceReq = req
	return m.mcpResourceResp, m.mcpResourceErr
}

func (m *mockBackend) AllowMCPPolicy(ctx context.Context, workspace, name string, req api.MCPAllowPolicyRequest) (api.MCPServer, error) {
	m.allowPolicyName = name
	m.allowPolicyReq = req
	return m.allowPolicyServer, m.allowPolicyErr
}

func (m *mockBackend) ListModels(ctx context.Context, workspace string) ([]api.Model, error) {
	return m.modelsResp, m.modelsErr
}

func (m *mockBackend) GetModel(ctx context.Context, workspace, id string) (*api.Model, error) {
	return m.modelResp, m.modelErr
}

func (m *mockBackend) GetModelQuota(ctx context.Context, workspace, id string) (*api.ModelQuota, error) {
	return m.modelQuotaResp, m.modelQuotaErr
}

func (m *mockBackend) GetPermissions(ctx context.Context, workspace string) (*api.PermissionsResponse, error) {
	return m.permsResp, m.permsErr
}

func (m *mockBackend) AddPermissionRule(ctx context.Context, workspace string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	return m.permsResp, m.permsErr
}

func (m *mockBackend) RemovePermissionRule(ctx context.Context, workspace string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	return m.permsResp, m.permsErr
}

func (m *mockBackend) CreateSession(ctx context.Context, workspace string, req api.CreateSessionRequest) (*api.Session, bool, error) {
	return m.createSessResp, m.createSessCreated, m.createSessErr
}

func (m *mockBackend) ListSessions(ctx context.Context, workspace string) ([]api.Session, error) {
	return m.listSessResp, m.listSessErr
}

func (m *mockBackend) GetSession(ctx context.Context, workspace, id string) (*api.Session, error) {
	return m.getSessResp, m.getSessErr
}

func (m *mockBackend) UpdateSession(ctx context.Context, workspace, id string, req api.UpdateSessionRequest) (*api.Session, error) {
	return m.updateSessResp, m.updateSessErr
}

func (m *mockBackend) DeleteSession(ctx context.Context, workspace, id string) error {
	return m.deleteSessErr
}

func (m *mockBackend) Timeline(ctx context.Context, workspace, id string) ([]api.TimelineEntry, error) {
	return m.timelineResp, m.timelineErr
}

func (m *mockBackend) Send(ctx context.Context, workspace, id string, req api.SendRequest) error {
	return m.sendErr
}

func (m *mockBackend) Cancel(ctx context.Context, workspace, id string) error {
	return m.cancelErr
}

func (m *mockBackend) Changes(ctx context.Context, workspace, id string) ([]api.FileChange, error) {
	return m.changesResp, m.changesErr
}

func (m *mockBackend) Diff(ctx context.Context, workspace, id, path, entryID string) (*api.DiffResponse, error) {
	return m.diffResp, m.diffErr
}

func (m *mockBackend) Revert(ctx context.Context, workspace, id string, req api.RevertRequest) error {
	return m.revertErr
}

func (m *mockBackend) ListWorkspaceFiles(ctx context.Context, workspace string, req api.ListFilesRequest) ([]api.WorkspaceFile, error) {
	m.filesReq = req
	return m.filesResp, m.filesErr
}

func setupTestServer(mock *mockBackend) http.Handler {
	srv := server.NewServer(log.Default())
	return srv.CreateMux(
		context.Background(),
		server.NewSystemRouter(mock),
		server.NewEventRouter(mock),
		server.NewWorkspaceRouter(mock),
		server.NewMCPRouter(mock),
		server.NewSessionRouter(mock),
	)
}

func TestQuotaRoute(t *testing.T) {
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	t.Run("reports quota", func(t *testing.T) {
		mock := &mockBackend{modelQuotaResp: &api.ModelQuota{
			Model:             "antigravity/gemini-pro",
			RemainingFraction: 0.75,
			ResetAt:           reset,
		}}
		handler := setupTestServer(mock)

		req := httptest.NewRequest(http.MethodGet, "/v1/quotas?model=antigravity/gemini-pro", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("quota status = %d, want 200", rec.Code)
		}
		var quota api.ModelQuota
		if err := json.Unmarshal(rec.Body.Bytes(), &quota); err != nil {
			t.Fatalf("unmarshal quota: %v", err)
		}
		if quota.RemainingFraction != 0.75 {
			t.Errorf("RemainingFraction = %v, want 0.75", quota.RemainingFraction)
		}
	})

	t.Run("no content without quota", func(t *testing.T) {
		handler := setupTestServer(&mockBackend{})

		req := httptest.NewRequest(http.MethodGet, "/v1/quotas?model=openai/gpt-5.5", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("quota status = %d, want 204", rec.Code)
		}
	})

	t.Run("missing model parameter", func(t *testing.T) {
		handler := setupTestServer(&mockBackend{})

		req := httptest.NewRequest(http.MethodGet, "/v1/quotas", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("quota status = %d, want 400", rec.Code)
		}
	})
}

func TestMuxFallbackRouteNotFound(t *testing.T) {
	mock := &mockBackend{}
	handler := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/v1/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var errResp api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Code != "route_not_found" {
		t.Errorf("code = %q, want route_not_found", errResp.Code)
	}
}

func TestSystemRouterHealthAndShutdown(t *testing.T) {
	mock := &mockBackend{
		healthResp: &api.HealthResponse{
			PID:        1234,
			StartedAt:  time.Now().UTC(),
			Workspaces: []string{"/workspace"},
		},
	}
	handler := setupTestServer(mock)

	// Health
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var health api.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	if health.PID != 1234 || len(health.Workspaces) != 1 {
		t.Errorf("health = %+v", health)
	}

	// Shutdown
	req = httptest.NewRequest(http.MethodPost, "/v1/shutdown", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("shutdown status = %d, want 200", rec.Code)
	}
}

func TestWorkspaceRouterEndpoints(t *testing.T) {
	mock := &mockBackend{
		wsResp: &api.Workspace{Name: "ts-ws", RootDir: "/ws"},
		agentsResp: []api.Agent{
			{Name: "main", DisplayName: "Main Agent"},
		},
		agentResp: &api.Agent{Name: "main", DisplayName: "Main Agent"},
		commandsResp: []api.Command{
			{Name: "refactor", DisplayName: "Refactor Code"},
		},
		commandResp: &api.Command{Name: "refactor", DisplayName: "Refactor Code"},
		skillsResp: []api.Skill{
			{Name: "git-release", DisplayName: "Git Release"},
		},
		skillResp: &api.Skill{Name: "git-release", DisplayName: "Git Release"},
		modelsResp: []api.Model{
			{ID: "anthropic/claude-3-5-sonnet", Name: "claude-3-5-sonnet"},
		},
		modelResp: &api.Model{ID: "anthropic/claude-3-5-sonnet", Name: "claude-3-5-sonnet"},
	}
	handler := setupTestServer(mock)

	// Workspace
	req := httptest.NewRequest(http.MethodGet, "/v1/workspace", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get workspace status = %d", rec.Code)
	}

	// Reload Workspace
	req = httptest.NewRequest(http.MethodPost, "/v1/workspace/reload", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !mock.reloadWsCalled {
		t.Fatalf("reload workspace status = %d, called = %v", rec.Code, mock.reloadWsCalled)
	}

	// Agents
	req = httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list agents status = %d", rec.Code)
	}

	// Get Agent
	req = httptest.NewRequest(http.MethodGet, "/v1/agents/main", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get agent status = %d", rec.Code)
	}

	// Commands
	req = httptest.NewRequest(http.MethodGet, "/v1/commands", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list commands status = %d", rec.Code)
	}

	// Get Command
	req = httptest.NewRequest(http.MethodGet, "/v1/commands/refactor", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get command status = %d", rec.Code)
	}

	// Skills
	req = httptest.NewRequest(http.MethodGet, "/v1/skills", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list skills status = %d", rec.Code)
	}

	// Get Skill
	req = httptest.NewRequest(http.MethodGet, "/v1/skills/git-release", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get skill status = %d", rec.Code)
	}

	// Models
	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list models status = %d", rec.Code)
	}

	// Get Model wildcard {id...}
	req = httptest.NewRequest(http.MethodGet, "/v1/models/anthropic/claude-3-5-sonnet", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get model status = %d", rec.Code)
	}

	// Permissions
	mock.permsResp = &api.PermissionsResponse{
		Project: api.Permissions{Allow: []string{"bash(go *)"}},
		Global:  api.Permissions{Deny: []string{"bash(rm *)"}},
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/permissions", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get permissions status = %d", rec.Code)
	}

	// Add Permission Rule
	req = httptest.NewRequest(http.MethodPost, "/v1/permissions", strings.NewReader(`{"scope":"project","effect":"allow","rule":"bash(ls *)"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post permission rule status = %d", rec.Code)
	}

	// Remove Permission Rule
	req = httptest.NewRequest(http.MethodDelete, "/v1/permissions", strings.NewReader(`{"scope":"project","effect":"allow","rule":"bash(ls *)"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete permission rule status = %d", rec.Code)
	}
}

func TestWorkspaceRouterToolsEndpoint(t *testing.T) {
	mock := &mockBackend{
		toolsResp: []api.Tool{
			{
				Name:        "view",
				DisplayName: "View File",
				Namespace:   "system",
				Scope:       "Builtin",
				Annotations: api.ToolAnnotation{IsReadOnly: true, IsIdempotent: true},
				Tokens:      180,
			},
			{
				Name:        "linter",
				Description: "Lint the workspace",
				Namespace:   "workspace",
				Scope:       "Workspace",
				Command:     []string{"golangci-lint", "run"},
				Annotations: api.ToolAnnotation{IsDangerous: true, UserHint: "Lint workspace"},
				Status:      api.ToolStatusDeferred,
			},
		},
	}
	handler := setupTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/v1/workspace/tools?agent=main", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tools status = %d, want 200", rec.Code)
	}
	if mock.toolsAgent != "main" {
		t.Errorf("agent query param = %q, want main", mock.toolsAgent)
	}
	var tools []api.Tool
	if err := json.Unmarshal(rec.Body.Bytes(), &tools); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "view" || tools[1].Name != "linter" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].Scope != "Builtin" || !tools[0].Annotations.IsReadOnly || tools[0].Tokens != 180 {
		t.Errorf("tools[0] = %+v", tools[0])
	}
	if !slices.Equal(tools[1].Command, []string{"golangci-lint", "run"}) || !tools[1].Annotations.IsDangerous {
		t.Errorf("tools[1] = %+v", tools[1])
	}
	if tools[1].Status != api.ToolStatusDeferred {
		t.Errorf("tools[1].Status = %q, want %q", tools[1].Status, api.ToolStatusDeferred)
	}

	// Backend failures surface as an API error envelope.
	mock.toolsErr = &api.APIError{StatusCode: http.StatusInternalServerError, Code: "internal", Message: "workspace unavailable"}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspace/tools", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list tools error status = %d, want 500", rec.Code)
	}
	var errResp api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Code != "internal" {
		t.Errorf("error code = %q, want internal", errResp.Code)
	}
}

func TestSessionRouterEndpoints(t *testing.T) {
	mock := &mockBackend{
		createSessResp:    &api.Session{ID: "sess-1", Title: "Test"},
		createSessCreated: true,
		getSessResp:       &api.Session{ID: "sess-1", Title: "Test"},
		timelineResp: []api.TimelineEntry{
			{ID: "entry-1", SessionID: "sess-1", Kind: api.TimelineKindMessage},
		},
	}
	handler := setupTestServer(mock)

	// Create Session
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{"title":"Test"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session status = %d, want 201", rec.Code)
	}

	// Preview Session
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions/preview", strings.NewReader(`{"config":{"agent":"main"}}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview session status = %d, want 200", rec.Code)
	}

	// Get Session
	req = httptest.NewRequest(http.MethodGet, "/v1/sessions/sess-1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get session status = %d", rec.Code)
	}

	// Timeline
	req = httptest.NewRequest(http.MethodGet, "/v1/sessions/sess-1/timeline", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status = %d", rec.Code)
	}

	// Send
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions/sess-1/send", strings.NewReader(`{"content":"hello"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send status = %d, want 202", rec.Code)
	}
}

func TestWriteErrorFormatting(t *testing.T) {
	rec := httptest.NewRecorder()
	server.WriteError(rec, &api.APIError{
		StatusCode: http.StatusNotFound,
		Code:       "session_not_found",
		Message:    "Session was not found",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var errResp api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errResp.Code != "session_not_found" || errResp.Message != "Session was not found" {
		t.Errorf("errResp = %+v", errResp)
	}
}

func TestMCPRouterListAndMutations(t *testing.T) {
	mock := &mockBackend{
		mcpsResp: []api.MCPServer{
			{
				Name:      "github",
				Namespace: "workspace",
				Scope:     "Workspace",
				Path:      ".agents/github.mcp.yaml",
				Transport: "stdio",
				Command:   []string{"npx", "-y", "@modelcontextprotocol/server-github"},
				Status:    api.MCPStatusConnected,
				Tools: []api.MCPToolSummary{
					{Name: "github__create_issue", RawName: "create_issue", Allowed: true},
					{Name: "github__delete_repo", RawName: "delete_repo"},
				},
			},
		},
		restartMCPServer: api.MCPServer{Name: "github", Transport: "stdio", Status: api.MCPStatusConnected},
		authMCPResp:      api.MCPAuthResponse{AuthURL: "https://example.com/authorize", Status: api.MCPStatusAuthRequired},
	}
	handler := setupTestServer(mock)

	// List
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspace/mcps", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list mcps status = %d, want 200", rec.Code)
	}
	var listed []api.MCPServer
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal mcps: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "github" || listed[0].Status != api.MCPStatusConnected {
		t.Fatalf("listed mcps = %+v", listed)
	}
	if len(listed[0].Tools) != 2 || listed[0].Tools[1].Allowed {
		t.Errorf("prohibited tool should be listed with allowed=false: %+v", listed[0].Tools)
	}

	// Restart
	mock.restartMCPServer.Status = api.MCPStatusError
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/github/restart", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("restart status = %d, want 200", rec.Code)
	}
	if mock.restartMCPName != "github" {
		t.Errorf("restart name = %q, want github", mock.restartMCPName)
	}
	var restarted api.MCPServer
	if err := json.Unmarshal(rec.Body.Bytes(), &restarted); err != nil {
		t.Fatalf("unmarshal restarted server: %v", err)
	}
	if restarted.Status != api.MCPStatusError {
		t.Errorf("restarted status = %q, want %q", restarted.Status, api.MCPStatusError)
	}

	// Auth
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/github/auth", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status = %d, want 200", rec.Code)
	}
	if mock.authMCPName != "github" {
		t.Errorf("auth name = %q, want github", mock.authMCPName)
	}
	var auth api.MCPAuthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &auth); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if auth.AuthURL != "https://example.com/authorize" || auth.Status != api.MCPStatusAuthRequired {
		t.Errorf("auth response = %+v", auth)
	}

	// Unknown server surfaces the backend's 404.
	mock.restartMCPErr = &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: `MCP server "nope" not found in workspace`}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/nope/restart", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restart unknown server status = %d, want 404", rec.Code)
	}
}

// TestMCPRouterAuthCallbackEndpoint covers the endpoint the REPL posts a pasted
// redirect URL to when the browser could not reach the daemon's loopback listener.
// A paste that reaches an in-flight flow answers 204 with no body, since there is
// nothing left to report; everything the flow rejects surfaces as an error.
func TestMCPRouterAuthCallbackEndpoint(t *testing.T) {
	const pasted = "http://localhost:56014/callback?code=abc123&state=xyz"
	mock := &mockBackend{}
	handler := setupTestServer(mock)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/github/auth/callback",
		strings.NewReader(`{"callbackUrl":"`+pasted+`"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("callback status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("callback body = %q, want an empty body", rec.Body.String())
	}
	if mock.authCallbackName != "github" {
		t.Errorf("callback name = %q, want github", mock.authCallbackName)
	}
	if mock.authCallbackReq.CallbackURL != pasted {
		t.Errorf("callback URL = %q, want %q", mock.authCallbackReq.CallbackURL, pasted)
	}

	// A backend rejection keeps its status and message.
	mock.authCallbackErr = &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: `MCP server "nope" not found in workspace`}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/nope/auth/callback",
		strings.NewReader(`{"callbackUrl":"code=abc"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("callback status for an unknown server = %d, want 404", rec.Code)
	}
	var failed api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &failed); err != nil {
		t.Fatalf("unmarshal callback error: %v", err)
	}
	if failed.Code != "not_found" || !strings.Contains(failed.Message, "nope") {
		t.Errorf("callback error = %+v, want the backend's not-found response", failed)
	}

	// A malformed body never reaches the backend.
	mock.authCallbackErr = nil
	mock.authCallbackName = ""
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/github/auth/callback", strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("callback status for a malformed body = %d, want 400", rec.Code)
	}
	if mock.authCallbackName != "" {
		t.Errorf("callback name = %q, want the backend untouched for a malformed body", mock.authCallbackName)
	}
}

func TestMCPRouterPromptResourceAndPolicyEndpoints(t *testing.T) {
	mock := &mockBackend{
		mcpPromptResp: api.MCPPromptResponse{
			Description: "Review a pull request",
			Messages:    []api.MCPPromptMessage{{Role: api.RoleUser, Text: "Review PR 42"}},
		},
		mcpResourceResp: api.MCPResourceResponse{URI: "file://CONTRIBUTING.md", MIMEType: "text/markdown", Text: "# Contributing"},
		allowPolicyServer: api.MCPServer{
			Name:      "github",
			Transport: "stdio",
			Status:    api.MCPStatusConnected,
			Resources: []api.MCPResourceSummary{{Name: "contributing", URI: "file://CONTRIBUTING.md", Allowed: true}},
		},
	}
	handler := setupTestServer(mock)

	// Render an allowed prompt.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/v1/workspace/mcps/github/prompts/review-pr",
		strings.NewReader(`{"args":{"pr":"42"}}`),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("prompt status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if mock.mcpPromptServer != "github" || mock.mcpPromptName != "review-pr" {
		t.Errorf("prompt target = %q/%q", mock.mcpPromptServer, mock.mcpPromptName)
	}
	if mock.mcpPromptReq.Args["pr"] != "42" || mock.mcpPromptReq.AllowProhibited {
		t.Errorf("prompt request = %+v", mock.mcpPromptReq)
	}
	var promptResp api.MCPPromptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &promptResp); err != nil {
		t.Fatalf("unmarshal prompt response: %v", err)
	}
	if len(promptResp.Messages) != 1 || promptResp.Messages[0].Text != "Review PR 42" {
		t.Errorf("prompt response = %+v", promptResp)
	}

	// A prompt prohibited by spec.policies is rejected with 403 unless the user
	// authorizes a single use.
	mock.mcpPromptErr = &api.APIError{
		StatusCode: http.StatusForbidden,
		Code:       "mcp_policy_prohibited",
		Message:    `MCP prompt "danger" is prohibited by spec.policies.prompts in .agents/github.mcp.yaml`,
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/github/prompts/danger", strings.NewReader(`{}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("prohibited prompt status = %d, want 403", rec.Code)
	}
	var policyErr api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &policyErr); err != nil {
		t.Fatalf("unmarshal policy error: %v", err)
	}
	if policyErr.Code != "mcp_policy_prohibited" || !strings.Contains(policyErr.Message, "spec.policies.prompts") {
		t.Errorf("policy error = %+v", policyErr)
	}

	mock.mcpPromptErr = nil
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/v1/workspace/mcps/github/prompts/danger",
		strings.NewReader(`{"allowProhibited":true}`),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized one-off prompt status = %d, want 200", rec.Code)
	}
	if !mock.mcpPromptReq.AllowProhibited {
		t.Errorf("expected allowProhibited to reach the backend")
	}

	// Read a resource.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/v1/workspace/mcps/github/resources",
		strings.NewReader(`{"uri":"file://CONTRIBUTING.md"}`),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("resource status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if mock.mcpResourceServer != "github" || mock.mcpResourceReq.URI != "file://CONTRIBUTING.md" {
		t.Errorf("resource target = %q %+v", mock.mcpResourceServer, mock.mcpResourceReq)
	}
	var resourceResp api.MCPResourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resourceResp); err != nil {
		t.Fatalf("unmarshal resource response: %v", err)
	}
	if resourceResp.Text != "# Contributing" || resourceResp.MIMEType != "text/markdown" {
		t.Errorf("resource response = %+v", resourceResp)
	}

	// A missing body on the resource endpoint is a client error.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/workspace/mcps/github/resources", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resource without body status = %d, want 400", rec.Code)
	}

	// Permit a prohibited item in the manifest.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/v1/workspace/mcps/github/policies/allow",
		strings.NewReader(`{"kind":"resource","value":"file://CONTRIBUTING.md"}`),
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("policies/allow status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if mock.allowPolicyName != "github" ||
		mock.allowPolicyReq.Kind != api.MCPAllowPolicyKindResource ||
		mock.allowPolicyReq.Value != "file://CONTRIBUTING.md" {
		t.Errorf("allow policy request = %q %+v", mock.allowPolicyName, mock.allowPolicyReq)
	}
	var updated api.MCPServer
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal updated server: %v", err)
	}
	if len(updated.Resources) != 1 || !updated.Resources[0].Allowed {
		t.Errorf("updated server = %+v", updated)
	}
}

func TestMCPRouterEmptyListSerializesAsArray(t *testing.T) {
	handler := setupTestServer(&mockBackend{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/workspace/mcps", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list mcps status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("empty MCP list body = %q, want []", body)
	}
}

func TestWorkspaceFilesEndpoint(t *testing.T) {
	mock := &mockBackend{
		filesResp: []api.WorkspaceFile{
			{Path: "app.go", Name: "app.go", IsDir: false, Allowed: true},
		},
	}
	handler := setupTestServer(mock)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/workspace/files?q=app&session_id=sess-123&limit=10&hidden=false", nil)
	req.Header.Set("X-TaskSmith-CWD", "/test/workspace")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	if mock.filesReq.Query != "app" {
		t.Errorf("filesReq.Query = %q, want 'app'", mock.filesReq.Query)
	}
	if mock.filesReq.SessionID != "sess-123" {
		t.Errorf("filesReq.SessionID = %q, want 'sess-123'", mock.filesReq.SessionID)
	}
	if mock.filesReq.Limit != 10 {
		t.Errorf("filesReq.Limit = %d, want 10", mock.filesReq.Limit)
	}
	if mock.filesReq.IncludeHidden == nil || *mock.filesReq.IncludeHidden != false {
		t.Errorf("filesReq.IncludeHidden = %v, want false", mock.filesReq.IncludeHidden)
	}

	var files []api.WorkspaceFile
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(files) != 1 || files[0].Path != "app.go" {
		t.Errorf("files = %+v", files)
	}
}
