package journal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

type fileState struct {
	hash    string
	changed bool
}

type sqliteJournal struct {
	mu        sync.RWMutex
	db        *sql.DB
	sessionID string
	blobsDir  string
	files     map[string]fileState
	recent    []string
	metrics   metrics.Recorder
}

// Option configures a journal created by [New].
type Option func(*sqliteJournal)

// WithMetrics makes the journal record a file_change metric for every
// recorded change. A nil recorder disables them.
func WithMetrics(r metrics.Recorder) Option {
	return func(j *sqliteJournal) { j.metrics = r }
}

var _ Journal = (*sqliteJournal)(nil)

// Migrate executes SQLite migrations for the journal schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return ErrInvalidConfig
	}
	if err := sqlite.Migrate(ctx, db, "journal", migrations); err != nil {
		return fmt.Errorf("journal: migrate schema: %w", err)
	}
	return nil
}

// New loads or initializes the Journal for a session, running schema migrations
// and restoring in-memory CAS tracked file state from SQLite metadata.
func New(ctx context.Context, db *sql.DB, sessionID, blobsDir string, opts ...Option) (Journal, error) {
	if db == nil {
		return nil, ErrInvalidConfig
	}
	sessionID = strings.TrimSpace(sessionID)
	blobsDir = strings.TrimSpace(blobsDir)
	if sessionID == "" || blobsDir == "" {
		return nil, fmt.Errorf("%w: session ID and blobs dir must not be empty", ErrInvalidConfig)
	}

	if err := Migrate(ctx, db); err != nil {
		return nil, err
	}

	files := make(map[string]fileState)

	// Load files tracked in journal_files
	fileRows, err := db.QueryContext(ctx, "SELECT path, baseline FROM journal_files WHERE session_id = ? ORDER BY path ASC", sessionID)
	if err != nil {
		return nil, fmt.Errorf("journal: load files for session %q: %w", sessionID, err)
	}
	defer fileRows.Close()

	for fileRows.Next() {
		var p, baseline string
		if err := fileRows.Scan(&p, &baseline); err != nil {
			return nil, fmt.Errorf("journal: scan file row: %w", err)
		}
		normP := normalizeTrackPath(p)
		files[normP] = fileState{
			hash:    baseline,
			changed: true,
		}
	}
	if err := fileRows.Err(); err != nil {
		return nil, fmt.Errorf("journal: iterate files: %w", err)
	}

	// Replay journal_entries to determine latest CAS hash
	entryRows, err := db.QueryContext(ctx, "SELECT path, kind, after_hash FROM journal_entries WHERE session_id = ? ORDER BY id ASC", sessionID)
	if err != nil {
		return nil, fmt.Errorf("journal: load entries for session %q: %w", sessionID, err)
	}
	defer entryRows.Close()

	var recent []string
	for entryRows.Next() {
		var p, kind, afterHash string
		if err := entryRows.Scan(&p, &kind, &afterHash); err != nil {
			return nil, fmt.Errorf("journal: scan entry row: %w", err)
		}
		normP := normalizeTrackPath(p)
		var h string
		if kind != string(api.KindDeleted) && afterHash != "" {
			h = afterHash
		}
		files[normP] = fileState{
			hash:    h,
			changed: true,
		}
		recent = updateJournalMRU(recent, normP)
	}
	if err := entryRows.Err(); err != nil {
		return nil, fmt.Errorf("journal: iterate entries: %w", err)
	}

	j := &sqliteJournal{
		db:        db,
		sessionID: sessionID,
		blobsDir:  blobsDir,
		files:     files,
		recent:    recent,
	}
	for _, opt := range opts {
		opt(j)
	}
	return j, nil
}

