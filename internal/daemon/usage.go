package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
)

// defaultUsageWindow is the period covered by workspace and global usage
// views when the request gives no start time.
const defaultUsageWindow = 30 * 24 * time.Hour

// MetricsBackend implementation

func (d *Daemon) MetricsQuery(ctx context.Context, req api.MetricsQuery) (*api.MetricsResult, error) {
	if err := d.metricsStore.Flush(ctx); err != nil {
		return nil, err
	}
	res, err := d.metricsStore.Query(ctx, req)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (d *Daemon) SessionUsage(ctx context.Context, rawWS, id string) (*api.SessionUsage, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	usage, err := d.usage.SessionUsage(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

func (d *Daemon) Usage(ctx context.Context, rawWS string, req api.UsageRequest) (*api.UsageSummary, error) {
	if req.Scope == "" {
		req.Scope = api.UsageScopeWorkspace
	}
	if req.GroupBy == "" {
		req.GroupBy = api.UsageGroupByModel
	}
	if req.Since.IsZero() {
		req.Since = time.Now().UTC().Add(-defaultUsageWindow)
	}
	var workspaceRoot string
	switch req.Scope {
	case api.UsageScopeWorkspace:
		rt, err := d.workspace(ctx, rawWS)
		if err != nil {
			return nil, err
		}
		workspaceRoot = rt.Workspace().RootDir()
	case api.UsageScopeGlobal:
	default:
		return nil, fmt.Errorf("%w: unknown scope %q", metrics.ErrInvalidQuery, req.Scope)
	}
	summary, err := d.metricsStore.Summary(ctx, workspaceRoot, req)
	if err != nil {
		return nil, err
	}
	if req.Scope == api.UsageScopeWorkspace {
		d.fillSessionTitles(ctx, rawWS, &summary)
	}
	return &summary, nil
}

// fillSessionTitles adds titles to the top sessions of a workspace summary.
// Sessions that no longer exist keep an empty title.
func (d *Daemon) fillSessionTitles(ctx context.Context, rawWS string, summary *api.UsageSummary) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return
	}
	sessions, err := rt.ListSessions(ctx)
	if err != nil {
		return
	}
	titles := make(map[string]string, len(sessions))
	for _, s := range sessions {
		titles[s.ID] = s.Title
	}
	for i := range summary.TopSessions {
		summary.TopSessions[i].Title = titles[summary.TopSessions[i].SessionID]
	}
}
