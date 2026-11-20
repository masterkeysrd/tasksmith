package providers

import (
	"os"
	"testing"
	"time"

	loomcodex "github.com/masterkeysrd/loom/providers/codex"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"golang.org/x/oauth2"
)

func TestCodexRegistrationAndHostID(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-codex-test")
	t.Cleanup(xdg.ClearCache)
	driver := newCodexDriver()
	hostID, err := driver.hostID()
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Save(loomcodex.Credentials{
		HostID: hostID, ClientID: "test-client", Subject: "test-subject",
		Scopes: []string{"chatgpt.tokens.use.direct"},
		Token:  oauth2.Token{AccessToken: "fixture-access", Expiry: time.Now().Add(time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	path, err := xdg.SubDataDir("credentials", CodexName, "registration.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("registration mode = %o, want 600", got)
	}
	if deleted, err := driver.Logout(); err != nil || !deleted {
		t.Fatalf("Logout() = %v, %v", deleted, err)
	}
	if got, err := driver.hostID(); err != nil || got != hostID {
		t.Fatalf("host ID changed on logout: %q, %v", got, err)
	}
}