// Delete removes all journal entries and file tracking records for this session.
func (j *sqliteJournal) Delete(ctx context.Context) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, "DELETE FROM journal_entries WHERE session_id = ?", j.sessionID); err != nil {
		return fmt.Errorf("journal: delete entries for %q: %w", j.sessionID, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM journal_files WHERE session_id = ?", j.sessionID); err != nil {
		return fmt.Errorf("journal: delete files for %q: %w", j.sessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: commit delete session: %w", err)
	}
	j.mu.Lock()
	clear(j.files)
	j.recent = nil
	j.mu.Unlock()
	return nil
}

// LastHash returns the most recently recorded SHA-256 content hash for path.
func (j *sqliteJournal) LastHash(path string) (string, bool) {
	normPath := normalizeTrackPath(path)

	j.mu.RLock()
	st, ok := j.files[normPath]
	j.mu.RUnlock()

	if !ok || st.hash == "" {
		return "", false
	}
	return st.hash, true
}

// LastContent returns the most recently recorded content snapshot for path.
func (j *sqliteJournal) LastContent(path string) ([]byte, bool) {
	normPath := normalizeTrackPath(path)

	j.mu.RLock()
	st, ok := j.files[normPath]
	j.mu.RUnlock()

	if !ok || !st.changed || st.hash == "" {
		return nil, false
	}
	data, err := j.readBlob(st.hash)
	if err != nil {
		return nil, false
	}
	return data, true
}

// TrackedPaths returns a sorted slice of all workspace-relative file paths
// that have recorded changes in this session.
func (j *sqliteJournal) TrackedPaths() []string {
	j.mu.RLock()
	paths := make([]string, 0, len(j.files))
	for p, st := range j.files {
		if st.changed {
			paths = append(paths, p)
		}
	}
	j.mu.RUnlock()

	sort.Strings(paths)
	return paths
}

// RecentPaths returns the most recently read or modified file paths in MRU order.
func (j *sqliteJournal) RecentPaths() []string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]string, len(j.recent))
	copy(out, j.recent)
	return out
}

const maxRecentPaths = 100

func updateJournalMRU(recent []string, path string) []string {
	if path == "" || path == "." {
		return recent
	}
	out := make([]string, 0, min(len(recent)+1, maxRecentPaths))
	out = append(out, path)
	for _, p := range recent {
		if p != path {
			out = append(out, p)
			if len(out) >= maxRecentPaths {
				break
			}
		}
	}
	return out
}

// RecordRead records the observed SHA-256 hash of path in memory for
// Read-Before-Overwrite and Compare-And-Swap verification without persisting to SQLite.
func (j *sqliteJournal) RecordRead(ctx context.Context, path, hash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normPath := normalizeTrackPath(path)

	j.mu.Lock()
	st := j.files[normPath]
	st.hash = hash
	j.files[normPath] = st
	j.recent = updateJournalMRU(j.recent, normPath)
	j.mu.Unlock()
	return nil
}

// RecordChange persists file change metadata in SQLite and content snapshots
// in blobsDir, propagating scope metadata when source is agent.
func (j *sqliteJournal) RecordChange(ctx context.Context, change *std.FileChange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if change == nil {
		return errors.New("journal: file change must not be nil")
	}

	normPath := normalizeTrackPath(change.Path)
	change.Path = normPath

	if sc, ok := scope.From(ctx); ok && change.Source == api.SourceAgent {
		if change.TurnID == "" {
			change.TurnID = sc.TurnID
		}
		if change.MessageID == "" {
			change.MessageID = sc.MessageID
		}
		if change.ToolCallID == "" {
			change.ToolCallID = sc.ToolCallID
		}
		if change.Tool == "" {
			change.Tool = sc.ToolName
		}
	}

	if change.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("journal: generate change id: %w", err)
		}
		change.ID = id.String()
	}

	if change.Timestamp.IsZero() {
		change.Timestamp = time.Now().UTC()
	} else {
		change.Timestamp = change.Timestamp.UTC()
	}

	if change.BeforeContent != nil && change.BeforeHash == "" {
		change.BeforeHash = sha256Hex(change.BeforeContent)
	}
	if change.AfterContent != nil && change.AfterHash == "" {
		change.AfterHash = sha256Hex(change.AfterContent)
	}

	if change.BeforeHash != "" && change.BeforeContent != nil {
		if err := j.writeBlob(change.BeforeHash, change.BeforeContent); err != nil {
			return err
		}
	}
	if change.AfterHash != "" && change.AfterContent != nil {
		if err := j.writeBlob(change.AfterHash, change.AfterContent); err != nil {
			return err
		}
	}

	baselineHash := ""
	if change.BeforeContent != nil && change.BeforeHash != "" {
		baselineHash = change.BeforeHash
	}

	isBin := 0
	if change.IsBinary {
		isBin = 1
	}

	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	const insertFileQuery = `
INSERT OR IGNORE INTO journal_files (session_id, path, baseline, binary)
VALUES (?, ?, ?, ?);
`
	if _, err := tx.ExecContext(ctx, insertFileQuery, j.sessionID, normPath, baselineHash, isBin); err != nil {
		return fmt.Errorf("journal: insert journal_files for %q: %w", normPath, err)
	}

	const insertEntryQuery = `
INSERT INTO journal_entries (
    id, session_id, path, turn_id, message_id, tool_call_id,
    created_at, source, tool, kind, before_hash, after_hash,
    additions, deletions
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	createdAt := change.Timestamp.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, insertEntryQuery,
		change.ID, j.sessionID, normPath, change.TurnID, change.MessageID, change.ToolCallID,
		createdAt, string(change.Source), change.Tool, string(change.Kind),
		change.BeforeHash, change.AfterHash, change.Additions, change.Deletions,
	); err != nil {
		return fmt.Errorf("journal: insert journal_entries for %q: %w", normPath, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: commit change for %q: %w", normPath, err)
	}

	j.mu.Lock()
	afterHash := change.AfterHash
	if change.Kind == api.KindDeleted {
		afterHash = ""
	}
	j.files[normPath] = fileState{
		hash:    afterHash,
		changed: true,
	}
	j.recent = updateJournalMRU(j.recent, normPath)
	j.mu.Unlock()

	{
		mErr := metrics.RecordFileChange(scope.With(ctx, scope.Scope{SessionID: j.sessionID}), j.metrics, change.FileChange)
		if mErr != nil {
			log.Warn("failed to record file change metrics", log.String("error", mErr.Error()))
		}
	}

	return nil
}

func (j *sqliteJournal) queryChanges(ctx context.Context, extraWhere string, args ...any) ([]api.FileChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := `
SELECT e.id, e.path, e.turn_id, e.message_id, e.tool_call_id, e.created_at,
       e.source, e.tool, e.kind, e.before_hash, e.after_hash, e.additions, e.deletions
