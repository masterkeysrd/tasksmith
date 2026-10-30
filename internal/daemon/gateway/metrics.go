package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

// metricsLLMMiddleware records one llm_call metric record for every request
// that flows through the model, including failed and canceled ones. Cost is
// estimated here because loom attaches it outside the middleware chain.
func metricsLLMMiddleware(m models.Model, rec metrics.Recorder) llm.Middleware {
	profile := m.Profile()
	return func(next llm.Streamer) llm.Streamer {
		return func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			start := time.Now()
			sc, _ := scope.From(ctx)

			var (
				ttft    time.Duration
				last    *message.TokenMetrics
				streamE error
			)
			finish := func() {
				call := metrics.LLMCall{
					Provider: m.ID.Provider(),
					Model:    m.ID.Name(),
					Purpose:  metrics.Purpose(sc.Purpose),
					Status:   metrics.StatusOK,
					Duration: time.Since(start),
					TTFT:     ttft,
				}
				switch {
				case errors.Is(ctx.Err(), context.Canceled) || errors.Is(streamE, context.Canceled):
					call.Status = metrics.StatusCanceled
				case streamE != nil:
					call.Status = metrics.StatusError
					call.ErrorKind = errorKind(streamE)
				}
				var details message.TokenDetails
				if last != nil {
					details = last.Tokens
					call.Processing = last.Timing.Processing
					call.Generation = last.Timing.Generation
				}
				est := profile.Estimate(details)
				call.Tokens = metrics.Tokens{
					Input:      int64(details.Input),
					Output:     int64(details.Output),
					CacheRead:  int64(details.CacheRead),
					CacheWrite: int64(details.CacheWrite),
					Reasoning:  int64(details.Reasoning),
				}
				call.Cost = metrics.Cost{
					Input:      int64(est.Details.Input),
					Output:     int64(est.Details.Output),
					CacheRead:  int64(est.Details.CacheRead),
					CacheWrite: int64(est.Details.CacheWrite),
					Reasoning:  int64(est.Details.Reasoning),
					Total:      int64(est.Total),
				}
				call.CostTier = est.Tier
				call.Priced = m.Pricing != nil && est.Priced
				if err := metrics.RecordLLMCall(ctx, rec, call); err != nil {
					log.Warn("failed to record llm call metrics", log.String("error", err.Error()))
				}
			}

			resp, err := next(ctx, req)
			if err != nil {
				streamE = err
				finish()
				return nil, err
			}
			if resp == nil {
				finish()
				return nil, nil
			}
			wrapped := func(yield func(message.AssistantChunk, error) bool) {
				defer finish()
				first := true
				for chunk, sErr := range resp {
					if first {
						ttft = time.Since(start)
						first = false
					}
					if chunk.Metrics != nil {
						last = chunk.Metrics
					}
					if sErr != nil {
						streamE = sErr
					}
					if !yield(chunk, sErr) {
						return
					}
				}
			}
			return wrapped, nil
		}
	}
}

func errorKind(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "provider_error"
	}
}
