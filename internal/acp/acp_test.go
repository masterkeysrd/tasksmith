package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type editor struct {
	sdk.Client
	childScope sdk.SessionId
	choices    chan sdk.PermissionOptionId
	forms      chan sdk.CreateElicitationRequest
	responses  chan sdk.CreateElicitationResponse
}

func (e editor) CreateElicitation(
	ctx context.Context,
	r sdk.CreateElicitationRequest,
) (sdk.CreateElicitationResponse, error) {
	select {
	case e.forms <- r:
	case <-ctx.Done():
		return sdk.CreateElicitationResponse{}, ctx.Err()
	}
	select {
	case response := <-e.responses:
		return response, nil
	case <-ctx.Done():
		return sdk.CreateElicitationResponse{}, ctx.Err()
	}
}

func (e editor) SessionUpdate(context.Context, sdk.SessionNotification) error {
	return nil
}

func (e editor) RequestPermission(
	ctx context.Context,
	r sdk.RequestPermissionRequest,
) (sdk.RequestPermissionResponse, error) {
	select {
	case choice := <-e.choices:
		if r.ToolCall.ToolCallId == "child-perm" && r.SessionId != e.childScope {
			return sdk.RequestPermissionResponse{}, fmt.Errorf("unexpected permission scope %s", r.SessionId)
		}
		if choice == "" {
			return sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeCancelled()}, nil
		}

		for _, o := range r.Options {
			if o.OptionId == choice {
				return sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeSelected(choice)}, nil
			}
		}

		return sdk.RequestPermissionResponse{}, fmt.Errorf("missing choice %q", choice)
	case <-ctx.Done():
		return sdk.RequestPermissionResponse{}, ctx.Err()
	}
}

func TestEditorEventBridge(t *testing.T) {
	for _, caps := range []struct{ forms, children bool }{
		{},
		{
			true,
			false,
		},
		{
			true,
			true,
		},
	} {
		name := fmt.Sprintf("forms=%t/subagents=%t", caps.forms, caps.children)
		t.Run(name, func(t *testing.T) { testEditorBridge(t, caps.forms, caps.children) })
	}
}

