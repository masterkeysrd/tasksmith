package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/openai/openai-go/v3"
)

// formatProviderError inspects provider-specific error types (such as *openai.Error
// or *anthropic.Error) whose Error() methods sanitize away detailed API error
// messages, codes, and parameters. If details are available and not already present in
// err.Error(), formatProviderError returns a wrapped error including those details.
func formatProviderError(err error) error {
	if err == nil {
		return nil
	}

	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) && openaiErr != nil {
		detail := strings.TrimSpace(openaiErr.Message)
		if detail == "" {
			if openaiErr.Code != "" || openaiErr.Param != "" {
				var parts []string
				if openaiErr.Code != "" {
					parts = append(parts, fmt.Sprintf("code: %s", openaiErr.Code))
				}
				if openaiErr.Param != "" {
					parts = append(parts, fmt.Sprintf("param: %s", openaiErr.Param))
				}
				detail = strings.Join(parts, ", ")
			}
			if detail == "" {
				detail = strings.TrimSpace(openaiErr.RawJSON())
			}
		}
		if detail != "" && !strings.Contains(err.Error(), detail) {
			return fmt.Errorf("%w: %s", err, detail)
		}
	}

	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) && anthropicErr != nil {
		var detail string
		raw := strings.TrimSpace(anthropicErr.RawJSON())
		if raw != "" {
			var parsed struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
				Message string `json:"message"`
			}
			if jsonErr := json.Unmarshal([]byte(raw), &parsed); jsonErr == nil {
				if parsed.Error.Message != "" {
					detail = parsed.Error.Message
				} else if parsed.Message != "" {
					detail = parsed.Message
				}
			}
			if detail == "" {
				detail = raw
			}
		}
		if detail != "" && !strings.Contains(err.Error(), detail) {
			return fmt.Errorf("%w: %s", err, detail)
		}
	}

	return err
}

func errorEnrichingLLMMiddleware() llm.Middleware {
	return func(next llm.Streamer) llm.Streamer {
		return func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return nil, formatProviderError(err)
			}
			if resp == nil {
				return nil, nil
			}
			wrappedStream := func(yield func(message.AssistantChunk, error) bool) {
				for chunk, sErr := range resp {
					if sErr != nil {
						if !yield(chunk, formatProviderError(sErr)) {
							return
						}
					} else {
						if !yield(chunk, nil) {
							return
						}
					}
				}
			}
			return wrappedStream, nil
		}
	}
}

func errorEnrichingDecisionMiddleware() decision.Middleware {
	return func(next decision.Evaluator) decision.Evaluator {
		return func(ctx context.Context, req *decision.Request) (*decision.Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return nil, formatProviderError(err)
			}
			return resp, nil
		}
	}
}

func errorEnrichingEmbeddingMiddleware() embedding.Middleware {
	return func(next embedding.Embedder) embedding.Embedder {
		return func(ctx context.Context, req *embedding.Request) (*embedding.Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return nil, formatProviderError(err)
			}
			return resp, nil
		}
	}
}
