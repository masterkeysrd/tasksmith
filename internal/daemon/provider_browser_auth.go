package daemon

import (
	"context"
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

func (d *Daemon) browserAuthWriteLock(provider string) *sync.Mutex {
	d.providerAuthMu.Lock()
	defer d.providerAuthMu.Unlock()
	mu := d.browserAuthWriteMu[provider]
	if mu == nil {
		mu = new(sync.Mutex)
		d.browserAuthWriteMu[provider] = mu
	}
	return mu
}

func (d *Daemon) beginBrowserAuthFlow(provider string, authorize func(context.Context) (func() (string, error), error)) *api.ProviderAuthResponse {
	ctx, flow := d.registerProviderAuthFlow(provider)
	resp := flow.toAPI()
	go func() {
		defer flow.cancel()
		defer close(flow.settled)
		commit, err := authorize(ctx)
		if err != nil {
			d.failProviderAuthFlow(flow, err)
			return
		}

		account, err := d.commitBrowserAuth(ctx, flow, commit)
		if err != nil {
			d.failProviderAuthFlow(flow, err)
			return
		}
		d.reloadAllWorkspaces(ctx)

		d.providerAuthMu.Lock()
		if d.providerAuths[provider] == flow && ctx.Err() == nil {
			flow.settle("authenticated", account, "")
		} else {
			d.failProviderAuthFlow(flow, context.Canceled)
		}
		d.providerAuthMu.Unlock()
	}()
	return &resp
}

func (d *Daemon) commitBrowserAuth(ctx context.Context, flow *providerAuthFlow, commit func() (string, error)) (string, error) {
	mu := d.browserAuthWriteLock(flow.provider)
	mu.Lock()
	defer mu.Unlock()

	d.providerAuthMu.Lock()
	current := d.providerAuths[flow.provider] == flow
	d.providerAuthMu.Unlock()
	if !current {
		return "", context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return commit()
}

func (d *Daemon) logoutBrowserAuth(provider string, driver auth.BrowserOAuthDriver) (bool, error) {
	mu := d.browserAuthWriteLock(provider)
	mu.Lock()
	defer mu.Unlock()

	d.providerAuthMu.Lock()
	if flow := d.providerAuths[provider]; flow != nil {
		flow.cancel()
		delete(d.providerAuths, provider)
	}
	d.providerAuthMu.Unlock()
	return driver.Logout()
}