func testEditorBridge(t *testing.T, rich, children bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	ws := t.TempDir()
	events := make(chan api.EventPayload, 32)
	sends := make(chan api.SendRequest, 10)
	cancelled := make(chan struct{}, 1)
	childCancelled := make(chan struct{}, 2)

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != ws {
			t.Errorf("unexpected workspace")
		}

		switch r.URL.Path {
		case "/v1/sessions":
			_, _ = fmt.Fprint(w, `{"id":"s","config":{"model":"one"},`+
				`"diagnostics":[{"message":"Agent setting unavailable"}]}`)
		case "/v1/sessions/s":
			if r.Method == http.MethodGet {
				_, _ = fmt.Fprint(w, `{"id":"s","config":{"model":"one","mode":"ask"}}`)
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			var patch api.UpdateSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Error(err)
			}
			switch cfg := patch.Config; {
			case cfg.Model == "invalid" || cfg.Mode == "auto":
				http.Error(w, `{"message":"unsupported configuration"}`, http.StatusBadRequest)
				return
			case cfg.Model == "two", cfg.Mode == "plan", cfg.Agent == "main":
			case cfg.Thinking != nil && cfg.Thinking.Type == "effort" && cfg.Thinking.Effort == "high":
			default:
				t.Errorf("unexpected patch: %+v", cfg)
			}
			patch.Config.Model = "two"
			_ = json.NewEncoder(w).Encode(api.Session{
				ID:     "s",
				Config: patch.Config,
			})
		case "/v1/agents":
			_, _ = fmt.Fprint(w, `[{"name":"main"}]`)
		case "/v1/models":
			_, _ = fmt.Fprint(w, `[{"id":"one"},{"id":"two"}]`)
		case "/v1/commands":
			_, _ = fmt.Fprint(w, `[{"name":"review","description":"Review changes"}]`)
		case "/v1/sessions/s/send", "/v1/sessions/child/send":
			var req api.SendRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			childReply := (req.Decision != nil && strings.HasPrefix(req.Decision.RequestID, "child:")) ||
				(req.Question != nil && strings.HasPrefix(req.Question.RequestID, "child:")) ||
				(req.PlanReview != nil && strings.HasPrefix(req.PlanReview.RequestID, "child:"))
			if childReply != (r.URL.Path == "/v1/sessions/child/send") {
				t.Errorf("misrouted reply: %+v on %s", req, r.URL.Path)
			}
			sends <- req
		case "/v1/permissions":
			var rule api.PermissionRuleRequest
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil ||
				rule.Rule != "edit(file)" ||
				rule.Scope != api.ScopeProject ||
				rule.Effect != api.EffectDeny {
				t.Errorf("deny rule: %+v %v", rule, err)
			}
			_, _ = fmt.Fprint(w, `{}`)
		case "/v1/sessions/child/cancel":
			childCancelled <- struct{}{}
		case "/v1/sessions/s/cancel":
			cancelled <- struct{}{}
		case "/v1/sessions/s/timeline":
			_, _ = fmt.Fprint(w, `[]`)
		case "/v1/events/subscribe":
			var sub api.SubscriptionRequest
			if err := json.NewDecoder(r.Body).Decode(&sub); err != nil || sub.Since == "" ||
				len(sub.Topics) != 1 || sub.Topics[0] != api.SessionStreamTopic("child") {
				t.Errorf("unexpected child subscription: %+v, %v", sub, err)
			}
			_, _ = fmt.Fprint(w, `{}`)
		case "/v1/events":
			if r.URL.Query().Get("since") != "" {
				t.Error("new subscription should be live-only")
			}

			w.Header().Set(api.HeaderSubscriberID, "sub")
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()

			for {
				select {
				case p := <-events:
					topic := api.SessionStreamTopic("s")
					switch v := p.(type) {
					case api.SessionCreatedEvent, api.SessionUpdatedEvent:
						topic = api.TopicSessions
					case api.PermissionRequestedEvent:
						if v.SessionID != "" {
							topic = api.SessionStreamTopic(v.SessionID)
						}
					case api.QuestionRequestedEvent:
						if v.SessionID != "" {
							topic = api.SessionStreamTopic(v.SessionID)
						}
					case api.PlanReviewRequestedEvent:
						if v.SessionID != "" {
							topic = api.SessionStreamTopic(v.SessionID)
						}
					case api.ChunkEvent:
						if v.SessionID != "" {
							topic = api.SessionStreamTopic(v.SessionID)
						}
					case api.CompletedEvent:
						if v.SessionID != "" {
							topic = api.SessionStreamTopic(v.SessionID)
						}
					}
					b, err := json.Marshal(api.Event{
						ID:      client.NewCursor(),
						Type:    api.EventType(p.EventType()),
						Topic:   topic,
						Payload: p,
					})
					if err != nil {
						t.Error(err)
						return
					}
					if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
						return
					}
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	hc := &http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			r.URL.Host = u.Host
			return s.Client().Transport.RoundTrip(r)
		}),
	}
	c, err := client.New("test.sock", client.WithHTTPClient(hc))
	if err != nil {
		t.Fatal(err)
	}

	in, send := io.Pipe()
	out, recv := io.Pipe()
	defer func() {
		_ = in.Close()
		_ = out.Close()
		_ = recv.Close()
		_ = send.Close()
	}()

	var frames bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, c, in, io.MultiWriter(&frames, recv))
	}()

	ed := editor{
		choices:   make(chan sdk.PermissionOptionId, 8),
		forms:     make(chan sdk.CreateElicitationRequest, 1),
		responses: make(chan sdk.CreateElicitationResponse, 1),
	}
	ed.childScope = "s"
	if children {
		ed.childScope = "child"
	}
	peer := sdk.NewClientSideConnection(ed, send, out)
	var caps sdk.ClientCapabilities
	if rich {
		caps = sdk.ClientCapabilities{
			Elicitation: &sdk.ElicitationCapabilities{
				Form: &sdk.ElicitationFormCapabilities{},
			},
			Plan: &sdk.PlanCapabilities{},

			Session: &sdk.ClientSessionCapabilities{
				Compaction: &sdk.CompactionCapabilities{},
				Notices:    &sdk.NoticeCapabilities{},
			},
		}
	}
	if children {
		caps.Subagents = &sdk.SubagentCapabilities{}
	}
	init, err := peer.Initialize(ctx, sdk.InitializeRequest{
		ProtocolVersion:    2,
		ClientCapabilities: caps,
	})
	if err != nil || init.ProtocolVersion != 1 || init.AgentInfo.Name != "tasksmith" {
		t.Fatalf("init: %+v, %v", init, err)
	}

	created, err := peer.NewSession(ctx, sdk.NewSessionRequest{
		Cwd:        ws,
		McpServers: []sdk.McpServer{},
	})
	if err != nil || len(created.ConfigOptions) != 4 {
		t.Fatalf("new: %+v %v", created, err)
	}

	opts, err := peer.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{
		ValueId: &sdk.SetSessionConfigOptionValueId{
			SessionId: "s",
			ConfigId:  "model",
			Value:     "two",
		},
	})
	if err != nil || opts.ConfigOptions[0].Select.CurrentValue != "two" {
		t.Fatalf("models: %+v %v", opts, err)
	}

	for key, value := range map[sdk.SessionConfigId]sdk.SessionConfigValueId{
		"agent":         "main",
		"mode":          "plan",
		"thought_level": "high",
	} {
		updated, err := peer.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{
			ValueId: &sdk.SetSessionConfigOptionValueId{
				SessionId: "s",
				ConfigId:  key,
				Value:     value,
			},
		})
		if err != nil || len(updated.ConfigOptions) != 4 {
			t.Fatalf("set %s: %+v, %v", key, updated, err)
		}
	}

	_, err = peer.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{
		ValueId: &sdk.SetSessionConfigOptionValueId{
			SessionId: "s",
			ConfigId:  "model",
			Value:     "invalid",
		},
	})
	assertRPCCode(t, err, -32602)
	_, err = peer.SetSessionMode(ctx, sdk.SetSessionModeRequest{
		SessionId: "s",
		ModeId:    "auto",
	})
	assertRPCCode(t, err, -32602)

	result := make(chan sdk.PromptResponse, 1)
	start := func(text string) {
		go func() {
			res, e := peer.Prompt(ctx, sdk.PromptRequest{
				SessionId: "s",
				Prompt:    []sdk.ContentBlock{sdk.TextBlock(text)},
			})
			if e != nil {
				t.Errorf("prompt: %v", e)
			}
			result <- res
		}()
	}

	next := func() api.SendRequest {
		t.Helper()
		select {
		case v := <-sends:
			return v
		case <-ctx.Done():
			t.Fatal(ctx.Err())
			return api.SendRequest{}
		}
	}

	stop := func(want sdk.StopReason) {
		t.Helper()
		select {
		case v := <-result:
			if v.StopReason != want {
				t.Fatalf("stop = %s", v.StopReason)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	start("/review HEAD")
	if next().Content != "/review HEAD" {
		t.Fatal("slash command changed")
	}

	for i, choice := range []sdk.PermissionOptionId{
		"allow_once",
		"session:0",
		"project:0",
		"deny:0",
		"reject",
	} {
		requestID := fmt.Sprintf("request-%d", i)
		ed.choices <- choice
		events <- api.PermissionRequestedEvent{
			SessionID: "s",
			Permissions: []api.PermissionRequest{
				{
					ID:            requestID,
					ToolCallID:    "call",
					ToolName:      "edit",
					GrantPatterns: []string{"edit(file)"},
				},
			},
		}
		d := next().Decision
		if d == nil || d.RequestID != requestID || d.ToolCallID != "call" {
			t.Fatalf("decision %+v", d)
		}

		switch choice {
		case "allow_once":
			if d.Kind != api.DecisionAllowOnce {
				t.Fatal(d)
			}
		case "reject", "deny:0", "":
			if d.Kind != api.DecisionReject {
				t.Fatal(d)
			}
		default:
			if d.Kind != api.DecisionAllowSession ||
				d.GrantPattern != "edit(file)" ||
				string(d.Scope) != strings.Split(string(choice), ":")[0] {
				t.Fatal(d)
			}
		}
	}

	entry := func(m api.Message) {
		events <- api.EntryAddedEvent{
			SessionID: "s",
			Entry:     api.TimelineEntry{Message: &m},
		}
	}

	for _, name := range []string{
		"edit",
		"write",
		"remove",
	} {
		entry(api.Message{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{
					Type: api.BlockToolCall,
					ToolCall: &api.ToolCall{
						ID:   name,
						Name: name,
						Args: map[string]any{
							"path":     "file",
							"old_text": "old",
							"new_text": "new",
							"content":  "written",
						},
					},
				},
			},
		})
		events <- api.StatusChangedEvent{Status: api.StatusExecuting}
		entry(api.Message{
			Role:       api.RoleTool,
			ToolCallID: name,
			IsError:    name == "remove",
		})
	}

	events <- api.ChunkEvent{
		Chunk: api.Chunk{
			Content: []api.Block{
				{
					Type: api.BlockReasoning,
					Text: "thinking",
				},
				{
					Type: api.BlockText,
					Text: "reply",
				},
			},
		},
	}
	events <- api.TodosUpdatedEvent{
		Todos: []api.TodoItem{
			{
				Content: "verify",
				Status:  "completed",
			},
		},
	}
	events <- api.TodosUpdatedEvent{}
	events <- api.SessionUpdatedEvent{
		Session: api.Session{
			ID:        "s",
			Title:     "New title",
			UpdatedAt: time.Now(),
		},
	}
	events <- api.ContextUpdatedEvent{
		Context: api.ContextState{
			UsedTokens: 25,
			MaxTokens:  100,
		},
	}
	events <- api.UsageUpdatedEvent{
		Usage: api.SessionUsage{
			Totals: api.UsageGroup{CostNano: 500_000_000},
		},
	}
	events <- api.CompactionStartedEvent{ID: "compaction"}
	events <- api.EntryAddedEvent{
		Entry: api.TimelineEntry{
			ID:        "record",
			Compacted: &api.CompactedRecord{Summary: "summary"},
		},
	}
	events <- api.QuotaUpdatedEvent{
		Quota: api.ModelQuota{
			Model: "one",
			Buckets: []api.QuotaBucket{
				{RemainingFraction: 0},
			},
		},
	}
	events <- api.EntryAddedEvent{
		Entry: api.TimelineEntry{
			Mode: &api.ModeChange{To: "ask"},
		},
	}
	events <- api.SessionCreatedEvent{
		Session: api.Session{
			ID:       "child",
			ParentID: "s",
			WorkDir:  ws,
			Title:    "Worker",
			Status:   api.StatusExecuting,
		},
	}
	ed.choices <- "allow_once"
	events <- api.PermissionRequestedEvent{
		SessionID: "child",
		Permissions: []api.PermissionRequest{
			{
				ID:         "child:perm",
				ToolCallID: "child-perm",
				ToolName:   "view",
			},
		},
	}
	if r := next(); r.Decision == nil || r.Decision.RequestID != "child:perm" {
		t.Fatal(r)
	}
	if rich {
		shown := sdk.SessionId("s")
		if children {
			shown = "child"
		}
		testForms(t, ctx, ed, events, next, "child", shown)
	} else {
		for _, p := range []api.EventPayload{
			api.QuestionRequestedEvent{SessionID: "child"},
			api.PlanReviewRequestedEvent{SessionID: "child"},
		} {
			events <- p
			select {
			case <-childCancelled:
			case <-ctx.Done():
				t.Fatal("child remained blocked")
			}
		}
	}
	if children {
		events <- api.ChunkEvent{
			SessionID: "child",
			Chunk: api.Chunk{
				Content: []api.Block{
					{
						Type: api.BlockText,
						Text: "child reply",
					},
				},
			},
		}
		events <- api.CompletedEvent{SessionID: "child"}
	}
	if rich {
		testForms(t, ctx, ed, events, next, "s", "s")
	}
	events <- api.CompletedEvent{}
	stop(sdk.StopReasonEndTurn)
	if !rich {
		for _, request := range []api.EventPayload{
			api.QuestionRequestedEvent{},
			api.PlanReviewRequestedEvent{},
		} {
			start("requires input")
			next()
			events <- request
			stop(sdk.StopReasonCancelled)
			<-cancelled
		}
	}

	start("/compact keep tests")
	if v := next(); v.Compact == nil || v.Compact.Focus != "keep tests" {
		t.Fatal(v)
	}

	if err = peer.Cancel(ctx, sdk.CancelNotification{SessionId: "s"}); err != nil {
		t.Fatal(err)
	}
	stop(sdk.StopReasonCancelled)
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("daemon not cancelled")
	}

	start("close during turn")
	next()
	if _, err = peer.CloseSession(ctx, sdk.CloseSessionRequest{SessionId: "s"}); err != nil {
		t.Fatal(err)
	}
	stop(sdk.StopReasonCancelled)
	<-cancelled
	_, err = peer.CloseSession(ctx, sdk.CloseSessionRequest{SessionId: "s"})
	assertRPCCode(t, err, -32002)
	resumed, err := peer.ResumeSession(ctx, sdk.ResumeSessionRequest{
		Cwd:       ws,
		SessionId: "s",
	})
	if err != nil || resumed.Modes == nil || len(resumed.ConfigOptions) != 4 {
		t.Fatalf("resume: %+v, %v", resumed, err)
	}
	if _, err = peer.DeleteSession(ctx, sdk.DeleteSessionRequest{SessionId: "s"}); err != nil {
		t.Fatal(err)
	}
	_, err = peer.Prompt(ctx, sdk.PromptRequest{
		SessionId: "s",
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("deleted")},
	})
	assertRPCCode(t, err, -32002)

	if err = send.Close(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}

	for _, kind := range []string{
		"compaction_update",
		"compaction_summary_chunk",
		"notice",
		"plan_update",
		"plan_removed",
	} {
		if got := strings.Contains(frames.String(), `"sessionUpdate":"`+kind+`"`); got != rich {
			t.Errorf("%s present=%t with capabilities=%t", kind, got, rich)
		}
	}
	for _, warning := range []string{
		"Agent setting unavailable",
		"Rate limit reached for one",
	} {
		if strings.Contains(frames.String(), warning) != rich {
			t.Errorf("incorrect capability gate for %q", warning)
		}
	}
	if strings.Contains(frames.String(), "retry in the terminal UI") == rich {
		t.Error("incorrect elicitation fallback")
	}
	if strings.Contains(frames.String(), `"sessionUpdate":"plan"`) == rich {
		t.Error("incorrect legacy plan fallback")
	}
	if strings.Contains(frames.String(), `"sessionUpdate":"subagent_update"`) != children ||
		strings.Contains(frames.String(), `"state":"requires_action"`) != children {
		t.Fatal("incorrect child capability gating")
	}
	if !strings.Contains(frames.String(), `"currentModeId":"ask"`) ||
		!strings.Contains(frames.String(), `"sessionUpdate":"config_option_update"`) ||
		!strings.Contains(frames.String(), `"currentValue":"ask"`) {
		t.Fatal("live mode/configuration change was not synchronized")
	}
	if children {
		var idle, content bool
		for line := range strings.SplitSeq(strings.TrimSpace(frames.String()), "\n") {
			var frame struct {
				Params sdk.SessionNotification
				Method string
			}
			if err := json.Unmarshal([]byte(line), &frame); err != nil ||
				frame.Method != sdk.ClientMethodSessionUpdate {
				continue
			}
			n := frame.Params
			if u := n.Update.AgentMessageChunk; n.SessionId == "child" && u != nil &&
				u.Content.Text.Text == "child reply" {
				content = true
			}
			if n.SessionId == "s" && n.Update.SubagentUpdate != nil && strings.Contains(line, `"state":"idle"`) {
				idle = true
			}
		}
		if !idle || !content {
			t.Fatalf("child: idle=%t content=%t", idle, content)
		}
	}

	for _, text := range []string{
		`"sessionUpdate":"agent_thought_chunk"`,
		`"sessionUpdate":"agent_message_chunk"`,
		`"status":"pending"`,
		`"status":"in_progress"`,
		`"status":"failed"`,
		`"status":"completed"`,
		`"sessionUpdate":"usage_update"`,
		`"sessionUpdate":"session_info_update"`,
		`"amount":0.5`,
		`"used":25`,
		`"name":"edit"`,
		`"oldText":"old"`,
		`"newText":"new"`,
		`"newText":"written"`,
		filepath.Join(ws, "file"),
	} {
		if !strings.Contains(frames.String(), text) {
			t.Errorf("missing %s in ACP stream", text)
		}
	}
}