FROM journal_entries e
WHERE e.session_id = ?` + extraWhere + `
ORDER BY e.id ASC;
`
	allArgs := append([]any{j.sessionID}, args...)
	rows, err := j.db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("journal: query changes: %w", err)
	}
	defer rows.Close()

	out := make([]api.FileChange, 0)
	for rows.Next() {
		var fc api.FileChange
		var createdAt, sourceStr, kindStr string

		if err := rows.Scan(
			&fc.ID, &fc.Path, &fc.TurnID, &fc.MessageID, &fc.ToolCallID, &createdAt,
			&sourceStr, &fc.Tool, &kindStr, &fc.BeforeHash, &fc.AfterHash,
			&fc.Additions, &fc.Deletions,
		); err != nil {
			return nil, fmt.Errorf("journal: scan change: %w", err)
		}

		t, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, createdAt)
		}
		fc.Timestamp = t
		fc.Source = api.ChangeSource(sourceStr)
		fc.Kind = api.ChangeKind(kindStr)

		out = append(out, fc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: iterate changes: %w", err)
	}

	return out, nil
}

// Changes returns all journal entries for this session ordered chronologically by ID ascending.
func (j *sqliteJournal) Changes(ctx context.Context) ([]api.FileChange, error) {
	return j.queryChanges(ctx, "")
}

// ChangesForPath returns all journal entries for path in this session ordered by ID ascending.
func (j *sqliteJournal) ChangesForPath(ctx context.Context, path string) ([]api.FileChange, error) {
	normPath := normalizeTrackPath(path)
	return j.queryChanges(ctx, " AND e.path = ?", normPath)
}

func (j *sqliteJournal) fileMeta(ctx context.Context, normPath string) (baselineHash string, isBinary bool, err error) {
	var baseline string
	var binaryInt int
	err = j.db.QueryRowContext(ctx, "SELECT baseline, binary FROM journal_files WHERE session_id = ? AND path = ?", j.sessionID, normPath).Scan(&baseline, &binaryInt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, fmt.Errorf("%w: %s", ErrNotFound, normPath)
		}
		return "", false, fmt.Errorf("journal: query file baseline: %w", err)
	}
	return baseline, binaryInt != 0, nil
}

func (j *sqliteJournal) entryHashes(ctx context.Context, normPath, entryID string) (beforeHash, afterHash string, err error) {
	const query = `
