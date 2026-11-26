package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	loomcodex "github.com/masterkeysrd/loom/providers/codex"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"golang.org/x/oauth2"
)

func TestListModelsUsesConfiguredProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s, want GET /v1/models", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer catalog-test" {
			t.Error("configured authorization was not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"endpoint-model","object":"model","owned_by":"test"}]}`)
	}))
	t.Cleanup(server.Close)
	ctx := t.Context()
	gw, err := New(ctx, Config{
		Providers: []ProviderConfig{{
			Name:     "custom",
			Type:     "openai",
			Endpoint: server.URL + "/v1",
			Auth:     &auth.Config{Type: auth.TypeBearer, Token: &auth.CredentialSource{Env: "CATALOG_TEST_TOKEN"}},
		}},
		Env: map[string]string{"CATALOG_TEST_TOKEN": "catalog-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := gw.Close(); err != nil {
			t.Error(err)
		}
	})
	profiles, available, err := gw.ListModels(ctx, "custom")
	if err != nil || !available || len(profiles) != 1 || profiles[0].ID != "endpoint-model" {
		t.Fatalf("ListModels() = %+v, %v, %v", profiles, available, err)
	}
}

type profileCatalogProvider struct {
	mockStreamProvider
}

func (*profileCatalogProvider) ListProfiles() []profile.Profile {
	return []profile.Profile{{ID: "known-model"}}
}

func TestListModelsUsesProfilesWhenProviderHasNoLiveListing(t *testing.T) {
	gw := &gatewayImpl{providers: map[string]*providerInstance{
		"profiles": {llmProv: &profileCatalogProvider{}},
	}}
	profiles, available, err := gw.ListModels(t.Context(), "profiles")
	if err != nil || !available || len(profiles) != 1 || profiles[0].ID != "known-model" {
		t.Fatalf("ListModels() = %+v, %v, %v", profiles, available, err)
	}
}

type discoveryLister func(context.Context) ([]profile.Profile, error)

func (f discoveryLister) ListModels(ctx context.Context) ([]profile.Profile, error) {
	return f(ctx)
}

func TestProfileDiscoveryCachesCatalog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		d := &profileDiscovery{lister: discoveryLister(func(context.Context) ([]profile.Profile, error) {
			calls++
			if calls == 1 {
				return []profile.Profile{{ID: "first"}}, nil
			}
			return nil, nil
		})}
		ctx := t.Context()
		for _, id := range []string{"missing-a", "missing-b"} {
			if _, ok := d.find(ctx, id); ok {
				t.Fatalf("unexpected profile for %s", id)
			}
		}
		if p, ok := d.find(ctx, "first"); !ok || p.ID != "first" || calls != 1 {
			t.Fatalf("cached profile = %v, found = %v, calls = %d", p.ID, ok, calls)
		}
		time.Sleep(modelDiscoveryTTL)
		for range 2 {
			if _, ok := d.find(ctx, "first"); ok || calls != 2 {
				t.Fatalf("empty refreshed catalog found profile = %v, calls = %d", ok, calls)
			}
		}
	})
}

func TestProfileDiscoveryBacksOffAfterError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		errProbe := errors.New("catalog unavailable")
		d := &profileDiscovery{lister: discoveryLister(func(context.Context) ([]profile.Profile, error) {
			calls++
			if calls == 1 {
				return nil, errProbe
			}
			return []profile.Profile{{ID: "recovered"}}, nil
		})}
		ctx := t.Context()
		for range 2 {
			if _, err := d.load(ctx); !errors.Is(err, errProbe) || calls != 1 {
				t.Fatalf("backoff error = %v, calls = %d", err, calls)
			}
		}
		time.Sleep(modelDiscoveryTimeout)
		if _, ok := d.find(ctx, "recovered"); !ok || calls != 2 {
			t.Fatalf("discovery retry found profile = %v, calls = %d", ok, calls)
		}
	})
}

func TestProfileDiscoveryCancellationDoesNotPoisonCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		d := &profileDiscovery{lister: discoveryLister(func(ctx context.Context) ([]profile.Profile, error) {
			calls++
			if calls == 1 {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []profile.Profile{{ID: "next"}}, nil
		})}
		leader, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := d.load(leader)
			done <- err
		}()
		synctest.Wait()
		waiter, stopWaiting := context.WithTimeout(t.Context(), time.Second)
		defer stopWaiting()
		if _, err := d.load(waiter); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
			t.Fatalf("waiting caller error = %v, calls = %d", err, calls)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("discovery caller error = %v", err)
		}
		if _, ok := d.find(t.Context(), "next"); !ok || calls != 2 {
			t.Fatalf("discovery after cancellation found profile = %v, calls = %d", ok, calls)
		}
	})
}

type codexTestTransport func(*http.Request) (*http.Response, error)