func TestListPagesAndMissingSessions(t *testing.T) {
	ws := t.TempDir()
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sessions := make([]api.Session, 51)
	for i := range sessions {
		sessions[i].ID = fmt.Sprint(i)
	}
	sessions[0].UpdatedAt = when
	body, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.New("test", client.WithCWD(ws), client.WithHTTPClient(&http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/v1/sessions" {
				return jsonResponse(200, string(body)), nil
			}
			return jsonResponse(404, `{"message":"missing"}`), nil
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}

	a := &agent{base: c}
	ctx := t.Context()
	first, err := a.ListSessions(ctx, sdk.ListSessionsRequest{})
	if err != nil || len(first.Sessions) != 50 || first.NextCursor == nil ||
		*first.Sessions[0].UpdatedAt != when.Format(time.RFC3339) || first.Sessions[1].UpdatedAt != nil {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	last, err := a.ListSessions(ctx, sdk.ListSessionsRequest{Cursor: first.NextCursor})
	if err != nil || last.NextCursor != nil || len(last.Sessions) != 1 || last.Sessions[0].SessionId != "50" {
		t.Fatalf("last page: %+v, %v", last, err)
	}
	for _, cursor := range []string{
		"",
		"!",
		"NTE",
		"LTE",
	} {
		_, err := a.ListSessions(ctx, sdk.ListSessionsRequest{Cursor: &cursor})
		assertRPCCode(t, err, -32602)
	}

	_, err = a.Initialize(ctx, sdk.InitializeRequest{})
	assertRPCCode(t, err, -32602)
	_, err = a.LoadSession(ctx, sdk.LoadSessionRequest{
		SessionId: "missing",
		Cwd:       ws,
	})
	assertRPCCode(t, err, -32002)
	_, err = a.ResumeSession(ctx, sdk.ResumeSessionRequest{
		SessionId: "missing",
		Cwd:       ws,
	})
	assertRPCCode(t, err, -32002)
	_, err = a.DeleteSession(ctx, sdk.DeleteSessionRequest{SessionId: "missing"})
	assertRPCCode(t, err, -32002)
	assertRPCCode(t, a.Cancel(ctx, sdk.CancelNotification{SessionId: "missing"}), -32002)
	_, err = a.SetSessionMode(ctx, sdk.SetSessionModeRequest{
		SessionId: "missing",
		ModeId:    "ask",
	})
	assertRPCCode(t, err, -32002)
	_, err = a.ResumeSession(ctx, sdk.ResumeSessionRequest{
		SessionId: "missing",
		Cwd:       "relative",
	})
	assertRPCCode(t, err, -32602)
	_, err = a.ResumeSession(ctx, sdk.ResumeSessionRequest{
		SessionId: "missing",
		Cwd:       ws,
		McpServers: []sdk.McpServer{
			{},
		},
	})
	assertRPCCode(t, err, -32602)
}

func assertRPCCode(t *testing.T, err error, want int) {
	t.Helper()
	rpc, ok := errors.AsType[*sdk.RequestError](err)
	if !ok || rpc.Code != want {
		t.Fatalf("got %v, want RPC code %d", err, want)
	}
}

func testForms(
	t *testing.T,
	ctx context.Context,
	ed editor,
	events chan<- api.EventPayload,
	next func() api.SendRequest,
	target string,
	shown sdk.SessionId,
) {
	t.Helper()
	form := func() *sdk.CreateElicitationForm {
		t.Helper()
		select {
		case r := <-ed.forms:
			if r.Form == nil || r.Form.Session.SessionId != shown {
				t.Fatalf("unexpected form: %+v", r)
			}
			return r.Form
		case <-ctx.Done():
			t.Fatal(ctx.Err())
			return nil
		}
	}
	options := []api.QuestionOption{
		{
			ID:    "a",
			Label: "A",
		},
		{
			ID:    "b",
			Label: "B",
		},
	}
	for i, response := range []sdk.CreateElicitationResponse{
		{
			Accept: &sdk.CreateElicitationAccept{
				Content: map[string]sdk.ElicitationContentValue{
					"question_1": sdk.NewElicitationContentValueString("a"),
					"question_2": sdk.NewElicitationContentValueStringArray("a", "b"),
					"question_3": sdk.NewElicitationContentValueString("text"),
					"question_4": sdk.NewElicitationContentValueBoolean(false),
				},
			},
		},
		{
			Decline: &sdk.CreateElicitationDecline{},
		},
		{
			Cancel: &sdk.CreateElicitationCancel{},
		},
	} {
		q := api.QuestionRequest{
			ID:         fmt.Sprintf("%s:q%d", target, i),
			ToolCallID: "ask",
			Questions: []api.Question{
				{
					Question: "One?",
					Type:     "single",
					Options:  options,
				},
				{
					Question: "Several?",
					Type:     "multi",
					Options:  options,
				},
				{
					Question: "Why?",
					Type:     "text",
				},
				{
					Question: "Sure?",
					Type:     "confirm",
				},
			},
		}
		events <- api.QuestionRequestedEvent{
			SessionID: target,
			Questions: []api.QuestionRequest{
				q,
				q,
			},
		}
		if len(form().RequestedSchema.Required) != 4 {
			t.Fatal("missing required questions")
		}
		ed.responses <- response
		r := next().Question
		if r == nil || r.RequestID != q.ID || r.Canceled != (i != 0) {
			t.Fatalf("question response: %+v", r)
		}
		if i == 0 && (len(r.Answers) != 4 || r.Answers[0].Selected[0] != "a" ||
			len(r.Answers[1].Selected) != 2 || r.Answers[2].Text != "text" || *r.Answers[3].Confirmed) {
			t.Fatalf("answers: %+v", r.Answers)
		}
	}

	for _, decision := range []string{
		"approve_auto",
		"approve_default",
		"approve_accept_edits",
		"revise",
		"cancel",
	} {
		q := api.PlanReviewRequest{
			ID:         target + ":" + decision,
			ToolCallID: "review",
			Path:       "plan.md",
			Content:    "The plan",
		}
		events <- api.PlanReviewRequestedEvent{
			SessionID:   target,
			PlanReviews: []api.PlanReviewRequest{q},
		}
		if !strings.Contains(form().Message, q.Content) {
			t.Fatal("missing plan excerpt")
		}
		if decision == "cancel" {
			ed.responses <- sdk.CreateElicitationResponse{
				Cancel: &sdk.CreateElicitationCancel{},
			}
		} else {
			ed.responses <- sdk.CreateElicitationResponse{
				Accept: &sdk.CreateElicitationAccept{
					Content: map[string]sdk.ElicitationContentValue{
						"decision": sdk.NewElicitationContentValueString(decision),
						"feedback": sdk.NewElicitationContentValueString("feedback"),
					},
				},
			}
		}
		r := next().PlanReview
		mode, approve := strings.CutPrefix(decision, "approve_")
		if r == nil || r.RequestID != q.ID || r.Approved != approve || r.Canceled != (decision == "cancel") ||
			(approve && string(r.Mode) != mode) || (decision == "revise" && r.Feedback != "feedback") {
			t.Fatalf("plan response: %+v", r)
		}
	}
}

func jsonResponse(status int, data string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(data)),
	}
}

func TestChildTerminalReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		event  api.EventPayload
		reason string
	}{
		{
			"failed",
			api.ErrorEvent{Error: "provider failed"},
			"error",
		},
		{
			"interrupted",
			api.EntryAddedEvent{
				Entry: api.TimelineEntry{Kind: api.TimelineKindInterrupted},
			},
			"cancelled",
		},
		{
			"finished",
			api.CompletedEvent{},
			"end_turn",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := newTranslation(t.TempDir())
			child.childID = "child"
			first := child.finish(tc.event)
			if first == nil || child.finish(api.CompletedEvent{}) != nil {
				t.Fatal("missing or overwritten terminal state")
			}

			idle := child.announcement(&api.Session{
				ID:     "child",
				Status: api.StatusIdle,
			})
			for _, update := range []sdk.SessionUpdate{
				*first,
				idle,
			} {
				data, err := json.Marshal(update)
				if err != nil || !strings.Contains(string(data), `"stopReason":"`+tc.reason+`"`) {
					t.Fatalf("terminal update: %s, %v", data, err)
				}
			}
		})
	}
}

func TestModeChangeReplay(t *testing.T) {
	tr := newTranslation(t.TempDir())
	e := api.TimelineEntry{
		Mode: &api.ModeChange{
			From: "ask",
			To:   "plan",
		},
	}
	if got := tr.entry(e, true); len(got) != 0 {
		t.Fatalf("replayed mode change: %+v", got)
	}
	if got := tr.entry(e, false); len(got) != 1 || got[0].CurrentModeUpdate.CurrentModeId != "plan" {
		t.Fatalf("missing live mode change: %+v", got)
	}
}

