package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// quotaReader fetches the live quota of the model a call is running on. It may
// contact the provider, so it runs only on the error path, never on the hot
// success path.
type quotaReader func(ctx context.Context) (*api.ModelQuota, error)

// rateLimitMarkers are the phrases a provider leaves in an error once the
// status code has been flattened into a message. They are deliberately
// specific: a bare "429" would also match token counts and request IDs.
var rateLimitMarkers = []string{
	"resource_exhausted",
	"resource exhausted",
	"rate limit",
	"rate_limit",
	"ratelimit",
	"too many requests",
	"quota exceeded",
	"quota exhausted",
	"status 429",
	"(429",
	"429:",
}

// IsRateLimited reports whether err represents a provider rejecting a call for
// lack of capacity: a typed 429 from a provider SDK, or the equivalent wording
// when a provider has already flattened the status into a message string.
func IsRateLimited(err error) bool {
	if err == nil {
		return false
	}

	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) && openaiErr != nil && openaiErr.StatusCode == http.StatusTooManyRequests {
		return true
	}

	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) && anthropicErr != nil && anthropicErr.StatusCode == http.StatusTooManyRequests {
		return true
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range rateLimitMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// enrichRateLimit appends objective quota facts to a rate-limit error from a
// quota-billed provider: how much capacity is left and when the window resets.
// Non-rate-limit errors, providers without quota, and failed quota lookups are
// returned untouched, so a message never claims a reset time the provider did
// not actually report.
func enrichRateLimit(ctx context.Context, err error, readQuota quotaReader) error {
	if err == nil || readQuota == nil || !IsRateLimited(err) {
		return err
	}

	deadline, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	quota, qerr := readQuota(deadline)
	if qerr != nil || quota == nil {
		return err
	}

	fact := quotaFacts(*quota, time.Now())
	if fact == "" || strings.Contains(err.Error(), fact) {
		return err
	}
	return fmt.Errorf("%w · %s", err, fact)
}

// quotaFacts renders the objective state of a model's quota buckets, e.g.
// "0% quota remaining · resets in 42m at 15:20" (or for multi-bucket models,
// "5h: 0% quota remaining · resets in 42m at 15:20 · 7d: 64% remaining").
// A window with no reported reset still states the remaining share. When the
// bottleneck bucket is full or unlimited, it returns empty: a rate-limit error
// contradicts a full window, so a percentage would be noise.
func quotaFacts(q api.ModelQuota, now time.Time) string {
	bottleneck, ok := q.Bottleneck()
	if !ok || bottleneck.Unlimited {
		return ""
	}
	pct := int(bottleneck.RemainingFraction * 100)
	if pct >= 100 {
		return ""
	}
	pct = max(pct, 0)

	multi := len(q.Buckets) > 1
	var parts []string
	head := fmt.Sprintf("%d%% quota remaining", pct)
	if multi && bottleneck.ID != "" {
		head = fmt.Sprintf("%s: %s", bottleneck.ID, head)
	}
	parts = append(parts, head)

	if bottleneck.ResetAt != nil && !bottleneck.ResetAt.IsZero() {
		reset := bottleneck.ResetAt.Local()
		when := "resets at " + reset.Format("15:04")
		if until := reset.Sub(now); until > 0 {
			when = "resets in " + format.Duration(until, format.Compact()) + " at " + reset.Format("15:04")
		}
		parts = append(parts, when)
	}

	if multi {
		for _, b := range q.Buckets {
			if b.ID == bottleneck.ID || b.Unlimited {
				continue
			}
			bpct := int(b.RemainingFraction * 100)
			if bpct >= 100 {
				continue
			}
			bpct = max(bpct, 0)
			if b.ID != "" {
				parts = append(parts, fmt.Sprintf("%s: %d%% remaining", b.ID, bpct))
			}
		}
	}
	return strings.Join(parts, " · ")
}

// quotaReaderFor binds a quota reader to the model a call runs on.
func (g *gatewayImpl) quotaReaderFor(m models.Model) quotaReader {
	return func(ctx context.Context) (*api.ModelQuota, error) {
		if !g.QuotaCapable(m) {
			return nil, nil
		}
		return g.Quota(ctx, m)
	}
}
