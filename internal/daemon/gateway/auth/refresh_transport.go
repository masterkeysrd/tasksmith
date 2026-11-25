package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	loomauth "github.com/masterkeysrd/loom/auth"
	"golang.org/x/oauth2"
)

// RefreshFunc exchanges a failed access token for a refreshed OAuth token.
type RefreshFunc func(ctx context.Context, failedAccessToken string) (*oauth2.Token, error)

// RefreshableTokenSource is an [oauth2.TokenSource] that can force a token
// refresh when an upstream API rejects the current access token with HTTP 401.
type RefreshableTokenSource interface {
	oauth2.TokenSource
	ForceRefresh(ctx context.Context, failedAccessToken string) (*oauth2.Token, error)
}

// NewRefreshOnUnauthorizedTransport wraps base so that a single HTTP 401
// Unauthorized response triggers refresh once and replays the request with the
// rotated Bearer token. Streaming responses that already returned HTTP 200 are
// never replayed, and a second 401 is returned directly without looping.
func NewRefreshOnUnauthorizedTransport(base http.RoundTripper, refresh RefreshFunc) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if refresh == nil {
		return base
	}
	return &refreshOnUnauthorizedTransport{
		base:    base,
		refresh: refresh,
	}
}

type refreshOnUnauthorizedTransport struct {
	base    http.RoundTripper
	refresh RefreshFunc
}

func (t *refreshOnUnauthorizedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	firstReq := req
	hasBody := req.Body != nil && req.Body != http.NoBody
	if hasBody && req.GetBody == nil {
		bodyBytes, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		firstReq = req.Clone(req.Context())
		firstReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		firstReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
		firstReq.ContentLength = int64(len(bodyBytes))
	}

	sentToken := bearerTokenFromHeader(firstReq.Header.Get("Authorization"))

	resp, err := t.base.RoundTrip(firstReq)
	if err != nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	newTok, refreshErr := t.refresh(req.Context(), sentToken)
	if refreshErr != nil {
		if errors.Is(refreshErr, loomauth.ErrAuthRequired) || errors.Is(refreshErr, ErrMissingCredential) {
			drainAndClose(resp.Body)
			return nil, refreshErr
		}
		return resp, nil
	}
	if newTok == nil || strings.TrimSpace(newTok.AccessToken) == "" {
		return resp, nil
	}

	drainAndClose(resp.Body)

	retryReq := firstReq.Clone(req.Context())
	if hasBody {
		body, err := firstReq.GetBody()
		if err != nil {
			return nil, err
		}
		retryReq.Body = body
	}
	retryReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(newTok.AccessToken))
	return t.base.RoundTrip(retryReq)
}

func bearerTokenFromHeader(header string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 4096)
	_ = body.Close()
}
