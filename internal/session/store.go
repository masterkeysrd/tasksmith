package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/masterkeysrd/loom/message"
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

func (s *store) insertMessage(ctx context.Context, sessionID string, msg message.Message, now time.Time) error {
	payload, err := marshalMessage(msg)
	if err != nil {
		return err
	}

	ts := formatTime(now)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session: begin insert message transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO messages (id, session_id, role, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		msg.GetID(),
		sessionID,
		string(msg.Role()),
		payload,
		ts,
	)
	if err != nil {
		return fmt.Errorf("session: insert message %q for session %q: %w", msg.GetID(), sessionID, err)
	}

	res, err := tx.ExecContext(
		ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		ts,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("session: update session %q timestamp: %w", sessionID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: check updated rows for session %q: %w", sessionID, err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: commit insert message %q: %w", msg.GetID(), err)
	}
	return nil
}

func (s *store) listMessages(ctx context.Context, sessionID string) (message.MessageList, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, payload FROM messages WHERE session_id = ? ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("session: query messages for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	list := make(message.MessageList, 0)
	for rows.Next() {
		var id string
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, fmt.Errorf("session: scan message row: %w", err)
		}
		msg, err := unmarshalMessage(id, payload)
		if err != nil {
			return nil, err
		}
		list = append(list, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate messages for session %q: %w", sessionID, err)
	}
	return list, nil
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

func marshalMessage(msg message.Message) ([]byte, error) {
	data, err := json.Marshal(message.MessageList{msg})
	if err != nil {
		return nil, fmt.Errorf("session: marshal message %q: %w", msg.GetID(), err)
	}
	return data, nil
}

func unmarshalMessage(id string, payload []byte) (message.Message, error) {
	var list message.MessageList
	if err := json.Unmarshal(payload, &list); err != nil {
		return nil, fmt.Errorf("session: unmarshal message %q: %w", id, err)
	}
	if len(list) != 1 || list[0] == nil {
		return nil, fmt.Errorf("session: invalid message payload for %q (decoded %d messages)", id, len(list))
	}
	return list[0], nil
}
