package journal

import (
	"context"
	"errors"

	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

var (
	ErrInvalidConfig  = errors.New("journal: invalid configuration")
	ErrNotFound       = errors.New("journal: file change journal not found")
	ErrEntryNotFound  = errors.New("journal: journal entry not found")
	ErrBlobNotFound   = errors.New("journal: file snapshot blob not found")
	ErrRevertConflict = errors.New("journal: revert conflict with external changes")
)

// Journal tracks per-session file reads (CAS) and persists file change metadata
// in SQLite paired with content-addressable snapshots in the session's blobsDir.
type Journal interface {
	std.FileTracker

	Changes(ctx context.Context) ([]std.FileChange, error)
	ChangesForPath(ctx context.Context, path string) ([]std.FileChange, error)
	DiffEntry(ctx context.Context, path, entryID string) (string, error)
	RecoverAt(ctx context.Context, fsys sandbox.FS, path, entryID string, before bool) error
	RevertToBaseline(ctx context.Context, fsys sandbox.FS, path string, force bool) error
	RevertSession(ctx context.Context, fsys sandbox.FS, force bool) error
	Delete(ctx context.Context) error
}
