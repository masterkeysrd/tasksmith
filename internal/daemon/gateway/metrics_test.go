package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

type captureRecorder struct{ records []metrics.Record }

func (c *captureRecorder) Record(_ context.Context, r metrics.Record) error {
	c.records = append(c.records, r)
	return nil
}

func streamOf(chunks ...message.AssistantChunk) llm.Streamer {
	return func(context.Context, *llm.Request) (llm.StreamResponse, error) {
		return func(yield func(message.AssistantChunk, error) bool) {
			for _, c := range chunks {
				if !yield(c, nil) {
					return
				}
			}
		}, nil
	}
}

func drain(t *testing.T, s llm.Streamer, ctx context.Context) error {
	t.Helper()
	resp, err := s(ctx, &llm.Request{})
	if err != nil {
		return err
	}
	for _, e := range resp {
		if e != nil {
			return e
		}
	}
	return nil
}

func TestMetricsMiddlewareTieredCost(t *testing.T) {
	m := models.Model{
		ID: "p/m",
		Pricing: &api.ModelPricing{
			Input: 1, Output: 2, CacheRead: 0.5,
			Tiers: []api.ModelPricingTier{{Label: "long", ConditionType: "context", ConditionSize: 1000, Input: 2, Output: 4, CacheRead: 1}},
		},
	}
	rec := &captureRecorder{}
	mw := metricsLLMMiddleware(m, rec, false)
	next := streamOf(
		message.AssistantChunk{},
		message.AssistantChunk{Metrics: &message.TokenMetrics{
			Tokens: message.TokenDetails{Input: 2000, Output: 100, CacheRead: 1000},
			Timing: message.TimingMetrics{Processing: time.Second},
		}},
	)
	ctx := scope.With(context.Background(), scope.Scope{Purpose: "chat"})
	if err := drain(t, mw(next), ctx); err != nil {
		t.Fatal(err)
	}
	if len(rec.records) != 1 {
		t.Fatalf("records = %d", len(rec.records))
	}
	r := rec.records[0]
	if r.Kind != metrics.KindLLMCall || r.Status != metrics.StatusOK {
		t.Fatalf("record = %+v", r)
	}
	if r.Attr(metrics.AttrCostTier) != "long" || r.Attr(metrics.AttrPriced) != "true" || r.Attr(metrics.AttrPurpose) != "chat" ||
		r.Attr(metrics.AttrProvider) != "p" || r.Attr(metrics.AttrModel) != "m" {
		t.Fatalf("attrs = %+v", r.Attrs)
	}
	// uncached 1000*2 + output 100*4 + cache read 1000*1 = 3400 per-million units *1000 nano.
	if got := r.Value(metrics.CostTotal); got != 3_400_000 {
		t.Fatalf("cost.total = %d", got)
	}
	if r.Value(metrics.TokensInput) != 2000 || r.Value(metrics.LatencyProcessing) != 1000 {
		t.Fatalf("measurements = %+v", r.Measurements)
	}
}

