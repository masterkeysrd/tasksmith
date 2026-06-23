package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// MetricsEvent represents a single chronological event in the unified metrics stream.
type MetricsEvent struct {
	ID            string    `db:"id"`
	SessionID     string    `db:"session_id"`
	WorkspacePath string    `db:"workspace_path"`
	ProjectName   string    `db:"project_name"`
	AgentName     string    `db:"agent_name"`
	AppVersion    *string   `db:"app_version"` // nullable
	NodeName      *string   `db:"node_name"`   // nullable
	EventType     string    `db:"event_type"`
	Payload       string    `db:"payload"`
	CreatedAt     time.Time `db:"created_at"`
}

// LLMCallPayload represents the specific JSON metrics for an LLM API call.
type LLMCallPayload struct {
	Provider            string  `json:"provider"`
	Model               string  `json:"model"`
	SystemTokens        int     `json:"system_tokens,omitempty"`
	ToolsTokens         int     `json:"tools_tokens,omitempty"`
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int     `json:"cache_read_tokens,omitempty"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
}

// ToolCallPayload represents the specific JSON metrics for a tool execution.
type ToolCallPayload struct {
	ToolName         string  `json:"tool_name"`
	ExecutionTimeMs  int64   `json:"execution_time_ms"`
	Status           string  `json:"status"` // "success" or "error"
	ErrorMessage     *string `json:"error_message,omitempty"`
	OutputBytes      int     `json:"output_bytes"`
	OutputTokens     int     `json:"output_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`

	// Flexible metadata fields for tool-specific tracking
	LinesAdded    *int `json:"lines_added,omitempty"`
	LinesRemoved  *int `json:"lines_removed,omitempty"`
	FilesModified *int `json:"files_modified,omitempty"`
	ResultsCount  *int `json:"results_count,omitempty"`
	ExitCode      *int `json:"exit_code,omitempty"`
}

// InitMetricsDB opens the global metrics database and runs its schema migrations.
func InitMetricsDB() (*sqlx.DB, error) {
	metricsDB, err := OpenGlobal("metrics.db")
	if err != nil {
		return nil, fmt.Errorf("failed to open global metrics db: %w", err)
	}

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS metrics_events (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			workspace_path TEXT NOT NULL,
			project_name TEXT NOT NULL,
			agent_name TEXT NOT NULL,
			app_version TEXT,
			node_name TEXT,
			event_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_events_workspace ON metrics_events(workspace_path, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_events_session ON metrics_events(session_id, id);`,
		`CREATE INDEX IF NOT EXISTS idx_events_node ON metrics_events(node_name);`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON metrics_events(event_type);`,
	}

	if err := Migrate(metricsDB, "metrics", migrations); err != nil {
		metricsDB.Close()
		return nil, fmt.Errorf("failed to migrate metrics db: %w", err)
	}

	return metricsDB, nil
}

// LogLLMCall logs an LLM API token consumption event to the metrics database.
func LogLLMCall(db *sqlx.DB, event MetricsEvent, payload LLMCallPayload) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal llm payload: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate uuid v7: %w", err)
	}

	event.ID = id.String()
	event.EventType = "llm_call"
	event.Payload = string(payloadBytes)

	return insertEvent(db, event)
}

// LogToolCall logs a tool execution event to the metrics database.
func LogToolCall(db *sqlx.DB, event MetricsEvent, payload ToolCallPayload) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal tool payload: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate uuid v7: %w", err)
	}

	event.ID = id.String()
	event.EventType = "tool_call"
	event.Payload = string(payloadBytes)

	return insertEvent(db, event)
}

func insertEvent(db *sqlx.DB, event MetricsEvent) error {
	query := `INSERT INTO metrics_events (
		id, session_id, workspace_path, project_name, agent_name, app_version, node_name, event_type, payload
	) VALUES (
		:id, :session_id, :workspace_path, :project_name, :agent_name, :app_version, :node_name, :event_type, :payload
	)`

	_, err := db.NamedExec(query, event)
	if err != nil {
		return fmt.Errorf("failed to insert metrics event: %w", err)
	}

	return nil
}
