package metrics

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

// BaseTierKey is the group key used for calls billed at base pricing.
const BaseTierKey = "base"

// Usage accumulates the metrics of one session into an [api.SessionUsage].
type Usage struct {
	sessionID string
	totals    api.UsageGroup
	models    map[string]*api.UsageGroup
	tiers     map[string]*api.UsageGroup
	purposes  map[string]*api.UsageGroup
	tools     map[string]*api.ToolUsage
	changes   api.UsageChanges
	cache     api.UsageCache
	firstAt   time.Time
	lastAt    time.Time

	// cache analysis state
	havePrev       bool
	prevCacheRead  int64
	prevAt         time.Time
	compactedSince bool
}

// NewUsage returns an empty accumulator for sessionID.
func NewUsage(sessionID string) *Usage {
	return &Usage{
		sessionID: sessionID,
		models:    map[string]*api.UsageGroup{},
		tiers:     map[string]*api.UsageGroup{},
		purposes:  map[string]*api.UsageGroup{},
		tools:     map[string]*api.ToolUsage{},
	}
}

func group(m map[string]*api.UsageGroup, key string) *api.UsageGroup {
	g, ok := m[key]
	if !ok {
		g = &api.UsageGroup{Key: key}
		m[key] = g
	}
	return g
}

func addLLM(g *api.UsageGroup, r Record) {
	g.Calls++
	if r.Attr(AttrPriced) != "true" {
		g.UnpricedCalls++
	}
	if r.Status != StatusOK {
		g.Errors++
	}
	g.Tokens.Input += r.Value(TokensInput)
	g.Tokens.Output += r.Value(TokensOutput)
	g.Tokens.CacheRead += r.Value(TokensCacheRead)
	g.Tokens.CacheWrite += r.Value(TokensCacheWrite)
	g.Tokens.Reasoning += r.Value(TokensReasoning)
	g.CostNano += r.Value(CostTotal)
	g.CostCacheReadNano += r.Value(CostCacheRead)
	g.CostCacheWriteNano += r.Value(CostCacheWrite)
	g.DurationMS += r.Value(LatencyDuration)
	if r.Value(TokensInput) > api.LargeContextTokens {
		g.LargeContextCostNano += r.Value(CostTotal)
	}
}

func (u *Usage) tool(name string) *api.ToolUsage {
	t, ok := u.tools[name]
	if !ok {
		t = &api.ToolUsage{Tool: name}
		u.tools[name] = t
	}
	return t
}

// Apply adds one record to the accumulator. Kinds it does not know are ignored.
func (u *Usage) Apply(e Entry) {
	switch e.Kind {
	case KindLLMCall, KindToolCall, KindFileChange:
		if u.firstAt.IsZero() || e.CreatedAt.Before(u.firstAt) {
			u.firstAt = e.CreatedAt
		}
		if e.CreatedAt.After(u.lastAt) {
			u.lastAt = e.CreatedAt
		}
	}
	switch e.Kind {
	case KindLLMCall:
		addLLM(&u.totals, e.Record)
		addLLM(group(u.models, e.Attr(AttrProvider)+"/"+e.Attr(AttrModel)), e.Record)
		tier := e.Attr(AttrCostTier)
		if tier == "" {
			tier = BaseTierKey
		}
		addLLM(group(u.tiers, tier), e.Record)
		addLLM(group(u.purposes, e.Attr(AttrPurpose)), e.Record)
		u.applyCache(e)
	case KindToolCall:
		t := u.tool(e.Attr(AttrTool))
		t.Calls++
		switch e.Status {
		case StatusError:
			t.Errors++
		case StatusDenied:
			t.Denied++
		case StatusCanceled:
			t.Canceled++
		}
		d := e.Value(LatencyDuration)
		t.DurationMS += d
		t.MaxDurationMS = max(t.MaxDurationMS, d)
	case KindFileChange:
		if e.Attr(AttrRevert) == "true" {
			return
		}
		u.changes.LinesAdded += e.Value(LinesAdded)
		u.changes.LinesRemoved += e.Value(LinesRemoved)
		u.changes.Files += e.Value(FilesChanged)
		if tool := e.Attr(AttrTool); tool != "" {
			t := u.tool(tool)
			t.LinesAdded += e.Value(LinesAdded)
			t.LinesRemoved += e.Value(LinesRemoved)
		}
	}
}

