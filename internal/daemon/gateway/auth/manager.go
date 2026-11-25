package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"sync"
	"time"

	loomauth "github.com/masterkeysrd/loom/auth"
)

// Sentinel errors returned by Manager and callback parsing so transport layers
// can map them onto HTTP status codes without parsing error text.
var (
	ErrUnsupportedProvider = errors.New("auth: oauth not supported for provider")
	ErrInvalidCallback     = errors.New("auth: invalid callback input")
	ErrNoFlowInProgress    = errors.New("auth: no provider auth flow in progress")
	ErrBrowserHostOnly     = errors.New("auth: browser sign-in must be completed on the daemon host")
	ErrInvalidProviderName = errors.New("auth: provider name is required")
)

const flowTimeout = 5 * time.Minute

// FlowSnapshot captures the observable state of an interactive provider
// authentication flow at a point in time.
type FlowSnapshot struct {
	Provider string
	AuthURL  string
	Status   string
	Account  string
	Error    string
}

// ManagerConfig configures an instance-owned provider authentication Manager.
type ManagerConfig struct {
	// OnAuthenticated is invoked after credentials are persisted so callers
	// (such as the daemon) can reload active workspaces. Optional.
	OnAuthenticated func(context.Context)

	// OAuthDrivers overrides the package-registered PKCE OAuthDriver map when
	// non-nil, allowing tests to run isolated managers without mutating globals.
	OAuthDrivers map[string]OAuthDriver

	// BrowserDrivers overrides the package-registered BrowserOAuthDriver map
	// when non-nil, allowing tests to run isolated managers without mutating globals.
	BrowserDrivers map[string]BrowserOAuthDriver
}

// Manager owns interactive provider OAuth flows (PKCE loopback listeners,
// manual callback completion, and provider-owned browser sign-in commits) and
// provides uniform credential queries across both OAuth driver styles.
type Manager struct {
	onAuthenticated func(context.Context)
	oauthDrivers    map[string]OAuthDriver
	browserDrivers  map[string]BrowserOAuthDriver

	mu                 sync.Mutex
	flows              map[string]*authFlow
	browserAuthWriteMu map[string]*sync.Mutex
}

// NewManager constructs a provider authentication Manager.
func NewManager(cfg ManagerConfig) *Manager {
	var oauthDrivers map[string]OAuthDriver
	if cfg.OAuthDrivers != nil {
		oauthDrivers = maps.Clone(cfg.OAuthDrivers)
	}
	var browserDrivers map[string]BrowserOAuthDriver
	if cfg.BrowserDrivers != nil {
		browserDrivers = maps.Clone(cfg.BrowserDrivers)
	}
	return &Manager{
		onAuthenticated:    cfg.OnAuthenticated,
		oauthDrivers:       oauthDrivers,
		browserDrivers:     browserDrivers,
		flows:              make(map[string]*authFlow),
		browserAuthWriteMu: make(map[string]*sync.Mutex),
	}
}

type authFlow struct {
	mu       sync.Mutex
	provider string
	status   string
	authURL  string
	account  string
	err      string
	cancel   context.CancelFunc
	session  *loomauth.AuthSession
	settled  chan struct{}
	claimed  bool
}

// claim atomically takes exclusive ownership of completing a PKCE flow so a
// manually pasted callback and the loopback listener cannot exchange the same
// one-time authorization code concurrently.
func (f *authFlow) claim() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status != "authorizing" || f.claimed || f.session == nil {
		return false
	}
	f.claimed = true
	return true
}

func (f *authFlow) settle(status, account, errMsg string) {
	f.mu.Lock()
	f.status = status
	f.account = account
	f.err = errMsg
	f.mu.Unlock()
}

func (f *authFlow) snapshot() FlowSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FlowSnapshot{
		Provider: f.provider,
		AuthURL:  f.authURL,
		Status:   f.status,
		Account:  f.account,
		Error:    f.err,
	}
}

func (m *Manager) browserDriver(providerType string) (BrowserOAuthDriver, bool) {
	key := strings.TrimSpace(strings.ToLower(providerType))
	if m.browserDrivers != nil {
		d, ok := m.browserDrivers[key]
		return d, ok
	}
	return GetBrowserOAuthDriver(key)
}

