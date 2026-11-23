package providers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

func fakeSubscriptionBinary(t *testing.T, binary, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, binary), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestClaudeRuntimeIsolatesEnvironment(t *testing.T) {
	fakeSubscriptionBinary(t, "claude", `printf '%s\n' "$PWD" "$5" "$ANTHROPIC_API_KEY" "$GH_TOKEN" "$TASKSMITH_PRIVATE" "$CLAUDE_CONFIG_DIR" "$LANG"`)
	t.Setenv("ANTHROPIC_API_KEY", "api-key-not-for-cli")
	t.Setenv("GH_TOKEN", "github-token-not-for-cli")
	t.Setenv("TASKSMITH_PRIVATE", "daemon-only")
	t.Setenv("CLAUDE_CONFIG_DIR", "host-profile-not-for-cli")
	t.Setenv("LANG", "test'locale")
	profileDir := filepath.Join(t.TempDir(), "profile with 'quotes'")
	cli, err := claudeCodeOAuth.newCLI(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	runtime := claudeRuntime(cli)
	if runtime.Path != cli.executable || runtime.ConfigDir != profileDir || runtime.SourceAuthDir != "" || runtime.Env == nil {
		t.Fatal("Claude runtime does not use the isolated executable, profile, and environment")
	}
	if want := []string{"--setting-sources", "", "--strict-mcp-config", "--no-chrome"}; !slices.Equal(runtime.Args, want) {
		t.Fatalf("Claude runtime args = %q, want %q", runtime.Args, want)
	}
	argument := "literal argument with 'quotes'"
	cmd := exec.CommandContext(t.Context(), runtime.Path, append(runtime.Args, argument)...)
	cmd.Dir = runtime.WorkingDirectory
	cmd.Env = runtime.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("launch Claude fixture: %v: %s", err, out)
	}
	workingDir, err := filepath.EvalSymlinks(cli.workDir)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{workingDir, argument, "", "", "", profileDir, "test'locale", ""}, "\n")
	if string(out) != want {
		t.Fatalf("Claude fixture output = %q, want %q", out, want)
	}
}

type subscriptionFixtureBackend struct {
	llm.Provider
	closed  int
	onClose func()
}

func (b *subscriptionFixtureBackend) Close() error {
	b.closed++
	if b.onClose != nil {
		b.onClose()
	}
	return nil
}

func isolateSubscriptionData(t *testing.T) {
	t.Helper()
	t.Cleanup(xdg.ClearCache)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-subscription-test")
	xdg.ClearCache()
}

func assertSubscriptionRetired(t *testing.T, provider *subscriptionFixtureBackend) {
	t.Helper()
	if provider.closed != 1 {
		t.Fatalf("provider close calls = %d, want 1", provider.closed)
	}
}

