package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
)

// timestampFormat is a fixed-width UTC RFC3339Nano layout so SQLite TEXT
// ordering (ORDER BY updated_at DESC, created_at DESC) matches chronological order.
const timestampFormat = "2006-01-02T15:04:05.000000000Z"

type store struct {
	db *sql.DB
}

func newStore(ctx context.Context, db *sql.DB) (*store, error) {
	if err := sqlite.Migrate(ctx, db, "session", migrations); err != nil {
		return nil, fmt.Errorf("session: migrate schema: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) insertSession(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO sessions (id, title, agent, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID,
		sess.Title,
		sess.Agent,
		sess.Model,
		formatTime(sess.CreatedAt),
		formatTime(sess.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("session: insert session %q: %w", sess.ID, err)
	}
	return nil
}

func (s *store) updateSession(ctx context.Context, sess *Session) error {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE sessions SET title = ?, agent = ?, model = ?, updated_at = ? WHERE id = ?`,
		sess.Title,
		sess.Agent,
		sess.Model,
		formatTime(sess.UpdatedAt),
		sess.ID,
	)
	if err != nil {
		return fmt.Errorf("session: update session %q: %w", sess.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: check rows affected for session %q: %w", sess.ID, err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *store) touchSession(ctx context.Context, sessionID string, updatedAt time.Time) error {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		formatTime(updatedAt),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("session: touch session %q: %w", sessionID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: check rows affected for session %q: %w", sessionID, err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *store) getSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, title, agent, model, created_at, updated_at FROM sessions WHERE id = ?`,
		id,
	)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("session: get session %q: %w", id, err)
	}
	return sess, nil
}

func (s *store) listSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, title, agent, model, created_at, updated_at FROM sessions ORDER BY updated_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("session: list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("session: scan session row: %w", err)
		}
		sessions = append(sessions, *sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate sessions: %w", err)
	}
	return sessions, nil
}

func (s *store) deleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("session: delete session %q: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: check rows affected for session %q: %w", id, err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(r rowScanner) (*Session, error) {
	var (
		sess         Session
		createdAtRaw string
		updatedAtRaw string
	)
	if err := r.Scan(&sess.ID, &sess.Title, &sess.Agent, &sess.Model, &createdAtRaw, &updatedAtRaw); err != nil {
		return nil, err
	}

	createdAt, err := parseTime(createdAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", createdAtRaw, err)
	}
	updatedAt, err := parseTime(updatedAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at %q: %w", updatedAtRaw, err)
	}

	sess.CreatedAt = createdAt
	sess.UpdatedAt = updatedAt
	return &sess, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timestampFormat)
}

func parseTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
