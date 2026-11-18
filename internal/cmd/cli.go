package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli"
	"github.com/masterkeysrd/tasksmith/internal/version"
)

type replOptions struct {
	sessionID    string
	continueLast bool
	forkSession  bool
	agent        string
	model        string
	title        string
	sandboxMode  string
	backend      backend.Backend
}

// resolveSandboxMode returns the normalized --sandbox request ("native" or
// "local"); empty means the session default (native when the host provides a
// kernel backend). The request applies to sessions created by the REPL, not to
// the daemon process itself.
func (o *replOptions) resolveSandboxMode() (api.SandboxMode, error) {
	mode := api.SandboxMode(strings.ToLower(strings.TrimSpace(o.sandboxMode)))
	if mode == "" || mode.IsValid() {
		return mode, nil
	}
	return "", fmt.Errorf("invalid --sandbox value %q (want %q or %q)", o.sandboxMode, api.SandboxNative, api.SandboxLocal)
}

func bindREPLFlags(cmd *cobra.Command, opts *replOptions) {
	cmd.Flags().StringVar(&opts.sessionID, "session", "", "Existing session ID to resume")
	cmd.Flags().BoolVarP(&opts.continueLast, "continue", "c", false, "Resume the workspace's most recently updated session")
	cmd.Flags().BoolVar(&opts.forkSession, "fork", false, "Branch the resumed session (--session or -c) and attach to the fork")
	cmd.Flags().StringVar(&opts.agent, "agent", "", "Agent name when creating a new session")
	cmd.Flags().StringVar(&opts.model, "model", "", "Model identifier when creating a new session")
	cmd.Flags().StringVar(&opts.title, "title", "", "Session title when creating a new session")
	cmd.Flags().StringVar(&opts.sandboxMode, "sandbox", "", "Sandbox isolation backend for new sessions: \"native\" (OS kernel sandboxes) or \"local\" (in-process confinement)")
}

func newREPLCmd(rootOpts *rootOptions) *cobra.Command {
	opts := &replOptions{}
	cmd := &cobra.Command{
		Use:   "repl",
		Short: "Start an interactive REPL session attached to the Brain Daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runREPL(cmd, rootOpts, opts)
		},
	}
	bindREPLFlags(cmd, opts)
	return cmd
}

func runREPL(cmd *cobra.Command, rootOpts *rootOptions, opts *replOptions) error {
	ctx := cmd.Context()

	wsDir, err := rootOpts.resolveWorkspace()
	if err != nil {
		return err
	}

	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	sandboxMode, err := opts.resolveSandboxMode()
	if err != nil {
		return err
	}

	c, notices, err := connectOrStart(ctx, sockPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}

	sessionID := strings.TrimSpace(opts.sessionID)
	if opts.continueLast && sessionID == "" {
		latest, err := resolveLatestSession(ctx, c, wsDir)
		if err != nil {
			return err
		}
		sessionID = latest
	}
	if opts.forkSession {
		if sessionID == "" {
			return fmt.Errorf("--fork needs a session to branch: combine it with --session or -c")
		}
		probe := c
		if probe.CWD() == "" {
			probe = probe.WithCWD(wsDir)
		}
		forked, err := probe.ForkSession(ctx, sessionID, api.ForkSessionRequest{Title: strings.TrimSpace(opts.title)})
		if err != nil {
			return fmt.Errorf("fork session %q: %w", sessionID, err)
		}
		sessionID = forked.ID
	}

	if err := cli.Run(ctx, cli.Options{
		Client:         c,
		Workspace:      wsDir,
		Version:        version.Get(),
		SessionID:      sessionID,
		Agent:          strings.TrimSpace(opts.agent),
		Model:          strings.TrimSpace(opts.model),
		Title:          strings.TrimSpace(opts.title),
		Backend:        opts.backend,
		Sandbox:        sandboxMode,
		StartupNotices: notices,
	}); err != nil {
		return err
	}

	if sess := cli.ActiveSession(); sess != nil && sess.ID != "" {
		usage := cli.ActiveUsage()
		// Only subscription-billed models have a window to show: a per-token
		// model must never pay a quota round trip at exit, and a priced model
		// on a quota-capable provider still has no window. A failed fetch
		// falls back to the reading the store already holds.
		var quota *api.ModelQuota
		if sess.Model != nil && sess.Model.QuotaBilled {
			quota = cli.ActiveQuota()
			qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			if fresh, err := c.GetModelQuota(qctx, sess.Model.ID); err == nil && fresh != nil {
				quota = fresh
			}
			cancel()
		}
		summary := cli.FormatExitSummary(cli.ExitSummaryInput{
			SessionID: sess.ID,
			Usage:     usage,
			Quota:     quota,
			Color:     exitSummaryColors(cmd),
		})
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n\n\n%s\n\n", summary)
	}

	return nil
}

// resolveLatestSession resolves -c/--continue to the workspace's most recently
// updated top-level session ID. It returns "" (fresh session) when the
// workspace has no sessions yet, and surfaces transport errors.
func resolveLatestSession(ctx context.Context, c *client.Client, wsDir string) (string, error) {
	probe := c
	if probe.CWD() == "" {
		probe = probe.WithCWD(wsDir)
	}
	sess, err := probe.LatestSession(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve latest session: %w", err)
	}
	if sess == nil {
		return "", nil
	}
	return sess.ID, nil
}

// exitSummaryColors enables ANSI colors only for an interactive color-capable
// TTY, honoring NO_COLOR.
func exitSummaryColors(cmd *cobra.Command) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := cmd.OutOrStdout().(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}
