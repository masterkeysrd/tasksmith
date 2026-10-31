package daemon

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
)

func (r *workspaceRuntime) getLiveSession(ctx context.Context, id string) (*session.Session, error) {
	if sess, ok := r.sessions.Snapshot().Get(id); ok {
		return sess, nil
	}

	dbSess, err := r.sessionStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.loadOrCacheSession(ctx, dbSess)
}

func (r *workspaceRuntime) loadOrCacheSession(ctx context.Context, dbSess *session.Session) (*session.Session, error) {
	if sess, ok := r.sessions.Snapshot().Get(dbSess.ID); ok {
		return sess, nil
	}

	cfg := session.Config{
		DB:          r.db,
		Workspace:   r.ws,
		Gateway:     r.Gateway,
		Catalog:     r.Catalog,
		Permissions: r.perms,
		Publisher:   r.publisher,
		Logger:      r.logger,
		Metrics:     r.metrics,
		ReloadLock:  r.reloadMu,
		Context:     r.bgCtx,
		WaitGroup:   &r.wg,
		OnUpdate: func(ctx context.Context, s *session.Session) error {
			return s.CheckpointTo(ctx, r.sessionStore)
		},
	}

	sess, err := session.Open(ctx, dbSess, cfg)
	if err != nil {
		return nil, err
	}

	r.createMu.Lock()
	if existing, ok := r.sessions.Snapshot().Get(sess.ID); ok {
		r.createMu.Unlock()
		_ = sess.Close()
		return existing, nil
	}
	r.sessions.Save(sess)
	r.createMu.Unlock()

	return sess, nil
}

// CreateSession initializes and registers a new session in SQLite and in-memory cache.
func (r *workspaceRuntime) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*session.Session, bool, error) {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		return rt.CreateSession(ctx, req)
	}

	r.reloadMu.RLock()
	defer r.reloadMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	id := strings.TrimSpace(req.ID)
	if id != "" {
		r.createMu.Lock()
		defer r.createMu.Unlock()

		existing, err := r.getLiveSession(ctx, id)
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
		ID:          id,
		Title:       req.Title,
		Session:     req.Config,
		DB:          r.db,
		Workspace:   r.ws,
		Gateway:     r.Gateway,
		Catalog:     r.Catalog,
		Permissions: r.perms,
		Publisher:   r.publisher,
		Logger:      r.logger,
		Metrics:     r.metrics,
		ReloadLock:  r.reloadMu,
		Context:     r.bgCtx,
		WaitGroup:   &r.wg,
		OnUpdate: func(ctx context.Context, s *session.Session) error {
			return s.CheckpointTo(ctx, r.sessionStore)
		},
	}

	newSession, err := session.New(ctx, cfg)
	if err != nil {
		return nil, false, err
	}

	if err := newSession.CheckpointTo(ctx, r.sessionStore); err != nil {
		_ = newSession.Close()
		return nil, false, err
	}

	r.sessions.Save(newSession)

	r.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: r.ws.RootDir(),
		Session:   session.ToAPISession(newSession),
	})

	return newSession, true, nil
}

// PreviewSession inspects the proposed session configuration without creating a session.
func (r *workspaceRuntime) PreviewSession(ctx context.Context, config api.SessionConfig) (api.SessionConfig, models.Model, []api.SessionDiagnostic, bool, error) {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		return rt.PreviewSession(ctx, config)
	}

	r.reloadMu.RLock()
	defer r.reloadMu.RUnlock()
	return session.Preview(ctx, r.ws, r.Catalog(), config)
}

// GetSession returns an active or persisted session by ID.
func (r *workspaceRuntime) GetSession(ctx context.Context, id string) (*session.Session, error) {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		return rt.GetSession(ctx, id)
	}

	r.reloadMu.RLock()
	defer r.reloadMu.RUnlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, session.ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.getLiveSession(ctx, id)
}

// ListSessions lists all sessions for this workspace runtime.
func (r *workspaceRuntime) ListSessions(ctx context.Context) ([]*session.Session, error) {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		return rt.ListSessions(ctx)
	}

	r.reloadMu.RLock()
	defer r.reloadMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list, err := r.sessionStore.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*session.Session, len(list))
	for i, dbSess := range list {
		live, err := r.getLiveSession(ctx, dbSess.ID)
		if err != nil {
			return nil, err
		}
		out[i] = live
	}
	return out, nil
}

// UpdateSession updates an existing session's configuration or title.
func (r *workspaceRuntime) UpdateSession(ctx context.Context, id string, req api.UpdateSessionRequest) (*session.Session, error) {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		return rt.UpdateSession(ctx, id, req)
	}

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

// DeleteSession terminates, cancels, and purges a session and its data from SQLite and disk.
func (r *workspaceRuntime) DeleteSession(ctx context.Context, id string) error {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		return rt.DeleteSession(ctx, id)
	}

	r.reloadMu.RLock()
	defer r.reloadMu.RUnlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return session.ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	sess, _ := r.sessions.Snapshot().Get(id)
	r.sessions.Delete(id)

	if sess == nil {
		if live, err := r.getLiveSession(ctx, id); err == nil {
			sess = live
			r.sessions.Delete(id)
		}
	}

	if sess != nil {
		_ = sess.Cancel(ctx)
		_ = sess.Close()
		if sess.Timeline != nil {
			_ = sess.Timeline.Delete(ctx)
		}
		if sess.Journal != nil {
			_ = sess.Journal.Delete(ctx)
		}
	}

	if err := r.sessionStore.Delete(ctx, id); err != nil {
		return err
	}

	sessDir, err := session.SessionDir(r.ws.RootDir(), id)
	if err == nil {
		_ = os.RemoveAll(sessDir)
	}

	r.publisher.Publish(api.TopicSessions, api.SessionDeletedEvent{
		Workspace: r.ws.RootDir(),
		SessionID: id,
	})
	r.publisher.DeleteTopic(api.SessionStreamTopic(id))

	return nil
}

// RefreshSessions re-evaluates model diagnostics and harnesses for all live sessions.
func (r *workspaceRuntime) RefreshSessions() {
	if rt := r.SessionRuntime(); rt != nil && rt != r {
		rt.RefreshSessions()
		return
	}
	if r.sessions == nil {
		return
	}
	ctx := r.bgCtx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, sess := range r.sessions.Snapshot().All() {
		sess.Refresh(ctx)
	}
}
