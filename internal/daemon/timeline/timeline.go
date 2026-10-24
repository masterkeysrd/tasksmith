package timeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
)

const timestampFormat = "2006-01-02T15:04:05.000000000Z"

// Timeline provides append-only persistence and querying for a single session timeline.
type Timeline interface {
	Append(ctx context.Context, entry *Entry) error
	Get(ctx context.Context, id string) (*Entry, error)
	List(ctx context.Context) ([]Entry, error)
	ListByKind(ctx context.Context, kind Kind) ([]Entry, error)
	Messages(ctx context.Context) (message.MessageList, error)
	Delete(ctx context.Context) error
}

type sqliteTimeline struct {
	db        *sql.DB
	sessionID string
}

var _ Timeline = (*sqliteTimeline)(nil)

// New creates and initializes a session-scoped Timeline, running migrations on the provided db.
func New(ctx context.Context, db *sql.DB, sessionID string) (Timeline, error) {
	if db == nil {
		return nil, ErrInvalidConfig
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrEmptySessionID
	}
	if err := sqlite.Migrate(ctx, db, "timeline", migrations); err != nil {
		return nil, fmt.Errorf("timeline: migrate schema: %w", err)
	}
	return &sqliteTimeline{db: db, sessionID: sessionID}, nil
}

// Append validates and records a new timeline entry for this session.
func (s *sqliteTimeline) Append(ctx context.Context, entry *Entry) error {
	if entry == nil {
		return ErrInvalidEntry
	}
	if strings.TrimSpace(entry.SessionID) == "" {
		entry.SessionID = s.sessionID
	} else if entry.SessionID != s.sessionID {
		return ErrInvalidEntry
	}

	switch entry.Kind {
	case KindMessage:
		if entry.Message == nil {
			return ErrInvalidEntry
		}
	case KindModel:
		if entry.Model == nil || strings.TrimSpace(entry.Model.To) == "" {
			return ErrInvalidEntry
		}
	case KindEffort:
		if entry.Effort == nil || strings.TrimSpace(entry.Effort.To) == "" {
			return ErrInvalidEntry
		}
	case KindAgent:
		if entry.Agent == nil || strings.TrimSpace(entry.Agent.To) == "" {
			return ErrInvalidEntry
		}
	case KindMode:
		if entry.Mode == nil || strings.TrimSpace(string(entry.Mode.To)) == "" {
			return ErrInvalidEntry
		}
	case KindPermission:
		if entry.Permission == nil {
			return ErrInvalidEntry
		}
	case KindInterrupted:
		// no additional struct field validation needed
	default:
		return ErrInvalidEntry
	}

	if entry.ID == "" {
		if entry.Kind == KindMessage && entry.Message.GetID() != "" {
			entry.ID = entry.Message.GetID()
		} else {
			u, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("timeline: generate id: %w", err)
			}
			entry.ID = u.String()
		}
	}
	if entry.Kind == KindMessage && entry.Message.GetID() == "" {
		entry.Message.SetID(entry.ID)
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	} else {
		entry.CreatedAt = entry.CreatedAt.UTC()
	}

	var payload []byte
	var err error
	switch entry.Kind {
	case KindMessage:
		payload, err = json.Marshal(message.MessageList{entry.Message})
		if err != nil {
			return fmt.Errorf("timeline: marshal message %q: %w", entry.ID, err)
		}
	case KindModel:
		payload, err = json.Marshal(entry.Model)
		if err != nil {
			return fmt.Errorf("timeline: marshal model %q: %w", entry.ID, err)
		}
	case KindEffort:
		payload, err = json.Marshal(entry.Effort)
		if err != nil {
			return fmt.Errorf("timeline: marshal effort %q: %w", entry.ID, err)
		}
	case KindAgent:
		payload, err = json.Marshal(entry.Agent)
		if err != nil {
			return fmt.Errorf("timeline: marshal agent %q: %w", entry.ID, err)
		}
	case KindMode:
		payload, err = json.Marshal(entry.Mode)
		if err != nil {
			return fmt.Errorf("timeline: marshal mode %q: %w", entry.ID, err)
		}
	case KindPermission:
		payload, err = json.Marshal(entry.Permission)
		if err != nil {
			return fmt.Errorf("timeline: marshal permission %q: %w", entry.ID, err)
		}
	case KindInterrupted:
		payload = []byte("{}")
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO timeline (id, session_id, kind, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		entry.ID,
		entry.SessionID,
		string(entry.Kind),
		payload,
		formatTime(entry.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("timeline: insert entry %q: %w", entry.ID, err)
	}
	return nil
}

// Get retrieves an entry by its ID within this session.
func (s *sqliteTimeline) Get(ctx context.Context, id string) (*Entry, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrEntryNotFound
	}
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, kind, payload, created_at FROM timeline WHERE id = ? AND session_id = ?`,
		id,
		s.sessionID,
	)
	entry, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("timeline: get entry %q: %w", id, err)
	}
	return entry, nil
}

// List returns all timeline entries for this session ordered by id ASC.
func (s *sqliteTimeline) List(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, kind, payload, created_at FROM timeline WHERE session_id = ? ORDER BY id ASC`,
		s.sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("timeline: list entries for session %q: %w", s.sessionID, err)
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("timeline: scan entry row: %w", err)
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("timeline: iterate entries for session %q: %w", s.sessionID, err)
	}
	return entries, nil
}

