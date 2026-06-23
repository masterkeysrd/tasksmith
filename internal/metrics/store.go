package metrics

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

// Store encapsulates query operations against the global metrics SQLite database.
type Store struct {
	db *sqlx.DB
}

// NewStore initializes a Store with a database instance.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// GetTokenAnalytics aggregates token usage statistics over a given timeframe and optional provider filter.
func (s *Store) GetTokenAnalytics(ctx context.Context, timeframe string, providerFilter string) (*TokenAnalyticsResult, error) {
	resp := &TokenAnalyticsResult{
		ProvidersList: []string{},
		DailyActivity: []DailyActivity{},
		ByProject:     []ByProjectStats{},
		ByModel:       []ByModelStats{},
		ByAgent:       []ByAgentStats{},
		Tools:         []ToolAnalytics{},
	}

	if s.db == nil {
		return resp, nil
	}

	// Determine timeframe limit
	var limitTime time.Time
	switch timeframe {
	case "today":
		limitTime = time.Now().Add(-24 * time.Hour)
	case "30days":
		limitTime = time.Now().Add(-30 * 24 * time.Hour)
	default: // "7days"
		limitTime = time.Now().Add(-7 * 24 * time.Hour)
	}

	var err error

	resp.ProvidersList, err = s.queryProvidersList(ctx)
	if err != nil {
		log.Warn("Failed to query unique providers", log.Err(err))
	}

	resp.GlobalStats, err = s.queryGlobalStats(ctx, limitTime, providerFilter)
	if err != nil {
		log.Warn("Failed to query global stats", log.Err(err))
	}

	resp.SummaryBreakdown, err = s.querySummaryBreakdown(ctx, limitTime, providerFilter)
	if err != nil {
		log.Warn("Failed to query summary breakdown", log.Err(err))
	}

	resp.DailyActivity, err = s.queryDailyActivity(ctx, limitTime, providerFilter)
	if err != nil {
		log.Warn("Failed to query daily activity", log.Err(err))
	}

	resp.ByProject, err = s.queryByProject(ctx, limitTime, providerFilter)
	if err != nil {
		log.Warn("Failed to query project stats", log.Err(err))
	}

	resp.ByModel, err = s.queryByModel(ctx, limitTime, providerFilter)
	if err != nil {
		log.Warn("Failed to query model stats", log.Err(err))
	}

	resp.ByAgent, err = s.queryByAgent(ctx, limitTime, providerFilter)
	if err != nil {
		log.Warn("Failed to query agent stats", log.Err(err))
	}

	resp.Tools, err = s.queryTools(ctx, limitTime)
	if err != nil {
		log.Warn("Failed to query tools stats", log.Err(err))
	}

	return resp, nil
}

func (s *Store) queryProvidersList(ctx context.Context) ([]string, error) {
	var list []string
	err := s.db.SelectContext(ctx, &list, `
		SELECT DISTINCT COALESCE(json_extract(payload, '$.provider'), '') as provider
		FROM metrics_events
		WHERE event_type = 'llm_call'
		ORDER BY provider ASC
	`)
	if err != nil {
		return []string{}, err
	}
	return list, nil
}

