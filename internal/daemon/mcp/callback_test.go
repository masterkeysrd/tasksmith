package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/warp"
)

func TestParseCallbackSubmissionReadsWhatABrowserShows(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantCode  string
		wantState string
		wantIss   string
		wantErr   string
		rejected  bool
	}{
		{
			name:     "full redirect URL",
			raw:      "http://localhost:56014/callback?code=abc123&state=xyz789",
			wantCode: "abc123", wantState: "xyz789",
		},
		{
			name:     "issuer parameter survives",
			raw:      "http://localhost:56014/callback?code=abc&state=x&iss=https%3A%2F%2Fid.example.com",
			wantCode: "abc", wantState: "x", wantIss: "https://id.example.com",
		},
		{
			name:     "bare query string",
			raw:      "?code=abc123&state=xyz789",
			wantCode: "abc123", wantState: "xyz789",
		},
		{
			name:     "query string without a leading mark",
			raw:      "code=abc123&state=xyz789",
			wantCode: "abc123", wantState: "xyz789",
		},
		{
			name:     "parameters in the fragment",
			raw:      "http://localhost:56014/callback#code=abc123&state=xyz789",
			wantCode: "abc123", wantState: "xyz789",
		},
		{
			name:     "bare authorization code",
			raw:      "  abc123  ",
			wantCode: "abc123",
		},
		{
			name:     "denied consent",
			raw:      "http://localhost:56014/callback?error=access_denied",
			rejected: true,
		},
		{
			name:    "empty paste",
			raw:     "   ",
			wantErr: "paste the redirect URL",
		},
		{
			name:    "URL that carries no code",
			raw:     "http://localhost:56014/callback",
			wantErr: "carries no authorization code",
		},
		{
			name:    "query string without a code",
			raw:     "?state=xyz789",
			wantErr: "carries no authorization code",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCallbackSubmission(tc.raw)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseCallbackSubmission(%q) = %+v, want error", tc.raw, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseCallbackSubmission(%q) error = %q, want it to contain %q", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCallbackSubmission(%q) returned %v", tc.raw, err)
			}
			if tc.rejected {
				if got.err == nil {
					t.Fatalf("parseCallbackSubmission(%q) = %+v, want a rejection error", tc.raw, got)
				}
				if !strings.Contains(got.err.Error(), "access_denied") {
					t.Fatalf("rejection error = %q, want it to name the OAuth error", got.err)
				}
				if !errors.Is(got.err, ErrAuthRequired) {
					t.Fatalf("rejection error = %v, want it to stay an auth-required error", got.err)
				}
				return
			}
			if got.code != tc.wantCode || got.state != tc.wantState || got.iss != tc.wantIss {
				t.Fatalf("parseCallbackSubmission(%q) = %+v, want code=%q state=%q iss=%q",
					tc.raw, got, tc.wantCode, tc.wantState, tc.wantIss)
			}
		})
	}
}