// applyCache tracks prompt-cache behavior across the session's chat calls.
// A drop in cache read versus the previous chat call is a miss, unless a
// compaction ran in between, which makes it an expected rebuild.
func (u *Usage) applyCache(e Entry) {
	switch Purpose(e.Attr(AttrPurpose)) {
	case PurposeCompaction:
		u.compactedSince = true
	case PurposeChat:
		read := e.Value(TokensCacheRead)
		u.cache.Requests++
		u.cache.InputTokens += e.Value(TokensInput)
		u.cache.CacheReadTokens += read
		if u.havePrev && read < u.prevCacheRead {
			if u.compactedSince {
				u.cache.Rebuilds++
			} else {
				u.cache.Misses++
				u.cache.LastMissAt = e.CreatedAt
				u.cache.LastMissGapMS = e.CreatedAt.Sub(u.prevAt).Milliseconds()
				u.cache.LastMissRecachedTokens = e.Value(TokensCacheWrite)
			}
		}
		u.havePrev = true
		u.prevCacheRead = read
		u.prevAt = e.CreatedAt
		u.compactedSince = false
	}
}

// Snapshot returns a copy of the accumulated usage with groups sorted by key.
func (u *Usage) Snapshot() api.SessionUsage {
	return api.SessionUsage{
		SessionID: u.sessionID,
		Totals:    u.totals,
		Models:    sortedGroups(u.models),
		Tiers:     sortedGroups(u.tiers),
		Purposes:  sortedGroups(u.purposes),
		Tools:     sortedTools(u.tools),
		Changes:   u.changes,
		Cache:     u.cache,
		FirstAt:   u.firstAt,
		LastAt:    u.lastAt,
	}
}