func (s *Store) queryGlobalStats(ctx context.Context, limitTime time.Time, providerFilter string) (TokenAnalyticsStats, error) {
	queryGlobal := `
		SELECT 
			COUNT(*) as total_calls,
			COUNT(DISTINCT session_id) as total_sessions,
			COALESCE(SUM(CAST(json_extract(payload, '$.prompt_tokens') AS INTEGER)), 0) as prompt_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.completion_tokens') AS INTEGER)), 0) as completion_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.total_tokens') AS INTEGER)), 0) as total_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.cache_read_tokens') AS INTEGER)), 0) as cache_read_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.estimated_cost_usd') AS REAL)), 0.0) as estimated_cost_usd
		FROM metrics_events
		WHERE event_type = 'llm_call' AND created_at >= ?`
	args := []interface{}{limitTime}
	if providerFilter != "" && providerFilter != "ALL" {
		queryGlobal += " AND json_extract(payload, '$.provider') = ?"
		args = append(args, providerFilter)
	}

	var gStats struct {
		TotalCalls       int     `db:"total_calls"`
		TotalSessions    int     `db:"total_sessions"`
		PromptTokens     int     `db:"prompt_tokens"`
		CompletionTokens int     `db:"completion_tokens"`
		TotalTokens      int     `db:"total_tokens"`
		CacheReadTokens  int     `db:"cache_read_tokens"`
		EstimatedCostUSD float64 `db:"estimated_cost_usd"`
	}
	if err := s.db.GetContext(ctx, &gStats, queryGlobal, args...); err != nil {
		return TokenAnalyticsStats{}, err
	}

	return TokenAnalyticsStats{
		TotalCalls:       gStats.TotalCalls,
		PromptTokens:     gStats.PromptTokens,
		CompletionTokens: gStats.CompletionTokens,
		TotalTokens:      gStats.TotalTokens,
		CacheReadTokens:  gStats.CacheReadTokens,
		TotalSessions:    gStats.TotalSessions,
		EstimatedCostUSD: gStats.EstimatedCostUSD,
	}, nil
}

func (s *Store) querySummaryBreakdown(ctx context.Context, limitTime time.Time, providerFilter string) (AnalyticsSummaryBreakdown, error) {
	queryBreakdown := `
		SELECT 
			COALESCE(SUM(CAST(json_extract(payload, '$.system_tokens') AS INTEGER)), 0) as system_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.prompt_tokens') AS INTEGER)), 0) as prompt_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.completion_tokens') AS INTEGER)), 0) as completion_tokens
		FROM metrics_events
		WHERE event_type = 'llm_call' AND created_at >= ?`
	args := []interface{}{limitTime}
	if providerFilter != "" && providerFilter != "ALL" {
		queryBreakdown += " AND json_extract(payload, '$.provider') = ?"
		args = append(args, providerFilter)
	}

	var bd struct {
		SystemTokens     int `db:"system_tokens"`
		PromptTokens     int `db:"prompt_tokens"`
		CompletionTokens int `db:"completion_tokens"`
	}
	if err := s.db.GetContext(ctx, &bd, queryBreakdown, args...); err != nil {
		return AnalyticsSummaryBreakdown{}, err
	}

	chatTokens := bd.PromptTokens - bd.SystemTokens
	if chatTokens < 0 {
		chatTokens = 0
	}

	return AnalyticsSummaryBreakdown{
		SystemPromptTokens:   bd.SystemTokens,
		CompletionTokens:     bd.CompletionTokens,
		ContextAndChatTokens: chatTokens,
	}, nil
}

func (s *Store) queryDailyActivity(ctx context.Context, limitTime time.Time, providerFilter string) ([]DailyActivity, error) {
	queryDaily := `
		SELECT 
			date(created_at) as day,
			COUNT(*) as total_calls,
			COALESCE(SUM(CAST(json_extract(payload, '$.prompt_tokens') AS INTEGER)), 0) as prompt_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.completion_tokens') AS INTEGER)), 0) as completion_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.total_tokens') AS INTEGER)), 0) as total_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.estimated_cost_usd') AS REAL)), 0.0) as estimated_cost_usd
		FROM metrics_events
		WHERE event_type = 'llm_call' AND created_at >= ?`
	args := []interface{}{limitTime}
	if providerFilter != "" && providerFilter != "ALL" {
		queryDaily += " AND json_extract(payload, '$.provider') = ?"
		args = append(args, providerFilter)
	}
	queryDaily += " GROUP BY date(created_at) ORDER BY day ASC"

	var dbDaily []struct {
		Day              string  `db:"day"`
		TotalCalls       int     `db:"total_calls"`
		PromptTokens     int     `db:"prompt_tokens"`
		CompletionTokens int     `db:"completion_tokens"`
		TotalTokens      int     `db:"total_tokens"`
		EstimatedCostUSD float64 `db:"estimated_cost_usd"`
	}
	if err := s.db.SelectContext(ctx, &dbDaily, queryDaily, args...); err != nil {
		return []DailyActivity{}, err
	}

	var list []DailyActivity
	for _, d := range dbDaily {
		cleanDay := d.Day
		if t, err := time.Parse("2006-01-02", d.Day); err == nil {
			cleanDay = t.Format("01/02")
		}
		list = append(list, DailyActivity{
			Day:              cleanDay,
			TotalCalls:       d.TotalCalls,
			PromptTokens:     d.PromptTokens,
			CompletionTokens: d.CompletionTokens,
			TotalTokens:      d.TotalTokens,
			EstimatedCostUSD: d.EstimatedCostUSD,
		})
	}
	return list, nil
}

