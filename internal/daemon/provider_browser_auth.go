package daemon

import (
	"context"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

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

		// A replacement sign-in or sign-out may happen while the browser is open.
		d.providerAuthMu.Lock()
		if d.providerAuths[flow.provider] != flow || ctx.Err() != nil {
			d.providerAuthMu.Unlock()
			d.failProviderAuthFlow(flow, context.Canceled)
			return
		}
		account, err := commit()
		d.providerAuthMu.Unlock()
		if err != nil {
			d.failProviderAuthFlow(flow, err)
			return
		}
		d.reloadAllWorkspaces(ctx)
		flow.settle("authenticated", account, "")
	}()
	return &resp
}

func (d *Daemon) logoutBrowserAuth(provider string, driver auth.BrowserOAuthDriver) (bool, error) {
	d.providerAuthMu.Lock()
	defer d.providerAuthMu.Unlock()
	if flow := d.providerAuths[provider]; flow != nil {
		flow.cancel()
		delete(d.providerAuths, provider)
	}
	return driver.Logout()
}