func TestRecoverMixedInteractions(t *testing.T) {
	c, err := client.New("test", client.WithCWD(t.TempDir()), client.WithHTTPClient(&http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/v1/sessions/s/timeline" {
				return jsonResponse(200, `[]`), nil
			}
			return jsonResponse(200, `{"id":"s","status":"idle",`+
				`"pending_permissions":[{"id":"permission"}],"pending_questions":[{"id":"question"}],`+
				`"pending_plan_reviews":[{"id":"plan"}]}`), nil
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}

	s := turnStream{
		client:   c,
		id:       "s",
		children: make(map[string]*translation),
	}
	want := []api.EventType{
		api.EventPermissionRequested,
		api.EventQuestionRequested,
		api.EventPlanReviewRequested,
	}
	for stop := 0; stop <= len(want); stop++ {
		var got []api.EventType
		err, stopped := s.recover(t.Context(), func(e api.Event, err error) bool {
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, e.Type)
			return len(got) != stop
		})
		if err != nil {
			t.Fatalf("stop=%d: err=%v", stop, err)
		}
		n := stop
		if n == 0 {
			n = len(want)
		}
		if stopped != (stop != 0) || !slices.Equal(got, want[:n]) {
			t.Fatalf("stop=%d: got %v, stopped=%t", stop, got, stopped)
		}
	}
}