func (s *Store) queryByProject(ctx context.Context, limitTime time.Time, providerFilter string) ([]ByProjectStats, error) {
	queryProject := `
		SELECT 
			project_name,
			COUNT(*) as total_calls,
			COALESCE(SUM(CAST(json_extract(payload, '$.total_tokens') AS INTEGER)), 0) as total_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.estimated_cost_usd') AS REAL)), 0.0) as estimated_cost_usd
		FROM metrics_events
		WHERE event_type = 'llm_call' AND created_at >= ?`
	args := []interface{}{limitTime}
	if providerFilter != "" && providerFilter != "ALL" {
		queryProject += " AND json_extract(payload, '$.provider') = ?"
		args = append(args, providerFilter)
	}
	queryProject += " GROUP BY project_name ORDER BY total_tokens DESC"

	var dbProj []struct {
		ProjectName      string  `db:"project_name"`
		TotalCalls       int     `db:"total_calls"`
		TotalTokens      int     `db:"total_tokens"`
		EstimatedCostUSD float64 `db:"estimated_cost_usd"`
	}
	if err := s.db.SelectContext(ctx, &dbProj, queryProject, args...); err != nil {
		return []ByProjectStats{}, err
	}

	var list []ByProjectStats
	for _, p := range dbProj {
		name := p.ProjectName
		if name == "" {
			name = "default"
		}
		list = append(list, ByProjectStats{
			ProjectName:      name,
			TotalCalls:       p.TotalCalls,
			TotalTokens:      p.TotalTokens,
			EstimatedCostUSD: p.EstimatedCostUSD,
		})
	}
	return list, nil
}

func (s *Store) queryByModel(ctx context.Context, limitTime time.Time, providerFilter string) ([]ByModelStats, error) {
	queryModel := `
		SELECT 
			COALESCE(json_extract(payload, '$.model'), '') as model_name,
			COALESCE(json_extract(payload, '$.provider'), '') as provider,
			COUNT(*) as total_calls,
			COALESCE(SUM(CAST(json_extract(payload, '$.total_tokens') AS INTEGER)), 0) as total_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.estimated_cost_usd') AS REAL)), 0.0) as estimated_cost_usd
		FROM metrics_events
		WHERE event_type = 'llm_call' AND created_at >= ?`
	args := []interface{}{limitTime}
	if providerFilter != "" && providerFilter != "ALL" {
		queryModel += " AND json_extract(payload, '$.provider') = ?"
		args = append(args, providerFilter)
	}
	queryModel += " GROUP BY model_name, provider ORDER BY total_tokens DESC"

	var dbModel []struct {
		ModelName        string  `db:"model_name"`
		Provider         string  `db:"provider"`
		TotalCalls       int     `db:"total_calls"`
		TotalTokens      int     `db:"total_tokens"`
		EstimatedCostUSD float64 `db:"estimated_cost_usd"`
	}
	if err := s.db.SelectContext(ctx, &dbModel, queryModel, args...); err != nil {
		return []ByModelStats{}, err
	}

	var list []ByModelStats
	for _, m := range dbModel {
		list = append(list, ByModelStats{
			ModelName:        m.ModelName,
			Provider:         m.Provider,
			TotalCalls:       m.TotalCalls,
			TotalTokens:      m.TotalTokens,
			EstimatedCostUSD: m.EstimatedCostUSD,
		})
	}
	return list, nil
}

