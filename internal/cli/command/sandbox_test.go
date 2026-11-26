package command

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	coretestutils "github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

type sandboxFixture struct {
	mu       sync.Mutex
	requests []sandboxFixtureRequest
	response api.SandboxResponse
}

type sandboxFixtureRequest struct {
	method string
	path   string
	body   api.UpdateSandboxRequest
}

func sandboxResponse() api.SandboxResponse {
	tr := true
	fa := false
	return api.SandboxResponse{
		Project: api.SandboxSettings{
			Mode:              api.SandboxNative,
			FailIfUnavailable: &fa,
			ExcludedCommands:  []string{"git commit *"},
		},
		Global:    api.SandboxSettings{AllowUnsandboxed: &tr},
		Effective: api.SandboxSettings{Mode: api.SandboxNative, FailIfUnavailable: &fa, AllowUnsandboxed: &tr, AutoAllowBash: &fa},
		Availability: api.SandboxAvailability{
			Available: true,
			Backend:   "seatbelt",
		},
		ResolvedPaths: api.SandboxResolvedPaths{
			WorkDir:    "/repo",
			AllowWrite: []string{"/repo"},
		},
	}
}

func newSandboxFixture(t *testing.T) *sandboxFixture {
	t.Helper()
	f := &sandboxFixture{response: sandboxResponse()}
	c, err := client.New("/tmp/sandbox-test.sock", client.WithCWD("/test-workspace"), client.WithHTTPClient(&http.Client{Transport: f}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	store.InitSession(context.Background(), c, store.InitSessionOptions{})
	t.Cleanup(func() {
		store.ResetSession()
		store.ResetApp()
		store.ClearSlot()
		store.ResetStream()
		store.ResetTranscript()
	})
	return f
}

func (f *sandboxFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet && req.URL.Path == "/v1/events" {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/sandbox":
		f.mu.Lock()
		resp := f.response
		f.mu.Unlock()
		return sandboxJSONResponse(http.StatusOK, resp)
	case req.Method == http.MethodPut && req.URL.Path == "/v1/sandbox":
		raw, _ := io.ReadAll(req.Body)
		var body api.UpdateSandboxRequest
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, sandboxFixtureRequest{method: req.Method, path: req.URL.Path, body: body})
		resp := f.response
		f.mu.Unlock()
		return sandboxJSONResponse(http.StatusOK, resp)
	}
	return sandboxJSONResponse(http.StatusNotFound, map[string]string{"error": "not found"})
}

func (f *sandboxFixture) setResponse(resp api.SandboxResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.response = resp
}

func (f *sandboxFixture) putRequests() []sandboxFixtureRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sandboxFixtureRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func sandboxJSONResponse(status int, v any) (*http.Response, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(raw))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// renderSandboxBrowser mounts the view in a running trail loop behind a wind
// provider, so the async query resolves and repaints on its own.
func renderSandboxBrowser(t *testing.T, slotID string) *testutils.App {
	t.Helper()
	view := trail.SimpleFC("SandboxTestApp", func() kitex.Node {
		return SandboxBrowser(slotID)
	})
	app := testutils.StartApp(t,
		wind.Provider(wind.ProviderProps{Client: wind.NewClient()}, view()),
		100, 30,
	)
	t.Cleanup(app.Stop)
	return app
}

func TestSandboxCommandArgsValidation(t *testing.T) {
	store.ResetApp()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ClearSlot()

	reg := NewRegistry()

	if err := reg.Execute("/sandbox extra"); err == nil {
		t.Fatal("expected error when running /sandbox with args, got nil")
	} else if !strings.Contains(err.Error(), "does not accept arguments") {
		t.Fatalf("unexpected error message: %v", err)
	}

	cmd, ok := reg.Lookup("sandbox")
	if !ok || cmd.Name != "sandbox" {
		t.Fatalf("expected /sandbox to be registered, ok=%v", ok)
	}
	if cmd.Usage != "/sandbox" || cmd.Description == "" {
		t.Errorf("unexpected usage/description: %q / %q", cmd.Usage, cmd.Description)
	}
}

func TestSandboxCommandEnqueuesSlot(t *testing.T) {
	coretestutils.SetupXDG(t, "tasksmith")
	store.ResetApp()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ClearSlot()

	newSandboxFixture(t)

	reg := NewRegistry()
	if err := reg.Execute("/sandbox"); err != nil {
		t.Fatalf("Execute /sandbox: %v", err)
	}
	st := store.GetSlot()
	if st.Active == nil || st.Active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot to be enqueued, got %+v", st.Active)
	}
}

