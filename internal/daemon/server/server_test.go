package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	modelsResp        []api.Model
	modelsErr         error
	modelResp         *api.Model
	modelErr          error
	permsResp         *api.PermissionsResponse
	permsErr          error
	createSessResp    *api.Session
	createSessCreated bool
	createSessErr     error
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

func (m *mockBackend) ListModels(ctx context.Context, workspace string) ([]api.Model, error) {
	return m.modelsResp, m.modelsErr
}

func (m *mockBackend) GetModel(ctx context.Context, workspace, id string) (*api.Model, error) {
	return m.modelResp, m.modelErr
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

func setupTestServer(mock *mockBackend) http.Handler {
	srv := server.NewServer(log.Default())
	return srv.CreateMux(
		context.Background(),
		server.NewSystemRouter(mock),
		server.NewEventRouter(mock),
		server.NewWorkspaceRouter(mock),
		server.NewSessionRouter(mock),
	)
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
