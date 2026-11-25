package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
)

func (r *workspaceRuntime) sessionOwnerOrSelf() *workspaceRuntime {
	if r.sessionOwner != nil {
		return r.sessionOwner
	}
	return r
}

func (r *workspaceRuntime) getLiveSession(ctx context.Context, id string) (*session.Session, error) {
	return r.getLiveSessionWithLock(ctx, id, false)
}

func (r *workspaceRuntime) getLiveSessionWithLock(ctx context.Context, id string, createMuHeld bool) (*session.Session, error) {
	owner := r.sessionOwnerOrSelf()
	if sess, ok := owner.sessions.Snapshot().Get(id); ok {
		return sess, nil
	}

	dbSess, err := owner.sessionStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.loadOrCacheSession(ctx, dbSess, createMuHeld)
}

// sessionRuntimeForWorkDir resolves the workspaceRuntime matching dbSess.WorkDir
// so a persisted subproject session restored from SQLite retains its subproject
// Workspace (AGENTS.md, .agents/, .env) and model Catalog instead of falling back
// to whichever runtime triggered the load.
func (r *workspaceRuntime) sessionRuntimeForWorkDir(ctx context.Context, dbSess *session.Session) *workspaceRuntime {
	owner := r.sessionOwnerOrSelf()
	targetDir := strings.TrimSpace(dbSess.WorkDir)
	if targetDir == "" {
		return r
	}

	// Managed subagent worktrees live under <xdgWorkspaceDir>/trees/<id> outside
	// the repository root; resolve their Workspace/Catalog from the parent's
	// WorkDir so reopening never creates a synthetic workspace for the tree dir.
	if wsDataDir, err := xdg.WorkspaceDir(owner.ws.RootDir()); err == nil {
		treesDir := filepath.Join(wsDataDir, "trees")
		if fsutils.IsInsideDir(targetDir, treesDir) && targetDir != treesDir {
			if dbSess.ParentID != "" {
				if parentDB, err := owner.sessionStore.Get(ctx, dbSess.ParentID); err == nil {
					targetDir = strings.TrimSpace(parentDB.WorkDir)
				} else {
					targetDir = ""
				}
			} else {
				targetDir = ""
			}
		}
	}

	if targetDir == "" {
		return owner
	}
	if targetDir == r.dir {
		return r
	}
	if targetDir == owner.dir {
		return owner
	}
	if r.resolveWorkspace != nil {
		resolved, err := r.resolveWorkspace(ctx, targetDir)
		if err == nil && resolved.ws.RootDir() == owner.ws.RootDir() {
			return resolved
		}
		if err != nil && owner.logger != nil {
			owner.logger.Warn("daemon: resolve subproject workspace for session restore; falling back to workspace root",
				log.String("session", dbSess.ID),
				log.String("work_dir", targetDir),
				log.String("error", err.Error()),
			)
		}
	}
	return owner
}

func (r *workspaceRuntime) loadOrCacheSession(ctx context.Context, dbSess *session.Session, createMuHeld bool) (*session.Session, error) {
	owner := r.sessionOwnerOrSelf()
	if sess, ok := owner.sessions.Snapshot().Get(dbSess.ID); ok {
		return sess, nil
	}

	sessionRT := r.sessionRuntimeForWorkDir(ctx, dbSess)
	cfg := session.Config{
		DB:              owner.db,
		Workspace:       sessionRT.ws,
		Gateway:         sessionRT.Gateway,
		Catalog:         sessionRT.Catalog,
		Permissions:     owner.perms,
		SandboxSettings: sessionRT.SandboxConfig(),
		Publisher:       owner.publisher,
		Logger:          owner.logger,
		Metrics:         owner.metrics,
		ReloadLock:      owner.reloadMu,
		Context:         owner.bgCtx,
		WaitGroup:       &owner.wg,
		// Restored sessions need the caller too, or a session resumed after a
		// daemon restart would silently lose every MCP tool.
		MCPCaller: sessionRT.MCPCaller,
		OnUpdate:  owner.persistSession,
	}

	sess, err := session.Open(ctx, dbSess, cfg)
	if err != nil {
		return nil, err
	}

	if !createMuHeld {
		owner.createMu.Lock()
	}
	if existing, ok := owner.sessions.Snapshot().Get(sess.ID); ok {
		if !createMuHeld {
			owner.createMu.Unlock()
		}
		_ = sess.Close()
		return existing, nil
	}
	owner.sessions.Save(sess)
	if !createMuHeld {
		owner.createMu.Unlock()
	}

	if sess.ParentID == "" {
		children, err := owner.sessionStore.List(ctx, session.ListFilter{ParentID: new(sess.ID)})
		if err == nil {
			for _, chDB := range children {
				if child, err := sessionRT.getLiveSessionWithLock(ctx, chDB.ID, createMuHeld); err == nil {
					sess.AddChild(child)
					child.SetParent(sess)
				}
			}
		}
	}

	return sess, nil
}