// ListByKind returns entries for this session and kind ordered by id ASC.
func (s *sqliteTimeline) ListByKind(ctx context.Context, kind Kind) ([]Entry, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, kind, payload, created_at FROM timeline WHERE session_id = ? AND kind = ? ORDER BY id ASC`,
		s.sessionID,
		string(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("timeline: list entries for session %q by kind %q: %w", s.sessionID, kind, err)
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("timeline: scan entry row: %w", err)
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("timeline: iterate entries for session %q by kind %q: %w", s.sessionID, kind, err)
	}
	return entries, nil
}

// Messages returns all KindMessage payloads for this session ordered by id ASC.
func (s *sqliteTimeline) Messages(ctx context.Context) (message.MessageList, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, payload FROM timeline WHERE session_id = ? AND kind = ? ORDER BY id ASC`,
		s.sessionID,
		string(KindMessage),
	)
	if err != nil {
		return nil, fmt.Errorf("timeline: query messages for session %q: %w", s.sessionID, err)
	}
	defer rows.Close()

	list := make(message.MessageList, 0)
	for rows.Next() {
		var id string
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, fmt.Errorf("timeline: scan message row: %w", err)
		}
		msg, err := unmarshalMessagePayload(id, payload)
		if err != nil {
			return nil, err
		}
		list = append(list, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("timeline: iterate messages for session %q: %w", s.sessionID, err)
	}
	return list, nil
}

// Delete removes all timeline entries associated with this session.
func (s *sqliteTimeline) Delete(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM timeline WHERE session_id = ?`, s.sessionID); err != nil {
		return fmt.Errorf("timeline: delete session %q: %w", s.sessionID, err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(r rowScanner) (*Entry, error) {
	var (
		id           string
		sessionID    string
		kindRaw      string
		payload      []byte
		createdAtRaw string
	)
	if err := r.Scan(&id, &sessionID, &kindRaw, &payload, &createdAtRaw); err != nil {
		return nil, err
	}

	createdAt, err := parseTime(createdAtRaw)
	if err != nil {
		return nil, fmt.Errorf("timeline: parse created_at %q: %w", createdAtRaw, err)
	}

	kind := Kind(kindRaw)
	entry := &Entry{
		ID:        id,
		SessionID: sessionID,
		Kind:      kind,
		CreatedAt: createdAt,
	}

	switch kind {
	case KindMessage:
		msg, err := unmarshalMessagePayload(id, payload)
		if err != nil {
			return nil, err
		}
		entry.Message = msg
	case KindModel:
		var m ModelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, fmt.Errorf("timeline: unmarshal model change for %q: %w", id, err)
		}
		entry.Model = &m
	case KindEffort:
		var e EffortChange
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("timeline: unmarshal effort change for %q: %w", id, err)
		}
		entry.Effort = &e
	case KindAgent:
		var a AgentChange
		if err := json.Unmarshal(payload, &a); err != nil {
			return nil, fmt.Errorf("timeline: unmarshal agent change for %q: %w", id, err)
		}
		entry.Agent = &a
	case KindMode:
		var m ModeChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, fmt.Errorf("timeline: unmarshal mode change for %q: %w", id, err)
		}
		entry.Mode = &m
	case KindPermission:
		var p PermissionRecord
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("timeline: unmarshal permission record for %q: %w", id, err)
		}
		entry.Permission = &p
	case KindInterrupted:
		// No extra payload unmarshaling needed
	default:
		return nil, fmt.Errorf("timeline: unknown entry kind %q for %q", kindRaw, id)
	}

	return entry, nil
}

func unmarshalMessagePayload(id string, payload []byte) (message.Message, error) {
	var list message.MessageList
	if err := json.Unmarshal(payload, &list); err != nil {
		return nil, fmt.Errorf("timeline: unmarshal message %q: %w", id, err)
	}
	if len(list) != 1 || list[0] == nil {
		return nil, fmt.Errorf("timeline: invalid message payload for %q (decoded %d messages)", id, len(list))
	}
	return list[0], nil
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
