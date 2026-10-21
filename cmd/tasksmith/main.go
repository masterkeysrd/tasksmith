package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) > 0 && args[0] == "daemon" {
		return runDaemonCmd(ctx, args[1:])
	}

	return runREPL(ctx)
}

func runDaemonCmd(ctx context.Context, args []string) error {
	sub := "serve"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "serve":
		return runDaemonServe(ctx)
	case "start":
		return runDaemonStart(ctx)
	case "stop":
		return runDaemonStop(ctx)
	case "status":
		return runDaemonStatus(ctx)
	default:
		return fmt.Errorf("unknown daemon subcommand %q (expected serve, start, stop, or status)", sub)
	}
}

func runDaemonServe(ctx context.Context) error {
	logsDir, err := xdg.LogsDir()
	if err != nil {
		return fmt.Errorf("resolve logs directory: %w", err)
	}
	logger, rw, err := log.NewRotating(log.RotatingWriterConfig{
		Dir:    logsDir,
		Stream: "daemon",
	}, log.LevelInfo)
	if err != nil {
		return fmt.Errorf("initialize rotating logger: %w", err)
	}
	defer func() {
		_ = rw.Close()
	}()
	log.SetDefault(logger)

	sockPath, err := daemon.DefaultSocketPath()
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}
	lockPath, err := daemon.DefaultLockPath()
	if err != nil {
		return fmt.Errorf("resolve lock path: %w", err)
	}

	srv, err := daemon.New(daemon.Config{
		SocketPath:  sockPath,
		LockPath:    lockPath,
		GatewayPool: gateway.NewPool(),
		Logger:      logger,
	})
	if err != nil {
		return fmt.Errorf("initialize daemon server: %w", err)
	}

	return srv.Serve(ctx)
}

func runDaemonStart(ctx context.Context) error {
	c, err := daemon.ConnectOrStart(ctx, "")
	if err != nil {
		return err
	}
	h, err := c.Health(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Daemon running (pid=%d socket=%s)\n", h.PID, c.SocketPath())
	return nil
}

func runDaemonStop(ctx context.Context) error {
	sockPath, err := daemon.DefaultSocketPath()
	if err != nil {
		return err
	}
	c, err := client.New(sockPath)
	if err != nil {
		return err
	}
	if err := c.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}
	fmt.Println("Daemon stopped.")
	return nil
}

func runDaemonStatus(ctx context.Context) error {
	sockPath, err := daemon.DefaultSocketPath()
	if err != nil {
		return err
	}
	c, err := client.New(sockPath)
	if err != nil {
		return err
	}
	h, err := c.Health(ctx)
	if err != nil {
		fmt.Printf("Daemon is not running (socket=%s)\n", sockPath)
		return nil
	}
	fmt.Printf("Daemon running (pid=%d started_at=%s socket=%s workspaces=%d)\n",
		h.PID, h.StartedAt.Format(time.RFC3339), sockPath, len(h.Workspaces))
	for _, ws := range h.Workspaces {
		fmt.Printf("  - %s\n", ws)
	}
	return nil
}