// CreateSession initializes and registers a new session in SQLite and in-memory cache.
func (r *workspaceRuntime) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*session.Session, bool, error) {
	owner := r.sessionOwnerOrSelf()
	owner.reloadMu.RLock()
	defer owner.reloadMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	id := strings.TrimSpace(req.ID)
	if id != "" {
		owner.createMu.Lock()
		defer owner.createMu.Unlock()

		existing, err := r.getLiveSessionWithLock(ctx, id, true)
		switch {
		case err == nil:
			if err := existing.Matches(req); err != nil {
				return nil, false, err
			}
			return existing, false, nil
		case !errors.Is(err, session.ErrSessionNotFound):
			return nil, false, err
		}
	}

	cfg := session.Config{
		ID:              id,
		Title:           req.Title,
		WorkDir:         r.dir,
		Session:         req.Config,
		DB:              owner.db,
		Workspace:       r.ws,
		Gateway:         r.Gateway,
		Catalog:         r.Catalog,
		Permissions:     owner.perms,
		SandboxSettings: r.SandboxConfig(),
		Publisher:       owner.publisher,
		Logger:          owner.logger,
		Metrics:         owner.metrics,
		ReloadLock:      owner.reloadMu,
		Context:         owner.bgCtx,
		WaitGroup:       &owner.wg,
		MCPCaller:       r.MCPCaller,
		OnUpdate:        owner.persistSession,
	}

	newSession, err := session.New(ctx, cfg)
	if err != nil {
		return nil, false, err
	}

	if err := newSession.CheckpointTo(ctx, owner.sessionStore); err != nil {
		_ = newSession.Close()
		return nil, false, err
	}

	owner.sessions.Save(newSession)

	owner.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: r.ws.RootDir(),
		Session:   session.ToAPISession(newSession),
	})

	return newSession, true, nil
}

// persistSession checkpoints a live session unless teardown has started.
// Serializing with DeleteSession on createMu prevents background goroutines
// (such as async title generation) from resurrecting a deleted session row.
func (r *workspaceRuntime) persistSession(ctx context.Context, s *session.Session) error {
	owner := r.sessionOwnerOrSelf()
	owner.createMu.Lock()
	defer owner.createMu.Unlock()
	if s.IsClosing() {
		return nil
	}
	if err := s.CheckpointTo(ctx, owner.sessionStore); err != nil {
		return err
	}
	owner.sessions.Save(s)
	return nil
}

// PreviewSession inspects the proposed session configuration without creating a session.
func (r *workspaceRuntime) PreviewSession(ctx context.Context, config api.SessionConfig) (api.SessionConfig, models.Model, []api.SessionDiagnostic, bool, error) {
	owner := r.sessionOwnerOrSelf()
	owner.reloadMu.RLock()
	defer owner.reloadMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return api.SessionConfig{}, models.Model{}, nil, false, err
	}
	return session.Preview(ctx, r.ws, r.Catalog(), config)
}

// GetSession returns an active or persisted session by ID.
func (r *workspaceRuntime) GetSession(ctx context.Context, id string) (*session.Session, error) {
	owner := r.sessionOwnerOrSelf()
	owner.reloadMu.RLock()
	defer owner.reloadMu.RUnlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, session.ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sess, err := r.getLiveSession(ctx, id)
	if err != nil {
		return nil, err
	}

	if sess.ParentID != "" && sess.Parent() == nil {
		if parent, err := r.getLiveSession(ctx, sess.ParentID); err == nil {
			parent.AddChild(sess)
			sess.SetParent(parent)
		}
	}

	return sess, nil
}

// LatestSessionMeta returns metadata for the most recently updated top-level
// session in this runtime's WorkDir without hydrating any session runtime,
// backing quick resume (`-c` / `--continue`). It reports ErrSessionNotFound
// when the target directory has no sessions, and overlays the live status when
// the newest session happens to be resident.
func (r *workspaceRuntime) LatestSessionMeta(ctx context.Context) (api.Session, error) {
	if err := ctx.Err(); err != nil {
		return api.Session{}, err
	}
	owner := r.sessionOwnerOrSelf()
	dbSess, err := owner.sessionStore.Latest(ctx, session.LatestFilter{
		WorkDir:             r.dir,
		IncludeEmptyWorkDir: r.ws == nil || r.dir == r.ws.RootDir(),
	})
	if err != nil {
		return api.Session{}, err
	}
	meta := session.ToAPISession(dbSess)
	if live, ok := owner.sessions.Snapshot().Get(dbSess.ID); ok {
		meta = session.ToAPISession(live)
	}
	return meta, nil
}

