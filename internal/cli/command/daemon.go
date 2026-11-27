package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
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

	h := health()
	running := status == store.DaemonConnected && h != nil
	busy := status == store.DaemonRestarting || inProgress()
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"s", "r"}, Hint: "s/r", Label: "Start", Disabled: running || busy, OnPress: doStart},
		{Keys: []string{"r"}, Label: "Restart", Disabled: !running || busy, OnPress: doRestart},
		{Keys: []string{"x"}, Label: "Stop", Disabled: !running || busy, OnPress: doStop},
		{Keys: []string{"g"}, Label: "Refresh", Disabled: busy, OnPress: refreshHealth},
		{Keys: []string{"esc", "escape", "q"}, Label: "Close", OnPress: func() { store.RemoveSlot(slotID) }},
	}, true)
	sheet := styles.UseCommon()

	sockPath := store.SocketPath()
	if sockPath == "" {
		if c := store.Client(); c != nil {
			sockPath = c.SocketPath()
		}
	}

	badge := uicomponents.BadgeProps{Icon: "○", Label: "Offline", Tone: uicomponents.ToneError}
	notice := "  The background daemon is not running. Press s or r to start it."
	if busy {
		badge = uicomponents.BadgeProps{Icon: "↻", Label: "Restarting...", Tone: uicomponents.ToneWarning}
		notice = "  Stopping existing daemon and spawning a new background process..."
	} else if running {
		badge = uicomponents.BadgeProps{Icon: "●", Label: "Running", Tone: uicomponents.ToneSuccess}
		notice = ""
	}
	items := []uicomponents.KVItem{{Label: "Status", ValueNode: uicomponents.Badge(badge)}}
	if running && !busy {
		uptime := fmt.Sprintf("%s (started %s)",
			format.Duration(time.Since(h.StartedAt)),
			h.StartedAt.Format("2006-01-02 15:04:05"),
		)
		items = append(items,
			uicomponents.KVItem{Label: "Version", Value: daemonDisplayVersion(h)},
			uicomponents.KVItem{Label: "PID", Value: fmt.Sprint(h.PID)},
			uicomponents.KVItem{Label: "Uptime", Value: uptime},
		)
	}
	items = append(items, uicomponents.KVItem{Label: "Socket", Value: sockPath})
	if running && !busy {
		items = append(items, uicomponents.KVItem{
			Label: "Workspaces",
			ValueNode: trail.Box(trail.BoxProps{Style: sheet.Column},
				trail.Span(trail.SpanProps{Style: sheet.Primary}, trail.Text(fmt.Sprintf("%d active", len(h.Workspaces)))),
				kitex.Map(h.Workspaces, func(ws string, _ int) trail.Node {
					return trail.Box(trail.BoxProps{Style: sheet.Row},
						trail.Span(trail.SpanProps{Style: sheet.Secondary}, trail.Text("• "+ws)),
					)
				}),
			),
		})
	}
	return uicomponents.Panel(uicomponents.PanelProps{
		Title: "Brain Daemon", Notice: notice, Bindings: bindings,
	}, uicomponents.KVList(uicomponents.KVListProps{Items: items, Indent: 2}))
})

// daemonDisplayVersion renders the daemon build version from a health
// response, tolerating daemons that predate version reporting.
func daemonDisplayVersion(h *api.HealthResponse) string {
	if h == nil || strings.TrimSpace(h.Version) == "" {
		return "unknown"
	}
	return h.Version
}
