package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	loomclaudecode "github.com/masterkeysrd/loom/providers/claudecode"
	loomcodex "github.com/masterkeysrd/loom/providers/codex"
	loomcopilot "github.com/masterkeysrd/loom/providers/copilot"
	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"golang.org/x/oauth2"
)

func TestSubscriptionGatewayProviders(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith-subscription-gateway-test")
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	t.Setenv("COPILOT_DISABLE_KEYTAR", "0")
	t.Setenv("ANTHROPIC_API_KEY", "host-fixture-key")
	t.Setenv("GH_TOKEN", "host-fixture-token")
	for _, tc := range []struct {
		name, binary, model, fixture, bucket string
		rawType                              reflect.Type
	}{
		{providers.ClaudeCodeName, "claude", "claude-sonnet-5-5", "TestClaudeRuntimeFixture", "five_hour", reflect.TypeFor[*loomclaudecode.Provider]()},
		{providers.GitHubCopilotName, "copilot", "gpt-6.2-sol", "TestCopilotRuntimeFixture", "chat", reflect.TypeFor[*loomcopilot.Provider]()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			cfg := ProviderConfig{Name: tc.name, Type: tc.name, Auth: &auth.Config{Type: auth.TypeOAuth2}}
			if _, ok := auth.GetBrowserOAuthDriver(tc.name); !ok {
				t.Fatal("browser OAuth driver not registered")
			}
			unauthenticated, err := initProvider(ctx, cfg, nil)
			if err != nil || !errors.Is(unauthenticated.authErr, ErrMissingCredential) {
				t.Fatalf("missing registration: %+v, %v", unauthenticated, err)
			}
			invalid := cfg
			invalid.Endpoint = "custom"
			if inst, err := initProvider(ctx, invalid, nil); err != nil || !errors.Is(inst.authErr, ErrInvalidConfig) {
				t.Fatalf("endpoint override: inst=%+v, err=%v", inst, err)
			}
			invalid = cfg
			invalid.Auth = &auth.Config{Type: auth.TypeOAuth2, Token: &auth.CredentialSource{Env: "FIXTURE_TOKEN"}}
			if inst, err := initProvider(ctx, invalid, nil); err != nil || !errors.Is(inst.authErr, ErrInvalidConfig) {
				t.Fatalf("credential override: inst=%+v, err=%v", inst, err)
			}

			testBin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			binary := "#!/bin/sh\nexec '" + strings.ReplaceAll(testBin, "'", "'\"'\"'") + "' -test.run '^" + tc.fixture + "$' -- --fixture-" + tc.binary + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(binDir, tc.binary), []byte(binary), 0o700); err != nil {
				t.Fatal(err)
			}
			root, err := xdg.SubDataDir("credentials", tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "active.json"), []byte(`{"profile":"account-fixture","account":"fixture"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := auth.CloseBrowserOAuthDrivers(); err != nil {
					t.Error(err)
				}
			})
			pool, peerPool := NewPool(), NewPool()
			t.Cleanup(func() {
				if err := errors.Join(pool.Close(), peerPool.Close()); err != nil {
					t.Error(err)
				}
			})
			config := Config{Providers: []ProviderConfig{cfg}}
			gw, err := pool.Acquire(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := peerPool.Acquire(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			if gw == peer {
				t.Fatal("distinct pools acquired the same gateway")
			}
			inst := gw.(*gatewayImpl).providers[tc.name]
			raw := inst.llmProv
			if got := reflect.TypeOf(raw); got != tc.rawType {
				t.Fatalf("subscription provider type = %v; want %v", got, tc.rawType)
			}
			if other := peer.(*gatewayImpl).providers[tc.name].llmProv; other != raw {
				t.Fatalf("gateways did not share one raw provider: %p and %p", raw, other)
			}
			m := models.Model{ID: models.ID(tc.name + "/" + tc.model)}
			if inst.authErr != nil || !gw.QuotaCapable(m) || !inst.quotaBilled(m) {
				t.Fatalf("expected quota-billed provider, auth error %v", inst.authErr)
			}
			p, ok := gw.GetProfile(string(m.ID))
			if !ok || p.ID != tc.model || !p.Capabilities.Completion || !p.Capabilities.ToolCall ||
				p.Capabilities.Decision || p.Capabilities.Embedding || !p.Pricing.IsZero() || p.Limits.Context == 0 {
				t.Fatalf("model profile = %+v, found = %v", p, ok)
			}
			listed, available, err := gw.ListModels(ctx, tc.name)
			if err != nil || !available || len(listed) != 1 || listed[0].ID != tc.model || listed[0].Name != "Fixture Model" ||
				!listed[0].Pricing.IsZero() || listed[0].Capabilities.Decision || listed[0].Capabilities.Embedding {
				t.Fatalf("ListModels = %+v, available = %v, err = %v", listed, available, err)
			}
			if copilot, ok := raw.(*loomcopilot.Provider); ok {
				original, err := copilot.ListModels(ctx)
				if err != nil || len(original) != 1 || original[0].ID != tc.model || !original[0].Capabilities.Decision {
					t.Fatalf("raw Copilot ListModels = %+v, %v; want fixture model with Decision enabled", original, err)
				}
			}
			quota, err := gw.Quota(ctx, m)
			if err != nil || quota == nil || quota.Account != "fixture" || quota.Model != string(m.ID) || quota.Provider != tc.name ||
				len(quota.Buckets) != 1 || quota.Buckets[0].ID != tc.bucket || quota.Buckets[0].RemainingFraction != 0.5 {
				t.Fatalf("model quota = %+v, %v", quota, err)
			}
			snapshots, err := gw.QuotaSnapshots(ctx)
			if err != nil || snapshots == nil || len(snapshots.Providers) != 1 {
				t.Fatalf("quota snapshots = %+v, %v", snapshots, err)
			}
			snap := snapshots.Providers[0]
			if snap.Account != "fixture" || snap.Provider != tc.name || snap.CollectedAt.IsZero() ||
				len(snap.Buckets) != 1 || snap.Buckets[tc.bucket].RemainingFraction != 0.5 {
				t.Fatalf("provider quota snapshot = %+v", snap)
			}
			if tc.name == providers.ClaudeCodeName && !slices.Equal(snap.Models[string(m.ID)], []string{tc.bucket}) {
				t.Fatalf("Claude model quota snapshot mapping = %+v", snap.Models)
			}
			if err := gw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, _, err := gw.ListModels(ctx, tc.name); !errors.Is(err, ErrGatewayClosed) {
				t.Fatalf("ListModels on closed gateway: %v", err)
			}
			listed, available, err = peer.ListModels(ctx, tc.name)
			if err != nil || !available || len(listed) != 1 || listed[0].ID != tc.model {
				t.Fatalf("peer ListModels after gateway close = %+v, %v, %v", listed, available, err)
			}
			if err := pool.Close(); err != nil {
				t.Fatal(err)
			}
			listed, available, err = peer.ListModels(ctx, tc.name)
			if err != nil || !available || len(listed) != 1 || listed[0].ID != tc.model {
				t.Fatalf("peer ListModels after first pool close = %+v, %v, %v", listed, available, err)
			}
			if err := auth.CloseBrowserOAuthDrivers(); err != nil {
				t.Fatal(err)
			}
			req := &llm.Request{Model: tc.model, Messages: []message.Message{message.NewUserText("hello")}}
			if _, err := raw.Stream(ctx, req); err == nil || !strings.Contains(err.Error(), "provider is closed") {
				t.Fatalf("raw provider Stream after driver close: %v; want Loom closed-provider error", err)
			}
		})
	}
}

func TestSubscriptionGatewayNormalizesProfiles(t *testing.T) {
	pricing := profile.Pricing{
		Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Reasoning: 5,
		TieredLimits: []profile.TierPricing{{Label: "Fixture Tier", Input: 6, Output: 7}},
	}
	newProfile := func(id, name string) profile.Profile {
		p := profile.Profile{
			ID:           id,
			Name:         name,
			Pricing:      pricing,
			Capabilities: profile.Capabilities{Completion: true, ToolCall: true, Decision: true, Embedding: true},
		}
		p.Pricing.TieredLimits = slices.Clone(pricing.TieredLimits)
		return p
	}
	snapshot := func(t *testing.T, p profile.Profile) profile.Profile {
		t.Helper()
		if p.Pricing.IsZero() || !p.Capabilities.Decision || !p.Capabilities.Embedding {
			t.Fatalf("source profile %q must have pricing, Decision, and Embedding: %+v", p.ID, p)
		}
		p.Pricing.TieredLimits = slices.Clone(p.Pricing.TieredLimits)
		return p
	}
	for _, tc := range []struct {
		name, providerType string
		subscription       bool
	}{
		{name: "work-claude", providerType: providers.ClaudeCodeName, subscription: true},
		{name: "work-copilot", providerType: providers.GitHubCopilotName, subscription: true},
		{name: providers.ClaudeCodeName, providerType: "openai"},
	} {
		t.Run(tc.providerType, func(t *testing.T) {
			wantProfile := func(p profile.Profile) profile.Profile {
				if tc.subscription {
					p.Pricing = profile.Pricing{}
					p.Capabilities.Decision = false
					p.Capabilities.Embedding = false
				}
				return p
			}
			catalog := &subscriptionGatewayCatalog{profiles: []profile.Profile{newProfile("cached", "Cached Model")}}
			live := &subscriptionGatewayLiveCatalog{
				subscriptionGatewayCatalog: catalog,
				models: []profile.Profile{
					newProfile("discovered", "Discovered Model"),
					newProfile("another", "Another Model"),
				},
			}
			inst := &providerInstance{
				cfg:              ProviderConfig{Name: tc.name, Type: tc.providerType},
				llmProv:          live,
				profileDiscovery: &profileDiscovery{lister: live},
			}
			gw := &gatewayImpl{providers: map[string]*providerInstance{tc.name: inst}}
			for _, source := range []*profile.Profile{&catalog.profiles[0], &live.models[0]} {
				original := snapshot(t, *source)
				id := string(models.Normalize(tc.name, original.ID))
				got, found := gw.GetProfile(id)
				want := wantProfile(original)
				if !found || !reflect.DeepEqual(got, want) {
					t.Errorf("GetProfile(%q) = %+v, %v; want %+v, true", id, got, found, want)
				}
				if !reflect.DeepEqual(*source, original) {
					t.Errorf("GetProfile(%q) changed provider profile to %+v; want %+v", id, *source, original)
				}
			}
			for _, listing := range []struct {
				name   string
				client llm.Provider
				source []profile.Profile
			}{
				{"live", live, live.models},
				{"static", catalog, catalog.profiles},
			} {
				t.Run(listing.name, func(t *testing.T) {
					inst.llmProv = listing.client
					original := make([]profile.Profile, len(listing.source))
					want := make([]profile.Profile, len(listing.source))
					for i, p := range listing.source {
						original[i] = snapshot(t, p)
						want[i] = wantProfile(original[i])
					}
					got, available, err := gw.ListModels(t.Context(), tc.name)
					if err != nil || !available || !reflect.DeepEqual(got, want) {
						t.Errorf("ListModels() = %+v, %v, %v; want %+v, true, nil", got, available, err, want)
					}
					if !reflect.DeepEqual(listing.source, original) {
						t.Errorf("ListModels() changed provider profiles to %+v; want %+v", listing.source, original)
					}
					if tc.subscription && len(got) > 0 && &got[0] == &listing.source[0] {
						t.Error("ListModels() returned the provider's subscription slice instead of a clone")
					}
				})
			}
		})
	}
}

type subscriptionGatewayCatalog struct {
	mockStreamProvider
	profiles []profile.Profile
}

func (c *subscriptionGatewayCatalog) GetProfile(id string) (profile.Profile, bool) {
	for _, p := range c.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return profile.Profile{}, false
}

func (c *subscriptionGatewayCatalog) ListProfiles() []profile.Profile { return c.profiles }

type subscriptionGatewayLiveCatalog struct {
	*subscriptionGatewayCatalog
	models []profile.Profile
}

func (c *subscriptionGatewayLiveCatalog) ListModels(context.Context) ([]profile.Profile, error) {
	return c.models, nil
}

func (c *subscriptionGatewayLiveCatalog) GetModel(_ context.Context, id string) (profile.Profile, error) {
	for _, p := range c.models {
		if p.ID == id {
			return p, nil
		}
	}
	return profile.Profile{}, ErrModelNotFound
}

func TestGatewayQuotaAccountSources(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith-quota-account-test")
	const browserAccount = "browser@example.test"
	const tokenAccount = "token@example.test"
	for _, tc := range []struct {
		name, providerType, want string
		registration             bool
	}{
		{"browser-first", providers.CodexName, browserAccount, true},
		{"browser-unavailable", providers.CodexName, "", false},
		{"legacy-oauth", "antigravity", tokenAccount, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.registration {
				saveCodexRegistration(t, loomcodex.Credentials{Email: browserAccount})
				driver, _ := auth.GetBrowserOAuthDriver(providers.CodexName)
				t.Cleanup(func() {
					if _, err := driver.Logout(); err != nil {
						t.Error(err)
					}
				})
			}
			if err := auth.SaveProviderToken(tc.name, &oauth2.Token{AccessToken: "fixture"}, tokenAccount); err != nil {
				t.Fatal(err)
			}
			gw := &gatewayImpl{providers: map[string]*providerInstance{
				tc.name: {
					cfg: ProviderConfig{Name: tc.name, Type: tc.providerType},
					llmProv: &quotaCapableProvider{
						quota: llm.QuotaBuckets{{ID: "fixture"}}, found: true,
					},
				},
			}}
			m := models.Model{ID: models.Normalize(tc.name, "fixture-model")}
			q, err := gw.Quota(t.Context(), m)
			if err != nil || q == nil || q.Account != tc.want {
				t.Fatalf("Quota() = %+v, %v; want account %q", q, err, tc.want)
			}
			snap, err := gw.QuotaSnapshots(t.Context())
			if err != nil || snap == nil || len(snap.Providers) != 1 || snap.Providers[0].Account != tc.want {
				t.Fatalf("QuotaSnapshots() = %+v, %v; want account %q", snap, err, tc.want)
			}
		})
	}
}

func TestClaudeRuntimeFixture(t *testing.T) {
	if !slices.Contains(os.Args, "--fixture-claude") {
		return
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "GH_TOKEN"} {
		if _, found := os.LookupEnv(key); found {
			t.Fatalf("host %s leaked into Claude runtime", key)
		}
	}
	if slices.Contains(os.Args, "/usage") {
		if _, err := fmt.Fprintln(os.Stdout, `{"type":"assistant","usage_report":{"rate_limits":{"limits":[{"kind":"five_hour","percent":50,"resets_at":"2030-01-01T05:00:00Z"}]}}}`); err != nil {
			t.Fatal(err)
		}
		return
	}
	in := bufio.NewScanner(os.Stdin)
	if !in.Scan() {
		t.Fatalf("missing Claude initialize frame: %v", in.Err())
	}
	var frame struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal(in.Bytes(), &frame); err != nil || frame.Type != "control_request" || frame.Request.Subtype != "initialize" || frame.RequestID == "" {
		t.Fatalf("unexpected Claude initialize frame: %s (%v)", in.Bytes(), err)
	}
	if _, err := fmt.Fprintf(os.Stdout, `{"type":"system","subtype":"init","session_id":"fixture","model":"claude-sonnet-5-5"}
{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"models":[{"value":"claude-sonnet-5-5","displayName":"Fixture Model"}]}}}
`, frame.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestCopilotRuntimeFixture(t *testing.T) {
	if !slices.Contains(os.Args, "--fixture-copilot") {
		return
	}
	if os.Getenv("COPILOT_DISABLE_KEYTAR") != "1" {
		t.Fatal("Copilot runtime did not disable keytar")
	}
	if _, found := os.LookupEnv("GH_TOKEN"); found {
		t.Fatal("host GH_TOKEN leaked into Copilot runtime")
	}
	responses := map[string]string{
		"connect":          `{"ok":true,"protocolVersion":4,"version":"fixture"}`,
		"models.list":      `{"models":[{"id":"gpt-6.2-sol","name":"Fixture Model","capabilities":{"limits":{"max_context_window_tokens":128000}}}]}`,
		"account.getQuota": `{"quotaSnapshots":{"chat":{"entitlementRequests":100,"usedRequests":50,"remainingPercentage":50}}}`,
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		header, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			os.Exit(0)
		}
		var length int
		if _, scanErr := fmt.Sscanf(header, "Content-Length: %d", &length); err != nil || scanErr != nil || length <= 0 || length > 65536 {
			t.Fatalf("invalid fixture frame: %q (%v, %v)", header, err, scanErr)
		}
		if sep, err := reader.ReadString('\n'); err != nil || sep != "\r\n" {
			t.Fatalf("invalid fixture frame separator: %q (%v)", sep, err)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		var request struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if len(request.ID) == 0 {
			continue
		}
		result := responses[request.Method]
		if result == "" {
			result = "{}"
		}
		response := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, request.ID, result)
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(response), response); err != nil {
			t.Fatal(err)
		}
	}
}
