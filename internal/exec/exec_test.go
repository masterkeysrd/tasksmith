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