func TestRecoverActiveDelegatedChild(t *testing.T) {
	var subscribed atomic.Int32
	c, err := client.New("test", client.WithCWD(t.TempDir()), client.WithHTTPClient(&http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/v1/sessions/s/timeline":
				return jsonResponse(200, `[]`), nil
			case "/v1/sessions/s":
				return jsonResponse(200, `{"id":"s","status":"idle","subagents":[`+
					`{"id":"child","parent_id":"s","status":"running","command_delegated":true}]}`), nil
			case "/v1/sessions/child":
				return jsonResponse(200, `{"id":"child","parent_id":"s","status":"waiting_permission",`+
					`"pending_permissions":[{"id":"child-perm"}]}`), nil
			case "/v1/events/subscribe":
				subscribed.Add(1)
				return jsonResponse(200, `{}`), nil
			default:
				return jsonResponse(404, `{}`), nil
			}
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}

	s := turnStream{
		client:     c,
		id:         "s",
		subscriber: "sub-1",
		children:   make(map[string]*translation),
	}

	var got []api.EventType
	err, stopped := s.recover(t.Context(), func(e api.Event, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, e.Type)
		return true
	})
	if err != nil || stopped {
		t.Fatalf("recover = (%v, %t), want (nil, false)", err, stopped)
	}
	if subscribed.Load() != 1 {
		t.Fatalf("child subscriptions = %d, want 1", subscribed.Load())
	}
	want := []api.EventType{
		api.EventSessionUpdated,
		api.EventPermissionRequested,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if s.children[api.SessionStreamTopic("child")] == nil {
		t.Fatal("child topic was not tracked after recovery")
	}
}

func TestChildSubscriptionRecovery(t *testing.T) {
	for _, code := range []int{
		404,
		409,
	} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			const cursor = "00000000-0000-7000-8000-000000000001"
			const created = `data: {"id":"` + cursor + `","type":"session_created",` +
				`"payload":{"session":{"id":"child","parent_id":"s"}}}` + "\n\n"
			const done = `data: {"type":"completed","payload":{"session_id":"s"}}` + "\n\n"
			var connections, subscriptions, sends atomic.Int32
			hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/v1/events":
					body := done
					if connections.Add(1) == 1 {
						body = created + done
					} else if !strings.Contains(r.URL.RawQuery, api.SessionStreamTopic("child")) {
						t.Error("reconnect lost child topic")
					}
					resp := jsonResponse(200, body)
					resp.Header.Set(api.HeaderSubscriberID, "subscriber")
					return resp, nil
				case "/v1/sessions/s/send":
					sends.Add(1)
					return jsonResponse(200, ""), nil
				case "/v1/events/subscribe":
					var req api.SubscriptionRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						return nil, err
					}
					if subscriptions.Add(1) == 1 {
						if req.Since != cursor {
							t.Errorf("wrong subscription cursor %q", req.Since)
						}
						if code == 409 {
							return jsonResponse(409, `{"code":"need_sync"}`), nil
						}
						return jsonResponse(404, `{"message":"subscriber not found"}`), nil
					}
					if req.Since != "" || code != 409 {
						t.Error("unexpected subscription retry")
					}
					return jsonResponse(200, `{}`), nil
				case "/v1/sessions/child":
					return jsonResponse(200, `{"id":"child","pending_questions":[{"id":"q"}]}`), nil
				}
				return nil, fmt.Errorf("unexpected request %s", r.URL)
			})}
			c, err := client.New("test", client.WithCWD(t.TempDir()), client.WithHTTPClient(hc))
			if err != nil {
				t.Fatal(err)
			}

			s := turnStream{
				client: c,
				id:     "s",
			}
			var pending, finished bool
			for e, err := range s.events(t.Context()) {
				if err != nil {
					t.Fatal(err)
				}
				if e.Type == api.EventQuestionRequested && e.Topic == api.SessionStreamTopic("child") {
					pending = true
				}
				if e.Type == api.EventCompleted {
					finished = true
					break
				}
			}
			if !pending || !finished || sends.Load() != 1 {
				t.Fatalf("pending=%t finished=%t sends=%d", pending, finished, sends.Load())
			}
		})
	}
}

