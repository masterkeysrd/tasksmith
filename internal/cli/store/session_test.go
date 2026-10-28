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

func TestResolveModelInfoNilClient(t *testing.T) {
	ctx := context.Background()

	// Default fallback
	label, thinking := ResolveModelInfo(ctx, nil, "", "", nil)
	if label != "default" || thinking != ThinkingProviderDefault {
		t.Fatalf("expected default/provider default, got %q/%q", label, thinking)
	}

	// Passed config
	label, thinking = ResolveModelInfo(ctx, nil, "agent1", "gpt-4", nil)
	if label != "gpt-4" || thinking != ThinkingProviderDefault {
		t.Fatalf("expected gpt-4/provider default, got %q/%q", label, thinking)
	}

	// Sess overrides cfg
	sess := &api.Session{
		Config: api.SessionConfig{
			Agent: "sess-agent",
			Model: "claude-3-7",
		},
	}
	label, thinking = ResolveModelInfo(ctx, nil, "agent1", "gpt-4", sess)
	if label != "claude-3-7" || thinking != ThinkingProviderDefault {
		t.Fatalf("expected claude-3-7/provider default from sess, got %q/%q", label, thinking)
	}
	sess.Config.Thinking = &api.ThinkingSetting{Type: "effort", Effort: "auto"}
	_, thinking = ResolveModelInfo(ctx, nil, "", "", sess)
	if thinking != "auto" {
		t.Fatalf("expected explicit auto effort, got %q", thinking)
	}
	ApplyThinkingChange(nil)
	if got := GetSession(); got.Thinking != ThinkingProviderDefault || got.Config.Thinking != nil {
		t.Fatalf("expected provider default after reset, got %+v", got)
	}
	ResetSession()
}

func TestResolveModelInfoWithWorkspaceWarnings(t *testing.T) {
	ResetApp()
	defer ResetApp()
	ResetSession()
	defer ResetSession()

	var getWorkspaceCalled bool
	rt := &mockSessionRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet && req.URL.Path == "/v1/workspace" {
				getWorkspaceCalled = true
				ws := api.Workspace{
					Name:         "test-ws",
					RootDir:      "/workspace",
					DefaultAgent: "default-agent",
					DefaultModel: "default-model",
					Warnings:     []string{"Workspace warning: invalid rule in config"},
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

	// Both agent and model specified: previously GetWorkspace was skipped, now it must be called!
	label, thinking := ResolveModelInfo(ctx, c, "my-agent", "my-model", nil)
	if !getWorkspaceCalled {
		t.Fatal("expected GetWorkspace to be called even when agent and model are explicitly provided")
	}
	if label != "my-model" || thinking != ThinkingProviderDefault {
		t.Fatalf("unexpected label/thinking: %q/%q", label, thinking)
	}

	hd := CurrentHeaderData()
	if len(hd.Warnings) != 1 || hd.Warnings[0] != "Workspace warning: invalid rule in config" {
		t.Fatalf("unexpected Warnings in CurrentHeaderData: %v", hd.Warnings)
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

func TestCompactionThresholds(t *testing.T) {
	// Table from compaction.md Section 2.4:
	// 128k: reserve=24,384, trigger=103,616
	// 200k: reserve=40,000, trigger=160,000
	// 400k: reserve=44,000, trigger=356,000
	// 1M:   reserve=62,000, trigger=938,000
	tests := []struct {
		ctxLimit, outLimit int
		wantReserve        int
		wantTrigger        int
	}{
		{128000, 16384, 24384, 103616},
		{200000, 32000, 40000, 160000},
		{400000, 128000, 44000, 356000},
		{1000000, 65536, 62000, 938000},
	}
	for _, tc := range tests {
		res, trig := computeCompactionThresholds(tc.ctxLimit, tc.outLimit)
		if res != tc.wantReserve || trig != tc.wantTrigger {
			t.Errorf("computeCompactionThresholds(%d, %d) = (%d, %d), want (%d, %d)",
				tc.ctxLimit, tc.outLimit, res, trig, tc.wantReserve, tc.wantTrigger)
		}
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