func runREPL(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	c, err := daemon.ConnectOrStart(ctx, "")
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}

	activeSession, err := c.CreateSession(ctx, cwd, api.CreateSessionRequest{
		Title: "Terminal Session",
	})
	if err != nil {
		return fmt.Errorf("create initial session: %w", err)
	}

	var activeSessionID atomic.Value
	activeSessionID.Store(activeSession.ID)

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	go func() {
		for ev, err := range c.Subscribe(subCtx, cwd, "") {
			if err != nil {
				return
			}
			currID, _ := activeSessionID.Load().(string)
			if ev.SessionID != currID {
				continue
			}
			if ev.Type == api.EventChunk && ev.Chunk != nil {
				for _, b := range ev.Chunk.Content {
					if b.Type == api.BlockText && b.Text != "" {
						fmt.Print(b.Text)
					}
				}
			}
		}
	}()

	fmt.Printf("TaskSmith REPL (session=%s agent=%s model=%s)\n", activeSession.ID, activeSession.Agent, activeSession.Model)
	fmt.Println("Commands: /new [model], /sessions, /history, /mode [default|accept_edits|plan|ask|auto], /exit, /quit")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case line == "/exit" || line == "/quit":
			return nil

		case line == "/new" || strings.HasPrefix(line, "/new "):
			modelOverride := strings.TrimSpace(strings.TrimPrefix(line, "/new"))
			s, err := c.CreateSession(ctx, cwd, api.CreateSessionRequest{
				Title: "Terminal Session",
				Model: modelOverride,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
				continue
			}
			activeSession = s
			activeSessionID.Store(activeSession.ID)
			fmt.Printf("Switched to new session %s (agent=%s model=%s)\n", activeSession.ID, activeSession.Agent, activeSession.Model)

		case line == "/sessions":
			sessions, err := c.ListSessions(ctx, cwd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error listing sessions: %v\n", err)
				continue
			}
			for _, s := range sessions {
				marker := " "
				if s.ID == activeSession.ID {
					marker = "*"
				}
				fmt.Printf("%s %s\tstatus=%s\tagent=%s\tmodel=%s\tupdated_at=%s\n",
					marker, s.ID, s.Status, s.Agent, s.Model, s.UpdatedAt.Format(time.RFC3339))
			}

		case line == "/history":
			history, err := c.History(ctx, cwd, activeSession.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error loading history: %v\n", err)
				continue
			}
			for _, msg := range history {
				fmt.Printf("[%s] %s\n", msg.Role, extractMessageText(msg))
			}

		case line == "/mode" || strings.HasPrefix(line, "/mode "):
			arg := strings.TrimSpace(strings.TrimPrefix(line, "/mode"))
			if arg == "" {
				mode, err := c.PermissionMode(ctx, cwd, activeSession.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error reading permission mode: %v\n", err)
					continue
				}
				fmt.Printf("Permission mode: %s\n", mode)
				continue
			}
			nextMode := api.PermissionMode(arg)
			if err := c.SetPermissionMode(ctx, cwd, activeSession.ID, nextMode); err != nil {
				fmt.Fprintf(os.Stderr, "error setting permission mode: %v (valid: default, accept_edits, plan, ask, auto)\n", err)
				continue
			}
			fmt.Printf("Permission mode set to: %s\n", nextMode)

		default:
			err := c.Send(ctx, cwd, activeSession.ID, api.SendRequest{
				Content: line,
				Wait:    true,
			})
			fmt.Println()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			if err := promptPendingPermissions(ctx, scanner, c, cwd, activeSession.ID); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		}
	}

	return scanner.Err()
}

func promptPendingPermissions(ctx context.Context, scanner *bufio.Scanner, c *client.Client, workspace, sessionID string) error {
	for {
		perms, err := c.PendingPermissions(ctx, workspace, sessionID)
		if err != nil {
			return err
		}
		if len(perms) == 0 {
			return nil
		}

		perm := perms[0]
		fmt.Printf("\n[Permission Required] %s\n", perm.Summary)
		if perm.Diff != "" {
			fmt.Println(perm.Diff)
		}
		fmt.Print("Allow? [y]es once / [a]llow session / [n]o (reject): ")
		if !scanner.Scan() {
			fmt.Println()
			return scanner.Err()
		}

		raw := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(raw)

		decision := api.PermissionDecision{
			RequestID:  perm.ID,
			ToolCallID: perm.ToolCallID,
			Wait:       true,
		}

		switch {
		case lower == "y" || lower == "yes":
			decision.Kind = api.DecisionAllowOnce
		case lower == "a" || lower == "allow" || lower == "always":
			decision.Kind = api.DecisionAllowSession
			if len(perm.GrantPatterns) > 0 {
				decision.GrantPattern = perm.GrantPatterns[0]
			}
		default:
			decision.Kind = api.DecisionReject
			if after, ok := strings.CutPrefix(raw, "n "); ok && strings.HasPrefix(lower, "n ") {
				decision.Feedback = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(raw, "no "); ok && strings.HasPrefix(lower, "no ") {
				decision.Feedback = strings.TrimSpace(after)
			}
		}

		if err := c.RespondPermission(ctx, workspace, sessionID, decision); err != nil {
			return err
		}
		fmt.Println()
	}
}

func extractMessageText(msg api.Message) string {
	var sb strings.Builder
	for _, b := range msg.Content {
		if b.Type == api.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
