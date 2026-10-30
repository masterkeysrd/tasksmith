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
	q := metrics.Query{
		Kind:   metrics.Kind(req.Kind),
		Bucket: metrics.Bucket(req.Bucket),
		Since:  req.Since,
		Until:  req.Until,
		Filter: metrics.Filter{
			WorkspaceRoot:   req.Filter.WorkspaceRoot,
			SessionID:       req.Filter.SessionID,
			IncludeChildren: req.Filter.IncludeChildren,
			Agent:           req.Filter.Agent,
			Attrs:           req.Filter.Attrs,
		},
	}
	for _, a := range req.Aggregates {
		q.Aggregates = append(q.Aggregates, metrics.Aggregate{Metric: metrics.Metric(a.Metric), Func: metrics.AggFunc(a.Func)})
	}
	for _, g := range req.GroupBy {
		q.GroupBy = append(q.GroupBy, metrics.Dimension(g))
	}
	for _, s := range req.Filter.Status {
		q.Filter.Status = append(q.Filter.Status, metrics.Status(s))
	}
	if err := d.metricsStore.Flush(ctx); err != nil {
		return nil, err
	}
	res, err := d.metricsStore.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	out := &api.MetricsResult{Rows: make([]api.MetricsRow, len(res.Rows))}
	for i, row := range res.Rows {
		values := make(map[string]int64, len(row.Values))
		for m, v := range row.Values {
			values[string(m)] = v
		}
		out.Rows[i] = api.MetricsRow{Bucket: row.Bucket, Group: row.Group, Count: row.Count, Values: values}
	}
	return out, nil
}

func (d *Daemon) SessionUsage(ctx context.Context, rawWS, id string) (*api.SessionUsage, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	sess, err := rt.Sessions().Get(ctx, id)
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
	sreq := metrics.SummaryRequest{Scope: req.Scope, Since: req.Since, Until: req.Until, GroupBy: req.GroupBy}
	switch req.Scope {
	case api.UsageScopeWorkspace:
		rt, err := d.runtimes.Get(ctx, rawWS)
		if err != nil {
			return nil, err
		}
		sreq.WorkspaceRoot = rt.Workspace().RootDir()
	case api.UsageScopeGlobal:
	default:
		return nil, fmt.Errorf("%w: unknown scope %q", metrics.ErrInvalidQuery, req.Scope)
	}
	summary, err := d.metricsStore.Summary(ctx, sreq)
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
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return
	}
	sessions, err := rt.Sessions().List(ctx)
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
