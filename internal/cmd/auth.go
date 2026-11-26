package cmd

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/spf13/cobra"
)

// Login waits on the user's browser, so polling gets its own long deadline
// while every individual daemon round trip stays short.
const (
	authCallTimeout = 5 * time.Second
	authPollTimeout = 5 * time.Minute
	authPollEvery   = time.Second
)

func newAuthCmd(rootOpts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage provider sign-in credentials",
		Args:  cobra.NoArgs,
	}

	var noBrowser, paste bool
	login := &cobra.Command{
		Use:   "login [provider]",
		Short: "Sign in to an OAuth provider",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := resolveAuthProvider(cmd.Context(), rootOpts, args)
			if err != nil {
				return err
			}
			return runAuthLogin(cmd, rootOpts, provider, noBrowser, paste)
		},
	}
	login.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the authorization URL instead of opening a browser")
	login.Flags().BoolVar(&paste, "paste", false, "Finish by pasting the redirect URL (for remote or headless hosts)")

	var jsonStatus bool
	status := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show provider sign-in status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthStatus(cmd, rootOpts, args, jsonStatus)
		},
	}
	status.Flags().BoolVar(&jsonStatus, "json", false, "Print provider status as JSON")

	logout := &cobra.Command{
		Use:   "logout [provider]",
		Short: "Remove stored credentials for a provider",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := resolveAuthProvider(cmd.Context(), rootOpts, args)
			if err != nil {
				return err
			}
			return runAuthLogout(cmd, rootOpts, provider)
		},
	}

	cmd.AddCommand(login, status, logout)
	return cmd
}

// authClient connects to the already-running daemon without spawning one:
// credential status is meaningless against a daemon that isn't there, and
// silently starting one would surprise a status or logout caller.
func authClient(ctx context.Context, rootOpts *rootOptions) (*client.Client, error) {
	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return nil, err
	}
	c, err := client.New(sockPath)
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if _, err := c.Health(hctx); err != nil {
		return nil, fmt.Errorf("daemon not running; start it with `tasksmith daemon start`")
	}
	return c, nil
}

// resolveAuthProvider returns the named provider, or the only OAuth-capable
// preset when exactly one exists and none was named.
func resolveAuthProvider(ctx context.Context, rootOpts *rootOptions, args []string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return strings.ToLower(strings.TrimSpace(args[0])), nil
	}

	c, err := authClient(ctx, rootOpts)
	if err != nil {
		return "", err
	}
	sctx, cancel := context.WithTimeout(ctx, authCallTimeout)
	defer cancel()
	providers, err := c.ListProviders(sctx)
	if err != nil {
		return "", err
	}

	var oauthNames []string
	for _, p := range providers {
		if p.AuthType == "oauth2" {
			oauthNames = append(oauthNames, p.Name)
		}
	}
	switch len(oauthNames) {
	case 1:
		return oauthNames[0], nil
	case 0:
		return "", fmt.Errorf("no OAuth providers are available; name one explicitly")
	default:
		return "", fmt.Errorf("multiple OAuth providers are available (%s); name one explicitly", strings.Join(oauthNames, ", "))
	}
}

func runAuthLogin(cmd *cobra.Command, rootOpts *rootOptions, provider string, noBrowser, paste bool) error {
	ctx := cmd.Context()
	c, err := authClient(ctx, rootOpts)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	startCtx, cancelStart := context.WithTimeout(ctx, authCallTimeout)
	defer cancelStart()
	if noBrowser || paste {
		providers, err := c.ListProviders(startCtx)
		if err != nil {
			return err
		}
		for _, p := range providers {
			if p.Name == provider && p.BrowserAuthLabel != "" {
				return fmt.Errorf("signing in to %s requires a browser on the daemon host", provider)
			}
		}
	}
	resp, err := c.StartProviderAuth(startCtx, provider)
	cancelStart()
	if err != nil {
		return err
	}

	if resp.AuthURL == "" && resp.Status == "authorizing" {
		_, _ = fmt.Fprintf(out, "Continue %s sign-in in the browser on the daemon host.\n", provider)
		return waitForAuthLogin(cmd, c, provider)
	}
	_, _ = fmt.Fprintf(out, "Open this URL in your browser to sign in to %s:\n\n  %s\n\n", provider, resp.AuthURL)
	if !noBrowser && !paste {
		if berr := auth.OpenBrowser(resp.AuthURL); berr != nil {
			_, _ = fmt.Fprintf(out, "(could not open a browser automatically: %v)\n", berr)
		}
	}

	if paste {
		return finishAuthLoginWithCallback(cmd, c, provider)
	}
	return waitForAuthLogin(cmd, c, provider)
}

