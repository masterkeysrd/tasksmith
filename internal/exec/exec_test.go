package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

func TestParseFormats(t *testing.T) {
	inTests := []struct {
		raw     string
		want    InputFormat
		wantErr bool
	}{
		{"", InputText, false},
		{"text", InputText, false},
		{"TEXT", InputText, false},
		{"jsonl", InputJSONL, false},
		{"  JSONL  ", InputJSONL, false},
		{"json", "", true},
	}
	for _, tt := range inTests {
		got, err := ParseInputFormat(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ParseInputFormat(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("ParseInputFormat(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}

	outTests := []struct {
		raw     string
		want    OutputFormat
		wantErr bool
	}{
		{"", OutputText, false},
		{"text", OutputText, false},
		{"json", OutputJSON, false},
		{"jsonl", OutputJSONL, false},
		{"yaml", "", true},
	}
	for _, tt := range outTests {
		got, err := ParseOutputFormat(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ParseOutputFormat(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("ParseOutputFormat(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// Non-slash prompts that happen to start with local command names ("help ...",
// "model ...", "usage ...", "compact ...", "exit ...") must never be hijacked
// as local slash commands.
func TestLocalCommandRequiresLeadingSlash(t *testing.T) {
	var out bytes.Buffer
	r := &run{
		opts: &Options{Stdout: &out, Stderr: &bytes.Buffer{}},
		w:    newTextWriter(&out, OutputText, true),
		sess: &api.Session{ID: "s-1"},
		quit: make(chan struct{}, 1),
	}

	plainPrompts := []string{
		"help me fix this bug",
		"model the database schema",
		"models should be immutable",
		"mode switching edge case",
		"agent orchestration design",
		"agents in parallel",
		"usage of context.WithCancel",
		"compact this slice in place",
		"exit early when nil",
		"quit the loop on EOF",
		"/",
	}
	for _, prompt := range plainPrompts {
		handled, err := r.localCommand(prompt)
		if handled || err != nil {
			t.Fatalf("localCommand(%q) = (%v, %v), want (false, nil)", prompt, handled, err)
		}
	}

	handled, err := r.localCommand("/help")
	if !handled || err != nil {
		t.Fatalf("localCommand(\"/help\") = (%v, %v), want (true, nil)", handled, err)
	}
	if !strings.Contains(out.String(), "exec local commands:") {
		t.Fatalf("expected /help output, got %q", out.String())
	}
}

func TestTextWriterSeparatesDistinctAssistantMessages(t *testing.T) {
	var buf bytes.Buffer
	w := newTextWriter(&buf, OutputText, true)

	if err := w.delta("msg-1", "Let me inspect the file."); err != nil {
		t.Fatal(err)
	}
	if err := w.delta("msg-1", " Reading now."); err != nil {
		t.Fatal(err)
	}
	if err := w.delta("msg-2", "Found the root cause."); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(api.ExecResult{Status: api.ExecResultCompleted}); err != nil {
		t.Fatal(err)
	}

	want := "Let me inspect the file. Reading now.\n\nFound the root cause.\n"
	if buf.String() != want {
		t.Fatalf("textWriter output = %q, want %q", buf.String(), want)
	}
}

func TestJSONLWriterStampsResultFrameType(t *testing.T) {
	var buf bytes.Buffer
	w := newTextWriter(&buf, OutputJSONL, false)

	if err := w.finish(api.ExecResult{
		SessionID: "sess-1",
		Status:    api.ExecResultCompleted,
		Text:      "done",
	}); err != nil {
		t.Fatal(err)
	}

	var frame map[string]any
	if err := json.Unmarshal(buf.Bytes(), &frame); err != nil {
		t.Fatalf("decode terminal frame: %v", err)
	}
	if frame["type"] != api.ExecResultType {
		t.Fatalf("terminal jsonl frame type = %v, want %q", frame["type"], api.ExecResultType)
	}
}

// execTransport routes daemon API calls to canned session responses so
// resolveSession can be exercised without a live daemon.
type execTransport struct {
	latestBody string
	forkStatus int
}

func (tr execTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := "{}"
	status := http.StatusOK
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/sessions/latest":
		if tr.latestBody == "" {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{"code":"not_found","message":"no sessions"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}
		body = tr.latestBody
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/fork"):
		if tr.forkStatus != 0 && tr.forkStatus != http.StatusOK {
			status = tr.forkStatus
			body = `{"code":"conflict","message":"session is running"}`
			break
		}
		body = `{"id":"fork-1","title":"Source (fork)","forked_from":"latest-1","status":"idle","config":{"mode":"default"}}`
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/sessions/"):
		body = `{"id":"latest-1","status":"idle","config":{"mode":"default"}}`
	case req.Method == http.MethodPost && req.URL.Path == "/v1/sessions":
		body = `{"id":"fresh-1","status":"idle","config":{"mode":"default"}}`
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func newExecRun(t *testing.T, opts *Options, rt http.RoundTripper) *run {
	t.Helper()
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	r, err := newRun(context.Background(), opts)
	if err != nil {
		t.Fatalf("newRun: %v", err)
	}
	t.Cleanup(r.cancel)
	r.c = c
	return r
}

func TestResolveSessionContinueResumesLatest(t *testing.T) {
	r := newExecRun(t, &Options{Continue: true}, execTransport{
		latestBody: `{"id":"latest-1","status":"idle","config":{"mode":"default"}}`,
	})
	if err := r.resolveSession(); err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if r.sess == nil || r.sess.ID != "latest-1" {
		t.Fatalf("session = %+v, want latest-1", r.sess)
	}
}

func TestResolveSessionContinueWithoutSessionsCreatesFresh(t *testing.T) {
	r := newExecRun(t, &Options{Continue: true}, execTransport{})
	if err := r.resolveSession(); err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if r.sess == nil || r.sess.ID != "fresh-1" {
		t.Fatalf("session = %+v, want fresh-1 fallback", r.sess)
	}
}

func TestResolveSessionExplicitSessionBeatsContinue(t *testing.T) {
	var sawLatest bool
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/v1/sessions/latest" {
			sawLatest = true
		}
		return execTransport{latestBody: `{"id":"latest-1"}`}.RoundTrip(req)
	})
	r := newExecRun(t, &Options{SessionID: "latest-1", Continue: true}, rt)
	if err := r.resolveSession(); err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if sawLatest {
		t.Error("--session should short-circuit the -c latest lookup")
	}
	if r.sess == nil || r.sess.ID != "latest-1" {
		t.Fatalf("session = %+v, want latest-1", r.sess)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestResolveSessionForkBranchesResumedSession(t *testing.T) {
	var patchedID string
	var patchedMode api.PermissionMode
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch && strings.HasPrefix(req.URL.Path, "/v1/sessions/") {
			patchedID = strings.TrimPrefix(req.URL.Path, "/v1/sessions/")
			var body api.UpdateSessionRequest
			_ = json.NewDecoder(req.Body).Decode(&body)
			patchedMode = body.Config.Mode
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"fork-1","title":"Source (fork)","forked_from":"latest-1","status":"idle","config":{"mode":"auto"}}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}
		return execTransport{
			latestBody: `{"id":"latest-1","status":"idle","config":{"mode":"default"}}`,
		}.RoundTrip(req)
	})
	r := newExecRun(t, &Options{
		Continue: true,
		Fork:     true,
		Title:    "my branch",
		Config:   api.SessionConfig{Mode: api.ModeAuto},
	}, rt)
	if err := r.resolveSession(); err != nil {
		t.Fatalf("resolveSession: %v", err)
	}
	if r.sess == nil || r.sess.ID != "fork-1" {
		t.Fatalf("session = %+v, want fork-1", r.sess)
	}
	if r.sess.ForkedFrom != "latest-1" {
		t.Fatalf("forked_from = %q, want latest-1", r.sess.ForkedFrom)
	}
	if patchedID != "fork-1" || patchedMode != api.ModeAuto || r.sess.Config.Mode != api.ModeAuto {
		t.Fatalf("patched = (%q, %q) mode=%q, want fork-1 with auto mode", patchedID, patchedMode, r.sess.Config.Mode)
	}
}

func TestResolveSessionForkWithoutSessionIsUsageError(t *testing.T) {
	r := newExecRun(t, &Options{Fork: true}, execTransport{})
	err := r.resolveSession()
	if _, ok := errors.AsType[*usageError](err); !ok {
		t.Fatalf("resolveSession error = %v, want usage error", err)
	}
	if !strings.Contains(err.Error(), "--fork needs a session") {
		t.Fatalf("usage error message = %q, want it to name --fork", err.Error())
	}
}

func TestCmdForkRebindsRunSession(t *testing.T) {
	var out bytes.Buffer
	r := newExecRun(t, &Options{Stdout: &out, Stderr: &bytes.Buffer{}}, execTransport{})
	r.sess = &api.Session{ID: "latest-1", Title: "Source", Status: api.StatusIdle}
	handled, err := r.localCommand("/fork my branch")
	if !handled {
		t.Fatal("localCommand(\"/fork\") not handled")
	}
	if err != nil {
		t.Fatalf("localCommand(\"/fork\"): %v", err)
	}
	if r.sess.ID != "fork-1" {
		t.Fatalf("run session = %q, want fork-1", r.sess.ID)
	}
	if !strings.Contains(out.String(), `forked session fork-1 as "Source (fork)"`) {
		t.Fatalf("command output = %q, want fork confirmation", out.String())
	}
}

func TestCmdForkRunningSessionSurfacesConflict(t *testing.T) {
	var out bytes.Buffer
	r := newExecRun(t, &Options{Stdout: &out, Stderr: &bytes.Buffer{}}, execTransport{forkStatus: http.StatusConflict})
	r.sess = &api.Session{ID: "latest-1", Status: api.StatusIdle}
	if err := r.cmdFork(nil); err == nil {
		t.Fatal("cmdFork on a running source session should fail")
	}
}

func TestCmdLoginPointsToAuthSubcommand(t *testing.T) {
	var out bytes.Buffer
	r := newExecRun(t, &Options{Stdout: &out, Stderr: &bytes.Buffer{}}, execTransport{})
	r.sess = &api.Session{ID: "s1", Status: api.StatusIdle}

	cases := []struct {
		command string
		want    string
	}{
		{command: "/login", want: "tasksmith auth login"},
		{command: "/login antigravity", want: "tasksmith auth login antigravity"},
		{command: "/auth antigravity", want: "tasksmith auth login antigravity"},
		{command: "/signin", want: "tasksmith auth login"},
	}
	for _, tt := range cases {
		handled, err := r.localCommand(tt.command)
		if !handled {
			t.Fatalf("localCommand(%q) not handled", tt.command)
		}
		if err == nil {
			t.Fatalf("localCommand(%q) = nil error, want the auth subcommand pointer", tt.command)
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("localCommand(%q) error = %q, want it to name %q", tt.command, err, tt.want)
		}
	}
}

func TestCmdModelCapabilityMismatchRequiresForce(t *testing.T) {
	var sawForce []bool
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch && req.URL.Path == "/v1/sessions/s1" {
			var body api.UpdateSessionRequest
			_ = json.NewDecoder(req.Body).Decode(&body)
			sawForce = append(sawForce, body.Force)
			if !body.Force {
				raw, _ := json.Marshal(api.ErrorResponse{
					Code:              api.ErrCodeCapabilityMismatch,
					Message:           `model "local/text" does not support image used in this session`,
					Model:             "local/text",
					MissingModalities: []string{"image"},
				})
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(raw)),
				}, nil
			}
			raw, _ := json.Marshal(api.Session{
				ID:     "s1",
				Config: api.SessionConfig{Model: "local/text"},
				Model:  &api.Model{ID: "local/text", Name: "Text"},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(raw)),
			}, nil
		}
		return execTransport{}.RoundTrip(req)
	})

	var out bytes.Buffer
	r := newExecRun(t, &Options{Stdout: &out, Stderr: &bytes.Buffer{}}, rt)
	r.sess = &api.Session{ID: "s1", Status: api.StatusIdle, Config: api.SessionConfig{Model: "anthropic/claude"}}

	if handled, err := r.localCommand("/model local/text"); !handled || err == nil {
		t.Fatalf("expected /model without --force to fail, got handled=%v err=%v", handled, err)
	} else if !strings.Contains(err.Error(), "/model --force local/text") {
		t.Fatalf("expected --force hint in error, got %v", err)
	}

	if handled, err := r.localCommand("/model --force local/text"); !handled || err != nil {
		t.Fatalf("expected /model --force to succeed, got handled=%v err=%v", handled, err)
	}
	if len(sawForce) != 2 || sawForce[0] || !sawForce[1] {
		t.Fatalf("sawForce = %v, want [false true]", sawForce)
	}
}

func TestResolveSessionModelCapabilityMismatchHintAndForce(t *testing.T) {
	var sawForce []bool
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch && req.URL.Path == "/v1/sessions/latest-1" {
			var body api.UpdateSessionRequest
			_ = json.NewDecoder(req.Body).Decode(&body)
			sawForce = append(sawForce, body.Force)
			if !body.Force {
				raw, _ := json.Marshal(api.ErrorResponse{
					Code:              api.ErrCodeCapabilityMismatch,
					Message:           `model "local/text" does not support image used in this session`,
					Model:             "local/text",
					MissingModalities: []string{"image"},
				})
				return &http.Response{
					StatusCode: http.StatusConflict,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(raw)),
				}, nil
			}
			raw, _ := json.Marshal(api.Session{
				ID:     "latest-1",
				Config: api.SessionConfig{Model: "local/text"},
				Model:  &api.Model{ID: "local/text", Name: "Text"},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(raw)),
			}, nil
		}
		return execTransport{
			latestBody: `{"id":"latest-1","status":"idle","config":{"mode":"default","model":"anthropic/claude"}}`,
		}.RoundTrip(req)
	})

	r := newExecRun(t, &Options{
		Continue: true,
		Config:   api.SessionConfig{Model: "local/text"},
	}, rt)
	err := r.resolveSession()
	if err == nil || !strings.Contains(err.Error(), "pass --force") {
		t.Fatalf("resolveSession without Force = %v, want --force hint", err)
	}

	rForce := newExecRun(t, &Options{
		Continue: true,
		Force:    true,
		Config:   api.SessionConfig{Model: "local/text"},
	}, rt)
	if err := rForce.resolveSession(); err != nil {
		t.Fatalf("resolveSession with Force: %v", err)
	}
	if len(sawForce) != 2 || sawForce[0] || !sawForce[1] {
		t.Fatalf("sawForce = %v, want [false true]", sawForce)
	}
}