// ListSessions lists all sessions for this workspace runtime.
func (r *workspaceRuntime) ListSessions(ctx context.Context) ([]*session.Session, error) {
	owner := r.sessionOwnerOrSelf()
	owner.reloadMu.RLock()
	defer owner.reloadMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list, err := owner.sessionStore.List(ctx, session.ListFilter{})
	if err != nil {
		return nil, err
	}
	all := make([]*session.Session, len(list))
	byID := make(map[string]*session.Session, len(list))
	for i, dbSess := range list {
		live, err := r.getLiveSession(ctx, dbSess.ID)
		if err != nil {
			return nil, err
		}
		all[i] = live
		byID[live.ID] = live
	}

	var roots []*session.Session
	for _, sess := range all {
		if sess.ParentID != "" {
			if parent, ok := byID[sess.ParentID]; ok {
				parent.AddChild(sess)
				sess.SetParent(parent)
			}
		} else {
			roots = append(roots, sess)
		}
	}
	return roots, nil
}

// UpdateSession updates an existing session's configuration or title.
func (r *workspaceRuntime) UpdateSession(ctx context.Context, id string, req api.UpdateSessionRequest) (*session.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, session.ErrEmptySessionID
	}
	sess, err := r.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := sess.Update(ctx, req); err != nil {
		return nil, err
	}
	return sess, nil
}

// ForkSession branches an existing session into a new top-level session that
// starts from the source's full conversation, and registers it in SQLite and
// the in-memory cache.
func (r *workspaceRuntime) ForkSession(ctx context.Context, id string, req api.ForkSessionRequest) (*session.Session, error) {
	owner := r.sessionOwnerOrSelf()
	owner.reloadMu.RLock()
	defer owner.reloadMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return nil, session.ErrEmptySessionID
	}
	source, err := r.getLiveSession(ctx, id)
	if err != nil {
		return nil, err
	}

	child, err := source.Fork(ctx, req.Title)
	if err != nil {
		return nil, err
	}

	if err := child.CheckpointTo(ctx, owner.sessionStore); err != nil {
		_ = child.Close()
		return nil, err
	}

	owner.sessions.Save(child)

	return child, nil
}

// DeleteSession terminates, cancels, and purges a session and its data from SQLite and disk.
func (r *workspaceRuntime) DeleteSession(ctx context.Context, id string) error {
	owner := r.sessionOwnerOrSelf()
	owner.reloadMu.RLock()
	defer owner.reloadMu.RUnlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return session.ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	sess, _ := owner.sessions.Snapshot().Get(id)
	owner.sessions.Delete(id)

	if sess == nil {
		if live, err := r.getLiveSession(ctx, id); err == nil {
			sess = live
			owner.sessions.Delete(id)
		}
	}

	if sess != nil {
		if parent := sess.Parent(); parent != nil {
			parent.RemoveChild(sess.ID)
		}
		for _, child := range sess.Children() {
			_ = r.DeleteSession(ctx, child.ID)
		}
		_ = sess.Cancel(ctx)
		_ = sess.Close()
		if sess.Timeline != nil {
			_ = sess.Timeline.Delete(ctx)
		}
		if sess.Journal != nil {
			_ = sess.Journal.Delete(ctx)
		}
		// Only remove managed subagent git worktrees under <wsDataDir>/trees/;
		// never delete a user subproject WorkDir.
		if wsDataDir, dirErr := xdg.WorkspaceDir(owner.ws.RootDir()); dirErr == nil && sess.WorkDir != "" {
			treesDir := filepath.Join(wsDataDir, "trees")
			if fsutils.IsInsideDir(sess.WorkDir, treesDir) && sess.WorkDir != treesDir {
				repoDir := owner.ws.RootDir()
				if p := sess.Parent(); p != nil && p.WorkDir != "" {
					repoDir = p.WorkDir
				}
				_ = exec.CommandContext(ctx, "git", "-C", repoDir, "worktree", "remove", "--force", sess.WorkDir).Run()
				_ = os.RemoveAll(sess.WorkDir)
			}
		}
	}

	owner.createMu.Lock()
	owner.sessions.Delete(id)
	err := owner.sessionStore.Delete(ctx, id)
	owner.createMu.Unlock()
	if err != nil {
		return err
	}

	sessDir, err := session.SessionDir(owner.ws.RootDir(), id)
	if err == nil {
		_ = os.RemoveAll(sessDir)
	}

	owner.publisher.Publish(api.TopicSessions, api.SessionDeletedEvent{
		Workspace: owner.ws.RootDir(),
		SessionID: id,
	})
	owner.publisher.DeleteTopic(api.SessionStreamTopic(id))

	return nil
}

// RefreshSessions re-evaluates model diagnostics and harnesses for all live sessions.
func (r *workspaceRuntime) RefreshSessions() {
	owner := r.sessionOwnerOrSelf()
	if owner.sessions == nil {
		return
	}
	ctx := owner.bgCtx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, sess := range owner.sessions.Snapshot().All() {
		sess.Refresh(ctx)
	}
}