func (s *Store) queryByAgent(ctx context.Context, limitTime time.Time, providerFilter string) ([]ByAgentStats, error) {
	queryAgent := `
		SELECT 
			agent_name,
			COUNT(*) as total_calls,
			COALESCE(SUM(CAST(json_extract(payload, '$.total_tokens') AS INTEGER)), 0) as total_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.estimated_cost_usd') AS REAL)), 0.0) as estimated_cost_usd
		FROM metrics_events
		WHERE event_type = 'llm_call' AND created_at >= ?`
	args := []interface{}{limitTime}
	if providerFilter != "" && providerFilter != "ALL" {
		queryAgent += " AND json_extract(payload, '$.provider') = ?"
		args = append(args, providerFilter)
	}
	queryAgent += " GROUP BY agent_name ORDER BY total_tokens DESC"

	var dbAgent []struct {
		AgentName        string  `db:"agent_name"`
		TotalCalls       int     `db:"total_calls"`
		TotalTokens      int     `db:"total_tokens"`
		EstimatedCostUSD float64 `db:"estimated_cost_usd"`
	}
	if err := s.db.SelectContext(ctx, &dbAgent, queryAgent, args...); err != nil {
		return []ByAgentStats{}, err
	}

	var list []ByAgentStats
	for _, a := range dbAgent {
		name := a.AgentName
		if name == "" {
			name = "default"
		}
		list = append(list, ByAgentStats{
			AgentName:        name,
			TotalCalls:       a.TotalCalls,
			TotalTokens:      a.TotalTokens,
			EstimatedCostUSD: a.EstimatedCostUSD,
		})
	}
	return list, nil
}

func (s *Store) queryTools(ctx context.Context, limitTime time.Time) ([]ToolAnalytics, error) {
	queryTools := `
		SELECT 
			COALESCE(json_extract(payload, '$.tool_name'), '') as tool_name,
			COUNT(*) as total_calls,
			CAST(AVG(CAST(json_extract(payload, '$.execution_time_ms') AS REAL)) AS INTEGER) as avg_latency_ms,
			COALESCE(SUM(CASE WHEN json_extract(payload, '$.status') = 'success' THEN 1 ELSE 0 END) * 1.0 / COUNT(*), 0.0) as success_rate,
			COALESCE(SUM(CAST(json_extract(payload, '$.output_tokens') AS INTEGER)), 0) as total_tokens,
			COALESCE(SUM(CAST(json_extract(payload, '$.estimated_cost_usd') AS REAL)), 0.0) as estimated_cost_usd
		FROM metrics_events
		WHERE event_type = 'tool_call' AND created_at >= ?
		GROUP BY tool_name
		ORDER BY total_calls DESC`

	var dbTools []struct {
		ToolName         string  `db:"tool_name"`
		TotalCalls       int     `db:"total_calls"`
		AvgLatencyMs     int64   `db:"avg_latency_ms"`
		SuccessRate      float64 `db:"success_rate"`
		TotalTokens      int     `db:"total_tokens"`
		EstimatedCostUSD float64 `db:"estimated_cost_usd"`
	}
	if err := s.db.SelectContext(ctx, &dbTools, queryTools, limitTime); err != nil {
		return []ToolAnalytics{}, err
	}

	var list []ToolAnalytics
	for _, t := range dbTools {
		list = append(list, ToolAnalytics{
			ToolName:         t.ToolName,
			TotalCalls:       t.TotalCalls,
			AvgLatencyMs:     t.AvgLatencyMs,
			SuccessRate:      t.SuccessRate,
			TotalTokens:      t.TotalTokens,
			EstimatedCostUSD: t.EstimatedCostUSD,
		})
	}
	return list, nil
}

// LogLLMCall logs an LLM API token consumption event to the metrics database.
func (s *Store) LogLLMCall(event coredb.MetricsEvent, payload coredb.LLMCallPayload) error {
	return coredb.LogLLMCall(s.db, event, payload)
}

// LogToolCall logs a tool execution event to the metrics database.
func (s *Store) LogToolCall(event coredb.MetricsEvent, payload coredb.ToolCallPayload) error {
	return coredb.LogToolCall(s.db, event, payload)
}