func TestSandboxLockedKnobs(t *testing.T) {
	tr := true
	fa := false

	// A global failIfUnavailable=true locks Project only; Global stays
	// editable so the lock can always be undone where it was written.
	resp := sandboxResponse()
	resp.Global.FailIfUnavailable = &tr
	locked := sandboxLockedKnobs(resp)
	if reason := locked[api.ScopeProject][components.KnobFailIfUnavailable]; !strings.Contains(reason, "global scope pins it on") {
		t.Fatalf("expected failIfUnavailable locked for project scope, got %q", reason)
	}
	if _, ok := locked[api.ScopeGlobal][components.KnobFailIfUnavailable]; ok {
		t.Fatal("global scope must never be locked")
	}

	// A project-pinned value never locks the Project scope itself: the scope
	// that wrote the setting must always be able to undo it.
	resp = sandboxResponse()
	resp.Project.FailIfUnavailable = &tr
	if _, ok := sandboxLockedKnobs(resp)[api.ScopeProject][components.KnobFailIfUnavailable]; ok {
		t.Fatal("project scope must not lock itself")
	}
	if _, ok := sandboxLockedKnobs(resp)[api.ScopeGlobal][components.KnobFailIfUnavailable]; ok {
		t.Fatal("a project lock must not lock global scope")
	}

	// Nothing pinned: nothing locked.
	resp = sandboxResponse()
	for scope, knobs := range sandboxLockedKnobs(resp) {
		if len(knobs) != 0 {
			t.Fatalf("expected no locks for scope %q, got %+v", scope, knobs)
		}
	}

	// A global allowUnsandboxed=false locks Project only.
	resp = sandboxResponse()
	resp.Global.AllowUnsandboxed = &fa
	locked = sandboxLockedKnobs(resp)
	if reason := locked[api.ScopeProject][components.KnobAllowUnsandboxed]; !strings.Contains(reason, "global scope pins it off") {
		t.Fatalf("expected allowUnsandboxed locked for project scope, got %q", reason)
	}
	if _, ok := locked[api.ScopeGlobal][components.KnobAllowUnsandboxed]; ok {
		t.Fatal("global scope must never be locked")
	}

	// A restricted workspace forces allowUnsandboxed off for Project even
	// when no scope pinned it explicitly, and the reason names the posture.
	resp = sandboxResponse()
	resp.Restricted = true
	locked = sandboxLockedKnobs(resp)
	if reason := locked[api.ScopeProject][components.KnobAllowUnsandboxed]; !strings.Contains(reason, "workspace trust posture") {
		t.Fatalf("expected allowUnsandboxed forced off by trust posture, got %q", reason)
	}
	if _, ok := locked[api.ScopeGlobal][components.KnobAllowUnsandboxed]; ok {
		t.Fatal("restricted posture must not lock global scope edits")
	}

	// Restricted posture plus a project-pinned false still reports the trust
	// floor rather than misattributing the lock to the project scope.
	resp = sandboxResponse()
	resp.Restricted = true
	resp.Project.AllowUnsandboxed = &fa
	if reason := sandboxLockedKnobs(resp)[api.ScopeProject][components.KnobAllowUnsandboxed]; !strings.Contains(reason, "workspace trust posture") {
		t.Fatalf("expected trust-posture reason over project attribution, got %q", reason)
	}
}

