package filetrack

import (
	"context"
	"strings"
	"time"
)

type ChangeKind string

const (
	Created  ChangeKind = "created"
	Modified ChangeKind = "modified"
	Deleted  ChangeKind = "deleted"
)

// Change is the input to FileTracker.Record()
type Change struct {
	ToolName  string     `json:"tool_name"`
	Path      string     `json:"path"` // workspace-relative
	Kind      ChangeKind `json:"kind"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
}

// FileSummary is the aggregate per-file state.
type FileSummary struct {
	Path          string     `json:"path"`
	Kind          ChangeKind `json:"kind"`
	TotalEdits    int        `json:"total_edits"`
	NetAdditions  int        `json:"net_additions"`
	NetDeletions  int        `json:"net_deletions"`
	LastChangedAt time.Time  `json:"last_changed_at"`
}

// JournalEntry is one line in the JSONL journal file.
type JournalEntry struct {
	Timestamp time.Time  `json:"ts"`
	ToolName  string     `json:"tool,omitempty"`
	Kind      ChangeKind `json:"kind"`
	Content   string     `json:"content,omitempty"` // only for baseline
	Additions int        `json:"additions,omitempty"`
	Deletions int        `json:"deletions,omitempty"`
	Diff      string     `json:"diff,omitempty"` // only for changes
	IsBinary  bool       `json:"is_binary,omitempty"`
}

// FileEvent represents a file modification event broadcasted by the WorkspaceTracker.
type FileEvent struct {
	Path      string     `json:"path"` // workspace-relative
	Kind      ChangeKind `json:"kind"`
	Hash      string     `json:"hash"`             // SHA-256 hash computed once by the notifier
	Source    string     `json:"source,omitempty"` // ID of the session that wrote it, or empty/"watcher"
	Timestamp time.Time  `json:"timestamp"`
}

type SearchResult struct {
	Path      string
	ShortPath string // Pre-computed shortest unique suffix for autocomplete
	IsDir     bool
}

// WorkspaceTracker monitors the workspace filesystem, caches filenames, and acts as a pub/sub broker.
type WorkspaceTracker interface {
	// Start starts the recursive filesystem watcher and scans the workspace
	Start(ctx context.Context) error
	// Stop stops the watcher
	Stop() error

	// Autocomplete & Search lookup from the filename cache
	Search(query string) []SearchResult

	// Pub/Sub subscription for file changes
	SubscribeSession(sessionID string) (<-chan FileEvent, func())
	Publish(ctx context.Context, event FileEvent)

	// Register/Unregister interest in specific files (for demand-driven watcher routing)
	RegisterInterest(sessionID, path string)
	UnregisterInterest(sessionID, path string)

	// Activity Notifications (called by session trackers or editor actions)
	NotifyTouch(path string, isWrite bool)
	// MRU list of active files
	ActiveFiles() []string
}

// FileTracker records and queries file changes within a session.
type FileTracker interface {
	Record(ctx context.Context, change Change, diff string, oldContent string) error
	Summary(ctx context.Context) ([]FileSummary, error)
	ReadJournal(ctx context.Context, path string) ([]JournalEntry, error)
	RevertToBaseline(ctx context.Context, path string, force bool) error
	CheckConflict(ctx context.Context, path string) (bool, error)
	RecordRead(ctx context.Context, path string) error
	IsKnown(ctx context.Context, path string) (bool, error)
}

// splitLines splits a string into lines, normalizing line endings and removing trailing empty lines.
func splitLines(s string) []string {
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
