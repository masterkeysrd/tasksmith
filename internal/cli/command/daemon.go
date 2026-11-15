package command

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerDaemonCommands() {
	r.Register(Command{
		Name:        "daemon",
		Usage:       "/daemon [status|restart|start|stop]",
		Description: "Inspect or manage the background Brain Daemon",
		Handler:     handleDaemon,
		Complete:    completeDaemon,
	})
}

func handleDaemon(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch sub {
	case "", "status":
		store.EnqueueSlot(store.SlotItem{
			Kind: store.SlotKindPicker,
			Data: DaemonBrowser,
		})
		return nil

	case "restart":
		store.AppendCommandOutput("/daemon restart", "Restarting daemon...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h, err := store.RestartDaemon(ctx)
			if err != nil {
				store.AppendError("Error", err.Error())
				return
			}
			store.AppendOutput(fmt.Sprintf("Daemon restarted (pid=%d).", h.PID))
		}()
		return nil

	case "start":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := store.Client()
			if c != nil {
				pingCtx, pingCancel := context.WithTimeout(ctx, 300*time.Millisecond)
				h, err := c.Health(pingCtx)
				pingCancel()
				if err == nil && h != nil {
					store.AppendCommandOutput("/daemon start", fmt.Sprintf("Daemon is already running (pid=%d).", h.PID))
					return
				}
			}
			store.AppendCommandOutput("/daemon start", "Starting daemon...")
			h, err := store.StartDaemon(ctx)
			if err != nil {
				store.AppendError("Error", err.Error())
				return
			}
			store.AppendOutput(fmt.Sprintf("Daemon started (pid=%d).", h.PID))
		}()
		return nil

	case "stop":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := store.StopDaemon(ctx); err != nil {
				store.AppendError("Error", err.Error())
				return
			}
			store.AppendCommandOutput("/daemon stop", "Daemon stopped.")
		}()
		return nil

	default:
		return fmt.Errorf("unknown daemon subcommand %q (available: status, restart, start, stop)", sub)
	}
}

func completeDaemon(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []autocomplete.Candidate{
		{Value: "status", Label: "status", Description: "Inspect daemon health and connection status"},
		{Value: "restart", Label: "restart", Description: "Restart the background Brain Daemon"},
		{Value: "start", Label: "start", Description: "Ensure the Brain Daemon is running"},
		{Value: "stop", Label: "stop", Description: "Gracefully stop the Brain Daemon"},
	}, nil
}

