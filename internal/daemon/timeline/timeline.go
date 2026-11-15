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
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
)

const timestampFormat = "2006-01-02T15:04:05.000000000Z"

// Timeline provides append-only persistence and querying for a single session timeline.
type Timeline interface {
	Append(ctx context.Context, entry *Entry) error
	Get(ctx context.Context, id string) (*Entry, error)
	List(ctx context.Context) ([]Entry, error)
	Messages(ctx context.Context) (message.MessageList, error)
	Delete(ctx context.Context) error
}

type sqliteTimeline struct {
	db        *sql.DB
	sessionID string
}

var _ Timeline = (*sqliteTimeline)(nil)

// Migrate executes SQLite migrations for timeline schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return ErrInvalidConfig
	}
	if err := sqlite.Migrate(ctx, db, "timeline", migrations); err != nil {
		return fmt.Errorf("timeline: migrate schema: %w", err)
	}
	return nil
}

// New creates and initializes a session-scoped Timeline, running migrations on the provided db.
func New(ctx context.Context, db *sql.DB, sessionID string) (Timeline, error) {
	if db == nil {
		return nil, ErrInvalidConfig
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrEmptySessionID
	}
	if err := Migrate(ctx, db); err != nil {
		return nil, err
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

	if entry.ID == "" {
		if entry.Kind == api.TimelineKindMessage && entry.Message != nil && entry.Message.GetID() != "" {
			entry.ID = entry.Message.GetID()
		} else {
			u, err := uuid.NewV7()
			if err != nil {
				return fmt.Errorf("timeline: generate id: %w", err)
			}
			entry.ID = u.String()
		}
	}
	if entry.Kind == api.TimelineKindMessage && entry.Message != nil && entry.Message.GetID() == "" {
		entry.Message.SetID(entry.ID)
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	} else {
		entry.CreatedAt = entry.CreatedAt.UTC()
	}

	payload, err := marshalPayload(entry)
	if err != nil {
		return err
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

func marshalPayload(entry *Entry) ([]byte, error) {
	var src any
	switch entry.Kind {
	case api.TimelineKindMessage:
		if entry.Message == nil {
			return nil, ErrInvalidEntry
		}
		src = message.MessageList{entry.Message}
	case api.TimelineKindCommand:
		if entry.Command == nil || strings.TrimSpace(entry.Command.Command) == "" {
			return nil, ErrInvalidEntry
		}
		src = entry.Command
	case api.TimelineKindModel:
		if entry.Model == nil || strings.TrimSpace(entry.Model.To) == "" {
			return nil, ErrInvalidEntry
		}
		src = entry.Model
	case api.TimelineKindEffort:
		if entry.Effort == nil || strings.TrimSpace(entry.Effort.To) == "" {
			return nil, ErrInvalidEntry
		}
		src = entry.Effort
	case api.TimelineKindThinking:
		if entry.Thinking == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.Thinking
	case api.TimelineKindAgent:
		if entry.Agent == nil || strings.TrimSpace(entry.Agent.To) == "" {
			return nil, ErrInvalidEntry
		}
		src = entry.Agent
	case api.TimelineKindMode:
		if entry.Mode == nil || strings.TrimSpace(string(entry.Mode.To)) == "" {
			return nil, ErrInvalidEntry
		}
		src = entry.Mode
	case api.TimelineKindPermission:
		if entry.Permission == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.Permission
	case api.TimelineKindQuestion:
		if entry.Question == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.Question
	case api.TimelineKindCompacted:
		if entry.Compacted == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.Compacted
	case api.TimelineKindCacheBroken:
		if entry.CacheBroken == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.CacheBroken
	case api.TimelineKindTurnCompleted:
		if entry.TurnCompleted == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.TurnCompleted
	case api.TimelineKindRule:
		if entry.Rule == nil {
			return nil, ErrInvalidEntry
		}
		src = entry.Rule
	case api.TimelineKindInterrupted:
		return []byte("{}"), nil
	default:
		return nil, ErrInvalidEntry
	}
	data, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("timeline: marshal %s %q: %w", entry.Kind, entry.ID, err)
	}
	return data, nil
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

// Messages returns all KindMessage payloads for this session ordered by id ASC.
func (s *sqliteTimeline) Messages(ctx context.Context) (message.MessageList, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, payload FROM timeline WHERE session_id = ? AND kind = ? ORDER BY id ASC`,
		s.sessionID,
		string(api.TimelineKindMessage),
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

	kind := api.TimelineKind(kindRaw)
	entry := &Entry{
		TimelineEntry: api.TimelineEntry{
			ID:        id,
			SessionID: sessionID,
			Kind:      kind,
			CreatedAt: createdAt,
		},
	}

	var dst any
	switch kind {
	case api.TimelineKindMessage:
		msg, err := unmarshalMessagePayload(id, payload)
		if err != nil {
			return nil, err
		}
		entry.Message = msg
		return entry, nil
	case api.TimelineKindCommand:
		entry.Command = new(api.CommandRecord)
		dst = entry.Command
	case api.TimelineKindModel:
		entry.Model = new(api.ModelChange)
		dst = entry.Model
	case api.TimelineKindEffort:
		entry.Effort = new(api.EffortChange)
		dst = entry.Effort
	case api.TimelineKindThinking:
		entry.Thinking = new(api.ThinkingChange)
		dst = entry.Thinking
	case api.TimelineKindAgent:
		entry.Agent = new(api.AgentChange)
		dst = entry.Agent
	case api.TimelineKindMode:
		entry.Mode = new(api.ModeChange)
		dst = entry.Mode
	case api.TimelineKindPermission:
		entry.Permission = new(api.PermissionRecord)
		dst = entry.Permission
	case api.TimelineKindQuestion:
		entry.Question = new(api.QuestionRecord)
		dst = entry.Question
	case api.TimelineKindCompacted:
		entry.Compacted = new(api.CompactedRecord)
		dst = entry.Compacted
	case api.TimelineKindCacheBroken:
		entry.CacheBroken = new(api.CacheBrokenRecord)
		dst = entry.CacheBroken
	case api.TimelineKindTurnCompleted:
		entry.TurnCompleted = new(api.TurnCompletedRecord)
		dst = entry.TurnCompleted
	case api.TimelineKindRule:
		entry.Rule = new(api.RuleRecord)
		dst = entry.Rule
	case api.TimelineKindInterrupted:
		return entry, nil
	default:
		return nil, fmt.Errorf("timeline: unknown entry kind %q for %q", kindRaw, id)
	}
	if err := json.Unmarshal(payload, dst); err != nil {
		return nil, fmt.Errorf("timeline: unmarshal %s for %q: %w", kind, id, err)
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
