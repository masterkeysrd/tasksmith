package command

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

// authFixture serves the provider-auth endpoints over a fake transport so the
// /login flow can be driven without a daemon.
type authFixture struct {
	startStatus int
	startBody   api.ProviderAuthResponse
	startErr    string
	startDelay  time.Duration

	statuses []api.ProviderAuthResponse

	callbackStatus int
	callbackBody   api.ProviderAuthResponse
	callbackErr    string

	listBody []api.ProviderSummary

	mu        sync.Mutex
	requests  []string
	callbacks []string
	statusIdx int
}

func newAuthFixture(t *testing.T, f *authFixture) {
	t.Helper()
	c, err := client.New("/tmp/login-test.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: f}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	store.InitSession(context.Background(), c, store.InitSessionOptions{})
	store.SetDaemonStatus(store.DaemonConnected)
	t.Cleanup(func() {
		authBackground.Wait()
		store.ResetSession()
		store.ResetApp()
		store.ClearSlot()
		store.ResetStream()
		store.ResetTranscript()
	})
	store.SetClient(c)
}

func (f *authFixture) RoundTrip(req *http.Request) (*http.Response, error) {
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
	f.requests = append(f.requests, req.Method+" "+req.URL.Path)
	switch {
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/auth"):
		if f.startDelay > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(f.startDelay):
			}
		}
		status := f.startStatus
		if status == 0 {
			status = http.StatusOK
		}
		if f.startErr != "" {
			f.mu.Unlock()
			return authJSONResponse(http.StatusBadRequest, map[string]string{"error": f.startErr})
		}
		resp := f.startBody
		f.mu.Unlock()
		return authJSONResponse(status, resp)

	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/auth"):
		idx := f.statusIdx
		if idx >= len(f.statuses) {
			idx = len(f.statuses) - 1
		}
		if idx < 0 {
			f.mu.Unlock()
			return authJSONResponse(http.StatusOK, api.ProviderAuthResponse{Status: "idle"})
		}
		f.statusIdx++
		f.mu.Unlock()
		return authJSONResponse(http.StatusOK, f.statuses[idx])

	case strings.HasSuffix(req.URL.Path, "/auth/callback"):
		if pasted, ok := body["callbackUrl"].(string); ok {
			f.callbacks = append(f.callbacks, pasted)
		}
		if f.callbackErr != "" {
			f.mu.Unlock()
			return authJSONResponse(http.StatusBadRequest, map[string]string{"error": f.callbackErr})
		}
		status := f.callbackStatus
		if status == 0 {
			status = http.StatusOK
		}
		resp := f.callbackBody
		f.mu.Unlock()
		return authJSONResponse(status, resp)

	case req.Method == http.MethodGet && req.URL.Path == "/v1/providers":
		list := f.listBody
		f.mu.Unlock()
		return authJSONResponse(http.StatusOK, list)
	}
	f.mu.Unlock()
	return authJSONResponse(http.StatusNotFound, map[string]string{"error": "not found: " + req.URL.Path})
}

func (f *authFixture) callbackSubmissions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.callbacks))
	copy(out, f.callbacks)
	return out
}

func authJSONResponse(status int, payload any) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(data))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// stubAuthBrowser captures the URLs /login tries to open and shortens the poll
// budget so a whole flow resolves in test time.
func stubAuthBrowser(t *testing.T) *[]string {
	t.Helper()
	opened := &[]string{}
	original := authLaunchURL
	originalInterval, originalTimeout := authPollInterval, authPollTimeout
	authLaunchURL = func(url string) bool {
		*opened = append(*opened, url)
		return true
	}
	authPollInterval = time.Millisecond
	authPollTimeout = 2 * time.Second
	t.Cleanup(func() {
		authBackground.Wait()
		authLaunchURL = original
		authPollInterval, authPollTimeout = originalInterval, originalTimeout
	})
	return opened
}

