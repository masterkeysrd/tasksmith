package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

// authPollInterval and authPollTimeout bound how long the /login screen waits
// for the daemon to report a finished sign-in. They are variables so a test can
// exercise the whole flow without waiting out a human-sized budget.
var (
	authPollInterval = 500 * time.Millisecond
	authPollTimeout  = 5 * time.Minute
)

// authLaunchURL is the seam through which the provider login page is opened.
var authLaunchURL = openMCPAuthURL

// authBackground tracks the goroutines /login actions run on so a test can wait
// for them before tearing the store down, mirroring mcpBackground.
var authBackground sync.WaitGroup

func goAuthBackground(fn func()) {
	authBackground.Go(fn)
}

func (r *Registry) registerAuthCommands() {
	r.Register(Command{
		Name:        "login",
		Aliases:     []string{"auth", "signin"},
		Usage:       "/login [provider]",
		Description: "Sign in to an OAuth provider (antigravity)",
		Handler:     handleLogin,
		Complete:    completeOAuthProviders,
	})
}

func handleLogin(args []string) error {
	provider := strings.TrimSpace(strings.Join(args, " "))
	enqueueProviderAuth(provider)
	return nil
}

// completeOAuthProviders offers the OAuth-capable presets the daemon reports,
// which is the only set /login can act on.
func completeOAuthProviders(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := store.Client()
	if c == nil {
		return nil, nil
	}
	providers, err := c.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	var candidates []autocomplete.Candidate
	for _, p := range providers {
		if p.AuthType != "oauth2" {
			continue
		}
		candidates = append(candidates, autocomplete.Candidate{
			Value:       p.Name,
			Label:       p.Name,
			Description: "Sign in to " + p.Name,
		})
	}
	return candidates, nil
}

// enqueueProviderAuth opens the sign-in screen for one provider. An empty name
// lets the daemon resolve the sole OAuth preset.
func enqueueProviderAuth(provider string) {
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node {
			return providerAuthView(providerAuthViewProps{
				slotID:   slotID,
				provider: provider,
				state:    newProviderAuthStore(),
			})
		},
	})
}

// providerAuthState is the observable state of one provider sign-in.
type providerAuthState struct {
	provider string
	authURL  string
	// report is what the screen shows prominently: a copy confirmation, a
	// rejected paste, or the final result.
	report      string
	settled     bool
	failed      bool
	browserOnly bool
}

type providerAuthFlow struct {
	state *kites.Store[providerAuthState]
	stop  context.CancelFunc
}

// name returns the provider this flow is reporting on, which the daemon can
// canonicalize when /login was issued without a name. It reads through the
// store because the watch and submit goroutines both need it.
func (f *providerAuthFlow) name() string {
	return readProviderAuth(f.state).provider
}

func newProviderAuthStore() *kites.Store[providerAuthState] {
	return kites.Create(providerAuthState{})
}

func readProviderAuth(state *kites.Store[providerAuthState]) providerAuthState {
	if state == nil {
		return providerAuthState{}
	}
	return state.Get()
}

// startProviderAuthFlow begins sign-in for the named provider and reports its
// progress on a tracked background goroutine. Cancelling the flow only stops
// this client from reporting: the daemon holds its callback open on a timer of
// its own, so finishing in the browser still completes the login.
func startProviderAuthFlow(provider string, state *kites.Store[providerAuthState]) *providerAuthFlow {
	ctx, cancel := context.WithCancel(context.Background())
	flow := &providerAuthFlow{state: state, stop: cancel}
	if state == nil {
		cancel()
		return flow
	}

	flow.update(func(next *providerAuthState) { *next = providerAuthState{provider: provider} })
	goAuthBackground(func() {
		startCtx, cancel := context.WithTimeout(ctx, authPollTimeout)
		outcome := providerAuthStart(startCtx, provider)
		cancel()
		if outcome.settled {
			flow.finish(outcome)
			return
		}
		flow.update(func(next *providerAuthState) {
			if outcome.provider != "" {
				next.provider = outcome.provider
			}
			next.authURL = outcome.authURL
			next.report = outcome.message
			next.browserOnly = outcome.browserOnly
		})
		flow.watch(ctx)
	})
	return flow
}

type providerAuthOutcome struct {
	// provider is the daemon's canonical flow name; it can differ from the
	// requested one when an empty name resolved to the sole OAuth preset.
	provider    string
	authURL     string
	message     string
	settled     bool
	failed      bool
	browserOnly bool
}

// soleOAuthProvider resolves an unnamed /login to the only OAuth-capable preset
// the daemon reports; anything else needs the user to name one.
func soleOAuthProvider(ctx context.Context) (string, error) {
	c := store.Client()
	if c == nil {
		return "", errors.New("no daemon connection; cannot sign in")
	}
	providers, err := c.ListProviders(ctx)
	if err != nil {
		return "", fmt.Errorf("could not list providers: %w", err)
	}
	var names []string
	for _, p := range providers {
		if p.AuthType == "oauth2" {
			names = append(names, p.Name)
		}
	}
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", errors.New("no OAuth providers are available; run /login <provider>")
	default:
		return "", fmt.Errorf("multiple OAuth providers are available (%s); run /login <provider>", strings.Join(names, ", "))
	}
}