func TestMetricsMiddlewareUnpricedAndErrors(t *testing.T) {
	rec := &captureRecorder{}
	mw := metricsLLMMiddleware(models.Model{ID: "p/m"}, rec, false)

	ok := streamOf(message.AssistantChunk{Metrics: &message.TokenMetrics{Tokens: message.TokenDetails{Input: 10, Output: 5}}})
	if err := drain(t, mw(ok), context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := rec.records[0]; r.Attr(metrics.AttrPriced) != "false" || r.Value(metrics.CostTotal) != 0 || r.Value(metrics.TokensOutput) != 5 {
		t.Fatalf("unpriced record = %+v", r)
	}

	boom := errors.New("boom")
	failing := func(context.Context, *llm.Request) (llm.StreamResponse, error) { return nil, boom }
	if err := drain(t, mw(failing), context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if r := rec.records[1]; r.Status != metrics.StatusError || r.Attr(metrics.AttrErrorKind) == "" {
		t.Fatalf("error record = %+v", r)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := drain(t, mw(failing), canceled); err == nil {
		t.Fatal("expected error")
	}
	if r := rec.records[2]; r.Status != metrics.StatusCanceled {
		t.Fatalf("canceled record = %+v", r)
	}

	upstreamCanceled := func(context.Context, *llm.Request) (llm.StreamResponse, error) {
		return nil, fmt.Errorf("acquire token: %w", context.Canceled)
	}
	if err := drain(t, mw(upstreamCanceled), context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if r := rec.records[3]; r.Status != metrics.StatusError {
		t.Fatalf("upstream context.Canceled with active ctx should record StatusError, got %+v", r)
	}
}

func TestMetricsMiddlewareConsumerStopsEarly(t *testing.T) {
	rec := &captureRecorder{}
	mw := metricsLLMMiddleware(models.Model{ID: "p/m"}, rec, false)
	resp, err := mw(streamOf(message.AssistantChunk{}, message.AssistantChunk{}))(context.Background(), &llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range resp {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if len(rec.records) != 1 || rec.records[0].Status != metrics.StatusOK {
		t.Fatalf("records = %+v", rec.records)
	}
}

func TestMetricsDecisionMiddlewareNativeSingleRecord(t *testing.T) {
	m := models.Model{ID: "p/dec", Pricing: &api.ModelPricing{Input: 1, Output: 2}}
	rec := &captureRecorder{}
	mw := metricsDecisionMiddleware(m, rec, false)
	eval := mw(func(ctx context.Context, req *decision.Request) (*decision.Response, error) {
		return &decision.Response{
			Model:   "dec",
			Answers: map[string]decision.Answer{"q": {Name: "q", Choice: "yes"}},
			Metrics: &message.TokenMetrics{Tokens: message.TokenDetails{Input: 120, Output: 8}},
		}, nil
	})
	ctx := scope.With(context.Background(), scope.Scope{Purpose: "permission_review"})
	resp, err := eval(ctx, &decision.Request{Model: "dec"})
	if err != nil || resp == nil {
		t.Fatalf("eval: resp=%v err=%v", resp, err)
	}
	if len(rec.records) != 1 {
		t.Fatalf("records = %d, want exactly 1 llm_call", len(rec.records))
	}
	r := rec.records[0]
	if r.Kind != metrics.KindLLMCall || r.Status != metrics.StatusOK {
		t.Fatalf("record = %+v", r)
	}
	if r.Attr(metrics.AttrPurpose) != "permission_review" {
		t.Fatalf("purpose = %q", r.Attr(metrics.AttrPurpose))
	}
	if r.Attr(metrics.AttrProvider) != "p" || r.Attr(metrics.AttrModel) != "dec" {
		t.Fatalf("attrs = %+v", r.Attrs)
	}
}

func TestMetricsDecisionMiddlewareErrorAndCancel(t *testing.T) {
	m := models.Model{ID: "p/dec"}
	rec := &captureRecorder{}
	mw := metricsDecisionMiddleware(m, rec, true)
	boom := errors.New("boom")
	eval := mw(func(ctx context.Context, req *decision.Request) (*decision.Response, error) {
		return nil, boom
	})
	if _, err := eval(context.Background(), &decision.Request{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if len(rec.records) != 1 || rec.records[0].Status != metrics.StatusError {
		t.Fatalf("records = %+v", rec.records)
	}
	if rec.records[0].Attr(metrics.AttrBilling) != metrics.BillingQuota {
		t.Fatalf("billing = %q", rec.records[0].Attr(metrics.AttrBilling))
	}

	rec2 := &captureRecorder{}
	mw2 := metricsDecisionMiddleware(m, rec2, false)
	ctx, cancel := context.WithCancel(context.Background())
	eval2 := mw2(func(ctx context.Context, req *decision.Request) (*decision.Response, error) {
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := eval2(ctx, &decision.Request{}); err == nil {
		t.Fatal("expected canceled error")
	}
	if len(rec2.records) != 1 || rec2.records[0].Status != metrics.StatusCanceled {
		t.Fatalf("records = %+v", rec2.records)
	}
}

func TestGatewayDecisionResponseSchema(t *testing.T) {
	type schemaNode struct {
		Type                 string                `json:"type"`
		Properties           map[string]schemaNode `json:"properties"`
		AdditionalProperties json.RawMessage       `json:"additionalProperties"`
	}
	for providerType, wantAdditional := range map[string]string{
		"openai": "false",
		"codex":  "false",
		"ollama": "",
	} {
		t.Run(providerType, func(t *testing.T) {
			const providerName = "test-provider"
			provider := &schemaRecordingProvider{name: providerType}
			recorder := &captureRecorder{}
			gw := &gatewayImpl{
				cfg: Config{Metrics: recorder},
				providers: map[string]*providerInstance{
					providerName: {
						cfg:     ProviderConfig{Name: providerName, Type: providerType},
						llmProv: provider,
					},
				},
			}
			ctx := scope.With(t.Context(), scope.Scope{Purpose: string(metrics.PurposePermissionReview)})
			model, err := gw.Decision(ctx, models.Model{
				ID:           providerName + "/test-model",
				Capabilities: models.Capabilities{Completion: new(true)},
			}, llm.ModelConfig{})
			if err != nil {
				t.Fatal(err)
			}
			question := decision.Choice("verdict", "Choose a verdict",
				decision.Opt("approve_once", "Approve once"), decision.Opt("defer_to_user", "Ask the user"))
			resp, err := model.Evaluate(ctx, decision.State("test input"), question)
			if err != nil {
				t.Fatal(err)
			}
			if got := resp.Answers["verdict"].Choice; got != "approve_once" {
				t.Errorf("verdict = %q, want approve_once", got)
			}
			if req := provider.lastRequest; req == nil || req.ResponseSchema == nil {
				t.Fatal("provider did not receive a response schema")
			}
			raw, err := json.Marshal(provider.lastRequest.ResponseSchema)
			if err != nil {
				t.Fatal(err)
			}
			var node schemaNode
			if err := json.Unmarshal(raw, &node); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"root", "answers", "verdict", "probabilities"} {
				if name != "root" {
					node = node.Properties[name]
				}
				if node.Type != "object" || string(node.AdditionalProperties) != wantAdditional {
					t.Errorf("%s: type = %q, additionalProperties = %q; want object, %q", name, node.Type, node.AdditionalProperties, wantAdditional)
				}
			}
			if len(recorder.records) != 1 || recorder.records[0].Status != metrics.StatusOK ||
				recorder.records[0].Attr(metrics.AttrPurpose) != string(metrics.PurposePermissionReview) {
				t.Errorf("records = %+v, want one successful permission review call", recorder.records)
			}
		})
	}
}

type schemaRecordingProvider struct {
	capturingProvider
	name string
}

func (p *schemaRecordingProvider) Name() string { return p.name }

func (p *schemaRecordingProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	p.lastRequest = req
	return streamOf(message.AssistantChunk{
		Content: []message.Block{&message.TextBlock{Text: `{"answers":{"verdict":{"refused":false,"choice":"approve_once","probabilities":{"approve_once":0.98,"defer_to_user":0.02}}}}`}},
		Done:    true,
	})(ctx, req)
}