func TestCmdPlanSwitchesModeAndSubmitsPrompt(t *testing.T) {
	var modes []api.PermissionMode
	var sent []string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPatch && req.URL.Path == "/v1/sessions/s1":
			var body api.UpdateSessionRequest
			_ = json.NewDecoder(req.Body).Decode(&body)
			modes = append(modes, body.Config.Mode)
			raw, _ := json.Marshal(api.Session{
				ID:     "s1",
				Config: api.SessionConfig{Mode: body.Config.Mode},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(raw)),
			}, nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/sessions/s1/send":
			var body api.SendRequest
			_ = json.NewDecoder(req.Body).Decode(&body)
			sent = append(sent, body.Content)
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}
		return execTransport{}.RoundTrip(req)
	})

	var out bytes.Buffer
	r := newExecRun(t, &Options{Stdout: &out, Stderr: &bytes.Buffer{}}, rt)
	r.sess = &api.Session{ID: "s1", Status: api.StatusIdle, Config: api.SessionConfig{Mode: api.ModeDefault}}

	if handled, err := r.localCommand("/plan"); !handled || err != nil {
		t.Fatalf("/plan failed: handled=%v err=%v", handled, err)
	}
	if r.sess.Config.Mode != api.ModePlan || len(sent) != 0 {
		t.Fatalf("after /plan: mode=%q sent=%v", r.sess.Config.Mode, sent)
	}

	r.sess.Config.Mode = api.ModeDefault
	if handled, err := r.localCommand("/plan refactor auth"); !handled || err != nil {
		t.Fatalf("/plan <prompt> failed: handled=%v err=%v", handled, err)
	}
	if r.sess.Config.Mode != api.ModePlan || len(sent) != 1 || sent[0] != "refactor auth" {
		t.Fatalf("after /plan <prompt>: mode=%q sent=%v", r.sess.Config.Mode, sent)
	}
	if len(modes) != 2 || modes[0] != api.ModePlan || modes[1] != api.ModePlan {
		t.Fatalf("modes = %v, want [plan plan]", modes)
	}

	summary := planReviewSummary([]api.PlanReviewRequest{{
		ID:         "pr-1",
		ToolCallID: "tc-1",
		Path:       "plans/auth.md",
	}})
	if !strings.Contains(summary, "Review plan: plans/auth.md") {
		t.Fatalf("planReviewSummary = %q", summary)
	}
}

