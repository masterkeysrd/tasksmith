package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
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
		Pricing: &models.Pricing{
			Input: 1, Output: 2, CacheRead: 0.5,
			Tiers: []models.PricingTier{{Label: "long", Condition: models.TierCondition{Type: "context", Size: 1000}, Input: 2, Output: 4, CacheRead: 1}},
		},
	}
	rec := &captureRecorder{}
	mw := metricsLLMMiddleware(m, rec)
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
	mw := metricsLLMMiddleware(models.Model{ID: "p/m"}, rec)

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
}

func TestMetricsMiddlewareConsumerStopsEarly(t *testing.T) {
	rec := &captureRecorder{}
	mw := metricsLLMMiddleware(models.Model{ID: "p/m"}, rec)
	resp, err := mw(streamOf(message.AssistantChunk{}, message.AssistantChunk{}))(context.Background(), &llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range resp {
		break
	}
	if len(rec.records) != 1 || rec.records[0].Status != metrics.StatusOK {
		t.Fatalf("records = %+v", rec.records)
	}
}