func TestSandboxBrowserView(t *testing.T) {
	coretestutils.SetupXDG(t, "tasksmith")
	store.ResetApp()
	store.ResetSession()
	store.ClearSlot()
	store.ClearTranscript()
	defer store.ResetApp()
	defer store.ResetSession()
	defer store.ClearSlot()
	defer store.ClearTranscript()

	f := newSandboxFixture(t)

	store.EnqueueSlot(store.SlotItem{ID: "slot-sandbox", Kind: store.SlotKindPicker, Data: SandboxBrowser})
	app := renderSandboxBrowser(t, "slot-sandbox")

	text := app.WaitFor(func(s string) bool { return strings.Contains(s, "scope: [Project] · Global") })
	for _, want := range []string{"Host backend:      ✔ seatbelt available", "● native", "Fail if unavailable"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in view:\n%s", want, text)
		}
	}

	// Enter on the mode row switches native -> local, which must PUT the
	// merged project settings to the daemon.
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return len(f.putRequests()) == 1 })
	puts := f.putRequests()
	if puts[0].body.Scope != api.ScopeProject || puts[0].body.Settings.Mode != api.SandboxLocal {
		t.Fatalf("expected project-scope local mode PUT, got scope=%q mode=%q", puts[0].body.Scope, puts[0].body.Settings.Mode)
	}
	if puts[0].body.Settings.FailIfUnavailable == nil || *puts[0].body.Settings.FailIfUnavailable {
		t.Fatal("expected PUT to preserve existing failIfUnavailable=false")
	}

	// Tab to the excluded-commands tab, add a pattern, and check the second
	// PUT carries the merged exclusion list.
	app.Press(key.Key{Code: key.KeyTab})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Excluded commands") })
	app.Press(key.Key{Code: 'a', Text: "a"})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Examples: git commit *") })
	app.Type("docker *")
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return len(f.putRequests()) == 2 })
	puts = f.putRequests()
	joined := strings.Join(puts[len(puts)-1].body.Settings.ExcludedCommands, ",")
	if !strings.Contains(joined, "git commit *") || !strings.Contains(joined, "docker *") {
		t.Fatalf("expected PUT to keep existing and new exclusions, got %q", joined)
	}

	// e edits the selected pattern in place: the PUT must replace it rather
	// than append.
	app.Press(key.Key{Code: 'e', Text: "e"})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Edit excluded command pattern (Project):") })
	app.Type("x")
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return len(f.putRequests()) == 3 })
	puts = f.putRequests()
	joined = strings.Join(puts[len(puts)-1].body.Settings.ExcludedCommands, ",")
	if joined != "git commit *x" {
		t.Fatalf("expected in-place edit PUT with only the edited pattern, got %q", joined)
	}

	// Escape dismisses the slot and leaves a transcript line behind.
	app.Press(key.Key{Code: key.KeyEscape})
	app.WaitFor(func(string) bool { return store.GetSlot().Active == nil })
	items := store.GetTranscript().Items
	if items == nil || len(*items) == 0 {
		t.Fatal("expected transcript output after exiting /sandbox")
	}
}

func TestSandboxBrowserViewUnavailable(t *testing.T) {
	coretestutils.SetupXDG(t, "tasksmith")
	store.ResetApp()
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ResetSession()
	defer store.ClearSlot()

	f := newSandboxFixture(t)
	resp := sandboxResponse()
	resp.Availability = api.SandboxAvailability{
		Available:   false,
		Backend:     "bubblewrap",
		Error:       "bwrap: setting up uid map: Permission denied",
		Remediation: "on Ubuntu 24.04 run: sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0",
	}
	f.setResponse(resp)

	app := renderSandboxBrowser(t, "slot-sandbox-ua")
	text := app.WaitFor(func(s string) bool { return strings.Contains(s, "bubblewrap unavailable") })
	wrapped := strings.Join(testutils.TrimmedLines(text), " ")
	if !strings.Contains(wrapped, "Fix: on Ubuntu 24.04 run: sudo sysctl") {
		t.Fatalf("expected remediation line in unavailable-state view:\n%s", text)
	}
}