func (m *Manager) oauthDriver(providerType string) (OAuthDriver, bool) {
	key := strings.TrimSpace(strings.ToLower(providerType))
	if m.oauthDrivers != nil {
		d, ok := m.oauthDrivers[key]
		return d, ok
	}
	return GetOAuthDriver(key)
}

func (m *Manager) browserWriteLock(provider string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	mu := m.browserAuthWriteMu[provider]
	if mu == nil {
		mu = new(sync.Mutex)
		m.browserAuthWriteMu[provider] = mu
	}
	return mu
}

func (m *Manager) registerFlow(prov string) (context.Context, *authFlow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.flows[prov]; ok && existing != nil {
		existing.mu.Lock()
		if existing.cancel != nil {
			existing.cancel()
		}
		existing.mu.Unlock()
	}

	flowCtx, cancel := context.WithTimeout(context.Background(), flowTimeout)
	flow := &authFlow{
		provider: prov,
		status:   "authorizing",
		cancel:   cancel,
		settled:  make(chan struct{}),
	}
	m.flows[prov] = flow
	return flowCtx, flow
}

func (m *Manager) failFlow(flow *authFlow, err error) {
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.status != "authorizing" || flow.claimed {
		return
	}
	flow.status = "error"
	flow.err = err.Error()
}

func (m *Manager) notifyAuthenticated(ctx context.Context) {
	if m.onAuthenticated != nil {
		m.onAuthenticated(ctx)
	}
}

// Start begins an interactive OAuth sign-in flow for provider.
func (m *Manager) Start(_ context.Context, provider string) (*FlowSnapshot, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	if driver, ok := m.browserDriver(prov); ok {
		return m.beginBrowserFlow(prov, driver.Authorize), nil
	}
	driver, ok := m.oauthDriver(prov)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}
	return m.beginPKCEFlow(prov, driver)
}

func (m *Manager) beginPKCEFlow(prov string, driver OAuthDriver) (*FlowSnapshot, error) {
	flowCtx, flow := m.registerFlow(prov)
	oauthCfg := driver.OAuthConfig()
	session, err := loomauth.BeginAuth(oauthCfg)
	if err != nil {
		flow.cancel()
		flow.settle("error", "", err.Error())
		close(flow.settled)
		return nil, fmt.Errorf("begin oauth: %w", err)
	}

	flow.mu.Lock()
	flow.authURL = session.AuthURL
	flow.session = session
	flow.mu.Unlock()

	go func() {
		defer flow.cancel()
		defer close(flow.settled)

		res, err := loomauth.ReceiveCallback(flowCtx, oauthCfg.RedirectURL, session.AuthURL, loomauth.LoopbackOptions{
			Template: oauthCallbackTemplate,
		})
		if err != nil {
			m.failFlow(flow, err)
			return
		}
		if !flow.claim() {
			return
		}
		m.finishPKCEFlow(flowCtx, flow, driver, res.Code, res.State)
	}()

	snap := flow.snapshot()
	return &snap, nil
}

func (m *Manager) finishPKCEFlow(ctx context.Context, flow *authFlow, driver OAuthDriver, code, state string) {
	tok, err := loomauth.CompleteAuth(ctx, driver.OAuthConfig(), flow.session, code, state)
	if err != nil {
		flow.settle("error", "", err.Error())
		return
	}

	account := driver.ExtractAccount(tok)
	if err := SaveProviderToken(flow.provider, tok, account); err != nil {
		flow.settle("error", "", err.Error())
		return
	}

	m.notifyAuthenticated(ctx)
	flow.settle("authenticated", account, "")
}