func TestLoginCommandRegistration(t *testing.T) {
	r := NewRegistry()
	cmd, ok := r.Lookup("login")
	if !ok {
		t.Fatal("/login is not registered")
	}
	for _, alias := range []string{"auth", "signin"} {
		if _, ok := r.Lookup(alias); !ok {
			t.Errorf("/%s alias is not registered", alias)
		}
	}
	if !strings.Contains(cmd.Description, "OAuth provider") || strings.Contains(strings.ToLower(cmd.Description), "antigravity") {
		t.Errorf("/login needs a provider-neutral description for /help, got %q", cmd.Description)
	}
}

func TestLoginProviderCompletionDescriptions(t *testing.T) {
	newAuthFixture(t, &authFixture{listBody: []api.ProviderSummary{
		{Name: "claudecode", DisplayName: "claudecode", AuthType: "oauth2"},
		{Name: "github-copilot", AuthType: "oauth2"},
		{Name: "codex", AuthType: "oauth2"},
		{Name: "openai", AuthType: "bearer"},
	}})
	candidates, err := completeOAuthProviders(t.Context(), autocomplete.Request{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"claudecode": "Claude Code", "github-copilot": "GitHub Copilot", "codex": "Codex"}
	for _, candidate := range candidates {
		label, ok := want[candidate.Value]
		if !ok || candidate.Label != candidate.Value || candidate.Description != "Sign in to "+label {
			t.Errorf("unexpected completion: %+v", candidate)
		}
		delete(want, candidate.Value)
	}
	if len(want) != 0 {
		t.Errorf("missing provider completions: %v", want)
	}
}

func TestLoginEnqueuesAuthScreen(t *testing.T) {
	newAuthFixture(t, &authFixture{})
	store.ClearSlot()

	if err := handleLogin([]string{"antigravity"}); err != nil {
		t.Fatalf("/login: %v", err)
	}
	slot := store.GetSlot()
	if slot.Active == nil {
		t.Fatal("/login did not enqueue the sign-in screen")
	}
	store.ClearSlot()
}

func TestProviderAuthStartReportsTheURL(t *testing.T) {
	newAuthFixture(t, &authFixture{
		startBody: api.ProviderAuthResponse{Provider: "antigravity", AuthURL: "https://authorize.example/x", Status: "authorizing"},
	})
	opened := stubAuthBrowser(t)

	outcome := providerAuthStart(context.Background(), "antigravity")
	if outcome.settled || outcome.authURL != "https://authorize.example/x" {
		t.Fatalf("start outcome = %+v, want the URL and an unsettled flow", outcome)
	}
	if len(*opened) != 1 || (*opened)[0] != "https://authorize.example/x" {
		t.Errorf("opened URLs = %v, want the authorization URL", *opened)
	}
}

func TestProviderAuthStartFailureSettles(t *testing.T) {
	newAuthFixture(t, &authFixture{startErr: "oauth not supported for provider \"ghost\""})
	stubAuthBrowser(t)

	outcome := providerAuthStart(context.Background(), "ghost")
	if !outcome.settled || !outcome.failed || !strings.Contains(outcome.message, "oauth not supported") {
		t.Fatalf("start outcome = %+v, want a settled failure with the daemon's reason", outcome)
	}
}

func TestProviderAuthStartResolvesSoleOAuthProvider(t *testing.T) {
	f := &authFixture{
		listBody: []api.ProviderSummary{
			{Name: "anthropic", AuthType: "api_key"},
			{Name: "antigravity", AuthType: "oauth2"},
		},
		startBody: api.ProviderAuthResponse{Provider: "antigravity", AuthURL: "https://authorize.example/x", Status: "authorizing"},
	}
	newAuthFixture(t, f)
	stubAuthBrowser(t)

	outcome := providerAuthStart(context.Background(), "")
	if outcome.settled || outcome.provider != "antigravity" {
		t.Fatalf("start outcome = %+v, want the sole OAuth preset resolved", outcome)
	}
}

func TestProviderAuthStartWithoutSoleProvider(t *testing.T) {
	newAuthFixture(t, &authFixture{
		listBody: []api.ProviderSummary{
			{Name: "one", AuthType: "oauth2"},
			{Name: "two", AuthType: "oauth2"},
		},
	})
	stubAuthBrowser(t)

	outcome := providerAuthStart(context.Background(), "")
	if !outcome.settled || !outcome.failed || !strings.Contains(outcome.message, "multiple OAuth providers") {
		t.Fatalf("start outcome = %+v, want the ambiguity explained", outcome)
	}
}

func TestProviderAuthCheckMapsStatuses(t *testing.T) {
	tests := []struct {
		name        string
		statuses    []api.ProviderAuthResponse
		wantSettled bool
		wantFailed  bool
		wantMessage string
	}{
		{
			name:        "authorizing keeps waiting",
			statuses:    []api.ProviderAuthResponse{{Provider: "p", Status: "authorizing"}},
			wantSettled: false,
		},
		{
			name:        "authenticated settles with the account",
			statuses:    []api.ProviderAuthResponse{{Provider: "p", Status: "authenticated", Account: "dev@example.com"}},
			wantSettled: true,
			wantMessage: "Signed in to p as dev@example.com.",
		},
		{
			name:        "error settles as failure",
			statuses:    []api.ProviderAuthResponse{{Provider: "p", Status: "error", Error: "denied"}},
			wantSettled: true,
			wantFailed:  true,
			wantMessage: "Sign-in failed: denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newAuthFixture(t, &authFixture{statuses: tt.statuses})
			outcome := providerAuthCheck(context.Background(), "p")
			if outcome.settled != tt.wantSettled || outcome.failed != tt.wantFailed {
				t.Fatalf("outcome = %+v, want settled=%t failed=%t", outcome, tt.wantSettled, tt.wantFailed)
			}
			if tt.wantMessage != "" && outcome.message != tt.wantMessage {
				t.Errorf("message = %q, want %q", outcome.message, tt.wantMessage)
			}
		})
	}
}