func TestSubscriptionAccountReplacementAndLogout(t *testing.T) {
	isolateSubscriptionData(t)
	fakeSubscriptionBinary(t, "claude", "exit 0")
	ctx := t.Context()
	var created []*subscriptionFixtureBackend
	loggedOut := make(chan string, 4)
	d := &subscriptionDriver[*subscriptionFixtureBackend]{
		name: ClaudeCodeName, newCLI: claudeCodeOAuth.newCLI,
		login:   func(context.Context, vendorCLI) error { return nil },
		account: func(context.Context, vendorCLI) (string, bool, error) { return "fixture", true, nil },
		logout: func(_ context.Context, cli vendorCLI) (bool, error) {
			loggedOut <- filepath.Base(cli.profileDir)
			return true, nil
		},
		build: func(vendorCLI) (*subscriptionFixtureBackend, error) {
			b := &subscriptionFixtureBackend{}
			created = append(created, b)
			return b, nil
		},
	}
	credentials := &auth.Config{Type: auth.TypeOAuth2}
	open := func() *subscriptionFixtureBackend {
		t.Helper()
		provider, err := d.open(ClaudeCodeName, credentials, false)
		if err != nil {
			t.Fatal(err)
		}
		return provider
	}
	waitForLogout := func() string {
		t.Helper()
		select {
		case profile := <-loggedOut:
			return profile
		case <-time.After(2 * time.Second):
			t.Fatal("vendor cleanup was not attempted")
			return ""
		}
	}
	signIn := func() string {
		t.Helper()
		commit, err := d.Authorize(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if account, err := commit(); account != "fixture" || err != nil {
			t.Fatalf("commit = %q, %v", account, err)
		}
		return d.CredentialRevision()
	}
	first := signIn()
	provider := open()
	reloaded := open()
	if len(created) != 1 || provider != reloaded {
		t.Fatalf("opens allocated %d backends or did not share the active provider", len(created))
	}

	canceled, cancel := context.WithCancel(ctx)
	commit, err := d.Authorize(canceled)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := commit(); !errors.Is(err, context.Canceled) || d.CredentialRevision() != first || provider.closed != 0 {
		t.Fatalf("canceled replacement changed active registration or runtime: %v", err)
	}
	if abandoned := waitForLogout(); abandoned == first {
		t.Fatal("canceled replacement attempted to sign out the active account")
	}

	second := signIn()
	if first == "" || second == "" || first == second || waitForLogout() != first {
		t.Fatalf("replacement did not retire previous registration: %q, %q", first, second)
	}
	assertSubscriptionRetired(t, provider)
	next := open()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("repeat close: %v", err)
	}
	assertSubscriptionRetired(t, next)
	if d.CredentialRevision() != second {
		t.Fatal("driver shutdown changed the saved registration")
	}
	select {
	case profile := <-loggedOut:
		t.Fatalf("driver shutdown signed out %q", profile)
	default:
	}

	reopened := open()
	if reopened == next || len(created) != 3 {
		t.Fatal("opening after shutdown reused the retired provider")
	}
	if deleted, err := d.Logout(); !deleted || err != nil {
		t.Fatalf("logout = %v, %v", deleted, err)
	}
	assertSubscriptionRetired(t, reopened)
	if got := waitForLogout(); got != second {
		t.Fatalf("logged out profile %q, want %q", got, second)
	}
	if _, ok := d.Account(); ok {
		t.Fatal("registration still exists after logout")
	}
}

func TestSubscriptionOpenReplacesExternallyChangedRegistration(t *testing.T) {
	isolateSubscriptionData(t)
	var builtProfiles []string
	var startParallelOpen func()
	d := &subscriptionDriver[*subscriptionFixtureBackend]{
		name: ClaudeCodeName,
		newCLI: func(dir string) (vendorCLI, error) {
			return vendorCLI{profileDir: dir}, nil
		},
		build: func(cli vendorCLI) (*subscriptionFixtureBackend, error) {
			builtProfiles = append(builtProfiles, filepath.Base(cli.profileDir))
			if startParallelOpen != nil {
				startParallelOpen()
				startParallelOpen = nil
			}
			return &subscriptionFixtureBackend{}, nil
		},
		logout: func(_ context.Context, cli vendorCLI) (bool, error) {
			t.Errorf("unexpected vendor sign-out of %q", cli.profileDir)
			return true, nil
		},
	}
	path, err := d.registrationPath()
	if err != nil {
		t.Fatal(err)
	}
	register := func(profile string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(filepath.Dir(path), profile), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"profile":"`+profile+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credentials := &auth.Config{Type: auth.TypeOAuth2}
	open := func() *subscriptionFixtureBackend {
		t.Helper()
		provider, err := d.open(ClaudeCodeName, credentials, false)
		if err != nil {
			t.Fatal(err)
		}
		return provider
	}
	assertUnlocked := func() {
		t.Helper()
		if !d.mu.TryLock() {
			t.Fatal("vendor close ran under the driver mutex")
		}
		d.mu.Unlock()
	}
	closeDriver := func() {
		t.Helper()
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
	}

	register("account-original")
	old := open()
	register("account-intermediate")
	old.onClose = func() {
		assertUnlocked()
		register("account-restored")
	}
	type openResult struct {
		backend *subscriptionFixtureBackend
		err     error
	}
	parallel := make(chan openResult, 1)
	startParallelOpen = func() {
		started := make(chan struct{})
		go func() {
			close(started)
			backend, err := d.open(ClaudeCodeName, credentials, false)
			parallel <- openResult{backend, err}
		}()
		<-started
	}
	fresh := open()
	if fresh == old {
		t.Fatal("open reused the old backend after the registration changed")
	}
	select {
	case got := <-parallel:
		if got.err != nil || got.backend != fresh || len(builtProfiles) != 2 || builtProfiles[1] != "account-restored" {
			t.Fatalf("parallel open did not share the restored backend: error=%v, built profiles=%v", got.err, builtProfiles)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parallel open did not finish")
	}
	assertSubscriptionRetired(t, old)
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "account-original")); err != nil {
		t.Fatalf("original saved profile was not preserved: %v", err)
	}

	fresh.onClose = assertUnlocked
	closeDriver()
	closeDriver()
	assertSubscriptionRetired(t, fresh)
	reopened := open()
	if reopened == fresh || reopened == old || len(builtProfiles) != 3 || builtProfiles[2] != "account-restored" {
		t.Fatalf("reopen did not rebuild the saved profile; built profiles: %v", builtProfiles)
	}
	closeDriver()
	assertSubscriptionRetired(t, reopened)
}