func TestStreamReconnect(t *testing.T) {
	for _, stale := range []bool{
		false,
		true,
	} {
		t.Run(fmt.Sprint("stale=", stale), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			var connections, sends atomic.Int32
			var request api.SendRequest
			const cursor = "00000000-0000-7000-8000-000000000001"

			c, err := client.New("test", client.WithCWD(t.TempDir()), client.WithHTTPClient(&http.Client{
				Transport: transport(func(r *http.Request) (*http.Response, error) {
					switch r.URL.Path {
					case "/v1/events":
						n := connections.Add(1)
						since := r.URL.Query().Get("since")

						switch n {
						case 1:
							if since != "" {
								t.Error("expected live subscription")
							}
							return jsonResponse(
								200,
								`data: {"id":"`+cursor+`","type":"chunk","payload":{"chunk":{"content":[]}}}`+"\n\n",
							), nil
						case 2:
							if since != cursor {
								t.Errorf("reconnect cursor %q", since)
							}
							if stale {
								return jsonResponse(409, `{"code":"need_sync","error":"stale"}`), nil
							}
							return jsonResponse(
								200,
								`data: {"type":"completed","payload":{"session_id":"s"}}`+"\n\n",
							), nil
						case 3:
							if since != "" {
								t.Error("snapshot subscription reused stale cursor")
							}
							return jsonResponse(200, ""), nil
						}
					case "/v1/sessions/s/send":
						if connections.Load() != 1 {
							t.Errorf("send before ready")
						}
						sends.Add(1)
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							return nil, err
						}
						return jsonResponse(200, ""), nil
					case "/v1/sessions/s":
						return jsonResponse(200, `{"id":"s","status":"idle"}`), nil
					case "/v1/sessions/s/timeline":
						return jsonResponse(
							200,
							`[{"id":"past","message":{"id":"old","role":"assistant"}},`+
								`{"id":"anchor","message":{"role":"user","id":"`+request.ID+
								`"}},{"id":"missing","message":{"role":"assistant","id":"final"}}]`,
						), nil
					}
					return nil, fmt.Errorf("unexpected %s", r.URL)
				}),
			}))
			if err != nil {
				t.Fatal(err)
			}

			s := turnStream{
				client:  c,
				id:      "s",
				request: api.SendRequest{Content: "input"},
			}
			completed, recovered := false, false

			for e, err := range s.events(ctx) {
				if err != nil {
					t.Fatal(err)
				}
				if p, ok := e.Payload.(api.EntryAddedEvent); ok {
					if p.Entry.ID != "missing" {
						t.Fatal(p.Entry.ID)
					}
					recovered = true
				}
				if e.Type == api.EventCompleted {
					completed = true
					break
				}
			}

			if !completed || recovered != stale || sends.Load() != 1 {
				t.Fatalf("completed=%t recovered=%t sends=%d", completed, recovered, sends.Load())
			}
		})
	}
}

type failUpdates struct {
	io.Writer
	blocked, release chan struct{}
	fail             atomic.Bool
}

func (w *failUpdates) Write(p []byte) (int, error) {
	if w.fail.Load() && strings.Contains(string(p), `"method":"session/update"`) {
		if w.blocked != nil {
			close(w.blocked)
			<-w.release
		}
		return 0, errors.New("failed to notify")
	}

	return w.Writer.Write(p)
}