func TestExecPlanReviewRoutingAndNonInteractiveGuard(t *testing.T) {
	var sentSession string
	var sentReq api.SendRequest
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/send") && req.Method == http.MethodPost {
			parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/v1/sessions/"), "/")
			sentSession = parts[0]
			if err := json.NewDecoder(req.Body).Decode(&sentReq); err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}
		return execTransport{}.RoundTrip(req)
	})

	r := newExecRun(t, &Options{InputFormat: InputJSONL, OutputFormat: OutputJSONL, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}, rt)
	r.sess = &api.Session{ID: "root-sess", Status: api.StatusIdle}

	// Register plan review owner on a subagent session via EventPlanReviewRequested.
	r.handleEvent(api.Event{
		Type:  api.EventPlanReviewRequested,
		Topic: api.SessionStreamTopic("sub-sess"),
		Payload: api.PlanReviewRequestedEvent{
			SessionID: "sub-sess",
			PlanReviews: []api.PlanReviewRequest{{
				ID:         "pr-sub",
				ToolCallID: "tc-sub",
				Path:       "plans/sub.md",
			}},
		},
	})
	if err := r.applyFrame(api.ExecInputFrame{
		Type: api.ExecInputPlanReview,
		PlanReview: &api.PlanReviewDecision{
			RequestID: "pr-sub",
			Approved:  true,
			Mode:      api.ModeAcceptEdits,
		},
	}); err != nil {
		t.Fatalf("applyFrame(plan_review): %v", err)
	}
	if sentSession != "sub-sess" || sentReq.PlanReview == nil || sentReq.PlanReview.RequestID != "pr-sub" || !sentReq.PlanReview.Approved {
		t.Fatalf("plan_review routing mismatch: session=%q req=%+v", sentSession, sentReq.PlanReview)
	}

	// Non-interactive text mode triggers needs_input guard on EventPlanReviewRequested.
	nonInt := newExecRun(t, &Options{InputFormat: InputText, OutputFormat: OutputText, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}, rt)
	nonInt.sess = &api.Session{ID: "root-sess", Status: api.StatusIdle}
	stop := nonInt.handleEvent(api.Event{
		Type:  api.EventPlanReviewRequested,
		Topic: api.SessionStreamTopic("root-sess"),
		Payload: api.PlanReviewRequestedEvent{
			SessionID: "root-sess",
			PlanReviews: []api.PlanReviewRequest{{
				ID:         "pr-root",
				ToolCallID: "tc-root",
				Path:       "plans/root.md",
			}},
		},
	})
	if !stop || nonInt.lastStatus != api.ExecResultNeedsInput || !strings.Contains(nonInt.guardReason, "Review plan: plans/root.md") {
		t.Fatalf("non-interactive guard mismatch: stop=%v status=%q reason=%q", stop, nonInt.lastStatus, nonInt.guardReason)
	}
}