func TestSubscriptionLogoutClearsRegistrationOnVendorFailure(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "vendor logout failed"
		if missing {
			name = "vendor binary missing"
		}
		t.Run(name, func(t *testing.T) {
			isolateSubscriptionData(t)
			fakeSubscriptionBinary(t, "claude", "exit 0")
			backend := &subscriptionFixtureBackend{}
			attempted := false
			driver := &subscriptionDriver[*subscriptionFixtureBackend]{
				name: ClaudeCodeName, newCLI: claudeCodeOAuth.newCLI,
				build: func(vendorCLI) (*subscriptionFixtureBackend, error) { return backend, nil },
				logout: func(context.Context, vendorCLI) (bool, error) {
					attempted = true
					return false, errors.New("incompatible fixture runtime")
				},
			}
			path, err := driver.registrationPath()
			if err != nil {
				t.Fatal(err)
			}
			profileDir := filepath.Join(filepath.Dir(path), "account-fixture")
			if err := os.MkdirAll(profileDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"profile":"account-fixture","account":"fixture"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			credentials := &auth.Config{Type: auth.TypeOAuth2}
			if _, err := driver.open(ClaudeCodeName, credentials, false); err != nil {
				t.Fatal(err)
			}
			if missing {
				t.Setenv("PATH", t.TempDir())
			}
			deleted, err := driver.Logout()
			if !deleted || err != nil || attempted == missing || driver.CredentialRevision() != "" || backend.closed != 1 {
				t.Fatalf("logout = %t, %v; vendor attempted = %t; closes = %d", deleted, err, attempted, backend.closed)
			}
			if _, err := os.Stat(profileDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retired profile still present: %v", err)
			}
			if _, err := driver.open(ClaudeCodeName, credentials, false); !errors.Is(err, auth.ErrMissingCredential) {
				t.Fatalf("gateway can still use signed-out registration: %v", err)
			}
			if err := driver.Close(); err != nil || backend.closed != 1 {
				t.Fatalf("close after logout = %v (closes %d)", err, backend.closed)
			}
		})
	}
}

func TestCopilotLaunchDisablesHostKeyring(t *testing.T) {
	fakeSubscriptionBinary(t, "copilot", `[ "$COPILOT_DISABLE_KEYTAR" = 1 ] && [ -z "$GH_TOKEN" ]`)
	t.Setenv("COPILOT_DISABLE_KEYTAR", "0")
	t.Setenv("GH_TOKEN", "fixture-host-token")
	cli, err := copilotOAuth.newCLI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := copilotLogin(t.Context(), cli); err != nil {
		t.Fatalf("login inherited the host keyring or token: %v", err)
	}
	runtime := copilotRuntime(cli)
	cmd := exec.CommandContext(t.Context(), runtime.Path, runtime.Args...)
	cmd.Env = runtime.Env
	cmd.Dir = runtime.WorkingDirectory
	if err := cmd.Run(); err != nil {
		t.Fatalf("inference inherited the host keyring or token: %v", err)
	}
}
