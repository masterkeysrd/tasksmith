package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/tools"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

type terminalPublisher struct{}

func (p *terminalPublisher) Publish(_ context.Context, ev session.Event) {
	if ev.Type == session.EventChunk && ev.Chunk != nil {
		for _, block := range ev.Chunk.Content {
			if tb, ok := block.(*message.TextBlock); ok && tb != nil {
				fmt.Print(tb.Text)
			}
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logsDir, err := xdg.LogsDir()
	if err != nil {
		return fmt.Errorf("resolve logs directory: %w", err)
	}
	logger, rw, err := log.NewRotating(log.RotatingWriterConfig{
		Dir:    logsDir,
		Stream: "cli",
	}, log.LevelInfo)
	if err != nil {
		return fmt.Errorf("initialize rotating logger: %w", err)
	}
	defer func() {
		_ = rw.Close()
	}()
	log.SetDefault(logger)

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	ws, err := workspace.Open(ctx, cwd, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: cwd,
		Network: sandbox.NetworkPolicy{
			AllowLoopback: true,
		},
	})
	if err != nil {
		return fmt.Errorf("initialize sandbox: %w", err)
	}
	defer func() {
		_ = sb.Close()
	}()

	gwCfg := buildGatewayConfig(ws)
	gw, err := gateway.New(ctx, gwCfg)
	if err != nil {
		return fmt.Errorf("initialize gateway: %w", err)
	}
	defer func() {
		_ = gw.Close()
	}()

	wsDataDir, err := xdg.WorkspaceDir(ws.RootDir())
	if err != nil {
		return fmt.Errorf("resolve workspace data directory: %w", err)
	}

	dbPath := filepath.Join(wsDataDir, "tasksmith.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite database %q: %w", dbPath, err)
	}
	defer func() {
		_ = db.Close()
	}()

	pub := &terminalPublisher{}
	mgr, err := session.New(session.Config{
		DB:        db,
		Workspace: ws,
		Gateway:   gw,
		Sandbox:   sb,
		Publisher: pub,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("initialize session manager: %w", err)
	}

	activeSession, err := mgr.Create(ctx, session.CreateParams{
		Title: "Terminal Session",
	})
	if err != nil {
		return fmt.Errorf("create initial session: %w", err)
	}

	fmt.Printf("TaskSmith REPL (session=%s agent=%s model=%s)\n", activeSession.ID, activeSession.Agent, activeSession.Model)
	fmt.Println("Commands: /new [model], /sessions, /history, /mode [default|accept_edits|plan|ask|auto], /system <text>, /exit, /quit")

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
			s, err := mgr.Create(ctx, session.CreateParams{
				Title: "Terminal Session",
				Model: modelOverride,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
				continue
			}
			activeSession = s
			fmt.Printf("Switched to new session %s (agent=%s model=%s)\n", activeSession.ID, activeSession.Agent, activeSession.Model)

		case line == "/sessions":
			sessions, err := mgr.List(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error listing sessions: %v\n", err)
				continue
			}
			for _, s := range sessions {
				marker := " "
				if s.ID == activeSession.ID {
					marker = "*"
				}
				fmt.Printf("%s %s\tagent=%s\tmodel=%s\tupdated_at=%s\n",
					marker, s.ID, s.Agent, s.Model, s.UpdatedAt.Format(time.RFC3339))
			}

		case line == "/history":
			history, err := mgr.History(ctx, activeSession.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error loading history: %v\n", err)
				continue
			}
			for _, msg := range history {
				fmt.Printf("[%s] %s\n", msg.Role(), extractMessageText(msg))
			}

		case line == "/mode" || strings.HasPrefix(line, "/mode "):
			arg := strings.TrimSpace(strings.TrimPrefix(line, "/mode"))
			if arg == "" {
				mode, err := mgr.PermissionMode(ctx, activeSession.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error reading permission mode: %v\n", err)
					continue
				}
				fmt.Printf("Permission mode: %s\n", mode)
				continue
			}
			nextMode := tools.PermissionMode(arg)
			if err := mgr.SetPermissionMode(ctx, activeSession.ID, nextMode); err != nil {
				fmt.Fprintf(os.Stderr, "error setting permission mode: %v (valid: default, accept_edits, plan, ask, auto)\n", err)
				continue
			}
			fmt.Printf("Permission mode set to: %s\n", nextMode)

		case line == "/system" || strings.HasPrefix(line, "/system "):
			text := strings.TrimSpace(strings.TrimPrefix(line, "/system"))
			if text == "" {
				fmt.Fprintln(os.Stderr, "usage: /system <text>")
				continue
			}
			if err := mgr.AppendMessage(ctx, activeSession.ID, message.NewSystemText(text)); err != nil {
				fmt.Fprintf(os.Stderr, "error appending system message: %v\n", err)
				continue
			}
			fmt.Printf("[system] %s\n", text)

		default:
			_, err := mgr.Send(ctx, activeSession.ID, message.NewUserText(line))
			fmt.Println()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			if err := promptPendingPermissions(ctx, scanner, mgr, activeSession.ID); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		}
	}

	return scanner.Err()
}