// finishAuthLoginWithCallback completes an in-flight flow with a redirect URL
// pasted by the user, the path taken on SSH or headless hosts where the
// browser cannot reach the daemon's loopback listener.
func finishAuthLoginWithCallback(cmd *cobra.Command, c *client.Client, provider string) error {
	_, _ = fmt.Fprint(cmd.OutOrStdout(), "After authorizing, paste the full redirect URL here: ")
	callback, err := readAuthLine(cmd.InOrStdin())
	if err != nil {
		return err
	}

	doneCtx, cancel := context.WithTimeout(cmd.Context(), authCallTimeout)
	defer cancel()
	done, err := c.CompleteProviderAuth(doneCtx, provider, api.ProviderAuthCallbackRequest{CallbackURL: callback})
	if err != nil {
		return err
	}
	return reportAuthLoginResult(cmd.OutOrStdout(), provider, done)
}

func waitForAuthLogin(cmd *cobra.Command, c *client.Client, provider string) error {
	ctx := cmd.Context()
	ticker := time.NewTicker(authPollEvery)
	defer ticker.Stop()
	deadline := time.NewTimer(authPollTimeout)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for the %s sign-in to complete", provider)
		case <-ticker.C:
			sctx, cancel := context.WithTimeout(ctx, authCallTimeout)
			status, err := c.GetProviderAuthStatus(sctx, provider)
			cancel()
			if err != nil {
				return err
			}
			switch status.Status {
			case "authenticated":
				return reportAuthLoginResult(cmd.OutOrStdout(), provider, status)
			case "error":
				return fmt.Errorf("sign-in failed: %s", status.Error)
			}
		}
	}
}

func reportAuthLoginResult(out io.Writer, provider string, resp *api.ProviderAuthResponse) error {
	if resp == nil {
		return fmt.Errorf("empty auth response for %s", provider)
	}
	switch resp.Status {
	case "authenticated":
		if resp.Account != "" {
			_, _ = fmt.Fprintf(out, "Signed in to %s as %s\n", provider, resp.Account)
		} else {
			_, _ = fmt.Fprintf(out, "Signed in to %s\n", provider)
		}
		return nil
	case "error":
		return fmt.Errorf("sign-in failed: %s", resp.Error)
	default:
		return fmt.Errorf("%s sign-in did not complete (status %q)", provider, resp.Status)
	}
}

func runAuthStatus(cmd *cobra.Command, rootOpts *rootOptions, args []string, asJSON bool) error {
	ctx := cmd.Context()
	c, err := authClient(ctx, rootOpts)
	if err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx, authCallTimeout)
	defer cancel()

	out := cmd.OutOrStdout()
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		provider := strings.ToLower(strings.TrimSpace(args[0]))
		status, err := singleAuthStatus(sctx, c, provider)
		if err != nil {
			return err
		}
		if asJSON {
			return encoder.Encode(status)
		}
		return status.writeDetails(out)
	}

	providers, err := c.ListProviders(sctx)
	if err != nil {
		return err
	}
	if asJSON {
		if providers == nil {
			providers = []api.ProviderSummary{}
		}
		return encoder.Encode(providers)
	}
	if len(providers) == 0 {
		_, err := fmt.Fprintln(out, "No providers are configured.")
		return err
	}

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROVIDER\tAUTH\tSTATUS\tACCOUNT\tSCOPE")
	for _, p := range providers {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, cmp.Or(p.AuthType, "bearer"),
			providerReadiness(p), cmp.Or(p.Account, "-"), cmp.Or(p.Scope, "-"))
	}
	return w.Flush()
}

