package metrics

// TokenAnalyticsStats aggregates top-level statistics.
type TokenAnalyticsStats struct {
	TotalCalls       int     `json:"total_calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	TotalSessions    int     `json:"total_sessions"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// DailyActivity represents token metrics grouped by day.
type DailyActivity struct {
	Day              string  `json:"day"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCalls       int     `json:"total_calls"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// AnalyticsSummaryBreakdown groups tokens into functional categories.
type AnalyticsSummaryBreakdown struct {
	SystemPromptTokens   int `json:"system_prompt_tokens"`
	CompletionTokens     int `json:"completion_tokens"`
	ContextAndChatTokens int `json:"context_and_chat_tokens"`
}

// ByProjectStats represents token metrics for a specific workspace project.
type ByProjectStats struct {
	ProjectName      string  `json:"project_name"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCalls       int     `json:"total_calls"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// ByModelStats represents token metrics for a specific model.
type ByModelStats struct {
	ModelName        string  `json:"model_name"`
	Provider         string  `json:"provider"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCalls       int     `json:"total_calls"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// ByAgentStats represents token metrics for a specific agent.
type ByAgentStats struct {
	AgentName        string  `json:"agent_name"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCalls       int     `json:"total_calls"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// ToolAnalytics aggregates telemetry stats for a single tool.
type ToolAnalytics struct {
	ToolName         string  `json:"tool_name"`
	TotalCalls       int     `json:"total_calls"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	SuccessRate      float64 `json:"success_rate"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// TokenAnalyticsResult aggregates all metrics queried over a timeframe.
type TokenAnalyticsResult struct {
	GlobalStats      TokenAnalyticsStats
	DailyActivity    []DailyActivity
	SummaryBreakdown AnalyticsSummaryBreakdown
	ByProject        []ByProjectStats
	ByModel          []ByModelStats
	ByAgent          []ByAgentStats
	Tools            []ToolAnalytics
	ProvidersList    []string
}
