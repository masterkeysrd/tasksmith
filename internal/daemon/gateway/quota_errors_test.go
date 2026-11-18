package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

func TestIsRateLimited(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("context deadline exceeded"), false},
		{errors.New("antigravity: API error (status 429): quota exhausted"), true},
		{errors.New("RESOURCE_EXHAUSTED: quota exceeded for project"), true},
		{errors.New("Too Many Requests"), true},
		{errors.New("rate limit reached"), true},
		{errors.New("model returned 500 internal error"), false},
	}
	for _, tc := range tests {
		if got := IsRateLimited(tc.err); got != tc.want {
			t.Errorf("IsRateLimited(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestQuotaFacts(t *testing.T) {
	now := time.Now()

	if got := quotaFacts(api.ModelQuota{Buckets: []api.QuotaBucket{{ID: "default", RemainingFraction: 1}}}, now); got != "" {
		t.Errorf("full window facts = %q, want empty (a rate limit contradicts it)", got)
	}
	if got := quotaFacts(api.ModelQuota{Buckets: []api.QuotaBucket{{ID: "default", Unlimited: true, RemainingFraction: 0}}}, now); got != "" {
		t.Errorf("unlimited window facts = %q, want empty", got)
	}
	if got := quotaFacts(api.ModelQuota{Buckets: []api.QuotaBucket{{ID: "default", RemainingFraction: 0.5}}}, now); got != "50% quota remaining" {
		t.Errorf("no-reset facts = %q", got)
	}
	// +30s of slack: quotaFacts measures the countdown against its own clock.
	reset := now.Add(42*time.Minute + 30*time.Second)
	withReset := api.ModelQuota{Buckets: []api.QuotaBucket{{ID: "default", RemainingFraction: 0.04, ResetAt: &reset}}}
	want := "4% quota remaining · resets in 42m at " + reset.Local().Format("15:04")
	if got := quotaFacts(withReset, now); got != want {
		t.Errorf("reset facts = %q, want %q", got, want)
	}
	if got := quotaFacts(api.ModelQuota{Buckets: []api.QuotaBucket{{ID: "default", RemainingFraction: -0.2}}}, now); got != "0% quota remaining" {
		t.Errorf("negative fraction facts = %q, want clamped to zero", got)
	}

	multi := api.ModelQuota{Buckets: []api.QuotaBucket{
		{ID: "7d", RemainingFraction: 0.64},
		{ID: "5h", RemainingFraction: 0.0, ResetAt: &reset},
	}}
	wantMulti := "5h: 0% quota remaining · resets in 42m at " + reset.Local().Format("15:04") + " · 7d: 64% remaining"
	if got := quotaFacts(multi, now); got != wantMulti {
		t.Errorf("multi-bucket facts = %q, want %q", got, wantMulti)
	}
}

func TestEnrichRateLimit(t *testing.T) {
	ctx := context.Background()
	reset := time.Now().Add(time.Hour + time.Minute)
	quota := &api.ModelQuota{
		Model:   "p/m",
		Buckets: []api.QuotaBucket{{ID: "default", RemainingFraction: 0, ResetAt: &reset}},
	}

	t.Run("rate limit gains facts", func(t *testing.T) {
		err := errors.New("upstream: API error (status 429): slow down")
		got := enrichRateLimit(ctx, err, func(context.Context) (*api.ModelQuota, error) { return quota, nil })
		if !strings.Contains(got.Error(), "0% quota remaining") || !strings.Contains(got.Error(), "resets in 1h") {
			t.Fatalf("enriched error = %q", got.Error())
		}
		if !errors.Is(got, err) {
			t.Fatal("enrichment must wrap the original error")
		}
	})

	t.Run("non rate limit untouched", func(t *testing.T) {
		err := errors.New("boom")
		if got := enrichRateLimit(ctx, err, func(context.Context) (*api.ModelQuota, error) { return quota, nil }); got != err {
			t.Fatalf("got %v, want the original error", got)
		}
	})

	t.Run("failed quota read leaves error untouched", func(t *testing.T) {
		err := errors.New("status 429")
		if got := enrichRateLimit(ctx, err, func(context.Context) (*api.ModelQuota, error) { return nil, errors.New("offline") }); got != err {
			t.Fatalf("got %v, want the original error", got)
		}
	})

	t.Run("no reader leaves error untouched", func(t *testing.T) {
		err := errors.New("status 429")
		if got := enrichRateLimit(ctx, err, nil); got != err {
			t.Fatalf("got %v, want the original error", got)
		}
	})
}

func TestProviderInstanceQuotaBilled(t *testing.T) {
	prov := &quotaCapableProvider{found: true}
	inst := &providerInstance{cfg: ProviderConfig{Name: "pool", Type: "antigravity"}, llmProv: prov}

	if !inst.quotaBilled(models.Model{}) {
		t.Fatal("nil-pricing model on a quota provider should be quota-billed")
	}
	if inst.quotaBilled(models.Model{Pricing: &api.ModelPricing{}}) {
		t.Fatal("a priced model stays per-token billed even on a quota provider")
	}
}
