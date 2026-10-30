package api

import "time"

// UsageTokens is a token breakdown. Input is the total prompt size and
// includes CacheRead and CacheWrite; uncached input is
// Input - CacheRead - CacheWrite.
type UsageTokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
}

// Uncached returns the prompt tokens that were neither read from nor written to the cache.
func (t UsageTokens) Uncached() int64 { return max(0, t.Input-t.CacheRead-t.CacheWrite) }

// Total returns the total tokens consumed: prompt plus output. Reasoning is
// reported by providers as part of output and is not added again.
func (t UsageTokens) Total() int64 { return t.Input + t.Output }

// UsageGroup aggregates LLM calls. Key identifies the group and is empty for totals.
type UsageGroup struct {
	Key           string      `json:"key,omitempty"`
	Calls         int64       `json:"calls"`
	UnpricedCalls int64       `json:"unpriced_calls"`
	Tokens        UsageTokens `json:"tokens"`
	// CostNano is the frozen cost in nano-USD (1e-9 USD).
	CostNano int64 `json:"cost_nano"`
	// CostCacheReadNano and CostCacheWriteNano are the cache parts of CostNano.
	CostCacheReadNano  int64 `json:"cost_cache_read_nano"`
	CostCacheWriteNano int64 `json:"cost_cache_write_nano"`
	// LargeContextCostNano is the cost of calls whose prompt exceeded
	// [LargeContextTokens]. It is only computed for session usage.
	LargeContextCostNano int64 `json:"large_context_cost_nano,omitempty"`
	// Errors counts calls that failed or were canceled.
	Errors     int64 `json:"errors"`
	DurationMS int64 `json:"duration_ms"`
}

// LargeContextTokens is the prompt size above which a call counts as a
// large-context call in usage insights.
const LargeContextTokens = 150_000

// ToolUsage aggregates executions of one tool. Calls includes every outcome;
// Errors, Denied and Canceled count the non-ok ones separately.
type ToolUsage struct {
	Tool          string `json:"tool"`
	Calls         int64  `json:"calls"`
	Errors        int64  `json:"errors"`
	Denied        int64  `json:"denied"`
	Canceled      int64  `json:"canceled"`
	DurationMS    int64  `json:"duration_ms"`
	MaxDurationMS int64  `json:"max_duration_ms"`
	// LinesAdded and LinesRemoved are the code changes made through the tool.
	LinesAdded   int64 `json:"lines_added,omitempty"`
	LinesRemoved int64 `json:"lines_removed,omitempty"`
}

// UsageChanges aggregates code changes. Reverts are excluded.
type UsageChanges struct {
	LinesAdded   int64 `json:"lines_added"`
	LinesRemoved int64 `json:"lines_removed"`
	Files        int64 `json:"files"`
}

// UsageCache describes prompt-cache behavior over chat calls.
type UsageCache struct {
	Requests int64 `json:"requests"`
	// InputTokens and CacheReadTokens cover chat calls only.
	InputTokens     int64 `json:"input_tokens"`
	CacheReadTokens int64 `json:"cache_read_tokens"`
	// Misses counts calls whose cache read dropped versus the previous chat
	// call. Rebuilds counts the drops that followed a compaction, which are
	// expected and are not counted as misses.
	Misses   int64 `json:"misses"`
	Rebuilds int64 `json:"rebuilds"`
	// LastMiss* describe the most recent miss. The gap is the time since the
	// previous chat call; the cause is not guessed.
	LastMissAt             time.Time `json:"last_miss_at"`
	LastMissGapMS          int64     `json:"last_miss_gap_ms,omitempty"`
	LastMissRecachedTokens int64     `json:"last_miss_recached_tokens,omitempty"`
}

// HitRate returns the share of chat prompt tokens served from cache (0-1).
func (c UsageCache) HitRate() float64 {
	if c.InputTokens <= 0 {
		return 0
	}
	return float64(c.CacheReadTokens) / float64(c.InputTokens)
}

// SessionUsage is the usage of one session (including child sessions).
type SessionUsage struct {
	SessionID string       `json:"session_id"`
	Totals    UsageGroup   `json:"totals"`
	Models    []UsageGroup `json:"models,omitempty"`
	Tiers     []UsageGroup `json:"tiers,omitempty"`
	Purposes  []UsageGroup `json:"purposes,omitempty"`
	Tools     []ToolUsage  `json:"tools,omitempty"`
	Changes   UsageChanges `json:"changes"`
	Cache     UsageCache   `json:"cache"`
	// FirstAt and LastAt bound the session's recorded activity; their
	// difference is the wall-clock duration.
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
}

