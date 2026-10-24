package std

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

type trackedState struct {
	hash    string
	content []byte
	changed bool
}

// MemoryTracker is a thread-safe, in-memory implementation of [FileTracker]
// suitable for unit tests and stateless callers that do not persist change
// journals to disk.
type MemoryTracker struct {
	mu      sync.RWMutex
	files   map[string]trackedState
	changes []FileChange
}

var _ FileTracker = (*MemoryTracker)(nil)

// NewMemoryTracker creates a new initialized [MemoryTracker].
func NewMemoryTracker() *MemoryTracker {
	return &MemoryTracker{
		files: make(map[string]trackedState),
	}
}

func cleanTrackerPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "." {
		return "."
	}
	return filepath.ToSlash(filepath.Clean(trimmed))
}

// LastHash returns the most recently recorded SHA-256 content hash for path
// (from either RecordRead or RecordChange). It returns ("", false) if path has
// not been read/changed or if its most recent recorded change was a deletion.
func (t *MemoryTracker) LastHash(path string) (string, bool) {
	key := cleanTrackerPath(path)

	t.mu.RLock()
	state, ok := t.files[key]
	t.mu.RUnlock()

	if !ok || state.hash == "" {
		return "", false
	}
	return state.hash, true
}

// LastContent returns a defensive copy of the most recently recorded content
// snapshot for path from RecordChange. It returns (nil, false) if path has not
// had a recorded change or if its most recent recorded change was a deletion.
func (t *MemoryTracker) LastContent(path string) ([]byte, bool) {
	key := cleanTrackerPath(path)

	t.mu.RLock()
	state, ok := t.files[key]
	t.mu.RUnlock()

	if !ok || !state.changed || state.hash == "" {
		return nil, false
	}
	cp := make([]byte, len(state.content))
	copy(cp, state.content)
	return cp, true
}

// TrackedPaths returns a sorted slice of all file paths that have had at least
// one change recorded via RecordChange.
func (t *MemoryTracker) TrackedPaths() []string {
	t.mu.RLock()
	paths := make([]string, 0, len(t.files))
	for p, state := range t.files {
		if state.changed {
			paths = append(paths, p)
		}
	}
	t.mu.RUnlock()

	sort.Strings(paths)
	return paths
}

// RecordRead records the latest observed SHA-256 hash for path without storing
// file content snapshots.
func (t *MemoryTracker) RecordRead(ctx context.Context, path, hash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	key := cleanTrackerPath(path)

	t.mu.Lock()
	state := t.files[key]
	state.hash = hash
	t.files[key] = state
	t.mu.Unlock()
	return nil
}

// RecordChange assigns a UUIDv7 identifier to change (when empty), appends a
// defensive copy of change to the in-memory change history, and updates the
// tracked state for change.Path.
func (t *MemoryTracker) RecordChange(ctx context.Context, change *FileChange) error {
	if change == nil {
		return fmt.Errorf("%w: file change cannot be nil", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	key := cleanTrackerPath(change.Path)
	change.Path = key
	if sc, ok := scope.From(ctx); ok && change.Source == SourceAgent {
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
			return err
		}
		change.ID = id.String()
	}
	if change.Timestamp.IsZero() {
		change.Timestamp = time.Now().UTC()
	}

	stored := cloneFileChange(*change)

	t.mu.Lock()
	t.changes = append(t.changes, stored)
	if stored.Kind == KindDeleted || stored.AfterHash == "" {
		t.files[key] = trackedState{
			hash:    "",
			content: nil,
			changed: true,
		}
	} else {
		var afterSnap []byte
		if stored.AfterContent != nil {
			afterSnap = make([]byte, len(stored.AfterContent))
			copy(afterSnap, stored.AfterContent)
		}
		t.files[key] = trackedState{
			hash:    stored.AfterHash,
			content: afterSnap,
			changed: true,
		}
	}
	t.mu.Unlock()
	return nil
}

// Changes returns a chronological copy of all recorded [FileChange] entries.
func (t *MemoryTracker) Changes() []FileChange {
	t.mu.RLock()
	out := make([]FileChange, len(t.changes))
	for i, c := range t.changes {
		out[i] = cloneFileChange(c)
	}
	t.mu.RUnlock()
	return out
}

// ChangesForPath returns a chronological copy of all recorded [FileChange]
// entries for the given path.
func (t *MemoryTracker) ChangesForPath(path string) []FileChange {
	key := cleanTrackerPath(path)

	t.mu.RLock()
	var out []FileChange
	for _, c := range t.changes {
		if c.Path == key {
			out = append(out, cloneFileChange(c))
		}
	}
	t.mu.RUnlock()
	return out
}

func cloneFileChange(c FileChange) FileChange {
	if c.BeforeContent != nil {
		cp := make([]byte, len(c.BeforeContent))
		copy(cp, c.BeforeContent)
		c.BeforeContent = cp
	}
	if c.AfterContent != nil {
		cp := make([]byte, len(c.AfterContent))
		copy(cp, c.AfterContent)
		c.AfterContent = cp
	}
	return c
}