// providerAuthStart asks the daemon to open a flow and reports the page to
// open. The daemon parks the flow for its own callback, so this returns as soon
// as there is a URL rather than waiting for the user. An empty provider is
// resolved to the sole OAuth preset when exactly one exists.
func providerAuthStart(ctx context.Context, provider string) providerAuthOutcome {
	c := store.Client()
	if c == nil {
		return providerAuthOutcome{message: "No daemon connection; cannot sign in", settled: true, failed: true}
	}
	if strings.TrimSpace(provider) == "" {
		resolved, err := soleOAuthProvider(ctx)
		if err != nil {
			return providerAuthOutcome{message: err.Error(), settled: true, failed: true}
		}
		provider = resolved
	}
	resp, err := c.StartProviderAuth(ctx, provider)
	if err != nil {
		return providerAuthOutcome{message: "Failed to start sign-in: " + err.Error(), settled: true, failed: true}
	}
	if resp.AuthURL == "" {
		if resp.Status == "authorizing" {
			return providerAuthOutcome{provider: resp.Provider, browserOnly: true, message: "Continue sign-in in the browser on the daemon host."}
		}
		return providerAuthOutcome{provider: resp.Provider, message: "The daemon returned no authorization URL for " + resp.Provider, settled: true, failed: true}
	}
	if !authLaunchURL(resp.AuthURL) {
		return providerAuthOutcome{provider: resp.Provider, authURL: resp.AuthURL, message: "No browser could be opened; copy the URL below."}
	}
	return providerAuthOutcome{provider: resp.Provider, authURL: resp.AuthURL, message: "A browser window should have opened for " + resp.Provider + "."}
}

// watch polls the daemon until the flow settles or the reporting budget runs
// out. Running out is not a failure: the daemon keeps its callback open longer
// than the terminal will chatter, and a slow login still needs this screen to
// hold the URL and the paste field.
func (f *providerAuthFlow) watch(ctx context.Context) {
	deadline := time.Now().Add(authPollTimeout)
	for {
		if !authWait(ctx, authPollInterval) {
			return
		}
		if readProviderAuth(f.state).settled {
			return
		}
		outcome := providerAuthCheck(ctx, f.name())
		if outcome.settled {
			f.finish(outcome)
			return
		}
		if !time.Now().Before(deadline) {
			name := f.name()
			f.update(func(next *providerAuthState) {
				next.report = "Timed out waiting for the " + name + " sign-in to complete."
			})
			return
		}
	}
}

// providerAuthCheck reads the daemon's view of the flow currently in progress.
func providerAuthCheck(ctx context.Context, provider string) providerAuthOutcome {
	c := store.Client()
	if c == nil {
		return providerAuthOutcome{message: "No daemon connection; cannot check the sign-in", settled: true, failed: true}
	}
	resp, err := c.GetProviderAuthStatus(ctx, provider)
	if err != nil {
		return providerAuthOutcome{}
	}
	switch resp.Status {
	case "authenticated":
		message := "Signed in to " + resp.Provider + "."
		if resp.Account != "" {
			message = "Signed in to " + resp.Provider + " as " + resp.Account + "."
		}
		return providerAuthOutcome{message: message, settled: true}
	case "error":
		return providerAuthOutcome{message: "Sign-in failed: " + resp.Error, settled: true, failed: true}
	}
	return providerAuthOutcome{}
}

// providerAuthSubmit hands a pasted redirect URL to the daemon, which exchanges
// the code synchronously and answers with the final status.
func providerAuthSubmit(ctx context.Context, provider, callbackURL string) providerAuthOutcome {
	c := store.Client()
	if c == nil {
		return providerAuthOutcome{message: "No daemon connection; cannot submit the callback", settled: true, failed: true}
	}
	resp, err := c.CompleteProviderAuth(ctx, provider, api.ProviderAuthCallbackRequest{CallbackURL: callbackURL})
	if err != nil {
		// A structured answer means the daemon understood the paste and refused
		// it (usually no flow is open any more); waiting would only time out.
		if _, ok := errors.AsType[*api.APIError](err); ok {
			return providerAuthOutcome{message: "Could not use that callback URL: " + err.Error(), settled: true, failed: true}
		}
		return providerAuthOutcome{message: "Could not reach the daemon to submit the callback: " + err.Error()}
	}
	switch resp.Status {
	case "authenticated":
		message := "Signed in to " + resp.Provider + "."
		if resp.Account != "" {
			message = "Signed in to " + resp.Provider + " as " + resp.Account + "."
		}
		return providerAuthOutcome{message: message, settled: true}
	case "error":
		return providerAuthOutcome{message: "Sign-in failed: " + resp.Error, settled: true, failed: true}
	}
	return providerAuthOutcome{message: "Callback received; finishing sign-in for " + resp.Provider + "…"}
}

