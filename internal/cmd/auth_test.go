package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
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
	header, _, _ := strings.Cut(out, "\n")
	if strings.Join(strings.Fields(header), " ") != "PROVIDER AUTH STATUS ACCOUNT SCOPE" ||
		!strings.Contains(strings.Join(strings.Fields(out), " "), "antigravity oauth2 needs credentials - system") {
		t.Fatalf("unexpected auth status table: %q", out)
	}

	out, stderr, err := execCLI(t, "", append(base, "auth", "status", "antigravity")...)
	if err != nil || stderr != "" || !strings.HasPrefix(out, "Provider:") || strings.Contains(out, "\n\n") {
		t.Fatalf("auth status antigravity: stdout=%q stderr=%q err=%v", out, stderr, err)
	}
	fields := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"Provider: antigravity", "Auth: oauth2", "Status: idle",
		"Scope: system", "Default model:", "Models:",
	} {
		if !strings.Contains(fields, want) {
			t.Errorf("missing %q in status: %q", want, out)
		}
	}

	tok := &oauth2.Token{
		AccessToken:  "cli-access",
		RefreshToken: "cli-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	if err := auth.SaveProviderToken("antigravity", tok, "dev@example.com"); err != nil {
		t.Fatalf("SaveProviderToken: %v", err)
	}

	out, _, err = execCLI(t, "", append(base, "auth", "status", "antigravity")...)
	if err != nil {
		t.Fatalf("auth status after sign-in: %v", err)
	}
	fields = strings.Join(strings.Fields(out), " ")
	if !strings.Contains(fields, "Status: authenticated") || !strings.Contains(fields, "Account: dev@example.com") {
		t.Fatalf("signed-in status = %q, want authenticated with the account", out)
	}

	out, _, err = execCLI(t, "", append(base, "auth", "logout", "antigravity")...)
	if err != nil {
		t.Fatalf("auth logout: %v", err)
	}
	if !strings.Contains(out, "Signed out of antigravity") {
		t.Fatalf("logout output = %q", out)
	}
	if auth.HasProviderToken("antigravity") {
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

func TestAuthStatusJSON(t *testing.T) {
	_, sockPath, _ := setupCLITestDaemon(t, nil)
	base := []string{"--socket", sockPath, "auth", "status", "--json"}

	out, stderr, err := execCLI(t, "", base...)
	var providers []struct {
		Name string `json:"name"`
	}
	if err != nil || stderr != "" || !strings.HasPrefix(out, "[\n  {") ||
		json.Unmarshal([]byte(out), &providers) != nil || len(providers) == 0 || providers[0].Name == "" {
		t.Fatalf("provider list: stdout=%q stderr=%q err=%v", out, stderr, err)
	}

	for _, tc := range []struct{ provider, status string }{
		{"antigravity", "idle"},
		{"ollama", "local"},
	} {
		out, stderr, err := execCLI(t, "", append(base, tc.provider)...)
		var details authStatusDetails
		if err != nil || stderr != "" || !strings.HasPrefix(out, "{\n  ") || json.Unmarshal([]byte(out), &details) != nil {
			t.Fatalf("provider details: stdout=%q stderr=%q err=%v", out, stderr, err)
		}
		if details.Provider != tc.provider || details.Status != tc.status || details.Scope != "system" {
			t.Errorf("unexpected details for %s: %s", tc.provider, out)
		}
	}

	out, _, err = execCLI(t, "", append(base, "unknown-provider")...)
	if err == nil || out != "" {
		t.Fatalf("unknown provider: stdout=%q err=%v", out, err)
	}
}

func TestAuthLogoutRequiresNameWithMultiplePresets(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour).UTC()}
	if err := auth.SaveProviderToken("antigravity", tok, "solo@example.com"); err != nil {
		t.Fatalf("SaveProviderToken: %v", err)
	}

	_, _, err := execCLI(t, "", "--socket", sockPath, "-w", wsDir, "auth", "logout")
	if err == nil || !strings.Contains(err.Error(), "multiple OAuth providers") ||
		!strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "antigravity") {
		t.Fatalf("unnamed auth logout should list both candidates, got %v", err)
	}
	if !auth.HasProviderToken("antigravity") {
		t.Fatal("ambiguous logout deleted a provider credential")
	}
}

// TestAuthLoginPasteSurfacesDaemonRejections drives `auth login --paste`
// through the daemon: the CLI prints the authorization URL and reports the
// daemon's refusal of a callback whose state does not match. The pasted state never
// matches here, so the exchange is rejected before any network call.
func TestAuthLoginPasteSurfacesDaemonRejections(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the exchange must not be reached with a mismatched state", http.StatusBadRequest)
	}))
	defer tokenSrv.Close()

	const prov = "cliauth"
	auth.RegisterOAuthDriver(prov, cliFakeOAuthDriver{tokenURL: tokenSrv.URL, account: "pasted@example.com"})

	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, _, err := execCLI(t, "http://localhost:0/callback?code=cli-code&state=MISMATCH\n",
		"--socket", sockPath, "-w", wsDir, "auth", "login", prov, "--paste", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("a wrong-state paste = (err=%v, out=%q), want a state-mismatch failure", err, out)
	}
	if !strings.Contains(out, "Open this URL in your browser") || !strings.Contains(out, "authorize.invalid") {
		t.Fatalf("login output = %q, want the printed authorization URL", out)
	}

	out, _, err = execCLI(t, "code=x&state=y\n",
		"--socket", sockPath, "-w", wsDir, "auth", "login", "antigravity", "--paste", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("antigravity login = (err=%v), want the flow to reject the paste", err)
	}
	if !strings.Contains(out, "sign in to antigravity") {
		t.Fatalf("output = %q, want the antigravity sign-in", out)
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
