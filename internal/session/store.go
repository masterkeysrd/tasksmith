package session

import (
	"context"
	"time"

	loomsqlite "github.com/masterkeysrd/loom/checkpoint/sqlite"
)

// SessionData represents the raw persistent session record.
type SessionData struct {
	ID           string    `db:"id"`
	Title        string    `db:"title"`
	AgentName    *string   `db:"agent_name"`
	ProviderName *string   `db:"provider_name"`
	ModelName    *string   `db:"model_name"`
	Todos        *string   `db:"todos"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
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
	AgentName    string `db:"agent_name"`
	ProviderName string `db:"provider_name"`
	ModelName    string `db:"model_name"`
}

// Store defines the storage repository contract.
// It remains completely decoupled from Loom-specific logic, UUID formatting, and validation.
type Store interface {
	CreateSession(ctx context.Context, s SessionData) error
	GetSession(ctx context.Context, id string) (*SessionData, error)
	ListSessions(ctx context.Context) ([]SessionData, error)
	RenameSession(ctx context.Context, id, title string) error
	UpdateSessionConfig(ctx context.Context, id string, cfg SessionConfig) error
	UpdateSessionTodos(ctx context.Context, id string, todosJSON string) error
	ArchiveSession(ctx context.Context, id string) error
	DeleteSession(ctx context.Context, id string) error

	AppendMessage(ctx context.Context, msg MessageData, updatedAt time.Time) error
	GetMessages(ctx context.Context, sessionID string) ([]MessageData, error)

	NewCheckpointer() (*loomsqlite.Checkpointer, error)
}
