package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

type cliFixtureRuntime struct {
	closed  int
	onClose func()
}

func (r *cliFixtureRuntime) Close() error {
	r.closed++
	if r.onClose != nil {
		r.onClose()
	}
	return nil
}

func isolateCLISessionData(t *testing.T) {
	t.Helper()
	t.Cleanup(xdg.ClearCache)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-cli-session-test")
	xdg.ClearCache()
}

func fakeCLIBinary(t *testing.T, binary, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, binary), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestCLISessionAccountReplacementAndLogout(t *testing.T) {
	isolateCLISessionData(t)
	fakeCLIBinary(t, "vendorcli", "exit 0")
	ctx := t.Context()
	var created []*cliFixtureRuntime
	loggedOut := make(chan string, 4)
	store := NewCLISessionStore(CLISessionConfig[*cliFixtureRuntime]{
		Provider: "vendor-sub",
		Label:    "Continue with Vendor",
		NewCLI: func(dir string) (VendorCLI, error) {
			return NewVendorCLI("vendorcli", "VENDOR_CONFIG_DIR", dir)
		},
		Login:   func(context.Context, VendorCLI) error { return nil },
		Account: func(context.Context, VendorCLI) (string, bool, error) { return "fixture", true, nil },
		Logout: func(_ context.Context, cli VendorCLI) (bool, error) {
			loggedOut <- filepath.Base(cli.ProfileDir)
			return true, nil
		},
		Build: func(VendorCLI) (*cliFixtureRuntime, error) {
			r := &cliFixtureRuntime{}
			created = append(created, r)
			return r, nil
		},
	})
	if store.LoginLabel() != "Continue with Vendor" {
		t.Fatalf("LoginLabel() = %q", store.LoginLabel())
	}

	open := func() *cliFixtureRuntime {
		t.Helper()
		rt, err := store.SharedRuntime()
		if err != nil {
			t.Fatal(err)
		}
		return rt
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
		commit, err := store.Authorize(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if account, err := commit(); account != "fixture" || err != nil {
			t.Fatalf("commit = %q, %v", account, err)
		}
		return store.CredentialRevision()
	}

	first := signIn()
	rt := open()
	reloaded := open()
	if len(created) != 1 || rt != reloaded {
		t.Fatalf("opens allocated %d runtimes or did not share the active runtime", len(created))
	}

	canceled, cancel := context.WithCancel(ctx)
	commit, err := store.Authorize(canceled)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := commit(); !errors.Is(err, context.Canceled) || store.CredentialRevision() != first || rt.closed != 0 {
		t.Fatalf("canceled replacement changed active registration or runtime: %v", err)
	}
	if abandoned := waitForLogout(); abandoned == first {
		t.Fatal("canceled replacement attempted to sign out the active account")
	}

	second := signIn()
	if first == "" || second == "" || first == second || waitForLogout() != first {
		t.Fatalf("replacement did not retire previous registration: %q, %q", first, second)
	}
	if rt.closed != 1 {
		t.Fatalf("first runtime close calls = %d, want 1", rt.closed)
	}
	next := open()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("repeat close: %v", err)
	}
	if next.closed != 1 {
		t.Fatalf("next runtime close calls = %d, want 1", next.closed)
	}
	if store.CredentialRevision() != second {
		t.Fatal("store shutdown changed the saved registration")
	}
	select {
	case profile := <-loggedOut:
		t.Fatalf("store shutdown signed out %q", profile)
	default:
	}

	reopened := open()
	if reopened == next || len(created) != 3 {
		t.Fatal("opening after shutdown reused the retired runtime")
	}
	if deleted, err := store.Logout(); !deleted || err != nil {
		t.Fatalf("logout = %v, %v", deleted, err)
	}
	if reopened.closed != 1 {
		t.Fatalf("reopened runtime close calls = %d, want 1", reopened.closed)
	}
	if got := waitForLogout(); got != second {
		t.Fatalf("logged out profile %q, want %q", got, second)
	}
	if _, ok := store.Account(); ok {
		t.Fatal("registration still exists after logout")
	}
}

