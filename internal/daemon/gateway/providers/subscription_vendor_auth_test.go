package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	copilot "github.com/microsoft/copilot-sdk/go/v2"
	"github.com/microsoft/copilot-sdk/go/v2/rpc"
)

func TestClaudeAccount(t *testing.T) {
	for _, tt := range []struct {
		name string
		json string
		code int
		want string
		fail bool
	}{
		{name: "subscription", json: `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"alice+work@example.com","subscriptionType":"max"}`, want: "alice+work@example.com"},
		{name: "signed out", json: `{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}`, code: 1},
		{name: "API key", json: `{"loggedIn":true,"authMethod":"api_key","apiProvider":"firstParty"}`},
		{name: "console OAuth", json: `{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}`},
		{name: "gateway", json: `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"gateway","email":"alice@example.com"}`},
		{name: "missing status", json: `{"authMethod":"none","apiProvider":"firstParty"}`, code: 1, fail: true},
		{name: "missing email", json: `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":null}`, fail: true},
		{name: "token as email", json: `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"fixture-secret"}`, fail: true},
		{name: "terminal escape in email", json: `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"alice\u001b@example.com"}`, fail: true},
		{name: "non-JSON failure", json: `fixture-secret`, code: 1, fail: true},
		{name: "unexpected exit status", json: `{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}`, code: 2, fail: true},
		{name: "inconsistent exit status", json: `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"alice@example.com"}`, code: 1, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cli := fakeVendorAuthCLI(t, "CLAUDE_CONFIG_DIR", fmt.Sprintf(`
[ "$#" = 3 ] && [ "$1" = auth ] && [ "$2" = status ] && [ "$3" = --json ] || exit 99
printf '%%s\n' '%s'
printf '%%s\n' fixture-secret >&2
exit %d
`, tt.json, tt.code))
			account, ok, err := claudeAccount(t.Context(), cli)
			if account != tt.want || ok != (tt.want != "") || (err != nil) != tt.fail {
				t.Fatalf("claudeAccount = (%q, %t, %v), want (%q, %t, fail=%t)", account, ok, err, tt.want, tt.want != "", tt.fail)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("auth error exposed vendor output")
			}
		})
	}
}

func TestClaudeLogout(t *testing.T) {
	cli := fakeVendorAuthCLI(t, "CLAUDE_CONFIG_DIR", `
case "$*" in
  'auth status --json')
    if [ -f signed-out ]; then
      printf '%s\n' '{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}'
      exit 1
    fi
    printf '%s\n' '{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"alice@example.com"}'
    ;;
  'auth logout')
    [ ! -f signed-out ] || exit 99
    : > signed-out
    ;;
  *) exit 99 ;;
esac
`)
	if removed, err := claudeLogout(t.Context(), cli); !removed || err != nil {
		t.Fatalf("first claudeLogout = (%t, %v), want (true, nil)", removed, err)
	}
	if removed, err := claudeLogout(t.Context(), cli); removed || err != nil {
		t.Fatalf("second claudeLogout = (%t, %v), want (false, nil)", removed, err)
	}
}

func TestVendorAuthLoginCommands(t *testing.T) {
	for _, tt := range []struct {
		name    string
		profile string
		args    string
		login   func(context.Context, auth.VendorCLI) error
	}{
		{"Claude", "CLAUDE_CONFIG_DIR", "auth login --claudeai", claudeLogin},
		{"Copilot", "COPILOT_HOME", "--no-auto-update login --web-flow", copilotLogin},
	} {
		t.Run(tt.name, func(t *testing.T) {
			command := fmt.Sprintf("[ \"$*\" = '%s' ] || exit 99\n", tt.args)
			cli := fakeVendorAuthCLI(t, tt.profile, command)
			if err := tt.login(t.Context(), cli); err != nil {
				t.Fatal(err)
			}
			failing := fakeVendorAuthCLI(t, tt.profile, command+"printf '%s' fixture-secret >&2; exit 1\n")
			if err := tt.login(t.Context(), failing); err == nil || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("want redacted login error, got %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := tt.login(ctx, cli); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled login error = %v", err)
			}
		})
	}
}

func TestCopilotLoginStopsAfterFailedPreflight(t *testing.T) {
	cli := fakeVendorAuthCLI(t, "COPILOT_HOME", `
if [ "$*" = '--no-auto-update login --web-flow' ]; then
  : > login-attempted
  exit 0
fi
printf '%s' fixture-secret >&2
exit 1
`)
	if err := copilotLoginCompatible(t.Context(), cli); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("expected a redacted preflight error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(cli.WorkDir, "login-attempted")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("browser login was not gated by the preflight: %v", err)
	}
}

func TestCopilotAccountIdentity(t *testing.T) {
	for _, tt := range []struct {
		name          string
		authenticated bool
		kind          string
		login         string
		host          string
		want          rpc.AuthInfo
		fail          bool
	}{
		{name: "signed out"},
		{name: "stored OAuth", authenticated: true, kind: "user", login: "alice-work", host: "https://github.com", want: rpc.UserAuthInfo{Host: "https://github.com", Login: "alice-work"}},
		{name: "managed account", authenticated: true, kind: "account", login: "alice_work", host: "example.ghe.com", want: rpc.AccountAuthInfo{Host: "example.ghe.com", Login: "alice_work"}},
		{name: "GitHub CLI fallback", authenticated: true, kind: "gh-cli", login: "alice", host: "https://github.com"},
		{name: "environment token", authenticated: true, kind: "env"},
		{name: "missing login", authenticated: true, kind: "user", host: "https://github.com", fail: true},
		{name: "control character", authenticated: true, kind: "user", login: "alice\x1b", host: "https://github.com", fail: true},
		{name: "invalid host", authenticated: true, kind: "user", login: "alice", host: "https://fixture-secret@github.com", fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status := copilot.GetAuthStatusResponse{IsAuthenticated: tt.authenticated, AuthType: &tt.kind, Login: &tt.login, Host: &tt.host}
			account, identity, err := parseCopilotAccount(status)
			if (err != nil) != tt.fail || identity != tt.want {
				t.Fatalf("identity = %v, err = %v; want %v, fail = %t", identity, err, tt.want, tt.fail)
			}
			if (tt.want == nil && account != "") || (tt.want != nil && account != tt.login) {
				t.Fatalf("unexpected account %q", account)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("auth error exposed vendor output")
			}
		})
	}
}

func fakeVendorAuthCLI(t *testing.T, configKey, script string) auth.VendorCLI {
	t.Helper()
	root := t.TempDir()
	workDir, profileDir := filepath.Join(root, "work"), filepath.Join(root, "profile")
	for _, dir := range []string{workDir, profileDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(root, "fake-vendor")
	checks := fmt.Sprintf("[ %s -ef \".\" ] && [ %s -ef \"%s\" ] || exit 98\n", `"$AUTH_TEST_WORK"`, `"$AUTH_TEST_PROFILE"`, "$"+configKey)
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"+checks+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return auth.VendorCLI{
		Executable: executable,
		ProfileDir: profileDir,
		WorkDir:    workDir,
		Env:        []string{configKey + "=" + profileDir, "AUTH_TEST_PROFILE=" + profileDir, "AUTH_TEST_WORK=" + workDir},
	}
}
