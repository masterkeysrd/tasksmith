package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

type lineRunner struct {
	ctx             context.Context
	cfg             Config
	reg             *command.Registry
	in              *bufio.Scanner
	out             io.Writer
	errOut          io.Writer
	activeSession   *api.Session
	activeSessionID atomic.Value
	mode            api.PermissionMode
	turnSettledCh   chan struct{}
}

func runLineLoop(ctx context.Context, cfg Config, sess *api.Session, mode api.PermissionMode) error {
	r := &lineRunner{
		ctx:           ctx,
		cfg:           cfg,
		reg:           command.NewRegistry(),
		in:            bufio.NewScanner(cfg.In),
		out:           &syncWriter{w: cfg.Out},
		errOut:        &syncWriter{w: cfg.ErrOut},
		activeSession: sess,
		mode:          mode,
		turnSettledCh: make(chan struct{}, 1),
	}
	var initialID string
	if sess != nil {
		initialID = sess.ID
	}
	r.activeSessionID.Store(initialID)

	subCtx, cancelSub := context.WithCancel(ctx)
	subDone := r.startEventSubscription(subCtx)
	defer func() {
		cancelSub()
		<-subDone
	}()

	hdr := buildHeader(ctx, cfg, sess)
	_, _ = fmt.Fprintln(r.out, ui.FormatHeader(hdr))

	return r.loop()
}

func (r *lineRunner) startEventSubscription(subCtx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range r.cfg.Client.Subscribe(subCtx, "") {
			if err != nil {
				return
			}
			currID, _ := r.activeSessionID.Load().(string)
			if currID == "" || ev.SessionID != currID {
				continue
			}
			if ev.Type == api.EventChunk && ev.Chunk != nil {
				for _, b := range ev.Chunk.Content {
					if b.Type == api.BlockText && b.Text != "" {
						_, _ = fmt.Fprint(r.out, b.Text)
					}
				}
			}
			if ev.Type == api.EventCompleted || ev.Type == api.EventPermissionRequested || ev.Type == api.EventError {
				select {
				case r.turnSettledCh <- struct{}{}:
				default:
				}
			}
		}
	}()
	time.Sleep(15 * time.Millisecond)
	return done
}

func (r *lineRunner) drainTurnSettled() {
	select {
	case <-r.turnSettledCh:
	default:
	}
}

func (r *lineRunner) waitTurnSettled() {
	select {
	case <-r.turnSettledCh:
	case <-time.After(250 * time.Millisecond):
	case <-r.ctx.Done():
	}
}

func (r *lineRunner) loop() error {
	for {
		_, _ = fmt.Fprint(r.out, "> ")
		if !r.in.Scan() {
			_, _ = fmt.Fprintln(r.out)
			break
		}

		line := strings.TrimSpace(r.in.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if quit := r.handleSlashCommand(line); quit {
				return nil
			}
			continue
		}

		r.handlePrompt(line)
	}
	return r.in.Err()
}

func (r *lineRunner) handleSlashCommand(line string) bool {
	env := &command.Env{
		Client:       r.cfg.Client,
		Workspace:    r.cfg.Workspace,
		Session:      r.activeSession,
		Mode:         r.mode,
		DefaultAgent: r.cfg.Agent,
		DefaultModel: r.cfg.Model,
		DefaultTitle: r.cfg.Title,
	}

	res, err := r.reg.Execute(r.ctx, env, line)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "%v\n", err)
		return false
	}

	if res.ResetSession {
		r.activeSession = nil
		r.activeSessionID.Store("")
		r.cfg.Model = res.ModelOverride
	}
	if res.SwitchTo != nil {
		r.activeSession = res.SwitchTo
		r.activeSessionID.Store(res.SwitchTo.ID)
	}
	if res.ModeChange != "" {
		r.mode = res.ModeChange
	}
	if res.Output != "" {
		_, _ = fmt.Fprintln(r.out, res.Output)
	}
	return res.Quit
}

func (r *lineRunner) handlePrompt(line string) {
	if r.activeSession == nil || strings.TrimSpace(r.activeSession.ID) == "" {
		sess, err := ensureSession(r.ctx, r.cfg.Client, r.cfg.Title, r.cfg.Agent, r.cfg.Model, r.mode)
		if err != nil {
			_, _ = fmt.Fprintf(r.errOut, "error: %v\n", err)
			return
		}
		r.activeSession = sess
		r.activeSessionID.Store(sess.ID)
	}

	r.drainTurnSettled()
	err := r.cfg.Client.Send(r.ctx, r.activeSession.ID, api.SendRequest{
		Content: line,
		Wait:    true,
	})
	if err == nil {
		r.waitTurnSettled()
	}
	_, _ = fmt.Fprintln(r.out)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error: %v\n", err)
		return
	}
	if err := r.promptPendingPermissions(); err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error: %v\n", err)
	}
}

func (r *lineRunner) promptPendingPermissions() error {
	for {
		perms, err := r.cfg.Client.PendingPermissions(r.ctx, r.activeSession.ID)
		if err != nil {
			return err
		}
		if len(perms) == 0 {
			return nil
		}

		perm := perms[0]
		_, _ = fmt.Fprintln(r.out)
		_, _ = fmt.Fprintln(r.out, ui.FormatPermissionCard(perm, 0, false, ""))
		_, _ = fmt.Fprint(r.out, "Allow? [y]es once / [a]llow session / [n]o (reject): ")
		if !r.in.Scan() {
			_, _ = fmt.Fprintln(r.out)
			return r.in.Err()
		}

		decision := ui.ParsePermissionInput(perm, strings.TrimSpace(r.in.Text()))

		r.drainTurnSettled()
		if err := r.cfg.Client.RespondPermission(r.ctx, r.activeSession.ID, decision); err != nil {
			return err
		}
		if len(perms) == 1 {
			r.waitTurnSettled()
		}
		_, _ = fmt.Fprintln(r.out)
	}
}
