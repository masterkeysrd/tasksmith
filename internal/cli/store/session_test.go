package store

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

func TestDiagnosticNotices(t *testing.T) {
	ResetSession()
	ClearTranscript()
	defer ResetSession()
	defer ClearTranscript()
	sess := &api.Session{ID: "one", Config: api.SessionConfig{Agent: "agent", Model: "model"}, Diagnostics: []api.SessionDiagnostic{{Code: "temperature_unsupported", Message: "Temperature omitted"}}}
	showSessionDiagnostics(sess, true)
	sess.Diagnostics = append(sess.Diagnostics, sess.Diagnostics[0])
	showSessionDiagnostics(sess, false)
	items := GetTranscript().Items
	if items == nil || len(*items) != 1 || (*items)[0].Kind != TranscriptKindNotice || (*items)[0].Data.(NoticeData) != (NoticeData{Text: "Warning: Temperature omitted", Severity: NoticeSeverityWarning}) {
		t.Fatalf("notices: %+v", items)
	}
	ClearScreen()
	showSessionDiagnostics(sess, false)
	if items := GetTranscript().Items; items != nil && len(*items) != 0 {
		t.Fatalf("dismissed notice returned: %+v", *items)
	}
	sess.Config.Model = "other"
	showSessionDiagnostics(sess, false)
	if items := GetTranscript().Items; items == nil || len(*items) != 1 {
		t.Fatalf("changed model notice missing: %+v", items)
	}
	sess.Diagnostics = nil
	showSessionDiagnostics(sess, false)
	sess.Diagnostics = []api.SessionDiagnostic{{Code: "temperature_unsupported", Message: "Temperature omitted"}}
	showSessionDiagnostics(sess, false)
	if items := GetTranscript().Items; items == nil || len(*items) != 2 {
		t.Fatalf("reappearing notice missing: %+v", items)
	}
}

func TestSessionStoreBasic(t *testing.T) {
	ResetSession()
	defer ResetSession()

	s := GetSession()
	if s.Config.Mode != api.ModeDefault {
		t.Fatalf("expected ModeDefault initially, got %q", s.Config.Mode)
	}
	if s.Ready {
		t.Fatal("expected Ready to be false initially")
	}

	SetMode(api.ModePlan)
	if GetSession().Config.Mode != api.ModePlan {
		t.Fatalf("expected ModePlan, got %q", GetSession().Config.Mode)
	}

	SetModelInfo("Claude 3.7", "high")
	s = GetSession()
	if s.ModelLabel != "Claude 3.7" || s.Thinking != "high" || !s.Ready {
		t.Fatalf("unexpected model info: %+v", s)
	}

	// SetSession with empty mode should default to ModeDefault
	sess := &api.Session{
		ID: "sess-1",
		Config: api.SessionConfig{
			Agent: "coding",
			Model: "claude-3-7",
		},
	}
	SetSession(sess)
	s = GetSession()
	if s.Session != sess || s.Config.Mode != api.ModeDefault {
		t.Fatalf("unexpected session after SetSession: %+v", s)
	}

	// SetSession with explicit mode
	sess.Config.Mode = api.ModeAcceptEdits
	SetSession(sess)
	if GetSession().Config.Mode != api.ModeAcceptEdits {
		t.Fatalf("expected ModeAcceptEdits, got %q", GetSession().Config.Mode)
	}

	ResetSession()
	s = GetSession()
	if s.Config.Mode != api.ModeDefault || s.Session != nil || s.Ready {
		t.Fatalf("expected clean state after ResetSession, got %+v", s)
	}
}

func TestResolvedSessionModelDisplay(t *testing.T) {
	sess := &api.Session{
		Config: api.SessionConfig{Model: "unavailable-first"},
		Model:  &api.Model{ID: "provider/available", Label: "Available model", ThinkingDefault: &api.ThinkingSetting{Type: "effort", Effort: "medium"}},
	}
	label, thinking := modelDisplay(sess.Model, sess.Config.Thinking)
	if label != "Available model" || thinking != "medium" {
		t.Fatalf("resolved model display: %q / %q", label, thinking)
	}
	sess.Config.Thinking = &api.ThinkingSetting{Type: "effort", Effort: "high"}
	_, thinking = modelDisplay(sess.Model, sess.Config.Thinking)
	if thinking != "high" {
		t.Fatalf("session override: %q", thinking)
	}
}

func TestModelDisplayThinkingCapabilityFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		thinking []api.ThinkingCapability
		want     string
	}{
		{"none", nil, ThinkingProviderDefault},
		{"effort default", []api.ThinkingCapability{{Type: "effort", Default: "high"}}, "high"},
		{"isDefault budget", []api.ThinkingCapability{{Type: "budget_tokens", Default: "4096", IsDefault: true}}, "budget 4096 tokens"},
		{"isDefault toggle", []api.ThinkingCapability{{Type: "toggle", IsDefault: true}}, "thinking on"},
		{"isDefault adaptive", []api.ThinkingCapability{{Type: "adaptive", IsDefault: true}}, "adaptive"},
		// An isDefault strategy with nothing usable to apply means thinking stays
		// unset, so no other strategy's default is reported in its place.
		{"isDefault budget without range", []api.ThinkingCapability{
			{Type: "budget_tokens", Max: 8000, IsDefault: true},
			{Type: "effort", Default: "low"},
		}, ThinkingProviderDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, thinking := modelDisplay(&api.Model{ID: "provider/model", Thinking: tc.thinking}, nil)
			if thinking != tc.want {
				t.Fatalf("thinking = %q, want %q", thinking, tc.want)
			}
		})
	}
}

func TestModelDisplayWithoutModel(t *testing.T) {
	label, thinking := modelDisplay(nil, nil)
	if label != "default" || thinking != ThinkingProviderDefault {
		t.Fatalf("expected default/provider default, got %q/%q", label, thinking)
	}
	_, thinking = modelDisplay(nil, &api.ThinkingSetting{Type: "effort", Effort: "auto"})
	if thinking != "auto" {
		t.Fatalf("expected explicit auto effort, got %q", thinking)
	}
	ApplyThinkingChange(nil)
	if got := GetSession(); got.Thinking != ThinkingProviderDefault || got.Config.Thinking != nil {
		t.Fatalf("expected provider default after reset, got %+v", got)
	}
	ResetSession()
}

func TestDraftModelUsesDaemonSelection(t *testing.T) {
	ResetSession()
	defer ResetSession()
	cfg := api.SessionConfig{Agent: "custom-agent"}
	session.Set(func(s SessionState) SessionState { s.Config = cfg; return s })
	rt := &mockSessionRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/sessions/preview" || req.Method != http.MethodPost {
			t.Fatalf("unexpected preview request: %s %s", req.Method, req.URL)
		}
		var payload api.PreviewSessionRequest
		_ = json.NewDecoder(req.Body).Decode(&payload)
		if payload.Config.Agent != "custom-agent" {
			t.Fatalf("unexpected preview payload: %+v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"model":{"id":"provider/available","label":"Available model"}}`)), Header: make(http.Header)}, nil
	}}
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	updateDraftModel(context.Background(), c, cfg)
	if got := GetSession(); got.ModelLabel != "Available model" {
		t.Fatalf("draft model = %q", got.ModelLabel)
	}
}