func promptPendingPermissions(ctx context.Context, scanner *bufio.Scanner, mgr *session.Manager, sessionID string) error {
	for {
		perms, err := mgr.PendingPermissions(ctx, sessionID)
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

		decision := tools.PermissionDecision{
			RequestID:  perm.ID,
			ToolCallID: perm.ToolCallID,
		}

		switch {
		case lower == "y" || lower == "yes":
			decision.Kind = tools.DecisionAllowOnce
		case lower == "a" || lower == "allow" || lower == "always":
			decision.Kind = tools.DecisionAllowSession
			if len(perm.GrantPatterns) > 0 {
				decision.GrantPattern = perm.GrantPatterns[0]
			}
		default:
			decision.Kind = tools.DecisionReject
			if strings.HasPrefix(lower, "n ") {
				decision.Feedback = strings.TrimSpace(raw[2:])
			} else if strings.HasPrefix(lower, "no ") {
				decision.Feedback = strings.TrimSpace(raw[3:])
			}
		}

		if _, err := mgr.RespondPermission(ctx, sessionID, decision); err != nil {
			return err
		}
		fmt.Println()
	}
}

func buildGatewayConfig(ws *workspace.Workspace) gateway.Config {
	wsEnv := ws.Env()
	wsProviders := ws.Providers()
	providers := make([]gateway.ProviderConfig, 0, len(wsProviders))
	for _, p := range wsProviders {
		providers = append(providers, toGatewayProvider(p))
	}

	defaultModels := make(map[gateway.ModelRole]string)
	if models := ws.ResolveModels(workspace.RoleCompletion); len(models) > 0 {
		defaultModels[gateway.RoleCompletion] = models[0]
	}

	return gateway.Config{
		Providers:     providers,
		DefaultModels: defaultModels,
		Env:           wsEnv,
	}
}

func toGatewayProvider(p workspace.Provider) gateway.ProviderConfig {
	var auth *gateway.AuthConfig
	if p.Auth != nil {
		auth = &gateway.AuthConfig{
			Type:   gateway.AuthType(p.Auth.Type),
			Header: p.Auth.Header,
			Env:    p.Auth.Env,
			File:   p.Auth.File,
		}
	}

	var defaultModels map[gateway.ModelRole]string
	if len(p.DefaultModels) > 0 {
		defaultModels = make(map[gateway.ModelRole]string, len(p.DefaultModels))
		for r, m := range p.DefaultModels {
			defaultModels[gateway.ModelRole(r)] = m
		}
	}

	var models []gateway.ModelSpec
	if len(p.Models) > 0 {
		models = make([]gateway.ModelSpec, len(p.Models))
		for i, m := range p.Models {
			var caps *gateway.ModelCapabilities
			if m.Capabilities != nil {
				caps = &gateway.ModelCapabilities{
					Completion: m.Capabilities.Completion,
					Decision:   m.Capabilities.Decision,
					Embedding:  m.Capabilities.Embedding,
					Attachment: m.Capabilities.Attachment,
					Tools:      m.Capabilities.Tools,
					Dimensions: m.Capabilities.Dimensions,
				}
			}
			models[i] = gateway.ModelSpec{
				ID:    m.ID,
				Name:  m.Name,
				Label: m.Label,
				Limits: gateway.ModelLimits{
					Context:    m.Limits.Context,
					Output:     m.Limits.Output,
					Dimensions: m.Limits.Dimensions,
				},
				Capabilities: caps,
			}
		}
	}

	return gateway.ProviderConfig{
		Name:          p.Name,
		Type:          p.Type,
		Endpoint:      p.Endpoint,
		DefaultModel:  p.DefaultModel,
		DefaultModels: defaultModels,
		Auth:          auth,
		Headers:       p.Headers,
		Config:        p.Config,
		Models:        models,
	}
}

func extractMessageText(msg message.Message) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range msg.GetContent() {
		if tb, ok := b.(*message.TextBlock); ok && tb != nil {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}