// UsageSession is one session's share of a [UsageSummary].
type UsageSession struct {
	SessionID string `json:"session_id"`
	Workspace string `json:"workspace,omitempty"`
	Title     string `json:"title,omitempty"`
	CostNano  int64  `json:"cost_nano"`
	Calls     int64  `json:"calls"`
}

// UsageScope selects the extent of a [UsageSummary].
type UsageScope string

// Usage scopes.
const (
	UsageScopeWorkspace UsageScope = "workspace"
	UsageScopeGlobal    UsageScope = "global"
)

// UsageGroupBy selects how a [UsageSummary] groups LLM calls.
type UsageGroupBy string

// Usage group-by values.
const (
	UsageGroupByModel    UsageGroupBy = "model"
	UsageGroupByProvider UsageGroupBy = "provider"
	UsageGroupByAgent    UsageGroupBy = "agent"
	UsageGroupByPurpose  UsageGroupBy = "purpose"
	UsageGroupByTier     UsageGroupBy = "tier"
	UsageGroupByDay      UsageGroupBy = "day"
)

// UsageSummary is the usage across sessions of a workspace or of all workspaces.
type UsageSummary struct {
	Scope   UsageScope   `json:"scope"`
	GroupBy UsageGroupBy `json:"group_by"`
	Since   time.Time    `json:"since"`
	Until   time.Time    `json:"until"`
	Totals  UsageGroup   `json:"totals"`
	Groups  []UsageGroup `json:"groups,omitempty"`
	Tools   []ToolUsage  `json:"tools,omitempty"`
	Changes UsageChanges `json:"changes"`
	// Sessions is the number of sessions with recorded calls.
	Sessions int64 `json:"sessions"`
	// TopSessions lists the most expensive sessions, highest cost first.
	TopSessions []UsageSession `json:"top_sessions,omitempty"`
	// Workspaces groups usage by workspace root (global scope only).
	Workspaces []UsageGroup `json:"workspaces,omitempty"`
	// Previous holds the totals of the period of equal length that precedes
	// Since; it is nil when the summary has no start time.
	Previous *UsageGroup `json:"previous,omitempty"`
}

// MetricsQuery is the request body of POST /v1/metrics/query.
type MetricsQuery struct {
	Kind       string             `json:"kind,omitempty"`
	Aggregates []MetricsAggregate `json:"aggregates,omitempty"`
	GroupBy    []string           `json:"group_by,omitempty"`
	Bucket     string             `json:"bucket,omitempty"`
	Filter     MetricsFilter      `json:"filter"`
	Since      time.Time          `json:"since"`
	Until      time.Time          `json:"until"`
}

// MetricsAggregate selects a metric and aggregate function (sum, min, max).
type MetricsAggregate struct {
	Metric string `json:"metric"`
	Func   string `json:"func,omitempty"`
}

// MetricsFilter restricts the records a [MetricsQuery] considers.
type MetricsFilter struct {
	WorkspaceRoot   string            `json:"workspace_root,omitempty"`
	SessionID       string            `json:"session_id,omitempty"`
	IncludeChildren bool              `json:"include_children,omitempty"`
	Agent           string            `json:"agent,omitempty"`
	Status          []string          `json:"status,omitempty"`
	Attrs           map[string]string `json:"attrs,omitempty"`
}

// MetricsResult is the response of POST /v1/metrics/query.
type MetricsResult struct {
	Rows []MetricsRow `json:"rows"`
}

// MetricsRow is one group of a [MetricsResult].
type MetricsRow struct {
	Bucket time.Time         `json:"bucket"`
	Group  map[string]string `json:"group,omitempty"`
	Count  int64             `json:"count"`
	Values map[string]int64  `json:"values,omitempty"`
}

// UsageUpdatedEvent represents the payload for an [EventUsageUpdated] event.
type UsageUpdatedEvent struct {
	SessionID string       `json:"session_id"`
	Usage     SessionUsage `json:"usage"`
}

func (UsageUpdatedEvent) EventType() string { return string(EventUsageUpdated) }

// UsageRequest selects a [UsageSummary]. Zero values select the default:
// workspace scope, grouped by model, over the last 30 days.
type UsageRequest struct {
	Scope   UsageScope
	GroupBy UsageGroupBy
	Since   time.Time
	Until   time.Time
}