func TestStaleDraftModelResponseIsIgnored(t *testing.T) {
	ResetSession()
	defer ResetSession()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	cfg := api.SessionConfig{Agent: "old"}
	session.Set(func(s SessionState) SessionState { s.Config = cfg; return s })
	rt := &mockSessionRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"model":{"id":"provider/old"}}`)), Header: make(http.Header)}, nil
	}}
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	go func() { updateDraftModel(context.Background(), c, cfg); close(done) }()
	<-started
	session.Set(func(s SessionState) SessionState { s.Config.Agent = "new"; s.ModelLabel = "new model"; return s })
	close(release)
	<-done
	if got := GetSession().ModelLabel; got != "new model" {
		t.Fatalf("stale preview replaced model: %q", got)
	}
}

func TestInitSessionWithWorkspaceWarnings(t *testing.T) {
	ResetApp()
	defer ResetApp()
	ResetSession()
	defer ResetSession()

	rt := &mockSessionRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet && req.URL.Path == "/v1/workspace" {
				ws := api.Workspace{
					Name:     "test-ws",
					RootDir:  "/workspace",
					Warnings: []string{"warning 1", "warning 2"},
				}
				data, _ := json.Marshal(ws)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(data)),
					Header:     make(http.Header),
				}
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	ctx := context.Background()
	InitSession(ctx, c, InitSessionOptions{
		Agent: "test-agent",
		Model: "test-model",
	})

	hd := CurrentHeaderData()
	if len(hd.Warnings) != 2 || hd.Warnings[0] != "warning 1" || hd.Warnings[1] != "warning 2" {
		t.Fatalf("unexpected Warnings in CurrentHeaderData after InitSession: %v", hd.Warnings)
	}
}

func TestInitSessionDraftSandboxBadge(t *testing.T) {
	newClient := func(defaultMode api.SandboxMode) *client.Client {
		rt := &mockSessionRoundTripper{
			handler: func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodGet && req.URL.Path == "/v1/workspace" {
					ws := api.Workspace{Name: "test-ws", RootDir: "/workspace", SandboxMode: defaultMode}
					data, _ := json.Marshal(ws)
					resp := &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader(data)),
						Header:     make(http.Header),
					}
					resp.Header.Set("Content-Type", "application/json")
					return resp, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			},
		}
		c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
		if err != nil {
			t.Fatalf("client.New: %v", err)
		}
		return c
	}

	ResetApp()
	defer ResetApp()
	ResetSession()
	defer ResetSession()

	InitSession(context.Background(), newClient("native"), InitSessionOptions{Agent: "test-agent", Model: "test-model"})
	if got := GetSession().SandboxMode; got != "native" {
		t.Errorf("draft SandboxMode = %q, want workspace default %q", got, "native")
	}

	InitSession(context.Background(), newClient("local"), InitSessionOptions{Agent: "test-agent", Model: "test-model"})
	if got := GetSession().SandboxMode; got != "local" {
		t.Errorf("draft SandboxMode = %q, want workspace default %q", got, "local")
	}

	InitSession(context.Background(), newClient("native"), InitSessionOptions{Agent: "test-agent", Model: "test-model", Sandbox: "local"})
	if got := GetSession().SandboxMode; got != "local" {
		t.Errorf("draft SandboxMode = %q, want explicit request %q", got, "local")
	}
}

func TestNextPermissionMode(t *testing.T) {
	tests := []struct {
		current api.PermissionMode
		want    api.PermissionMode
	}{
		{"", api.ModeAcceptEdits},
		{api.ModeDefault, api.ModeAcceptEdits},
		{api.ModeAcceptEdits, api.ModePlan},
		{api.ModePlan, api.ModeAsk},
		{api.ModeAsk, api.ModeAuto},
		{api.ModeAuto, api.ModeDefault},
		{api.PermissionMode("other"), api.ModeDefault},
	}

	for _, tt := range tests {
		if got := NextPermissionMode(tt.current); got != tt.want {
			t.Errorf("NextPermissionMode(%q) = %q, want %q", tt.current, got, tt.want)
		}
	}
}

func TestCycleMode(t *testing.T) {
	ResetSession()
	defer ResetSession()

	if GetSession().Config.Mode != api.ModeDefault {
		t.Fatalf("expected initial ModeDefault, got %q", GetSession().Config.Mode)
	}

	expectedSequence := []api.PermissionMode{
		api.ModeAcceptEdits,
		api.ModePlan,
		api.ModeAsk,
		api.ModeAuto,
		api.ModeDefault,
	}

	for _, want := range expectedSequence {
		got := CycleMode()
		if got != want {
			t.Fatalf("CycleMode() = %q, want %q", got, want)
		}
		if GetSession().Config.Mode != want {
			t.Fatalf("GetSession().Config.Mode = %q, want %q", GetSession().Config.Mode, want)
		}
	}
}

func TestSetSessionMode(t *testing.T) {
	ResetSession()
	defer ResetSession()

	sess := &api.Session{
		ID:     "sess-1",
		Config: api.SessionConfig{Mode: api.ModeDefault},
	}
	SetSession(sess)

	SetSessionMode(api.ModePlan)
	if got := GetSession().Config.Mode; got != api.ModePlan {
		t.Fatalf("expected ModePlan, got %q", got)
	}
	if got := GetSession().Session.Config.Mode; got != api.ModePlan {
		t.Fatalf("expected session.Config.Mode ModePlan, got %q", got)
	}
}

func TestTasksStore(t *testing.T) {
	ResetSession()
	defer ResetSession()

	if tasks := GetTasks(); len(tasks) != 0 {
		t.Fatalf("expected 0 initial tasks, got %d", len(tasks))
	}
	if running := GetRunningTasks(); len(running) != 0 {
		t.Fatalf("expected 0 running tasks, got %d", len(running))
	}

	task1 := api.Task{
		ID:      "t-1",
		Command: "sleep 10",
		Status:  api.TaskStatusRunning,
	}
	UpsertTask(task1)

	tasks := GetTasks()
	if len(tasks) != 1 || tasks[0].ID != "t-1" || tasks[0].Status != api.TaskStatusRunning {
		t.Fatalf("unexpected tasks after UpsertTask: %+v", tasks)
	}

	running := GetRunningTasks()
	if len(running) != 1 || running[0].ID != "t-1" {
		t.Fatalf("unexpected running tasks: %+v", running)
	}

	// Update task1 to completed
	task1.Status = api.TaskStatusCompleted
	UpsertTask(task1)

	tasks = GetTasks()
	if len(tasks) != 1 || tasks[0].Status != api.TaskStatusCompleted {
		t.Fatalf("unexpected tasks after completion update: %+v", tasks)
	}

	running = GetRunningTasks()
	if len(running) != 0 {
		t.Fatalf("expected 0 running tasks after completion, got %d", len(running))
	}

	// Upsert second task
	task2 := api.Task{
		ID:      "t-2",
		Command: "npm run dev",
		Status:  api.TaskStatusPending,
	}
	UpsertTask(task2)

	tasks = GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	running = GetRunningTasks()
	if len(running) != 1 || running[0].ID != "t-2" {
		t.Fatalf("expected 1 running task (t-2), got %+v", running)
	}

	// Verify NewDraftSession clears tasks
	NewDraftSession("")
	if tasks = GetTasks(); len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after NewDraftSession, got %d", len(tasks))
	}
	if running = GetRunningTasks(); len(running) != 0 {
		t.Fatalf("expected 0 running tasks after NewDraftSession, got %d", len(running))
	}
}

func TestNewDraftSessionClearsUsage(t *testing.T) {
	ResetSession()
	defer ResetSession()

	SetSession(&api.Session{ID: "s-1", Config: api.SessionConfig{Mode: api.ModeDefault}})
	SetUsage(api.SessionUsage{
		SessionID: "s-1",
		Totals:    api.UsageGroup{Calls: 3, Tokens: api.UsageTokens{Input: 1000, Output: 250}},
	})
	if u := GetUsage(); u.Totals.Calls != 3 || u.Totals.Tokens.Input != 1000 {
		t.Fatalf("expected seeded usage, got %+v", u.Totals)
	}

	NewDraftSession("")
	if u := GetUsage(); u.Totals.Calls != 0 || u.Totals.Tokens.Input != 0 || u.Totals.Tokens.Output != 0 {
		t.Fatalf("expected usage cleared after NewDraftSession, got %+v", u.Totals)
	}
}

type mockSessionRoundTripper struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (m *mockSessionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

func TestCycleModeDebounce(t *testing.T) {
	ResetSession()
	defer ResetSession()

	origDelay := modeDebounceDelay
	modeDebounceDelay = 40 * time.Millisecond
	defer func() { modeDebounceDelay = origDelay }()

	var updateRequests []api.UpdateSessionRequest
	var mu sync.Mutex

	rt := &mockSessionRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPatch && strings.Contains(req.URL.Path, "/sessions/") {
				var upReq api.UpdateSessionRequest
				_ = json.NewDecoder(req.Body).Decode(&upReq)
				mu.Lock()
				updateRequests = append(updateRequests, upReq)
				mu.Unlock()
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"id":"sess-1","config":{"mode":"plan"}}`)),
					Header:     make(http.Header),
				}
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	sessMu.Lock()
	activeClient = c
	activeCtx = context.Background()
	sessMu.Unlock()

	SetSession(&api.Session{
		ID:     "sess-1",
		Config: api.SessionConfig{Mode: api.ModeDefault},
	})

	// 1. Rapidly cycle modes: Default -> AcceptEdits -> Plan
	m1 := CycleMode()
	if m1 != api.ModeAcceptEdits || GetSession().Config.Mode != api.ModeAcceptEdits {
		t.Fatalf("expected immediate mode AcceptEdits, got %q", GetSession().Config.Mode)
	}

	// Immediate next cycle (within debounce window)
	m2 := CycleMode()
	if m2 != api.ModePlan || GetSession().Config.Mode != api.ModePlan {
		t.Fatalf("expected immediate mode Plan, got %q", GetSession().Config.Mode)
	}

	// Wait before debounce window elapses: no request should have been sent yet
	time.Sleep(15 * time.Millisecond)
	mu.Lock()
	if len(updateRequests) != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 requests before debounce expires, got %d", len(updateRequests))
	}
	mu.Unlock()

	// Wait for debounce timer to fire (delay is 40ms, total elapsed > 40ms)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(updateRequests)
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	if len(updateRequests) != 1 {
		mu.Unlock()
		t.Fatalf("expected exactly 1 UpdateSession request after debounce, got %d", len(updateRequests))
	}
	if updateRequests[0].Config.Mode != api.ModePlan {
		mu.Unlock()
		t.Fatalf("expected persisted mode to be Plan, got %q", updateRequests[0].Config.Mode)
	}
	updateRequests = nil
	mu.Unlock()

	// 2. Cycle full-circle back to the persisted mode: Plan -> Ask -> Auto -> Default -> AcceptEdits -> Plan
	for range 5 {
		CycleMode()
		time.Sleep(2 * time.Millisecond) // rapid cycling
	}
	if GetSession().Config.Mode != api.ModePlan {
		t.Fatalf("expected cycled back to Plan, got %q", GetSession().Config.Mode)
	}

	// Wait past debounce window
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	if len(updateRequests) != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 requests when cycled full-circle back to persisted mode, got %d", len(updateRequests))
	}
	mu.Unlock()

	// 3. Test with a brand-new session whose Session.Config.Mode is "" (empty string)
	SetSession(&api.Session{
		ID:     "sess-2",
		Config: api.SessionConfig{Mode: ""},
	})
	session.Set(func(s SessionState) SessionState {
		s.Session.Config.Mode = ""
		return s
	})

	// Cycle full-circle back to ModeDefault: Default -> AcceptEdits -> Plan -> Ask -> Auto -> Default
	for range 5 {
		CycleMode()
		time.Sleep(2 * time.Millisecond)
	}
	if GetSession().Config.Mode != api.ModeDefault {
		t.Fatalf("expected cycled back to Default, got %q", GetSession().Config.Mode)
	}

	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	if len(updateRequests) != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 requests when cycling back to default with empty persisted mode, got %d", len(updateRequests))
	}
	mu.Unlock()
}