func (m *Manager) beginBrowserFlow(provider string, authorize func(context.Context) (func() (string, error), error)) *FlowSnapshot {
	ctx, flow := m.registerFlow(provider)
	snap := flow.snapshot()
	go func() {
		defer flow.cancel()
		defer close(flow.settled)
		commit, err := authorize(ctx)
		if err != nil {
			m.failFlow(flow, err)
			return
		}

		account, err := m.commitBrowserFlow(ctx, flow, commit)
		if err != nil {
			m.failFlow(flow, err)
			return
		}
		m.notifyAuthenticated(ctx)

		m.mu.Lock()
		if m.flows[provider] == flow && ctx.Err() == nil {
			flow.settle("authenticated", account, "")
		} else {
			m.failFlow(flow, context.Canceled)
		}
		m.mu.Unlock()
	}()
	return &snap
}

func (m *Manager) commitBrowserFlow(ctx context.Context, flow *authFlow, commit func() (string, error)) (string, error) {
	mu := m.browserWriteLock(flow.provider)
	mu.Lock()
	defer mu.Unlock()

	m.mu.Lock()
	current := m.flows[flow.provider] == flow
	m.mu.Unlock()
	if !current {
		return "", context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return commit()
}

// Complete finishes an in-flight PKCE flow using a manually pasted callback URL
// or authorization code.
func (m *Manager) Complete(ctx context.Context, provider, callbackInput string) (*FlowSnapshot, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	if _, ok := m.browserDriver(prov); ok {
		return nil, fmt.Errorf("%w: finish sign-in for %q in the browser on the daemon host", ErrBrowserHostOnly, prov)
	}
	driver, ok := m.oauthDriver(prov)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}

	m.mu.Lock()
	flow := m.flows[prov]
	m.mu.Unlock()

	if flow == nil {
		return nil, fmt.Errorf("%w: no provider auth flow in progress for %q; start it with auth login", ErrNoFlowInProgress, prov)
	}

	code, state, err := ParseCallbackInput(callbackInput)
	if err != nil {
		return nil, err
	}

	if !flow.claim() {
		return m.Status(prov)
	}
	flow.mu.Lock()
	cancel := flow.cancel
	flow.mu.Unlock()
	cancel()

	m.finishPKCEFlow(ctx, flow, driver, code, state)

	snap := flow.snapshot()
	if snap.Status == "error" {
		return nil, fmt.Errorf("complete oauth: %s", snap.Error)
	}
	return &snap, nil
}

// FlowStatus returns the current snapshot of an active or settled flow for
// provider if one is tracked.
func (m *Manager) FlowStatus(provider string) (*FlowSnapshot, bool) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	m.mu.Lock()
	flow, ok := m.flows[prov]
	m.mu.Unlock()
	if !ok || flow == nil {
		return nil, false
	}
	snap := flow.snapshot()
	if snap.Status == "authorizing" || snap.Status == "error" || (snap.Status == "authenticated" && snap.Error == "") {
		return &snap, true
	}
	return nil, false
}

// Status returns the flow status if one is active, or falls back to the stored
// account status for provider.
func (m *Manager) Status(provider string) (*FlowSnapshot, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	if !m.HasOAuth(prov) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}
	if snap, ok := m.FlowStatus(prov); ok {
		return snap, nil
	}
	if email, ok := m.Account(prov, prov); ok {
		return &FlowSnapshot{
			Provider: prov,
			Status:   "authenticated",
			Account:  email,
		}, nil
	}
	return &FlowSnapshot{
		Provider: prov,
		Status:   "idle",
	}, nil
}

// Logout cancels any in-flight OAuth flow and deletes stored credentials for
// the provider, reporting whether any credential material was removed.
func (m *Manager) Logout(providerType, providerName string) (bool, error) {
	provType := strings.TrimSpace(strings.ToLower(providerType))
	provName := strings.TrimSpace(strings.ToLower(providerName))
	if provName == "" {
		provName = provType
	}
	if provType == "" {
		provType = provName
	}
	if provName == "" {
		return false, ErrInvalidProviderName
	}

	if driver, ok := m.browserDriver(provType); ok {
		return m.logoutBrowser(provType, driver)
	}

	m.mu.Lock()
	flow := m.flows[provName]
	if flow != nil {
		delete(m.flows, provName)
	}
	m.mu.Unlock()

	if flow != nil {
		flow.mu.Lock()
		cancel, settled := flow.cancel, flow.settled
		flow.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		// Wait briefly so a flow completing concurrently cannot re-save its
		// token after the credential file is removed.
		if settled != nil {
			select {
			case <-settled:
			case <-time.After(5 * time.Second):
			}
		}
	}

	return DeleteProviderToken(provName, "")
}