func providerReadiness(p api.ProviderSummary) string {
	if p.Status != "" {
		return p.Status
	}
	if p.HasKey {
		return "ready"
	}
	return "needs credentials"
}

type authStatusDetails struct {
	Provider     string   `json:"provider"`
	DisplayName  string   `json:"display_name,omitempty"`
	AuthType     string   `json:"auth_type"`
	AuthEnv      string   `json:"auth_env,omitempty"`
	Status       string   `json:"status"`
	Account      string   `json:"account,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	DefaultModel string   `json:"default_model,omitempty"`
	Models       []string `json:"models,omitempty"`
	Error        string   `json:"error,omitempty"`
}

func singleAuthStatus(ctx context.Context, c *client.Client, provider string) (authStatusDetails, error) {
	flow, flowErr := c.GetProviderAuthStatus(ctx, provider)
	var details authStatusDetails
	apiErr, badRequest := errors.AsType[*api.APIError](flowErr)
	if flowErr != nil && (!badRequest || apiErr.Code != "bad_request") {
		return details, flowErr
	}

	details.Provider = provider
	providers, listErr := c.ListProviders(ctx)
	if listErr != nil && flowErr != nil {
		return details, listErr
	}
	for _, p := range providers {
		if !strings.EqualFold(p.Name, provider) {
			continue
		}
		details.DisplayName = p.DisplayName
		details.AuthType = cmp.Or(p.AuthType, "bearer")
		details.AuthEnv = p.AuthEnv
		details.Account = p.Account
		details.Status = providerReadiness(p)
		details.Scope = p.Scope
		details.DefaultModel = p.DefaultModel
		details.Models = p.Models
		if flowErr != nil && details.AuthType != "oauth2" {
			return details, nil
		}
		break
	}
	if flowErr != nil {
		return details, flowErr
	}

	details.AuthType = cmp.Or(details.AuthType, "oauth2")
	details.Status = cmp.Or(flow.Status, details.Status)
	details.Account = cmp.Or(flow.Account, details.Account)
	details.Error = flow.Error
	return details, nil
}

func (d authStatusDetails) writeDetails(out io.Writer) error {
	name := d.Provider
	if d.DisplayName != "" && d.DisplayName != name {
		name = fmt.Sprintf("%s (%s)", d.DisplayName, name)
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	row := func(label, value string) {
		if value != "" {
			_, _ = fmt.Fprintf(w, "%s:\t%s\n", label, value)
		}
	}
	row("Provider", name)
	row("Auth", d.AuthType)
	row("Status", d.Status)
	row("Account", d.Account)
	row("Scope", d.Scope)
	row("Default model", d.DefaultModel)
	if len(d.Models) > 0 {
		row("Models", fmt.Sprint(len(d.Models)))
	}
	row("Error", d.Error)
	return w.Flush()
}

func runAuthLogout(cmd *cobra.Command, rootOpts *rootOptions, provider string) error {
	ctx := cmd.Context()
	c, err := authClient(ctx, rootOpts)
	if err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(ctx, authCallTimeout)
	defer cancel()

	resp, err := c.LogoutProviderAuth(sctx, provider)
	if err != nil {
		return err
	}
	if resp.Status == "signed-out" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s\n", provider)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s was not signed in\n", provider)
	}
	return nil
}

// readAuthLine reads one trimmed line from the CLI's stdin.
func readAuthLine(in io.Reader) (string, error) {
	line, err := bufio.NewReader(in).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if err != nil && err != io.EOF {
			return "", err
		}
		return "", fmt.Errorf("no callback URL provided")
	}
	return line, nil
}