func TestSwitchSessionSameIDReloadsTranscript(t *testing.T) {
	ResetSession()
	ResetStream()
	ClearTranscript()
	FreezeTranscript()
	defer ResetSession()
	defer ResetStream()
	defer ClearTranscript()

	now := time.Now()
	timelineEntries := []api.TimelineEntry{
		{
			ID:   "e-msg-1",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "msg-1",
				Role:      api.RoleUser,
				CreatedAt: now,
				Content: []api.Block{
					{Type: api.BlockText, Text: "Message 1"},
				},
			},
		},
		{
			ID:   "e-msg-2",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "msg-2",
				Role:      api.RoleAssistant,
				CreatedAt: now.Add(time.Second),
				Content: []api.Block{
					{Type: api.BlockText, Text: "Message 2"},
				},
			},
		},
	}

	rt := &mockSessionRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/v1/sessions/sess-same-id") {
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"id": "sess-same-id",
						"config": {"mode": "default", "agent": "coding", "model": "claude-3-7"}
					}`)),
					Header: make(http.Header),
				}
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/v1/sessions/sess-same-id/timeline") {
				data, _ := json.Marshal(timelineEntries)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(data)),
					Header:     make(http.Header),
				}
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	sessMu.Lock()
	activeClient = c
	activeCtx = context.Background()
	sessMu.Unlock()

	// Initial SwitchSession to sess-same-id
	SwitchSession("sess-same-id")

	// Wait for timeline to load
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		tr := GetTranscript()
		if tr.Items != nil && len(*tr.Items) >= 3 { // Header + 2 messages
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) < 3 {
		t.Fatalf("expected at least 3 transcript items after first switch, got %+v", tr.Items)
	}

	// Verify the active session ID is now sess-same-id
	if GetSession().Session == nil || GetSession().Session.ID != "sess-same-id" {
		t.Fatalf("expected active session sess-same-id, got %+v", GetSession().Session)
	}

	// Now call SwitchSession again with the EXACT SAME session ID (e.g. user re-selects active session in /sessions)
	SwitchSession("sess-same-id")

	// Wait for reload
	deadline = time.Now().Add(time.Second)
	var reloadedItems []TranscriptItem
	for time.Now().Before(deadline) {
		tr = GetTranscript()
		if tr.Items != nil {
			var msgs []TranscriptItem
			for _, it := range *tr.Items {
				if it.Kind == TranscriptKindMessage {
					msgs = append(msgs, it)
				}
			}
			if len(msgs) == 2 {
				reloadedItems = msgs
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	if len(reloadedItems) != 2 {
		t.Fatalf("expected 2 reloaded messages after switching to same session ID, got %d (transcript items: %+v)", len(reloadedItems), GetTranscript().Items)
	}
	if reloadedItems[0].Data.(Message).ID != "msg-1" || reloadedItems[1].Data.(Message).ID != "msg-2" {
		t.Fatalf("unexpected reloaded message IDs: %+v", reloadedItems)
	}
}

func TestSessionContextStateAndHelpers(t *testing.T) {
	ResetSession()
	defer ResetSession()

	ctxState := api.ContextState{
		UsedTokens:    15000,
		MaxTokens:     200000,
		OutputLimit:   32000,
		ReserveTokens: 40000,
		TriggerTokens: 160000,
		FreeTokens:    145000,
		Breakdown: api.ContextBreakdown{
			SystemPrompt: 5000,
			SystemTools:  10000,
		},
	}

	sess := &api.Session{
		ID:      "sess-ctx",
		Context: ctxState,
	}
	SetSession(sess)

	if GetContext().UsedTokens != 15000 {
		t.Errorf("expected GetContext().UsedTokens = 15000, got %d", GetContext().UsedTokens)
	}
	if GetContext().MaxTokens != 200000 {
		t.Errorf("expected GetContext().MaxTokens = 200000, got %d", GetContext().MaxTokens)
	}

	// SendCompact in draft mode (no session) appends "Nothing to compact" notice
	ResetSession()
	SendCompact("")
	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) == 0 {
		t.Fatalf("expected transcript item for SendCompact in draft mode")
	}
	lastItem := (*tr.Items)[len(*tr.Items)-1]
	if lastItem.Kind != TranscriptKindNotice {
		t.Errorf("expected Notice transcript item, got %v", lastItem.Kind)
	}
	if d, ok := lastItem.Data.(NoticeData); !ok || d.Text != "Nothing to compact" {
		t.Errorf("expected 'Nothing to compact' notice, got %+v", lastItem.Data)
	}
}

// The tool cache is keyed on the agent the statuses were classified for, and an
// agent-scoped listing that the daemon cannot serve must still leave the browser
// usable instead of retry-looping.
func TestRefreshWorkspaceToolsCachingAndFallback(t *testing.T) {
	ResetApp()
	t.Cleanup(ResetApp)

	var requests []string
	newClient := func(t *testing.T, scopedOK bool) *client.Client {
		t.Helper()
		rt := &mockSessionRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/workspace/tools" {
				t.Fatalf("unexpected request path: %s", req.URL.Path)
			}
			agent := req.URL.Query().Get("agent")
			requests = append(requests, agent)
			if agent != "" && !scopedOK {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"not_found","message":"agent not found"}}`)),
					Header:     make(http.Header),
				}, nil
			}
			payload, _ := json.Marshal([]api.Tool{{Name: "view", Status: api.ToolStatusEager}})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
		}}
		c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
		if err != nil {
			t.Fatalf("client.New: %v", err)
		}
		return c
	}

	// A fresh cache for the requested agent skips the fetch entirely.
	SetWorkspaceTools([]api.Tool{{Name: "cached"}}, "main")
	if count, err := refreshWorkspaceToolsForce(context.Background(), newClient(t, true), "main", false); err != nil || count != 0 {
		t.Fatalf("fresh cache refresh = (%d, %v), want (0, nil)", count, err)
	}
	if len(requests) != 0 {
		t.Fatalf("fresh cache triggered %d requests, want none", len(requests))
	}

	// A different agent triggers a scoped fetch that replaces the cache.
	count, err := refreshWorkspaceToolsForce(context.Background(), newClient(t, true), "reviewer", false)
	if err != nil || count != 1 {
		t.Fatalf("agent refresh = (%d, %v), want (1, nil)", count, err)
	}
	app := GetApp()
	if app.ToolsAgent != "reviewer" || app.Tools == nil || len(*app.Tools) != 1 {
		t.Fatalf("unexpected cache after scoped fetch: agent=%q tools=%v", app.ToolsAgent, app.Tools)
	}

	// An unresolvable agent falls back to the unscoped catalog but keeps the
	// requested agent as the cache label so the browser stops retrying.
	ResetApp()
	count, err = refreshWorkspaceToolsForce(context.Background(), newClient(t, false), "ghost", false)
	if err != nil {
		t.Fatalf("fallback refresh: %v", err)
	}
	if count != 1 {
		t.Fatalf("fallback refresh count = %d, want 1", count)
	}
	app = GetApp()
	if app.ToolsAgent != "ghost" || app.Tools == nil || len(*app.Tools) != 1 {
		t.Fatalf("unexpected cache after fallback: agent=%q tools=%v", app.ToolsAgent, app.Tools)
	}
	if !app.ToolsFetched {
		t.Fatal("expected fallback to mark the tools as fetched")
	}

	// Forcing a refresh bypasses the fresh-cache check.
	before := len(requests)
	if _, err := refreshWorkspaceToolsForce(context.Background(), newClient(t, true), "ghost", true); err != nil {
		t.Fatalf("forced refresh: %v", err)
	}
	if len(requests) <= before {
		t.Fatal("forced refresh did not hit the daemon")
	}

	// Without a client the refresh reports the missing connection.
	if _, err := refreshWorkspaceToolsForce(context.Background(), nil, "main", true); err == nil {
		t.Fatal("expected error when no daemon connection is available")
	}
}

