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
	modelsResp        []api.Model
	modelsErr         error
	modelResp         *api.Model
	modelErr          error
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
	permsResp         []api.PermissionRequest
	permsErr          error
	respPermErr       error
	getModeResp       api.PermissionMode
	getModeErr        error
	setModeResp       api.PermissionMode
	setModeErr        error
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

func (m *mockBackend) AddSubscriber(ctx context.Context, subscriberID string, topics ...string) (*broker.Subscriber, []string, error) {
	return m.connectSub, m.connectTopics, m.connectErr
}

func (m *mockBackend) RemoveSubscriber(sub *broker.Subscriber) {}

func (m *mockBackend) FormatEvent(msg broker.Message) (api.Event, error) {
	if payload, ok := msg.Event.(api.EventPayload); ok {
		return api.Event{
			Type:    api.EventType(payload.EventType()),
			Topic:   msg.Topic,
			Payload: payload,
		}, nil
	}
	return api.Event{}, nil
}

func (m *mockBackend) Subscribe(ctx context.Context, subscriberID string, topics ...string) ([]string, error) {
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

func (m *mockBackend) ListModels(ctx context.Context, workspace string) ([]api.Model, error) {
	return m.modelsResp, m.modelsErr
}

func (m *mockBackend) GetModel(ctx context.Context, workspace, id string) (*api.Model, error) {
	return m.modelResp, m.modelErr
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

func (m *mockBackend) PendingPermissions(ctx context.Context, workspace, id string) ([]api.PermissionRequest, error) {
	return m.permsResp, m.permsErr
}

func (m *mockBackend) RespondPermission(ctx context.Context, workspace, id string, dec api.PermissionDecision) error {
	return m.respPermErr
}

func (m *mockBackend) GetMode(ctx context.Context, workspace, id string) (api.PermissionMode, error) {
	return m.getModeResp, m.getModeErr
}

func (m *mockBackend) SetMode(ctx context.Context, workspace, id string, mode api.PermissionMode) (api.PermissionMode, error) {
	return m.setModeResp, m.setModeErr
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
}

func TestSessionRouterEndpoints(t *testing.T) {
	mock := &mockBackend{
		createSessResp:    &api.Session{ID: "sess-1", Title: "Test"},
		createSessCreated: true,
		getSessResp:       &api.Session{ID: "sess-1", Title: "Test"},
		timelineResp: []api.TimelineEntry{
			{ID: "entry-1", SessionID: "sess-1", Kind: api.TimelineKindMessage},
		},
		getModeResp: api.ModeDefault,
		setModeResp: api.ModePlan,
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

	// Send (non-blocking)
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions/sess-1/send", strings.NewReader(`{"content":"hello"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send non-blocking status = %d, want 202", rec.Code)
	}

	// Send (blocking)
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions/sess-1/send", strings.NewReader(`{"content":"hello","wait":true}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("send blocking status = %d, want 200", rec.Code)
	}

	// Mode GET & PUT
	req = httptest.NewRequest(http.MethodGet, "/v1/sessions/sess-1/mode", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get mode status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPut, "/v1/sessions/sess-1/mode", strings.NewReader(`{"mode":"plan"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set mode status = %d", rec.Code)
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