func sortedGroups(m map[string]*api.UsageGroup) []api.UsageGroup {
	out := make([]api.UsageGroup, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func sortedTools(m map[string]*api.ToolUsage) []api.ToolUsage {
	out := make([]api.ToolUsage, 0, len(m))
	for _, t := range m {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

// LoadEntries reads every record of sessionID and of its child sessions.
func (s *Store) LoadEntries(ctx context.Context, sessionID string) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, r.created_at, r.kind, r.status, r.workspace_root, r.session_id, r.parent_session_id,
       r.turn_id, r.message_id, r.agent, r.attrs, m.metric, m.value
FROM metrics_records r LEFT JOIN metrics_measurements m ON m.record_id = r.id
WHERE r.session_id = ? OR r.parent_session_id = ?
ORDER BY r.created_at, r.id`, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("metrics: load entries: %w", err)
	}
	defer rows.Close()

	var (
		out []Entry
		cur *Entry
	)
	for rows.Next() {
		var (
			e         Entry
			createdAt string
			kind, st  string
			attrsJSON string
			metric    *string
			value     *int64
		)
		if err := rows.Scan(&e.ID, &createdAt, &kind, &st, &e.WorkspaceRoot, &e.SessionID, &e.ParentSessionID,
			&e.TurnID, &e.MessageID, &e.Agent, &attrsJSON, &metric, &value); err != nil {
			return nil, fmt.Errorf("metrics: scan entry: %w", err)
		}
		if cur == nil || cur.ID != e.ID {
			e.Kind, e.Status = Kind(kind), Status(st)
			e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
			attrs, err := decodeAttrs(attrsJSON)
			if err != nil {
				return nil, err
			}
			e.Attrs = attrs
			out = append(out, e)
			cur = &out[len(out)-1]
		}
		if metric != nil && value != nil {
			cur.Measurements = append(cur.Measurements, Measurement{Metric: Metric(*metric), Value: *value})
		}
	}
	return out, rows.Err()
}

// SessionUsage computes the persisted usage of sessionID.
func (s *Store) SessionUsage(ctx context.Context, sessionID string) (api.SessionUsage, error) {
	if err := s.Flush(ctx); err != nil {
		return api.SessionUsage{}, err
	}
	u, err := s.loadUsage(ctx, sessionID)
	if err != nil {
		return api.SessionUsage{}, err
	}
	return u.Snapshot(), nil
}

func (s *Store) loadUsage(ctx context.Context, sessionID string) (*Usage, error) {
	entries, err := s.LoadEntries(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	u := NewUsage(sessionID)
	for _, e := range entries {
		u.Apply(e)
	}
	return u, nil
}

// SummaryRequest selects a [Store.Summary].
type SummaryRequest struct {
	Scope         api.UsageScope
	WorkspaceRoot string
	Since, Until  time.Time
	GroupBy       api.UsageGroupBy
}

const topSessionsLimit = 10

var llmMetrics = []Metric{
	TokensInput, TokensOutput, TokensCacheRead, TokensCacheWrite, TokensReasoning,
	CostTotal, CostCacheRead, CostCacheWrite, LatencyDuration,
}

func sumAggregates(ms ...Metric) []Aggregate {
	out := make([]Aggregate, len(ms))
	for i, m := range ms {
		out[i] = Aggregate{Metric: m, Func: AggSum}
	}
	return out
}

// llmGroups runs an llm_call query grouped by dim (and bucket) and returns the
// groups in key order plus their combined totals.
func (s *Store) llmGroups(ctx context.Context, base Query, dim Dimension, groupKey func(Row) string) ([]api.UsageGroup, api.UsageGroup, error) {
	q := base
	q.Kind = KindLLMCall
	q.Aggregates = sumAggregates(llmMetrics...)
	q.GroupBy = []Dimension{"attr.priced", "status"}
	if dim != "" {
		q.GroupBy = append([]Dimension{dim}, q.GroupBy...)
	}
	res, err := RunQuery(ctx, s.db, q)
	if err != nil {
		return nil, api.UsageGroup{}, err
	}
	index := map[string]*api.UsageGroup{}
	var order []string
	var total api.UsageGroup
	for _, row := range res.Rows {
		key := groupKey(row)
		g, ok := index[key]
		if !ok {
			g = &api.UsageGroup{Key: key}
			index[key] = g
			order = append(order, key)
		}
		for _, t := range []*api.UsageGroup{g, &total} {
			t.Calls += row.Count
			if row.Group["attr.priced"] != "true" {
				t.UnpricedCalls += row.Count
			}
			if row.Group["status"] != string(StatusOK) {
				t.Errors += row.Count
			}
			t.Tokens.Input += row.Values[TokensInput]
			t.Tokens.Output += row.Values[TokensOutput]
			t.Tokens.CacheRead += row.Values[TokensCacheRead]
			t.Tokens.CacheWrite += row.Values[TokensCacheWrite]
			t.Tokens.Reasoning += row.Values[TokensReasoning]
			t.CostNano += row.Values[CostTotal]
			t.CostCacheReadNano += row.Values[CostCacheRead]
			t.CostCacheWriteNano += row.Values[CostCacheWrite]
			t.DurationMS += row.Values[LatencyDuration]
		}
	}
	sort.Strings(order)
	groups := make([]api.UsageGroup, len(order))
	for i, k := range order {
		groups[i] = *index[k]
	}
	return groups, total, nil
}

// Summary aggregates LLM, tool and code-change usage across sessions.
func (s *Store) Summary(ctx context.Context, req SummaryRequest) (api.UsageSummary, error) {
	if err := s.Flush(ctx); err != nil {
		return api.UsageSummary{}, err
	}
	filter := Filter{}
	switch req.Scope {
	case api.UsageScopeWorkspace:
		filter.WorkspaceRoot = req.WorkspaceRoot
	case api.UsageScopeGlobal:
	default:
		return api.UsageSummary{}, fmt.Errorf("%w: unknown scope %q", ErrInvalidQuery, req.Scope)
	}

	var (
		dim    Dimension
		bucket Bucket
	)
	switch req.GroupBy {
	case api.UsageGroupByModel:
		dim = "attr.model"
	case api.UsageGroupByProvider:
		dim = "attr.provider"
	case api.UsageGroupByAgent:
		dim = "agent"
	case api.UsageGroupByPurpose:
		dim = "attr.purpose"
	case api.UsageGroupByTier:
		dim = "attr.cost_tier"
	case api.UsageGroupByDay:
		bucket = BucketDay
	default:
		return api.UsageSummary{}, fmt.Errorf("%w: unknown group_by %q", ErrInvalidQuery, req.GroupBy)
	}

	base := Query{Filter: filter, Since: req.Since, Until: req.Until, Bucket: bucket}
	out := api.UsageSummary{Scope: req.Scope, GroupBy: req.GroupBy, Since: req.Since, Until: req.Until}

	var err error
	out.Groups, out.Totals, err = s.llmGroups(ctx, base, dim, func(row Row) string {
		switch {
		case dim != "":
			key := row.Group[string(dim)]
			if key == "" && req.GroupBy == api.UsageGroupByTier {
				return BaseTierKey
			}
			return key
		case bucket != BucketNone:
			return row.Bucket.Format("2006-01-02")
		}
		return ""
	})
	if err != nil {
		return api.UsageSummary{}, err
	}

	if req.Scope == api.UsageScopeGlobal {
		out.Workspaces, _, err = s.llmGroups(ctx, Query{Filter: filter, Since: req.Since, Until: req.Until}, "workspace_root",
			func(row Row) string { return row.Group["workspace_root"] })
		if err != nil {
			return api.UsageSummary{}, err
		}
	}

	if !req.Since.IsZero() {
		until := req.Until
		if until.IsZero() {
			until = time.Now().UTC()
		}
		span := until.Sub(req.Since)
		_, prev, err := s.llmGroups(ctx, Query{Filter: filter, Since: req.Since.Add(-span), Until: req.Since}, "",
			func(Row) string { return "" })
		if err != nil {
			return api.UsageSummary{}, err
		}
		out.Previous = &prev
	}

	if err := s.summarizeSessions(ctx, base, &out); err != nil {
		return api.UsageSummary{}, err
	}
	if err := s.summarizeTools(ctx, base, &out); err != nil {
		return api.UsageSummary{}, err
	}
	return out, nil
}

func (s *Store) summarizeSessions(ctx context.Context, base Query, out *api.UsageSummary) error {
	q := base
	q.Bucket = BucketNone
	q.Kind = KindLLMCall
	q.Aggregates = sumAggregates(CostTotal)
	q.GroupBy = []Dimension{"session_id", "workspace_root"}
	res, err := RunQuery(ctx, s.db, q)
	if err != nil {
		return err
	}
	var sessions []api.UsageSession
	for _, row := range res.Rows {
		id := row.Group["session_id"]
		if id == "" {
			continue
		}
		sessions = append(sessions, api.UsageSession{
			SessionID: id, Workspace: row.Group["workspace_root"],
			CostNano: row.Values[CostTotal], Calls: row.Count,
		})
	}
	out.Sessions = int64(len(sessions))
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].CostNano > sessions[j].CostNano })
	if len(sessions) > topSessionsLimit {
		sessions = sessions[:topSessionsLimit]
	}
	out.TopSessions = sessions
	return nil
}

func (s *Store) summarizeTools(ctx context.Context, base Query, out *api.UsageSummary) error {
	q := base
	q.Bucket = BucketNone
	q.Kind = KindToolCall
	q.Aggregates = []Aggregate{{Metric: LatencyDuration, Func: AggSum}}
	q.GroupBy = []Dimension{"attr.tool", "status"}
	res, err := RunQuery(ctx, s.db, q)
	if err != nil {
		return err
	}
	byTool := map[string]*api.ToolUsage{}
	get := func(name string) *api.ToolUsage {
		t, ok := byTool[name]
		if !ok {
			t = &api.ToolUsage{Tool: name}
			byTool[name] = t
		}
		return t
	}
	for _, row := range res.Rows {
		t := get(row.Group["attr.tool"])
		t.Calls += row.Count
		switch Status(row.Group["status"]) {
		case StatusError:
			t.Errors += row.Count
		case StatusDenied:
			t.Denied += row.Count
		case StatusCanceled:
			t.Canceled += row.Count
		}
		t.DurationMS += row.Values[LatencyDuration]
	}
	// The max aggregate shares the metric key with the sum, so it needs its own query.
	maxQ := q
	maxQ.Aggregates = []Aggregate{{Metric: LatencyDuration, Func: AggMax}}
	maxQ.GroupBy = []Dimension{"attr.tool"}
	maxRes, err := RunQuery(ctx, s.db, maxQ)
	if err != nil {
		return err
	}
	for _, row := range maxRes.Rows {
		get(row.Group["attr.tool"]).MaxDurationMS = row.Values[LatencyDuration]
	}

	cq := base
	cq.Bucket = BucketNone
	cq.Kind = KindFileChange
	cq.Aggregates = sumAggregates(LinesAdded, LinesRemoved, FilesChanged)
	cq.GroupBy = []Dimension{"attr.tool", "attr.revert"}
	cres, err := RunQuery(ctx, s.db, cq)
	if err != nil {
		return err
	}
	for _, row := range cres.Rows {
		if row.Group["attr.revert"] == "true" {
			continue
		}
		out.Changes.LinesAdded += row.Values[LinesAdded]
		out.Changes.LinesRemoved += row.Values[LinesRemoved]
		out.Changes.Files += row.Values[FilesChanged]
		if name := row.Group["attr.tool"]; name != "" {
			t := get(name)
			t.LinesAdded += row.Values[LinesAdded]
			t.LinesRemoved += row.Values[LinesRemoved]
		}
	}
	out.Tools = sortedTools(byTool)
	return nil
}

// Live decorates a [Store] with running per-session totals. After each
// record it calls publish with the updated usage of the affected sessions.
type Live struct {
	store   *Store
	publish func(sessionID string, usage api.SessionUsage)

	mu       sync.Mutex
	sessions map[string]*Usage
}

var _ Recorder = (*Live)(nil)

// NewLive wraps store. publish is called outside of any lock.
func NewLive(store *Store, publish func(sessionID string, usage api.SessionUsage)) *Live {
	return &Live{store: store, publish: publish, sessions: map[string]*Usage{}}
}

// Store returns the wrapped store.
func (l *Live) Store() *Store { return l.store }

// Record attributes and stores r, then updates and publishes live totals.
func (l *Live) Record(ctx context.Context, r Record) error {
	e := NewEntry(ctx, r)
	ids := []string{}
	if e.SessionID != "" {
		ids = append(ids, e.SessionID)
	}
	if e.ParentSessionID != "" {
		ids = append(ids, e.ParentSessionID)
	}

	// Totals must be loaded before the new entry is queued so it isn't counted twice.
	for _, id := range ids {
		if err := l.ensure(ctx, id); err != nil {
			return err
		}
	}
	if err := l.store.Write(e); err != nil {
		return err
	}

	updates := make(map[string]api.SessionUsage, len(ids))
	l.mu.Lock()
	for _, id := range ids {
		u := l.sessions[id]
		u.Apply(e)
		updates[id] = u.Snapshot()
	}
	l.mu.Unlock()
	if l.publish != nil {
		for id, usage := range updates {
			l.publish(id, usage)
		}
	}
	return nil
}

func (l *Live) ensure(ctx context.Context, sessionID string) error {
	l.mu.Lock()
	_, ok := l.sessions[sessionID]
	l.mu.Unlock()
	if ok {
		return nil
	}
	if err := l.store.Flush(ctx); err != nil {
		return err
	}
	u, err := l.store.loadUsage(ctx, sessionID)
	if err != nil {
		return err
	}
	l.mu.Lock()
	if _, ok := l.sessions[sessionID]; !ok {
		l.sessions[sessionID] = u
	}
	l.mu.Unlock()
	return nil
}

// SessionUsage returns the live usage of sessionID, loading it on first access.
func (l *Live) SessionUsage(ctx context.Context, sessionID string) (api.SessionUsage, error) {
	if err := l.ensure(ctx, sessionID); err != nil {
		return api.SessionUsage{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sessions[sessionID].Snapshot(), nil
}