// mcpInitHandler serves a workspace listing plus an MCP listing whose status can
// change between calls, so a test can watch servers settle while a session
// initializes.
type mcpInitHandler struct {
	mu       sync.Mutex
	servers  []api.MCPServer
	warnings []string
	lists    int
	// startConnecting reports every server as connecting on the first listing,
	// mirroring the background connect a workspace starts when it opens.
	startConnecting bool
}

func (h *mcpInitHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/workspace":
		return sessionJSONResponse(api.Workspace{Name: "test-ws", RootDir: "/workspace", Warnings: h.currentWarnings()})
	case req.Method == http.MethodGet && req.URL.Path == "/v1/workspace/mcps":
		return sessionJSONResponse(h.nextServers())
	default:
		return sessionJSONResponse(map[string]any{})
	}
}

func (h *mcpInitHandler) currentWarnings() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.warnings
}

// nextServers returns the current listing and advances it, reporting servers as
// still connecting on the first look when startConnecting is set.
func (h *mcpInitHandler) nextServers() []api.MCPServer {
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.servers
	h.lists++
	if h.startConnecting && h.lists == 1 && len(h.servers) > 0 {
		connecting := make([]api.MCPServer, len(h.servers))
		copy(connecting, h.servers)
		for i := range connecting {
			connecting[i].Status = api.MCPStatusConnecting
		}
		current = connecting
	}
	return current
}

