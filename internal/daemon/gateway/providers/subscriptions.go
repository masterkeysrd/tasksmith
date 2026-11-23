package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

const subscriptionProfilePrefix = "account-"

type subscriptionBackend interface {
	llm.Provider
	io.Closer
}

type subscriptionDriver[P subscriptionBackend] struct {
	name, label string
	newCLI      func(string) (vendorCLI, error)
	login       func(context.Context, vendorCLI) error
	account     func(context.Context, vendorCLI) (string, bool, error)
	logout      func(context.Context, vendorCLI) (bool, error)
	build       func(vendorCLI) (P, error)

	mu      sync.Mutex
	runtime *subscriptionRuntime[P]
}

type subscriptionAccount struct {
	Profile string `json:"profile"`
	Account string `json:"account,omitempty"`
}

func (d *subscriptionDriver[P]) LoginLabel() string { return d.label }

func (d *subscriptionDriver[P]) registrationPath() (string, error) {
	return xdg.SubDataDir("credentials", d.name, "active.json")
}

func readSubscriptionAccount(path string) (subscriptionAccount, error) {
	var account subscriptionAccount
	data, err := os.ReadFile(path)
	if err != nil {
		return account, err
	}
	if err := json.Unmarshal(data, &account); err != nil {
		return account, fmt.Errorf("read provider registration: %w", err)
	}
	if !strings.HasPrefix(account.Profile, subscriptionProfilePrefix) || filepath.Base(account.Profile) != account.Profile {
		return account, errors.New("invalid provider profile in registration")
	}
	return account, nil
}

func (d *subscriptionDriver[P]) Account() (string, bool) {
	path, err := d.registrationPath()
	if err != nil {
		return "", false
	}
	account, err := readSubscriptionAccount(path)
	return account.Account, err == nil
}

func (d *subscriptionDriver[P]) CredentialRevision() string {
	path, err := d.registrationPath()
	if err != nil {
		return ""
	}
	account, err := readSubscriptionAccount(path)
	if err != nil {
		return ""
	}
	return account.Profile
}

func (d *subscriptionDriver[P]) Authorize(ctx context.Context) (func() (string, error), error) {
	path, err := d.registrationPath()
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(parent, subscriptionProfilePrefix)
	if err != nil {
		return nil, err
	}
	cli, err := d.newCLI(dir)
	if err != nil {
		discardSubscriptionProfile(dir)
		return nil, err
	}
	if err := d.login(ctx, cli); err != nil {
		discardSubscriptionProfile(dir)
		return nil, err
	}
	account, ok, err := d.account(ctx, cli)
	if err != nil || !ok {
		d.retire(nil, dir)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s did not report a signed-in subscription account", d.name)
	}
	data, err := json.Marshal(subscriptionAccount{Profile: filepath.Base(dir), Account: account})
	if err != nil {
		d.retire(nil, dir)
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { d.retire(nil, dir) })
	return func() (string, error) {
		if !stop() {
			return "", context.Canceled
		}
		previous, retired, err := d.activate(ctx, path, data)
		if err != nil {
			d.retire(nil, dir)
			return "", err
		}
		d.retire(retired, previous)
		return account, nil
	}, nil
}

func (d *subscriptionDriver[P]) activate(ctx context.Context, path string, data []byte) (string, *subscriptionRuntime[P], error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	previous, err := readSubscriptionAccount(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, err
	}
	if err := fsutils.WriteFileAtomic(path, data, 0o600); err != nil {
		return "", nil, err
	}
	var dir string
	if previous.Profile != "" {
		dir = filepath.Join(filepath.Dir(path), previous.Profile)
	}
	return dir, d.detachRuntimeLocked(), nil
}

func (d *subscriptionDriver[P]) Logout() (bool, error) {
	dir, retired, deleted, err := d.clearRegistration()
	if err != nil {
		return false, err
	}
	d.retire(retired, dir)
	return deleted, nil
}

func (d *subscriptionDriver[P]) clearRegistration() (string, *subscriptionRuntime[P], bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	path, err := d.registrationPath()
	if err != nil {
		return "", nil, false, err
	}
	account, readErr := readSubscriptionAccount(path)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", d.detachRuntimeLocked(), false, nil
		}
		return "", nil, false, err
	}
	var dir string
	if readErr == nil {
		dir = filepath.Join(filepath.Dir(path), account.Profile)
	} else {
		slog.Warn("removed unreadable provider registration", "provider", d.name, "error", readErr)
		if d.runtime != nil {
			dir = d.runtime.dir
		}
	}
	return dir, d.detachRuntimeLocked(), true, nil
}

func (d *subscriptionDriver[P]) retire(runtime *subscriptionRuntime[P], dir string) {
	if err := runtime.close(); err != nil {
		slog.Warn("close subscription runtime", "provider", d.name, "error", err)
	}
	if dir == "" {
		return
	}
	defer discardSubscriptionProfile(dir)
	cli, err := d.newCLI(dir)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_, err = d.logout(ctx, cli)
	}
	if err != nil {
		slog.Warn("vendor sign-out did not complete", "provider", d.name, "error", err)
	}
}

func discardSubscriptionProfile(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("remove retired provider profile", "error", err)
	}
}

type subscriptionRuntime[P subscriptionBackend] struct {
	provider P
	dir      string
}

func (r *subscriptionRuntime[P]) close() error {
	if r == nil {
		return nil
	}
	return r.provider.Close()
}

func (d *subscriptionDriver[P]) detachRuntimeLocked() *subscriptionRuntime[P] {
	runtime := d.runtime
	d.runtime = nil
	return runtime
}

func (d *subscriptionDriver[P]) Close() error {
	d.mu.Lock()
	runtime := d.detachRuntimeLocked()
	d.mu.Unlock()
	return runtime.close()
}

func (d *subscriptionDriver[P]) open(name string, credentials *auth.Config, hasOverrides bool) (P, error) {
	var zero P
	if name != d.name || credentials.Scheme() != auth.TypeOAuth2 || hasOverrides ||
		credentials.Token != nil || credentials.Header != "" || credentials.Username != "" || credentials.Password != nil {
		return zero, fmt.Errorf("%w: %s requires the provider name %q and browser OAuth without endpoint, header, or config overrides", ErrInvalidConfig, d.name, d.name)
	}

	for {
		d.mu.Lock()
		provider, stale, err := d.openLocked()
		d.mu.Unlock()
		if stale == nil {
			return provider, err
		}
		d.retire(stale, "")
	}
}

func (d *subscriptionDriver[P]) openLocked() (P, *subscriptionRuntime[P], error) {
	var zero P
	path, err := d.registrationPath()
	if err != nil {
		return zero, nil, err
	}
	account, err := readSubscriptionAccount(path)
	if errors.Is(err, os.ErrNotExist) {
		return zero, nil, fmt.Errorf("%w: sign in with `tasksmith auth login %s`", auth.ErrMissingCredential, d.name)
	}
	if err != nil {
		return zero, nil, err
	}
	dir := filepath.Join(filepath.Dir(path), account.Profile)
	if d.runtime != nil {
		if d.runtime.dir != dir {
			return zero, d.detachRuntimeLocked(), nil
		}
		return d.runtime.provider, nil, nil
	}
	cli, err := d.newCLI(dir)
	if err != nil {
		return zero, nil, err
	}
	provider, err := d.build(cli)
	if err != nil {
		return zero, nil, fmt.Errorf("initialize %s: %w", d.name, err)
	}
	d.runtime = &subscriptionRuntime[P]{provider: provider, dir: dir}
	return provider, nil, nil
}