func (m *Manager) logoutBrowser(provider string, driver BrowserOAuthDriver) (bool, error) {
	mu := m.browserWriteLock(provider)
	mu.Lock()
	defer mu.Unlock()

	m.mu.Lock()
	if flow := m.flows[provider]; flow != nil {
		flow.cancel()
		delete(m.flows, provider)
	}
	m.mu.Unlock()
	return driver.Logout()
}

// Close cancels all in-flight auth flows and closes browser OAuth drivers.
func (m *Manager) Close() error {
	m.mu.Lock()
	flows := maps.Clone(m.flows)
	clear(m.flows)
	m.mu.Unlock()

	for _, flow := range flows {
		if flow == nil {
			continue
		}
		flow.mu.Lock()
		cancel := flow.cancel
		flow.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	if m.browserDrivers != nil {
		var errs []error
		for provider, driver := range m.browserDrivers {
			if closer, ok := driver.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					errs = append(errs, fmt.Errorf("close browser OAuth driver %q: %w", provider, err))
				}
			}
		}
		return errors.Join(errs...)
	}
	return CloseBrowserOAuthDrivers()
}

// Account returns the active account identifier and whether credentials exist
// for the given provider type and manifest name.
func (m *Manager) Account(providerType, providerName string) (string, bool) {
	if driver, ok := m.browserDriver(providerType); ok {
		return driver.Account()
	}
	if providerName == "" {
		providerName = providerType
	}
	tok, email, err := LoadProviderToken(providerName)
	return email, err == nil && tok != nil
}

// CredentialRevision returns a stable identity digest/revision for the
// provider's OAuth credentials so configuration fingerprints detect sign-in,
// sign-out, and account switches.
func (m *Manager) CredentialRevision(providerType, providerName string) string {
	if driver, ok := m.browserDriver(providerType); ok {
		return driver.CredentialRevision()
	}
	if providerName == "" {
		providerName = providerType
	}
	return OAuthCredentialDigest(providerName)
}

// LoginLabel returns the browser sign-in button label for browser-only drivers,
// or "" for other providers.
func (m *Manager) LoginLabel(providerType string) string {
	if driver, ok := m.browserDriver(providerType); ok {
		return driver.LoginLabel()
	}
	return ""
}

// HasOAuth reports whether any OAuth driver (browser or PKCE) is registered
// for providerType.
func (m *Manager) HasOAuth(providerType string) bool {
	if _, ok := m.browserDriver(providerType); ok {
		return true
	}
	_, ok := m.oauthDriver(providerType)
	return ok
}

// IsCanonicalBrowserOnly reports whether providerType uses a canonical
// browser-only driver whose credentials are keyed strictly by provider type.
func (m *Manager) IsCanonicalBrowserOnly(providerType string) bool {
	_, ok := m.browserDriver(providerType)
	return ok
}

var defaultQueryManager = NewManager(ManagerConfig{})

// Account queries the active OAuth account using the package-registered drivers.
func Account(providerType, providerName string) (string, bool) {
	return defaultQueryManager.Account(providerType, providerName)
}

// CredentialRevision queries the OAuth credential revision using the
// package-registered drivers.
func CredentialRevision(providerType, providerName string) string {
	return defaultQueryManager.CredentialRevision(providerType, providerName)
}

// LoginLabel queries the browser sign-in label using the package-registered drivers.
func LoginLabel(providerType string) string {
	return defaultQueryManager.LoginLabel(providerType)
}

// HasOAuth reports whether an OAuth driver is registered for providerType.
func HasOAuth(providerType string) bool {
	return defaultQueryManager.HasOAuth(providerType)
}

// IsCanonicalBrowserOnly reports whether providerType uses a browser-only driver.
func IsCanonicalBrowserOnly(providerType string) bool {
	return defaultQueryManager.IsCanonicalBrowserOnly(providerType)
}