func sessionJSONResponse(payload any) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     make(http.Header),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

// TestInitSessionAnnouncesMCPServerWarnings checks that servers the daemon reports
// as needing authentication or failing to connect are announced in the timeline
// immediately, while the header keeps only the workspace's own durable warnings.
func TestInitSessionAnnouncesMCPServerWarnings(t *testing.T) {
	ResetApp()
	defer ResetApp()
	ResetSession()
	defer ResetSession()
	ClearTranscript()
	defer ClearTranscript()
	defer SetWorkspaceWarnings(nil)
	defer SetWorkspaceMCPServers(nil)

	handler := &mcpInitHandler{
		warnings: []string{"provider warning"},
		servers: []api.MCPServer{
			{Name: "healthy", Status: api.MCPStatusConnected},
			{Name: "github", Status: api.MCPStatusAuthRequired},
			{Name: "search", Status: api.MCPStatusError, Error: "connection refused"},
		},
	}
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: handler}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	InitSession(context.Background(), c, InitSessionOptions{Agent: "test-agent", Model: "test-model"})

	if warnings := CurrentHeaderData().Warnings; len(warnings) != 1 || warnings[0] != "provider warning" {
		t.Fatalf("expected the header to carry only the workspace warning, got %v", warnings)
	}

	warnings := transcriptWarnings(t)
	if len(warnings) != 2 {
		t.Fatalf("expected one timeline warning per unhealthy server, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "github") || !strings.Contains(warnings[1], "search") {
		t.Errorf("unexpected MCP warnings: %v", warnings)
	}
}

