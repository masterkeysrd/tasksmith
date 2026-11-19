package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"golang.org/x/oauth2"
)

// cliFakeOAuthDriver completes a PKCE exchange against a local token endpoint
// so `auth login --paste` can run end to end in a test.
type cliFakeOAuthDriver struct {
	tokenURL string
	account  string
}

func (d cliFakeOAuthDriver) OAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "cli-fake",
		ClientSecret: "secret",
		RedirectURL:  "http://localhost:0/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://authorize.invalid/authorize",
			TokenURL: d.tokenURL,
		},
	}
}

func (d cliFakeOAuthDriver) ExtractAccount(*oauth2.Token) string { return d.account }

func TestAuthStatusAndLogoutCommands(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)
	base := []string{"--socket", sockPath, "-w", wsDir}

	out, _, err := execCLI(t, "", append(base, "auth", "status")...)
	if err != nil {
		t.Fatalf("auth status: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "PROVIDER") || !strings.Contains(out, "antigravity") || !strings.Contains(out, "oauth2") {
		t.Fatalf("auth status table = %q, want a provider table listing antigravity/oauth2", out)
	}

	out, _, err = execCLI(t, "", append(base, "auth", "status", "antigravity")...)
	if err != nil {
		t.Fatalf("auth status antigravity: %v", err)
	}
	if !strings.Contains(out, "antigravity: idle") {
		t.Fatalf("signed-out status = %q, want 'antigravity: idle'", out)
	}

	tok := &oauth2.Token{
		AccessToken:  "cli-access",
		RefreshToken: "cli-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	if err := gateway.SaveProviderToken("antigravity", tok, "dev@example.com"); err != nil {
		t.Fatalf("SaveProviderToken: %v", err)
	}

	out, _, err = execCLI(t, "", append(base, "auth", "status", "antigravity")...)
	if err != nil {
		t.Fatalf("auth status after sign-in: %v", err)
	}
	if !strings.Contains(out, "authenticated") || !strings.Contains(out, "dev@example.com") {
		t.Fatalf("signed-in status = %q, want authenticated with the account", out)
	}

	out, _, err = execCLI(t, "", append(base, "auth", "logout", "antigravity")...)
	if err != nil {
		t.Fatalf("auth logout: %v", err)
	}
	if !strings.Contains(out, "Signed out of antigravity") {
		t.Fatalf("logout output = %q", out)
	}
	if gateway.HasProviderToken("antigravity") {
		t.Error("credentials survived `auth logout`")
	}

	out, _, err = execCLI(t, "", append(base, "auth", "logout", "antigravity")...)
	if err != nil {
		t.Fatalf("second auth logout: %v", err)
	}
	if !strings.Contains(out, "was not signed in") {
		t.Fatalf("repeat logout output = %q, want the not-signed-in notice", out)
	}
}

// TestAuthLogoutResolvesSoleOAuthProvider covers the unnamed form: with exactly
// one OAuth preset, the CLI fills the provider in.
func TestAuthLogoutResolvesSoleOAuthProvider(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour).UTC()}
	if err := gateway.SaveProviderToken("antigravity", tok, "solo@example.com"); err != nil {
		t.Fatalf("SaveProviderToken: %v", err)
	}

	out, _, err := execCLI(t, "", "--socket", sockPath, "-w", wsDir, "auth", "logout")
	if err != nil {
		t.Fatalf("auth logout (unnamed): %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "Signed out of antigravity") {
		t.Fatalf("output = %q, want the resolved antigravity logout", out)
	}
}

// TestAuthLoginPasteSurfacesDaemonRejections drives `auth login --paste`
// through the daemon: the CLI prints the authorization URL, resolves an
// unnamed provider to the sole OAuth preset, and reports the daemon's refusal
// of a callback whose state does not match the flow. A pasted state never
// matches here, so the exchange is rejected before any network call.
func TestAuthLoginPasteSurfacesDaemonRejections(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the exchange must not be reached with a mismatched state", http.StatusBadRequest)
	}))
	defer tokenSrv.Close()

	const prov = "cliauth"
	gateway.RegisterOAuthDriver(prov, cliFakeOAuthDriver{tokenURL: tokenSrv.URL, account: "pasted@example.com"})

	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, _, err := execCLI(t, "http://localhost:0/callback?code=cli-code&state=MISMATCH\n",
		"--socket", sockPath, "-w", wsDir, "auth", "login", prov, "--paste", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("a wrong-state paste = (err=%v, out=%q), want a state-mismatch failure", err, out)
	}
	if !strings.Contains(out, "Open this URL in your browser") || !strings.Contains(out, "authorize.invalid") {
		t.Fatalf("login output = %q, want the printed authorization URL", out)
	}

	// An unnamed login resolves to the sole OAuth preset, antigravity.
	out, _, err = execCLI(t, "code=x&state=y\n",
		"--socket", sockPath, "-w", wsDir, "auth", "login", "--paste", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("unnamed login = (err=%v), want the resolved flow to reject the paste", err)
	}
	if !strings.Contains(out, "sign in to antigravity") {
		t.Fatalf("output = %q, want the resolved antigravity sign-in", out)
	}
}

// TestAuthCommandsRequireRunningDaemon proves the CLI never spawns a daemon
// just to read credential status.
func TestAuthCommandsRequireRunningDaemon(t *testing.T) {
	shortTmp := t.TempDir()
	missingSock := shortTmp + "/none.sock"

	_, _, err := execCLI(t, "", "auth", "status", "--socket", missingSock)
	if err == nil || !strings.Contains(err.Error(), "daemon not running") {
		t.Fatalf("auth status against a stopped daemon = %v, want the not-running error", err)
	}
}

// TestReadAuthLine covers the stdin plumbing behind --paste.
func TestReadAuthLine(t *testing.T) {
	line, err := readAuthLine(strings.NewReader("  https://cb.example?code=1 \nrest"))
	if err != nil || line != "https://cb.example?code=1" {
		t.Fatalf("readAuthLine = (%q, %v)", line, err)
	}
	if _, err := readAuthLine(strings.NewReader("   \n")); err == nil {
		t.Error("expected a blank line to be rejected")
	}
	if _, err := readAuthLine(strings.NewReader("")); err == nil {
		t.Error("expected EOF with no content to be rejected")
	}
}