func TestCLISessionSharedRuntimeReplacesExternallyChangedRegistration(t *testing.T) {
	isolateCLISessionData(t)
	var builtProfiles []string
	var startParallelOpen func()
	store := NewCLISessionStore(CLISessionConfig[*cliFixtureRuntime]{
		Provider: "vendor-ext",
		NewCLI: func(dir string) (VendorCLI, error) {
			return VendorCLI{ProfileDir: dir}, nil
		},
		Build: func(cli VendorCLI) (*cliFixtureRuntime, error) {
			builtProfiles = append(builtProfiles, filepath.Base(cli.ProfileDir))
			if startParallelOpen != nil {
				startParallelOpen()
				startParallelOpen = nil
			}
			return &cliFixtureRuntime{}, nil
		},
		Logout: func(_ context.Context, cli VendorCLI) (bool, error) {
			t.Errorf("unexpected vendor sign-out of %q", cli.ProfileDir)
			return true, nil
		},
	})
	path, err := store.registrationPath()
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
	open := func() *cliFixtureRuntime {
		t.Helper()
		rt, err := store.SharedRuntime()
		if err != nil {
			t.Fatal(err)
		}
		return rt
	}
	assertUnlocked := func() {
		t.Helper()
		if !store.mu.TryLock() {
			t.Fatal("runtime close ran under the store mutex")
		}
		store.mu.Unlock()
	}

	register("account-original")
	old := open()
	register("account-intermediate")
	old.onClose = func() {
		assertUnlocked()
		register("account-restored")
	}
	type openResult struct {
		rt  *cliFixtureRuntime
		err error
	}
	parallel := make(chan openResult, 1)
	startParallelOpen = func() {
		started := make(chan struct{})
		go func() {
			close(started)
			rt, err := store.SharedRuntime()
			parallel <- openResult{rt, err}
		}()
		<-started
	}
	fresh := open()
	if fresh == old {
		t.Fatal("open reused the old runtime after the registration changed")
	}
	select {
	case got := <-parallel:
		if got.err != nil || got.rt != fresh || len(builtProfiles) != 2 || builtProfiles[1] != "account-restored" {
			t.Fatalf("parallel open did not share the restored runtime: error=%v, built profiles=%v", got.err, builtProfiles)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parallel open did not finish")
	}
	if old.closed != 1 {
		t.Fatalf("old runtime close calls = %d, want 1", old.closed)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "account-original")); err != nil {
		t.Fatalf("original saved profile was not preserved: %v", err)
	}

	fresh.onClose = assertUnlocked
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if fresh.closed != 1 {
		t.Fatalf("fresh runtime close calls = %d, want 1", fresh.closed)
	}
	reopened := open()
	if reopened == fresh || reopened == old || len(builtProfiles) != 3 || builtProfiles[2] != "account-restored" {
		t.Fatalf("reopen did not rebuild the saved profile; built profiles: %v", builtProfiles)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened.closed != 1 {
		t.Fatalf("reopened runtime close calls = %d, want 1", reopened.closed)
	}
}

func TestCLISessionLogoutClearsRegistrationOnVendorFailure(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "vendor logout failed"
		if missing {
			name = "vendor binary missing"
		}
		t.Run(name, func(t *testing.T) {
			isolateCLISessionData(t)
			fakeCLIBinary(t, "vendorcli", "exit 0")
			rt := &cliFixtureRuntime{}
			attempted := false
			store := NewCLISessionStore(CLISessionConfig[*cliFixtureRuntime]{
				Provider: "vendor-fail",
				NewCLI: func(dir string) (VendorCLI, error) {
					return NewVendorCLI("vendorcli", "VENDOR_CONFIG_DIR", dir)
				},
				Build: func(VendorCLI) (*cliFixtureRuntime, error) { return rt, nil },
				Logout: func(context.Context, VendorCLI) (bool, error) {
					attempted = true
					return false, errors.New("incompatible fixture runtime")
				},
			})
			path, err := store.registrationPath()
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
			if _, err := store.SharedRuntime(); err != nil {
				t.Fatal(err)
			}
			if missing {
				t.Setenv("PATH", t.TempDir())
			}
			deleted, err := store.Logout()
			if !deleted || err != nil || attempted == missing || store.CredentialRevision() != "" || rt.closed != 1 {
				t.Fatalf("logout = %t, %v; vendor attempted = %t; closes = %d", deleted, err, attempted, rt.closed)
			}
			if _, err := os.Stat(profileDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retired profile still present: %v", err)
			}
			if _, err := store.SharedRuntime(); !errors.Is(err, ErrMissingCredential) {
				t.Fatalf("store can still use signed-out registration: %v", err)
			}
			if err := store.Close(); err != nil || rt.closed != 1 {
				t.Fatalf("close after logout = %v (closes %d)", err, rt.closed)
			}
		})
	}
}