func authWait(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (f *providerAuthFlow) update(mutate func(*providerAuthState)) {
	if f == nil || f.state == nil {
		return
	}
	f.state.Set(func(current providerAuthState) providerAuthState {
		next := current
		mutate(&next)
		return next
	})
}

func (f *providerAuthFlow) finish(outcome providerAuthOutcome) {
	f.update(func(next *providerAuthState) {
		if outcome.message != "" {
			next.report = outcome.message
		}
		if outcome.authURL != "" {
			next.authURL = outcome.authURL
		}
		next.settled = true
		next.failed = outcome.failed
	})
}

func (f *providerAuthFlow) cancel() {
	if f == nil || f.stop == nil {
		return
	}
	f.stop()
}

// submit completes the waiting flow with a pasted redirect URL, the path taken
// when the browser cannot reach the daemon's loopback listener (SSH, headless).
func (f *providerAuthFlow) submit(ctx context.Context, callbackURL string) {
	if f == nil || f.state == nil {
		return
	}
	f.update(func(next *providerAuthState) {
		next.report = "Submitting the callback…"
	})
	goAuthBackground(func() {
		outcome := providerAuthSubmit(ctx, f.name(), callbackURL)
		if outcome.settled {
			f.finish(outcome)
			return
		}
		f.update(func(next *providerAuthState) { next.report = outcome.message })
		if !readProviderAuth(f.state).settled {
			f.watch(ctx)
		}
	})
}

type providerAuthViewProps struct {
	slotID   string
	provider string
	state    *kites.Store[providerAuthState]
}

// providerAuthView is the /login screen: the page to open, a way to copy it
// when no browser appeared, and a field to paste the redirect back when the
// browser could not reach the daemon's callback listener.
var providerAuthView = trail.FC("ProviderAuthView", func(props providerAuthViewProps) trail.Node {
	notice, setNotice := kitex.UseState("")
	reported, setReported := kitex.UseState(false)

	state := kites.Use(props.state, func(s providerAuthState) providerAuthState { return s })
	provider := strings.TrimSpace(props.provider)
	if provider == "" {
		provider = state.provider
	}

	paneStore := kitex.UseMemo(func() *kites.Store[mcpAuthState] {
		return kites.Create(mcpAuthState{
			server:  provider,
			authURL: state.authURL,
			report:  state.report,
		})
	}, []any{})

	kitex.UseEffect(func() {
		paneStore.Set(func(mcpAuthState) mcpAuthState {
			return mcpAuthState{
				server:  provider,
				authURL: state.authURL,
				report:  state.report,
			}
		})
	}, []any{provider, state.authURL, state.report})

	flowRef := kitex.UseRef[*providerAuthFlow](nil)
	kitex.UseEffect(func() {
		if flowRef.Current != nil {
			return
		}
		flowRef.Current = startProviderAuthFlow(provider, props.state)
	}, []any{})

	// Leaving the screen stops this client's reporting; the daemon keeps its
	// callback open, so finishing in the browser still signs the user in.
	kitex.UseEffectCleanup(func() func() {
		return func() {
			if flowRef.Current != nil {
				flowRef.Current.cancel()
			}
		}
	}, []any{})

	// A sign-in that finishes while the screen is open reports itself once in
	// the timeline and closes the slot so the outcome is immediately visible.
	kitex.UseEffect(func() {
		if !state.settled || reported() {
			return
		}
		setReported(true)
		message := strings.TrimSpace(state.report)
		if message != "" {
			if state.failed {
				store.AppendError("Auth", message)
			} else {
				store.AppendCommandOutput("/login", message)
			}
		}
		if flowRef.Current != nil {
			flowRef.Current.cancel()
		}
		store.RemoveSlot(props.slotID)
	}, []any{state.settled, state.report})

	closeView := func() {
		if flowRef.Current != nil {
			flowRef.Current.cancel()
		}
		if !state.settled && (state.authURL != "" || state.browserOnly) {
			store.AppendCommandOutput("/login", "Left the sign-in screen. The daemon keeps the flow open for five more minutes, so finishing in the browser still signs you in.")
		}
		store.RemoveSlot(props.slotID)
	}

	title := "Signing in"
	if provider != "" {
		title += " to " + provider
	}

	return mcpAuthPane(mcpAuthPaneProps{
		server:      provider,
		title:       title + "…",
		browserOnly: state.browserOnly,
		escLabel:    "Close",
		state:       paneStore,
		notice:      notice(),
		onCopy:      func() { setNotice(mcpAuthCopiedNotice) },
		onSubmit: func(callbackURL string) {
			setNotice("")
			if flowRef.Current != nil {
				flowRef.Current.submit(context.Background(), callbackURL)
			}
		},
		onBack: closeView,
	})
})