func TestSubmitCallbackURLResolvesAWaitingFlow(t *testing.T) {
	receiver := newCallbackReceiver("github")
	t.Cleanup(receiver.close)

	if err := receiver.SubmitCallbackURL("http://localhost:56014/callback?code=pasted-code&state=st1"); err != nil {
		t.Fatalf("SubmitCallbackURL returned %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	code, state, _, err := receiver.wait(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("wait returned %v", err)
	}
	if code != "pasted-code" || state != "st1" {
		t.Fatalf("wait returned code=%q state=%q, want pasted-code/st1", code, state)
	}
}

func TestSubmitCallbackURLRejectsWhenNothingIsWaiting(t *testing.T) {
	t.Run("closed receiver", func(t *testing.T) {
		receiver := newCallbackReceiver("github")
		receiver.close()
		err := receiver.SubmitCallbackURL("code=abc")
		if err == nil || !strings.Contains(err.Error(), "no longer waiting") {
			t.Fatalf("SubmitCallbackURL on a closed receiver = %v, want a no-longer-waiting error", err)
		}
		if !strings.Contains(err.Error(), "github") {
			t.Fatalf("error = %q, want it to name the server", err)
		}
	})

	t.Run("callback already delivered", func(t *testing.T) {
		receiver := newCallbackReceiver("github")
		t.Cleanup(receiver.close)
		if err := receiver.SubmitCallbackURL("code=first"); err != nil {
			t.Fatalf("first SubmitCallbackURL returned %v", err)
		}
		err := receiver.SubmitCallbackURL("code=second")
		if err == nil || !strings.Contains(err.Error(), "already received a callback") {
			t.Fatalf("second SubmitCallbackURL = %v, want an already-received error", err)
		}
	})

	t.Run("unreadable paste", func(t *testing.T) {
		receiver := newCallbackReceiver("github")
		t.Cleanup(receiver.close)
		if err := receiver.SubmitCallbackURL(""); err == nil {
			t.Fatal("SubmitCallbackURL with an empty value returned nil")
		}
	})
}

func TestOAuthProviderSubmitCallbackURLNamesTheServer(t *testing.T) {
	provider := &oauthProvider{name: "sentry"}
	err := provider.submitCallbackURL("code=abc")
	if err == nil {
		t.Fatal("submitCallbackURL without a receiver returned nil")
	}
	for _, want := range []string{"sentry", "/mcp auth sentry"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestOAuthProviderTracksTheActiveReceiver(t *testing.T) {
	receiver := newCallbackReceiver("sentry")
	t.Cleanup(receiver.close)
	provider := &oauthProvider{name: "sentry"}

	if err := provider.submitCallbackURL("code=abc"); err == nil {
		t.Fatal("submitCallbackURL before a flow started returned nil")
	}

	provider.setReceiver(receiver)
	if err := provider.submitCallbackURL("code=abc"); err != nil {
		t.Fatalf("submitCallbackURL while a flow was in flight returned %v", err)
	}

	other := newCallbackReceiver("sentry")
	t.Cleanup(other.close)
	provider.clearReceiver(other)
	provider.mu.Lock()
	current := provider.receiver
	provider.mu.Unlock()
	if current != receiver {
		t.Fatal("clearReceiver dropped a receiver other than the one it was given")
	}

	provider.clearReceiver(receiver)
	if err := provider.submitCallbackURL("code=def"); err == nil {
		t.Fatal("submitCallbackURL after the flow finished returned nil")
	}
}

func TestCallbackPageSuccessStates(t *testing.T) {
	receiver := newCallbackReceiver("github")
	if err := receiver.start(); err != nil {
		t.Fatalf("start returned %v", err)
	}
	t.Cleanup(receiver.close)

	resp, err := http.Get(receiver.url + "?code=abc123&state=xyz")
	if err != nil {
		t.Fatalf("GET callback returned %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body returned %v", err)
	}
	page := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}

	for _, want := range []string{
		"Authentication Complete",
		"Connected <strong>github</strong> to TaskSmith.",
		"You can close this tab and return to your terminal.",
		`onclick="window.close()"`,
		"Close Window",
		"Return to TaskSmith in your terminal to continue.",
		"<svg xmlns",
		`rel="icon" type="image/svg+xml" href="data:image/svg+xml,`,
		"@media (prefers-color-scheme: light)",
		callbackSurface, callbackBackdrop, callbackInk, callbackInkSecondary,
		callbackSuccess, callbackSuccessDeep,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("success page does not contain %q\n%s", want, page)
		}
	}
	if strings.Contains(page, "Authentication Failed") {
		t.Fatalf("success page rendered the failure heading:\n%s", page)
	}

	select {
	case result := <-receiver.codes:
		if result.code != "abc123" || result.state != "xyz" {
			t.Fatalf("delivered callback = %+v, want code=abc123 state=xyz", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the callback page did not deliver the redirect to the waiting flow")
	}
}

func TestCallbackPageErrorState(t *testing.T) {
	receiver := newCallbackReceiver("github")
	if err := receiver.start(); err != nil {
		t.Fatalf("start returned %v", err)
	}
	t.Cleanup(receiver.close)

	resp, err := http.Get(receiver.url + "?error=access_denied&error_description=user+said+no")
	if err != nil {
		t.Fatalf("GET callback returned %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body returned %v", err)
	}
	page := string(body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	for _, want := range []string{
		"Authentication Failed",
		"<code>access_denied</code>",
		"<span>user said no</span>",
		callbackFailure, callbackFailureDeep,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("failure page does not contain %q\n%s", want, page)
		}
	}

	select {
	case result := <-receiver.codes:
		if result.err == nil || !strings.Contains(result.err.Error(), "access_denied") {
			t.Fatalf("delivered callback = %+v, want an access_denied error", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the callback page did not deliver the rejection to the waiting flow")
	}
}

func TestCallbackPageEscapesValuesFromTheRedirect(t *testing.T) {
	page := callbackErrorHTML("<script>alert(1)</script>", "err\"><script>x", "desc <b>")
	if strings.Contains(page, "<script>") {
		t.Fatalf("callback page echoed unescaped markup:\n%s", page)
	}
	for _, want := range []string{"&lt;script&gt;", "&lt;b&gt;"} {
		if !strings.Contains(page, want) {
			t.Fatalf("callback page does not contain escaped %q", want)
		}
	}

	success := callbackSuccessHTML("<img src=x onerror=alert(1)>")
	if strings.Contains(success, "<img src=x") {
		t.Fatalf("success page echoed unescaped markup:\n%s", success)
	}
}

func TestCallbackPageNamesUnknownServersWithoutAnEmptyCard(t *testing.T) {
	page := callbackSuccessHTML("   ")
	if !strings.Contains(page, "<strong>MCP server</strong>") {
		t.Fatalf("page for an unnamed server does not fall back to a label:\n%s", page)
	}
}

func TestCallbackFailureDetailsOmitsAnEmptyBlock(t *testing.T) {
	if details := callbackFailureDetails("", ""); details != "" {
		t.Fatalf("callbackFailureDetails with nothing to show = %q, want an empty string", details)
	}
	if details := callbackFailureDetails("invalid_grant", ""); !strings.Contains(details, "<code>invalid_grant</code>") {
		t.Fatalf("details = %q, want the error code alone", details)
	}
}

func TestDataURISVGEscapesAttributeBreakingCharacters(t *testing.T) {
	got := dataURISVG("<svg a=\"b\">\n<path fill=\"#2ac3de\"/>\n</svg>")
	for _, forbidden := range []string{"<", ">", "#", "\"", "\n"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("data URIs kept %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"%3C", "%3E", "%23", "'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("data URIs missing %q: %s", want, got)
		}
	}
}

func TestCallbackLogoIsTheTaskSmithMark(t *testing.T) {
	if !strings.Contains(callbackLogoSVG, `viewBox="0 0 240 160"`) {
		t.Fatalf("embedded logo = %.120s, want the TaskSmith mark", callbackLogoSVG)
	}
	for _, want := range []string{callbackCyan, callbackSuccessDeep, callbackFailure} {
		if !strings.Contains(callbackLogoSVG, want) {
			t.Fatalf("embedded logo missing colour %s", want)
		}
	}
}

// TestSubmitMCPAuthCallbackHandsThePasteToTheWaitingFlow checks the manager finds the
// provider belonging to an in-flight authorization and delivers the pasted redirect to
// it, which is what lets a browser on another machine finish the flow.
func TestSubmitMCPAuthCallbackHandsThePasteToTheWaitingFlow(t *testing.T) {
	def := remoteDef("secure", "https://mcp.example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "oauth2"}
	})
	mgr, _, _ := newManager(t, def)

	_, provider, err := mgr.buildConfig(def, map[string]string{}, warp.MCPRenderOptions{})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	state := mgr.state(def)
	state.mu.Lock()
	state.provider = provider
	state.mu.Unlock()

	receiver := newCallbackReceiver("secure")
	t.Cleanup(receiver.close)
	provider.setReceiver(receiver)

	if err := mgr.SubmitMCPAuthCallback(context.Background(), "secure", "code=pasted"); err != nil {
		t.Fatalf("SubmitMCPAuthCallback returned %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	code, _, _, err := receiver.wait(ctx, 2*time.Second)
	if err != nil {
		t.Fatalf("wait returned %v", err)
	}
	if code != "pasted" {
		t.Fatalf("received code = %q, want pasted", code)
	}
}

// TestSubmitMCPAuthCallbackExplainsWhenNothingIsWaiting covers the two ways a paste can
// arrive with no flow behind it: a server the daemon has never started, and one whose
// authorization already finished. Both should say how to start one.
func TestSubmitMCPAuthCallbackExplainsWhenNothingIsWaiting(t *testing.T) {
	def := remoteDef("secure", "https://mcp.example.invalid/mcp", func(d *warp.MCP) {
		d.Spec.Auth = &warp.ProviderAuth{Type: "oauth2"}
	})
	mgr, _, _ := newManager(t, def)

	err := mgr.SubmitMCPAuthCallback(context.Background(), "secure", "code=abc")
	if err == nil || !strings.Contains(err.Error(), "/mcp auth secure") {
		t.Fatalf("SubmitMCPAuthCallback with no provider = %v, want guidance to start an authorization", err)
	}

	if err := mgr.SubmitMCPAuthCallback(context.Background(), "absent", "code=abc"); err == nil {
		t.Fatal("SubmitMCPAuthCallback for an unknown server returned nil")
	} else if !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("error = %v, want a not-found error", err)
	}
}