func TestProviderAuthSubmitPostsTheCallback(t *testing.T) {
	f := &authFixture{
		callbackBody: api.ProviderAuthResponse{Provider: "p", Status: "authenticated", Account: "pasted@example.com"},
	}
	newAuthFixture(t, f)

	outcome := providerAuthSubmit(context.Background(), "p", "http://localhost:1/cb?code=abc")
	if !outcome.settled || outcome.failed {
		t.Fatalf("submit outcome = %+v, want a settled success", outcome)
	}
	sent := f.callbackSubmissions()
	if len(sent) != 1 || sent[0] != "http://localhost:1/cb?code=abc" {
		t.Errorf("callback submissions = %v, want the pasted URL", sent)
	}
}

func TestProviderAuthSubmitRejectionSettlesAsFailure(t *testing.T) {
	f := &authFixture{callbackErr: "no provider auth flow in progress"}
	newAuthFixture(t, f)

	outcome := providerAuthSubmit(context.Background(), "p", "code=abc")
	if !outcome.settled || !outcome.failed || !strings.Contains(outcome.message, "Could not use that callback URL") {
		t.Fatalf("submit outcome = %+v, want a settled rejection", outcome)
	}
}

func TestProviderAuthScreenRendersTheFlow(t *testing.T) {
	const authURL = "https://authorize.example/long/authorization/page?state=abc"
	newAuthFixture(t, &authFixture{
		startBody: api.ProviderAuthResponse{Provider: "antigravity", AuthURL: authURL, Status: "authorizing"},
		statuses:  []api.ProviderAuthResponse{{Provider: "antigravity", Status: "authorizing"}},
	})
	stubAuthBrowser(t)
	store.ClearSlot()

	env := testutils.NewEnv(t, 100, 30)
	env.Render(providerAuthView(providerAuthViewProps{
		slotID:   "slot-render",
		provider: "antigravity",
	}))
	out, ok := waitForRender(t, env.Environment, authURL)
	if !ok {
		t.Fatalf("the authorization URL never reached the screen:\n%s", out)
	}

	for _, want := range []string{
		"paste the URL from your browser's address bar:",
		"URL >",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("sign-in screen missing %q, got:\n%s", want, out)
		}
	}
	// The headline names the provider from props and is deterministic; the
	// report line beneath it depends on how far the background flow got, so it
	// is asserted by the flow tests rather than here.
	if !strings.Contains(out, "Signing in to antigravity") {
		t.Fatalf("sign-in screen missing the provider headline, got:\n%s", out)
	}
	if !strings.Contains(out, "Copy URL") || !strings.Contains(out, "Submit pasted URL") {
		t.Errorf("sign-in screen must advertise its keys, got:\n%s", out)
	}
}