func (f codexTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCodexCatalogInferenceAndLogout(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith-codex-test")
	ctx := t.Context()
	cfg := Config{Providers: []ProviderConfig{{
		Name: providers.CodexName, Type: providers.CodexName, Auth: &auth.Config{Type: auth.TypeOAuth2},
	}}}
	unsigned := cfg.Fingerprint()
	modelID := models.Normalize(providers.CodexName, "gpt-6.2-sol")
	connect := func() Gateway {
		t.Helper()
		gw, err := New(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := gw.Close(); err != nil {
				t.Error(err)
			}
		})
		return gw
	}
	unsignedGateway := connect()
	if _, err := unsignedGateway.LLM(ctx, models.Model{ID: modelID}, llm.ModelConfig{}); !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("unsigned gateway: %v", err)
	}
	if _, ok := unsignedGateway.GetProfile(string(modelID)); ok {
		t.Fatal("unsigned gateway returned a profile before account discovery")
	}
	if _, available, err := unsignedGateway.ListModels(ctx, providers.CodexName); available || err != nil {
		t.Fatalf("catalog before sign-in: available = %v, error = %v", available, err)
	}

	saveCodexRegistration(t, loomcodex.Credentials{
		HostID: loomcodex.NewHostID(), ClientID: "test-client", Subject: "test-subject",
		Scopes: []string{"chatgpt.tokens.use.direct"},
		Token:  oauth2.Token{AccessToken: "fixture-access", Expiry: time.Now().Add(time.Hour)},
	})
	if cfg.Fingerprint() == unsigned {
		t.Error("gateway fingerprint did not change on login")
	}

	defaultClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = defaultClient })
	http.DefaultClient = &http.Client{Transport: codexTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.openai.com" || req.Header.Get("Authorization") != "Bearer fixture-access" {
			t.Errorf("unexpected Codex request to %s", req.URL.Host)
		}
		var body, contentType string
		switch req.URL.Path {
		case "/v1/models":
			contentType = "application/json"
			body = `{"models":[{"slug":"test-model","display_name":"Test model","visibility":"list"},{"slug":"gpt-6.2-sol","display_name":"Account Sol","visibility":"list","context_window":333333},{"slug":"hidden","visibility":"hide"}]}`
		case "/v1/responses":
			defer func() { _ = req.Body.Close() }()
			var payload struct {
				Model string `json:"model"`
				Store *bool  `json:"store"`
				Input []struct {
					Type  string `json:"type"`
					Tools []struct {
						Name string `json:"name"`
					} `json:"tools"`
				} `json:"input"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if payload.Model != modelID.Name() || payload.Store == nil || *payload.Store ||
				len(payload.Input) == 0 || payload.Input[0].Type != "additional_tools" ||
				len(payload.Input[0].Tools) != 1 || payload.Input[0].Tools[0].Name != "lookup" {
				t.Errorf("unexpected inference payload: %+v", payload)
			}
			contentType = "text/event-stream"
			body = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"test-response\"}}\n\n"
		default:
			t.Errorf("unexpected Codex endpoint %s", req.URL.Path)
			return nil, errors.New("unexpected Codex endpoint")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	gw := connect()
	secondGateway := connect()
	prof, ok := secondGateway.GetProfile(string(modelID))
	if !ok || prof.Name != "Account Sol" || prof.Limits.Context != 333333 ||
		!prof.Capabilities.ToolCall || !prof.Capabilities.Attachment || prof.Capabilities.Temperature ||
		len(prof.Capabilities.ReasoningOptions) == 0 || !prof.Pricing.IsZero() {
		t.Fatalf("unexpected profile before catalog browsing: %+v", prof)
	}
	list, available, err := gw.ListModels(ctx, providers.CodexName)
	if err != nil || !available || len(list) != 2 || list[0].ID != "test-model" || list[0].Capabilities.ToolCall ||
		list[1].ID != "gpt-6.2-sol" || list[1].Name != "Account Sol" || list[1].Limits.Context != prof.Limits.Context ||
		!list[1].Pricing.IsZero() {
		t.Fatalf("ListModels() = %+v, %v, %v", list, available, err)
	}
	m := models.Merge(modelID, providers.CodexName, nil, &prof)
	model, err := secondGateway.LLM(ctx, m, llm.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	model = model.BindToolDefs(tool.Definition{Name: "lookup", Description: "Look up a value"})
	history := message.MessageList{message.NewUserText("Hello")}
	response, err := model.Invoke(ctx, history)
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Content.Text(); got != "Hello" {
		t.Fatalf("response = %q, want Hello", got)
	}
	driver, ok := auth.GetBrowserOAuthDriver(providers.CodexName)
	if !ok {
		t.Fatal("Codex browser OAuth driver is missing")
	}
	if deleted, err := driver.Logout(); err != nil || !deleted {
		t.Fatalf("Logout() = %v, %v", deleted, err)
	}
	if cfg.Fingerprint() != unsigned {
		t.Error("gateway fingerprint did not return to unsigned value on logout")
	}
	if _, err := model.Invoke(ctx, history); !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("existing model continued to use signed-out credentials: %v", err)
	}
}

func saveCodexRegistration(t *testing.T, account loomcodex.Credentials) {
	t.Helper()
	path, err := xdg.SubDataDir("credentials", providers.CodexName, "registration.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Revision string                `json:"revision"`
		Account  loomcodex.Credentials `json:"account"`
	}{Revision: "fixture-registration", Account: account})
	if err != nil {
		t.Fatal(err)
	}
	if err := fsutils.WriteFileAtomic(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