SELECT before_hash, after_hash
FROM journal_entries
WHERE session_id = ? AND path = ? AND id = ?;
`
	err = j.db.QueryRowContext(ctx, query, j.sessionID, normPath, entryID).Scan(&beforeHash, &afterHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("%w: %s", ErrEntryNotFound, entryID)
		}
		return "", "", fmt.Errorf("journal: query entry %q: %w", entryID, err)
	}
	return beforeHash, afterHash, nil
}

// DiffEntry computes the unified diff for a specific journal entry by loading
// before and after blobs, or for the whole file relative to session baseline
// when entryID is empty.
func (j *sqliteJournal) DiffEntry(ctx context.Context, path, entryID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	normPath := normalizeTrackPath(path)

	if strings.TrimSpace(entryID) == "" {
		baseline, _, err := j.fileMeta(ctx, normPath)
		if err != nil {
			return "", err
		}

		var afterHash, kind string
		err = j.db.QueryRowContext(ctx, "SELECT after_hash, kind FROM journal_entries WHERE session_id = ? AND path = ? ORDER BY id DESC LIMIT 1", j.sessionID, normPath).Scan(&afterHash, &kind)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", fmt.Errorf("%w: %s", ErrNotFound, normPath)
			}
			return "", fmt.Errorf("journal: query latest entry for %q: %w", normPath, err)
		}

		if kind == string(api.KindDeleted) {
			afterHash = ""
		}

		beforeBytes, err := j.readBlob(baseline)
		if err != nil {
			return "", err
		}
		afterBytes, err := j.readBlob(afterHash)
		if err != nil {
			return "", err
		}

		return diff.FormatUnified("a/"+normPath, "b/"+normPath, string(beforeBytes), string(afterBytes)), nil
	}

	beforeHash, afterHash, err := j.entryHashes(ctx, normPath, entryID)
	if err != nil {
		return "", err
	}

	beforeBytes, err := j.readBlob(beforeHash)
	if err != nil {
		return "", err
	}
	afterBytes, err := j.readBlob(afterHash)
	if err != nil {
		return "", err
	}

	return diff.FormatUnified("a/"+normPath, "b/"+normPath, string(beforeBytes), string(afterBytes)), nil
}

// RecoverAt restores path in fsys to its state immediately before or after entryID.
func (j *sqliteJournal) RecoverAt(ctx context.Context, fsys sandbox.FS, path, entryID string, before bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normPath := normalizeTrackPath(path)

	_, isBinary, err := j.fileMeta(ctx, normPath)
	if err != nil {
		return err
	}

	beforeHash, afterHash, err := j.entryHashes(ctx, normPath, entryID)
	if err != nil {
		return err
	}

	targetHash := afterHash
	if before {
		targetHash = beforeHash
	}

	return j.applyTargetHash(ctx, fsys, normPath, targetHash, isBinary)
}

// RevertToBaseline reverts agent changes to path back to its session baseline,
// performing a 3-way merge when force is false.
func (j *sqliteJournal) RevertToBaseline(ctx context.Context, fsys sandbox.FS, path string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normPath := normalizeTrackPath(path)

	baseline, isBinary, err := j.fileMeta(ctx, normPath)
	if err != nil {
		return err
	}

	if force {
		return j.applyTargetHash(ctx, fsys, normPath, baseline, isBinary)
	}

	currentBytes, currentExists, err := readSandboxFile(ctx, fsys, normPath)
	if err != nil {
		return err
	}
	var currentHash string
	if currentExists {
		currentHash = sha256Hex(currentBytes)
	}

	// Determine the last state produced by the agent
	lastAgentHash := baseline
	rows, err := j.db.QueryContext(ctx, "SELECT after_hash FROM journal_entries WHERE session_id = ? AND path = ? AND source = ? ORDER BY id ASC", j.sessionID, normPath, string(api.SourceAgent))
	if err != nil {
		return fmt.Errorf("journal: query agent entries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ah string
		if err := rows.Scan(&ah); err != nil {
			return fmt.Errorf("journal: scan agent entry: %w", err)
		}
		lastAgentHash = ah
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("journal: iterate agent entries: %w", err)
	}

	// Fast path: if on-disk matches last agent state, restore directly to baseline
	if currentHash == lastAgentHash {
		return j.applyTargetHash(ctx, fsys, normPath, baseline, isBinary)
	}

	// External deletion cases
	if !currentExists && baseline == "" {
		return nil
	}
	if !currentExists {
		return ErrRevertConflict
	}

	lastAgentBytes, err := j.readBlob(lastAgentHash)
	if err != nil {
		return err
	}
	baselineBytes, err := j.readBlob(baseline)
	if err != nil {
		return err
	}

	ancestorLines := splitTrackerLines(string(lastAgentBytes))
	leftLines := splitTrackerLines(string(baselineBytes))
	rightLines := splitTrackerLines(string(currentBytes))

	mergedLines, conflict := diff.Merge3(ancestorLines, leftLines, rightLines)
	if conflict {
		return ErrRevertConflict
	}

	if baseline == "" && len(mergedLines) == 0 {
		return j.applyTargetHash(ctx, fsys, normPath, "", isBinary)
	}

	mergedText := strings.Join(mergedLines, "\n")
	if len(mergedLines) > 0 && (strings.HasSuffix(string(baselineBytes), "\n") || strings.HasSuffix(string(currentBytes), "\n")) {
		mergedText += "\n"
	}
	mergedBytes := []byte(mergedText)
	mergedHash := sha256Hex(mergedBytes)

	if err := writeSandboxFile(ctx, fsys, normPath, mergedBytes); err != nil {
		return err
	}

	add, del := diff.Stats(string(currentBytes), mergedText)
	return j.RecordChange(ctx, &std.FileChange{
		FileChange: api.FileChange{
			Path:       normPath,
			Source:     api.SourceAgent,
			Tool:       "revert",
			Kind:       api.KindModified,
			BeforeHash: currentHash,
			AfterHash:  mergedHash,
			Additions:  add,
			Deletions:  del,
			Timestamp:  time.Now().UTC(),
		},
		BeforeContent: currentBytes,
		AfterContent:  mergedBytes,
		IsBinary:      isBinary,
	})
}

// RevertSession reverts all tracked files in the session back to baseline.
func (j *sqliteJournal) RevertSession(ctx context.Context, fsys sandbox.FS, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, p := range j.TrackedPaths() {
		if err := j.RevertToBaseline(ctx, fsys, p, force); err != nil {
			return err
		}
	}
	return nil
}

func (j *sqliteJournal) applyTargetHash(ctx context.Context, fsys sandbox.FS, normPath, targetHash string, isBinary bool) error {
	currentBytes, currentExists, err := readSandboxFile(ctx, fsys, normPath)
	if err != nil {
		return err
	}
	var currentHash string
	if currentExists {
		currentHash = sha256Hex(currentBytes)
	}

	if targetHash == "" {
		if !currentExists {
			return nil
		}
		if err := fsys.Remove(ctx, normPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("journal: remove file %q during revert: %w", normPath, err)
		}
		_, del := diff.Stats(string(currentBytes), "")
		return j.RecordChange(ctx, &std.FileChange{
			FileChange: api.FileChange{
				Path:       normPath,
				Source:     api.SourceAgent,
				Tool:       "revert",
				Kind:       api.KindDeleted,
				BeforeHash: currentHash,
				AfterHash:  "",
				Deletions:  del,
				Timestamp:  time.Now().UTC(),
			},
			BeforeContent: currentBytes,
			IsBinary:      isBinary,
		})
	}

	targetBytes, err := j.readBlob(targetHash)
	if err != nil {
		return err
	}

	if currentExists && currentHash == targetHash {
		return nil
	}

	if err := writeSandboxFile(ctx, fsys, normPath, targetBytes); err != nil {
		return err
	}

	kind := api.KindModified
	if !currentExists {
		kind = api.KindCreated
	}
	add, del := diff.Stats(string(currentBytes), string(targetBytes))

	return j.RecordChange(ctx, &std.FileChange{
		FileChange: api.FileChange{
			Path:       normPath,
			Source:     api.SourceAgent,
			Tool:       "revert",
			Kind:       kind,
			BeforeHash: currentHash,
			AfterHash:  targetHash,
			Additions:  add,
			Deletions:  del,
			Timestamp:  time.Now().UTC(),
		},
		BeforeContent: currentBytes,
		AfterContent:  targetBytes,
		IsBinary:      isBinary,
	})
}

func (j *sqliteJournal) readBlob(hash string) ([]byte, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	blobPath := filepath.Join(j.blobsDir, filepath.Base(hash))
	data, err := os.ReadFile(blobPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, hash)
		}
		return nil, fmt.Errorf("journal: read blob %q: %w", hash, err)
	}
	return data, nil
}

func (j *sqliteJournal) writeBlob(hash string, content []byte) error {
	if strings.TrimSpace(hash) == "" || content == nil {
		return nil
	}

	blobPath := filepath.Join(j.blobsDir, filepath.Base(hash))
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(j.blobsDir, 0o700); err != nil {
		return fmt.Errorf("journal: create blobs directory %q: %w", j.blobsDir, err)
	}
	if err := os.WriteFile(blobPath, content, 0o600); err != nil {
		return fmt.Errorf("journal: write blob %q: %w", hash, err)
	}
	return nil
}

func readSandboxFile(ctx context.Context, fsys sandbox.FS, path string) ([]byte, bool, error) {
	data, err := fsys.ReadFile(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func writeSandboxFile(ctx context.Context, fsys sandbox.FS, path string, data []byte) error {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir != "" && dir != "." && dir != "/" {
		if err := fsys.MkdirAll(ctx, dir, 0o755); err != nil {
			return err
		}
	}
	return fsys.WriteFile(ctx, path, data, 0o644)
}

func normalizeTrackPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "." {
		return "."
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" {
		return "."
	}
	return clean
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func splitTrackerLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