func TestTurnCleanup(t *testing.T) {
	for _, cause := range []string{
		"EOF",
		"shutdown",
		"request",
		"permission",
		"notify",
		"blocked",
	} {
		t.Run(cause, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			running, shutdown := context.WithCancel(ctx)
			defer shutdown()

			sent := make(chan struct{})
			cancelled := make(chan struct{}, 2)
			var connected atomic.Bool
			rd, feed := io.Pipe()
			defer func() {
				_ = feed.Close()
				_ = rd.Close()
			}()

			c, err := client.New("test", client.WithHTTPClient(&http.Client{
				Transport: transport(func(r *http.Request) (*http.Response, error) {
					switch r.URL.Path {
					case "/v1/sessions":
						return jsonResponse(200, `{"id":"s","config":{"model":"one"}}`), nil
					case "/v1/models", "/v1/commands", "/v1/agents":
						return jsonResponse(200, "[]"), nil
					case "/v1/events":
						connected.Store(true)
						resp := jsonResponse(200, "")
						resp.Body = rd
						context.AfterFunc(r.Context(), func() {
							_ = rd.Close()
						})
						return resp, nil
					case "/v1/sessions/s/send":
						if !connected.Load() {
							t.Error("send before subscription")
						}
						close(sent)
						return jsonResponse(200, ""), nil
					case "/v1/sessions/s/cancel":
						if r.Context().Err() != nil {
							t.Error("cleanup context expired")
						}
						if _, ok := r.Context().Deadline(); !ok {
							t.Error("unbounded cleanup")
						}
						cancelled <- struct{}{}
						return jsonResponse(200, ""), nil
					}
					return nil, fmt.Errorf("unexpected %s", r.URL.Path)
				}),
			}))
			if err != nil {
				t.Fatal(err)
			}

			in, send := io.Pipe()
			out, recv := io.Pipe()
			defer func() {
				_ = in.Close()
				_ = send.Close()
				_ = out.Close()
				_ = recv.Close()
			}()

			wire := &failUpdates{Writer: recv}
			done := make(chan error, 1)
			go func() {
				done <- Serve(running, c, in, wire)
			}()

			ed := editor{choices: make(chan sdk.PermissionOptionId, 1)}
			peer := sdk.NewClientSideConnection(ed, send, out)
			if _, err = peer.NewSession(ctx, sdk.NewSessionRequest{
				Cwd:        t.TempDir(),
				McpServers: []sdk.McpServer{},
			}); err != nil {
				t.Fatal(err)
			}

			prompt, stopPrompt := context.WithCancel(ctx)
			defer stopPrompt()
			result := make(chan error, 1)
			go func() {
				_, err := peer.Prompt(prompt, sdk.PromptRequest{
					SessionId: "s",
					Prompt:    []sdk.ContentBlock{sdk.TextBlock("hello")},
				})
				result <- err
			}()

			select {
			case <-sent:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			switch cause {
			case "EOF":
				_ = send.Close()
			case "shutdown":
				shutdown()
			case "request":
				stopPrompt()
			case "permission":
				ed.choices <- ""
				_, err = fmt.Fprint(
					feed,
					`data: {"type":"permission_requested","payload":{"session_id":"s",`+
						`"permissions":[{"id":"req","tool_name":"edit","tool_call_id":"call"}]}}`+"\n\n",
				)
			case "notify", "blocked":
				if cause == "blocked" {
					wire.blocked = make(chan struct{})
					wire.release = make(chan struct{})
				}
				wire.fail.Store(true)
				_, err = fmt.Fprint(
					feed,
					`data: {"type":"chunk","payload":{"session_id":"s",`+
						`"chunk":{"content":[{"type":"text","text":"hello"}]}}}`+"\n\n",
				)
			}
			if err != nil {
				t.Fatal(err)
			}

			if cause == "blocked" {
				select {
				case <-wire.blocked:
					shutdown()
				case <-ctx.Done():
					t.Fatal("no blocked notification")
				}
			}

			select {
			case <-cancelled:
				if cause == "blocked" {
					close(wire.release)
				}
			case <-ctx.Done():
				t.Fatal("daemon did not receive cancel")
			}

			if cause == "notify" {
				select {
				case e := <-result:
					if e == nil {
						t.Fatal("missing notification error")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}

			_ = send.Close()
			select {
			case err := <-done:
				if !errors.Is(err, running.Err()) {
					t.Fatalf("shutdown: got %v, want %v", err, running.Err())
				}
			case <-ctx.Done():
				t.Fatal("adapter shutdown blocked")
			}
		})
	}
}

func TestPromptResourcesAndMedia(t *testing.T) {
	fail := false
	c, err := client.New("test.sock", client.WithCWD(t.TempDir()), client.WithHTTPClient(&http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			if fail {
				return nil, errors.New("media unavailable")
			}

			data, err := io.ReadAll(r.Body)
			if r.Header.Get("Content-Type") == "audio/wav" && string(data) == "audio" {
				return jsonResponse(200, `{"sha256":"audio-hash","mime_type":"audio/wav"}`), nil
			}
			if err != nil ||
				string(data) != "img" ||
				r.Header.Get("Content-Type") != "image/png" ||
				r.URL.Path != "/v1/sessions/s/media" {
				t.Fatalf("upload: %s %v", data, err)
			}

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"sha256":"image-hash","mime_type":"image/png"}`)),
			}, nil
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}

	r := sdk.PromptRequest{
		SessionId: "s",
		Prompt: []sdk.ContentBlock{
			sdk.TextBlock("Inspect"),
			sdk.ResourceLinkBlock("notes", "test:notes"),
			sdk.ResourceBlock(sdk.EmbeddedResourceResource{
				TextResourceContents: &sdk.TextResourceContents{
					Uri:  "test:context",
					Text: "contents",
				},
			}),
			sdk.ImageBlock("aW1n", "image/png"),
			sdk.AudioBlock("YXVkaW8=", "audio/wav"),
		},
	}
	req, err := promptInput(t.Context(), c, r)
	if err != nil ||
		len(req.Attachments) != 2 ||
		req.Attachments[1].Target != "audio-hash" ||
		req.Attachments[0].Target != "image-hash" ||
		req.Content != "Inspect\nnotes: test:notes\ntest:context\ncontents\n[Image #1]\n[Audio #2]" {
		t.Fatalf("input: %+v %v", req, err)
	}

	fail = true
	if _, err = promptInput(context.Background(), c, r); err == nil {
		t.Fatal("missing media upload error")
	}
}

func TestPartialChunkRecovery(t *testing.T) {
	tr := newTranslation(t.TempDir())
	tr.chunk(api.Chunk{
		Content: []api.Block{
			{
				Type: api.BlockText,
				Text: "an",
			},
			{
				Type: api.BlockReasoning,
				Text: "th",
			},
		},
	})

	tr.journalOnly = true
	got := tr.entry(api.TimelineEntry{
		Message: &api.Message{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{
					Type: api.BlockText,
					Text: "answer",
				},
				{
					Type: api.BlockReasoning,
					Text: "thought",
				},
			},
		},
	}, false)

	if len(got) != 2 ||
		got[0].AgentMessageChunk.Content.Text.Text != "swer" ||
		got[1].AgentThoughtChunk.Content.Text.Text != "ought" {
		t.Fatalf("partial recovery: %+v", got)
	}
}

func TestChunkFinalization(t *testing.T) {
	for _, kind := range []api.BlockType{
		api.BlockText,
		api.BlockReasoning,
	} {
		t.Run(string(kind), func(t *testing.T) {
			tr := newTranslation(t.TempDir())
			blocks := []api.Block{
				{
					Type: api.BlockText,
					Text: "answer",
				},
				{
					Type: api.BlockReasoning,
					Text: "thought",
				},
			}
			entry := api.TimelineEntry{
				Message: &api.Message{
					Role:    api.RoleAssistant,
					ID:      "journal",
					Content: blocks,
				},
			}

			for _, b := range blocks {
				if b.Type == kind {
					if got := tr.chunk(api.Chunk{
						ID:      "provider",
						Content: []api.Block{b},
					}); len(got) != 1 {
						t.Fatalf("chunk updates: %+v", got)
					}
				}
			}

			tr.entry(api.TimelineEntry{
				Message: &api.Message{Role: api.RoleUser},
			}, false)
			got := tr.entry(entry, false)
			if len(got) != 1 ||
				(kind == api.BlockText && got[0].AgentThoughtChunk == nil) ||
				(kind == api.BlockReasoning && got[0].AgentMessageChunk == nil) {
				t.Fatalf("final updates: %+v", got)
			}

			if got := tr.entry(entry, false); len(got) != 2 {
				t.Fatal("stream state carried into next message")
			}

			tr.chunk(api.Chunk{Content: blocks})
			if got := tr.entry(entry, true); len(got) != 2 {
				t.Fatal("replay was suppressed")
			}
		})
	}
}

func TestHunkContent(t *testing.T) {
	changes := hunkContent(
		"notes",
		"--- a/notes\n+++ b/notes\n"+
			"@@ -1,2 +1,2 @@\n context\n-original\n+replacement\n"+
			"@@ -8 +8 @@\n-second\n+last\n",
		false,
	)

	if len(changes) != 2 ||
		*changes[0].Diff.OldText != "context\noriginal\n" ||
		changes[0].Diff.NewText != "context\nreplacement\n" ||
		changes[1].Diff.NewText != "last\n" {
		t.Fatalf("unexpected hunks: %+v", changes)
	}
}
