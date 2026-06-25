package resource

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Resource is a single row in the session_resources table.
type Resource struct {
	ID        string    `db:"id"`
	SessionID string    `db:"session_id"`
	Kind      string    `db:"kind"`
	Key       string    `db:"key"`
	Data      string    `db:"data"`
	CreatedAt time.Time `db:"created_at"`
}

// Store provides generic CRUD for session resources.
type Store struct {
	db *sqlx.DB
}

// NewStore creates a new Store for session resources.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Insert inserts a session resource. If resource ID is empty, a UUID will be generated.
func (s *Store) Insert(ctx context.Context, r Resource) (Resource, error) {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	query := `INSERT INTO session_resources (id, session_id, kind, key, data, created_at)
              VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, r.ID, r.SessionID, r.Kind, r.Key, r.Data, r.CreatedAt)
	if err != nil {
		return Resource{}, err
	}
	return r, nil
}

// Query returns all resources for a specific session and kind.
func (s *Store) Query(ctx context.Context, sessionID, kind string) ([]Resource, error) {
	var resources []Resource
	query := `SELECT id, session_id, kind, key, data, created_at 
              FROM session_resources 
              WHERE session_id = ? AND kind = ? 
              ORDER BY created_at ASC`
	err := s.db.SelectContext(ctx, &resources, query, sessionID, kind)
	return resources, err
}

// QueryByKey returns all resources for a specific session, kind, and key.
func (s *Store) QueryByKey(ctx context.Context, sessionID, kind, key string) ([]Resource, error) {
	var resources []Resource
	query := `SELECT id, session_id, kind, key, data, created_at 
              FROM session_resources 
              WHERE session_id = ? AND kind = ? AND key = ? 
              ORDER BY created_at ASC`
	err := s.db.SelectContext(ctx, &resources, query, sessionID, kind, key)
	return resources, err
}

// DeleteBySession deletes all resources of any kind for a given session.
func (s *Store) DeleteBySession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM session_resources WHERE session_id = ?`
	_, err := s.db.ExecContext(ctx, query, sessionID)
	return err
}

// DeleteByKey deletes a specific resource by session, kind, and key.
func (s *Store) DeleteByKey(ctx context.Context, sessionID, kind, key string) error {
	query := `DELETE FROM session_resources WHERE session_id = ? AND kind = ? AND key = ?`
	_, err := s.db.ExecContext(ctx, query, sessionID, kind, key)
	return err
}