func TestProviderAuthScreenWaitsForTheURL(t *testing.T) {
	newAuthFixture(t, &authFixture{startDelay: 2 * time.Second})
	stubAuthBrowser(t)
	store.ClearSlot()

	env := testutils.NewEnv(t, 100, 30)
	out := env.Render(providerAuthView(providerAuthViewProps{
		slotID:   "slot-wait",
		provider: "antigravity",
	}))
	if !strings.Contains(out, "Waiting for the authorization URL…") {
		t.Fatalf("expected the waiting notice, got:\n%s", out)
	}
}

func TestProviderAuthScreenWaitsForChatGPTBrowser(t *testing.T) {
	newAuthFixture(t, &authFixture{
		startBody: api.ProviderAuthResponse{Provider: "codex", Status: "authorizing"},
		statuses:  []api.ProviderAuthResponse{{Provider: "codex", Status: "authorizing"}},
	})
	opened := stubAuthBrowser(t)
	store.ClearSlot()

	env := testutils.NewEnv(t, 100, 30)
	env.Render(providerAuthView(providerAuthViewProps{
		slotID:   "slot-chatgpt",
		provider: "codex",
	}))
	out, ok := waitForRender(t, env.Environment, "Continue sign-in in the browser on the daemon host.")
	if !ok || strings.Contains(out, "Submit pasted URL") || strings.Contains(out, "Copy URL") {
		t.Fatalf("expected local browser instructions:\n%s", out)
	}
	if len(*opened) != 0 {
		t.Fatalf("the terminal started a second browser: %v", *opened)
	}
}

func TestProviderAuthScreenSubmitsPastedURL(t *testing.T) {
	f := &authFixture{
		startBody:    api.ProviderAuthResponse{Provider: "antigravity", AuthURL: "https://authorize.example/x", Status: "authorizing"},
		statuses:     []api.ProviderAuthResponse{{Provider: "antigravity", Status: "authorizing"}},
		callbackBody: api.ProviderAuthResponse{Provider: "antigravity", Status: "authenticated", Account: "pasted@example.com"},
	}
	newAuthFixture(t, f)
	stubAuthBrowser(t)
	store.ClearSlot()
	store.ClearTranscript()

	env := testutils.NewEnv(t, 100, 30)
	env.Render(providerAuthView(providerAuthViewProps{
		slotID:   "slot-submit",
		provider: "antigravity",
	}))
	if _, ok := waitForRender(t, env.Environment, "https://authorize.example/x"); !ok {
		t.Fatalf("the authorization URL never reached the screen:\n%s", env.DumpText())
	}

	env.Type("http://localhost:1/cb?code=abc")
	env.Press(key.Key{Code: key.KeyEnter})

	out, ok := waitForRender(t, env.Environment, "Signed in to antigravity as pasted@example.com.")
	if !ok {
		t.Fatalf("the pasted callback never settled the flow:\n%s", out)
	}
	sent := f.callbackSubmissions()
	if len(sent) != 1 || sent[0] != "http://localhost:1/cb?code=abc" {
		t.Errorf("callback submissions = %v, want the pasted URL", sent)
	}
}
