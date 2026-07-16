package session

import (
	"context"
	"time"

	loomsqlite "github.com/masterkeysrd/loom/checkpoint/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/tasksmith/internal/session/resource"
)

// SessionData represents the raw persistent session record.
type SessionData struct {
	ID              string    `db:"id"`
	Title           string    `db:"title"`
	Todos           *string   `db:"todos"`
	LastTurnMetrics *string   `db:"last_turn_metrics"`
	Settings        *string   `db:"settings"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// SessionMetrics defines the structured tokens and cost metrics for a session turn.
type SessionMetrics struct {
	SystemTokens               int     `json:"system_tokens"`
	ToolsTokens                int     `json:"tools_tokens"`
	ToolResultTokens           int     `json:"tool_result_tokens"`
	WorkspaceFileTokens        int     `json:"workspace_file_tokens"`
	ChatTokens                 int     `json:"chat_tokens"`
	PromptTokens               int     `json:"prompt_tokens"`
	CompletionTokens           int     `json:"completion_tokens"`
	TotalTokens                int     `json:"total_tokens"`
	EstimatedCostUSD           float64 `json:"estimated_cost_usd"`
	CumulativePromptTokens     int     `json:"cumulative_prompt_tokens"`
	CumulativeCompletionTokens int     `json:"cumulative_completion_tokens"`
	CumulativeTotalTokens      int     `json:"cumulative_total_tokens"`
	CumulativeCostUSD          float64 `json:"cumulative_cost_usd"`
}

// MessageData represents the raw persistent message record.
type MessageData struct {
	ID        string    `db:"id"`
	SessionID string    `db:"session_id"`
	Role      string    `db:"role"`
	Content   string    `db:"content"`
	CreatedAt time.Time `db:"created_at"`
}

// SessionConfig represents the LLM configuration details of a session.
type SessionConfig struct {
	Settings model.SessionSettings `db:"settings"`
}

// ListSessionsQuery contains query parameters for listing sessions.
type ListSessionsQuery struct {
	Limit  int
	Offset int
}

// Store defines the storage repository contract.
// It remains completely decoupled from Loom-specific logic, UUID formatting, and validation.
type Store interface {
	CreateSession(ctx context.Context, s SessionData) error
	GetSession(ctx context.Context, id string) (*SessionData, error)
	ListSessions(ctx context.Context, query ListSessionsQuery) ([]SessionData, error)
	RenameSession(ctx context.Context, id, title string) error
	UpdateSessionConfig(ctx context.Context, id string, cfg SessionConfig) error
	UpdateSessionTodos(ctx context.Context, id string, todosJSON string) error
	UpdateSessionMetrics(ctx context.Context, id string, metrics SessionMetrics) error
	ArchiveSession(ctx context.Context, id string) error
	DeleteSession(ctx context.Context, id string) error

	AppendMessage(ctx context.Context, msg MessageData, updatedAt time.Time) error
	GetMessages(ctx context.Context, sessionID string) ([]MessageData, error)
	GetUserMessages(ctx context.Context, query string, limit int) ([]MessageData, error)
	DeleteLastMessage(ctx context.Context, sessionID string) error

	NewCheckpointer() (*loomsqlite.Checkpointer, error)
	ResourceStore() *resource.Store
}