// DaemonBrowser renders an interactive view of daemon health and controls in <Slot />.
var DaemonBrowser = trail.FC("DaemonBrowser", func(slotID string) trail.Node {
	status := store.UseDaemonStatus()
	health, setHealth := kitex.UseState[*api.HealthResponse](nil)
	inProgress, setInProgress := kitex.UseState(false)

	refreshHealth := func() {
		c := store.Client()
		if c == nil {
			setHealth(nil)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			h, err := c.Health(ctx)
			if err == nil {
				setHealth(h)
			} else {
				setHealth(nil)
			}
		}()
	}

	kitex.UseEffect(func() {
		c := store.Client()
		if c == nil {
			setHealth(nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		h, err := c.Health(ctx)
		if err == nil {
			setHealth(h)
		} else {
			setHealth(nil)
		}
	}, []any{status})

	doStart := func() {
		if inProgress() {
			return
		}
		setInProgress(true)
		go func() {
			defer setInProgress(false)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h, err := store.StartDaemon(ctx)
			if err == nil && h != nil {
				setHealth(h)
			} else {
				setHealth(nil)
			}
		}()
	}

	doRestart := func() {
		if inProgress() {
			return
		}
		setInProgress(true)
		go func() {
			defer setInProgress(false)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h, err := store.RestartDaemon(ctx)
			if err == nil && h != nil {
				setHealth(h)
			} else {
				setHealth(nil)
			}
		}()
	}

	doStop := func() {
		if inProgress() {
			return
		}
		setInProgress(true)
		go func() {
			defer setInProgress(false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = store.StopDaemon(ctx)
			setHealth(nil)
		}()
	}

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape", "q"):
			store.RemoveSlot(slotID)
		case k.MatchString("g"):
			refreshHealth()
		case k.MatchString("r"):
			if status == store.DaemonDisconnected || health() == nil {
				doStart()
			} else {
				doRestart()
			}
		case k.MatchString("s"):
			if status == store.DaemonDisconnected || health() == nil {
				doStart()
			}
		case k.MatchString("x"):
			if status == store.DaemonConnected && health() != nil {
				doStop()
			}
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	boxStyle := style.S().
		Border(border).
		Width(style.Percent(100)).
		FlexDirection(style.FlexColumn)

	titleStyle := style.S().Bold(true).Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Primary)

	sockPath := store.SocketPath()
	if sockPath == "" {
		if c := store.Client(); c != nil {
			sockPath = c.SocketPath()
		}
	}

	kvRow := func(label, value string, valColor color.Color) trail.Node {
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(label)),
			trail.Span(trail.SpanProps{
				Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(valColor),
			}, trail.Text(value)),
		)
	}

	msgRow := func(msg string) trail.Node {
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)).Margin(0, 0, 1, 0),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(msg)),
		)
	}

	var children []trail.Node
	children = append(children, trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Brain Daemon")))

	h := health()
	isRestarting := status == store.DaemonRestarting || inProgress()

	if isRestarting {
		children = append(children,
			kvRow("  Status:      ", "↻ Restarting...", scheme.Color.Text.Warning),
			kvRow("  Socket:      ", sockPath, scheme.Color.Text.Primary),
			msgRow("  Stopping existing daemon and spawning a new background process..."),
			uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
				Bindings: []uicomponents.KeyBinding{
					{Key: "esc", Label: "Close"},
				},
			}),
		)
	} else if status == store.DaemonConnected && h != nil {
		durationStr := format.Duration(time.Since(h.StartedAt))
		timeStr := h.StartedAt.Format("2006-01-02 15:04:05")

		runningChildren := []trail.Node{
			kvRow("  Status:      ", "● Running", scheme.Color.Text.Success),
			kvRow("  Version:     ", daemonDisplayVersion(h), scheme.Color.Text.Primary),
			kvRow("  PID:         ", fmt.Sprintf("%d", h.PID), scheme.Color.Text.Primary),
			kvRow("  Uptime:      ", fmt.Sprintf("%s (started %s)", durationStr, timeStr), scheme.Color.Text.Primary),
			kvRow("  Socket:      ", sockPath, scheme.Color.Text.Primary),
			kvRow("  Workspaces:  ", fmt.Sprintf("%d active", len(h.Workspaces)), scheme.Color.Text.Primary),
		}

		for _, ws := range h.Workspaces {
			runningChildren = append(runningChildren, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)),
			},
				trail.Span(trail.SpanProps{
					Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary),
				}, trail.Text(fmt.Sprintf("               • %s", ws))),
			))
		}

		runningChildren = append(runningChildren, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
			Bindings: []uicomponents.KeyBinding{
				{Key: "r", Label: "Restart"},
				{Key: "x", Label: "Stop"},
				{Key: "g", Label: "Refresh"},
				{Key: "esc", Label: "Close"},
			},
		}))

		children = append(children, runningChildren...)
	} else {
		// Offline
		children = append(children,
			kvRow("  Status:      ", "○ Offline", scheme.Color.Text.Error),
			kvRow("  Socket:      ", sockPath, scheme.Color.Text.Primary),
			msgRow("  The background daemon is not running. Press s or r to start it."),
			uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
				Bindings: []uicomponents.KeyBinding{
					{Key: "s/r", Label: "Start"},
					{Key: "g", Label: "Refresh"},
					{Key: "esc", Label: "Close"},
				},
			}),
		)
	}

	return trail.Box(trail.BoxProps{Style: boxStyle}, children...)
})

// daemonDisplayVersion renders the daemon build version from a health
// response, tolerating daemons that predate version reporting.
func daemonDisplayVersion(h *api.HealthResponse) string {
	if h == nil || strings.TrimSpace(h.Version) == "" {
		return "unknown"
	}
	return h.Version
}