// TestInitSessionAnnouncesAfterServersSettle covers the ordering problem: MCP
// servers connect in the background, so they are still reported as connecting when
// the session initializes. The timeline must gain the announcement once they settle
// rather than never saying anything at all.
func TestInitSessionAnnouncesAfterServersSettle(t *testing.T) {
	ResetApp()
	defer ResetApp()
	ResetSession()
	defer ResetSession()
	ClearTranscript()
	defer ClearTranscript()
	defer SetWorkspaceWarnings(nil)
	defer SetWorkspaceMCPServers(nil)

	origInterval, origTimeout := mcpSettleInterval, mcpSettleTimeout
	// The interval stays longer than the synchronous part of InitSession so the
	// test can assert nothing is announced while a server is still connecting.
	mcpSettleInterval = 50 * time.Millisecond
	mcpSettleTimeout = 2 * time.Second
	t.Cleanup(func() {
		mcpSettleInterval = origInterval
		mcpSettleTimeout = origTimeout
	})

	handler := &mcpInitHandler{
		startConnecting: true,
		servers: []api.MCPServer{
			{Name: "linear", Status: api.MCPStatusAuthRequired, AuthType: "oauth2"},
		},
	}
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: handler}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	InitSession(context.Background(), c, InitSessionOptions{Agent: "test-agent", Model: "test-model"})

	if warnings := transcriptWarnings(t); len(warnings) != 0 {
		t.Fatalf("a connecting server must not be announced yet, got %v", warnings)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if warnings := transcriptWarnings(t); len(warnings) == 1 {
			if !strings.Contains(warnings[0], "linear") || !strings.Contains(warnings[0], "requires authentication") {
				t.Fatalf("unexpected announcement after settling: %q", warnings[0])
			}
			if header := CurrentHeaderData().Warnings; len(header) != 0 {
				t.Fatalf("MCP health must never reach the header, got %v", header)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("announcement never appeared after the server settled: %+v", transcriptWarnings(t))
}

func TestActiveModelIDPrefersResolvedModel(t *testing.T) {
	ResetSession()
	defer ResetSession()

	if got := GetSession().ActiveModelID(); got != "" {
		t.Fatalf("unknown active model = %q, want empty", got)
	}

	session.Set(func(s SessionState) SessionState {
		s.Config = api.SessionConfig{Model: "@fast"}
		return s
	})
	if got := GetSession().ActiveModelID(); got != "@fast" {
		t.Fatalf("draft active model = %q, want the draft setting", got)
	}

	SetSession(&api.Session{Config: api.SessionConfig{Model: "@fast"}})
	if got := GetSession().ActiveModelID(); got != "@fast" {
		t.Fatalf("session active model = %q, want the configured identifier", got)
	}

	SetSession(&api.Session{
		Config: api.SessionConfig{Model: "@fast"},
		Model:  &api.Model{ID: "openai/gpt-5.5"},
	})
	if got := GetSession().ActiveModelID(); got != "openai/gpt-5.5" {
		t.Fatalf("session active model = %q, want the resolved model id", got)
	}

	SetSessionModelInfo(&api.Model{ID: "anthropic/claude"}, nil)
	if got := GetSession().ActiveModelID(); got != "anthropic/claude" {
		t.Fatalf("previewed active model = %q, want the previewed model id", got)
	}
	if got := GetSession().ModelLabel; got != "anthropic/claude" {
		t.Fatalf("model label = %q, want the previewed model label", got)
	}

	SetSessionModelInfo(nil, nil)
	if got := GetSession().ActiveModelID(); got != "openai/gpt-5.5" {
		t.Fatalf("active model after an empty preview = %q, want the session model kept", got)
	}
}

func TestActiveModelIDFollowsResolvedModelChanges(t *testing.T) {
	ResetSession()
	defer ResetSession()

	SetSession(&api.Session{Config: api.SessionConfig{Model: "openai/gpt-5.5"}})
	SetActiveModelID("anthropic/claude")
	if got := GetSession().ActiveModelID(); got != "anthropic/claude" {
		t.Fatalf("active model = %q, want the model the session last switched to", got)
	}
}
