package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/std"
)

type fileState struct {
	hash    string
	changed bool
}

// FileTracker is a persistent, per-session implementation of [std.FileTracker]
// backed by compact `<path_hash>.jsonl` change journals and content-addressable
// file snapshots under `<session_dir>/changes/blobs/`.
type FileTracker struct {
	mu         sync.RWMutex
	changesDir string
	blobsDir   string
	files      map[string]fileState
}

var _ std.FileTracker = (*FileTracker)(nil)

func newFileTracker(changesDir string) (*FileTracker, error) {
	t := &FileTracker{
		changesDir: changesDir,
		blobsDir:   filepath.Join(changesDir, "blobs"),
		files:      make(map[string]fileState),
	}
	if err := t.loadFromDisk(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *FileTracker) loadFromDisk() error {
	entries, err := os.ReadDir(t.changesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("session: read changes directory %q: %w", t.changesDir, err)
	}

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".jsonl") {
			continue
		}
		journalPath := filepath.Join(t.changesDir, de.Name())
		header, journalEntries, err := readJournalFile(journalPath)
		if err != nil {
			return err
		}
		normPath := normalizeTrackPath(header.Path)
		var h string
		if len(journalEntries) == 0 {
			h = header.Baseline
		} else {
			last := journalEntries[len(journalEntries)-1]
			if last.Kind != std.KindDeleted && last.After != "" {
				h = last.After
			}
		}
		t.files[normPath] = fileState{
			hash:    h,
			changed: true,
		}
	}
	return nil
}

// LastHash returns the most recently recorded SHA-256 content hash for path
// (from either a read or a change). It returns ("", false) if path has not been
// read or modified, or if its most recent recorded change was a deletion.
func (t *FileTracker) LastHash(path string) (string, bool) {
	normPath := normalizeTrackPath(path)

	t.mu.RLock()
	st, ok := t.files[normPath]
	t.mu.RUnlock()

	if !ok || st.hash == "" {
		return "", false
	}
	return st.hash, true
}

// LastContent returns the most recently recorded content snapshot for path by
// reading its content-addressable blob from `changes/blobs/<hash>`.
// It returns (nil, false) if path has not been modified or was deleted.
func (t *FileTracker) LastContent(path string) ([]byte, bool) {
	normPath := normalizeTrackPath(path)

	t.mu.RLock()
	st, ok := t.files[normPath]
	t.mu.RUnlock()

	if !ok || !st.changed || st.hash == "" {
		return nil, false
	}
	data, err := t.ReadBlob(st.hash)
	if err != nil {
		return nil, false
	}
	return data, true
}

// TrackedPaths returns a sorted slice of all workspace-relative file paths
// that have recorded change journals in this session.
func (t *FileTracker) TrackedPaths() []string {
	t.mu.RLock()
	paths := make([]string, 0, len(t.files))
	for p, st := range t.files {
		if st.changed {
			paths = append(paths, p)
		}
	}
	t.mu.RUnlock()

	sort.Strings(paths)
	return paths
}

// RecordRead records the observed SHA-256 hash of path in memory for
// Read-Before-Overwrite and Compare-And-Swap verification without creating any
// on-disk `.jsonl` journal or blob files.
func (t *FileTracker) RecordRead(ctx context.Context, path, hash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normPath := normalizeTrackPath(path)

	t.mu.Lock()
	st := t.files[normPath]
	st.hash = hash
	t.files[normPath] = st
	t.mu.Unlock()
	return nil
}

// RecordChange assigns a UUIDv7 to change.ID, persists before/after snapshots
// to `changes/blobs/`, initializes Line 1 (`JournalHeader`) of `<path_hash>.jsonl`
// if the file has not been modified yet, appends a compact [JournalEntry] line,
// and updates the in-memory tracked state.
func (t *FileTracker) RecordChange(ctx context.Context, change *std.FileChange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if change == nil {
		return errors.New("session: file change must not be nil")
	}

	normPath := normalizeTrackPath(change.Path)
	change.Path = normPath

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("session: generate change id: %w", err)
	}
	change.ID = id.String()

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

	t.mu.Lock()
	defer t.mu.Unlock()

	if change.BeforeHash != "" && change.BeforeContent != nil {
		if err := t.writeBlobLocked(change.BeforeHash, change.BeforeContent); err != nil {
			return err
		}
	}
	if change.AfterHash != "" && change.AfterContent != nil {
		if err := t.writeBlobLocked(change.AfterHash, change.AfterContent); err != nil {
			return err
		}
	}

	baselineHash := ""
	if change.BeforeContent != nil && change.BeforeHash != "" {
		baselineHash = change.BeforeHash
	}
	if err := t.ensureHeaderLocked(normPath, baselineHash, change.IsBinary); err != nil {
		return err
	}

	entry := JournalEntry{
		ID:        change.ID,
		Timestamp: change.Timestamp,
		Source:    change.Source,
		Tool:      change.Tool,
		Kind:      change.Kind,
		Before:    change.BeforeHash,
		After:     change.AfterHash,
		Additions: change.Additions,
		Deletions: change.Deletions,
	}

	if err := t.appendEntryLocked(normPath, entry); err != nil {
		return err
	}

	afterHash := change.AfterHash
	if change.Kind == std.KindDeleted {
		afterHash = ""
	}
	t.files[normPath] = fileState{
		hash:    afterHash,
		changed: true,
	}
	return nil
}

// ReadJournal reads the [JournalHeader] (Line 1) and all [JournalEntry] records
// (Lines 2..N) for path from `<path_hash>.jsonl`.
func (t *FileTracker) ReadJournal(path string) (*JournalHeader, []JournalEntry, error) {
	normPath := normalizeTrackPath(path)

	t.mu.RLock()
	defer t.mu.RUnlock()

	return readJournalFile(t.journalPath(normPath))
}

// ReadBlob reads the raw snapshot bytes for the given SHA-256 hex digest from
// `changes/blobs/<hash>`. If hash is empty, it returns (nil, nil).
func (t *FileTracker) ReadBlob(hash string) ([]byte, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	blobPath := filepath.Join(t.blobsDir, filepath.Base(hash))
	data, err := os.ReadFile(blobPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, hash)
		}
		return nil, fmt.Errorf("session: read blob %q: %w", hash, err)
	}
	return data, nil
}

// DiffEntry computes the unified diff for a specific [JournalEntry] on demand
// by loading its `before` and `after` blobs from `changes/blobs/`.
func (t *FileTracker) DiffEntry(path string, entryID string) (string, error) {
	header, entries, err := t.ReadJournal(path)
	if err != nil {
		return "", err
	}

	var target *JournalEntry
	for i := range entries {
		if entries[i].ID == entryID {
			target = &entries[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("%w: %s", ErrEntryNotFound, entryID)
	}

	beforeBytes, err := t.ReadBlob(target.Before)
	if err != nil {
		return "", err
	}
	afterBytes, err := t.ReadBlob(target.After)
	if err != nil {
		return "", err
	}

	return diff.FormatUnified("a/"+header.Path, "b/"+header.Path, string(beforeBytes), string(afterBytes)), nil
}

// RecoverAt restores path in fsys to its exact state either immediately before
// (`before == true`) or immediately after (`before == false`) the specified
// journal entryID, and records a new `revert` change entry in the journal.
func (t *FileTracker) RecoverAt(ctx context.Context, fsys sandbox.FS, path string, entryID string, before bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	header, entries, err := t.ReadJournal(path)
	if err != nil {
		return err
	}

	var target *JournalEntry
	for i := range entries {
		if entries[i].ID == entryID {
			target = &entries[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: %s", ErrEntryNotFound, entryID)
	}

	targetHash := target.After
	if before {
		targetHash = target.Before
	}

	return t.applyTargetHash(ctx, fsys, header, targetHash)
}

// RevertToBaseline reverts agent modifications to path back to the file's
// initial session baseline. When force is false, it performs a 3-way merge
// (`diff.Merge3`) between the last agent state, the initial baseline, and the
// current on-disk file so non-overlapping external edits are preserved; if
// overlapping conflicts exist, it returns [ErrRevertConflict].
func (t *FileTracker) RevertToBaseline(ctx context.Context, fsys sandbox.FS, path string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	header, entries, err := t.ReadJournal(path)
	if err != nil {
		return err
	}

	if force {
		return t.applyTargetHash(ctx, fsys, header, header.Baseline)
	}

	currentBytes, currentExists, err := readSandboxFile(ctx, fsys, header.Path)
	if err != nil {
		return err
	}
	var currentHash string
	if currentExists {
		currentHash = sha256Hex(currentBytes)
	}

	// Determine the last state produced by the agent.
	lastAgentHash := header.Baseline
	for _, e := range entries {
		if e.Source == std.SourceAgent {
			lastAgentHash = e.After
		}
	}

	// Fast path: if the on-disk file matches the last agent state (no external edits),
	// restore directly to header.Baseline.
	if currentHash == lastAgentHash {
		return t.applyTargetHash(ctx, fsys, header, header.Baseline)
	}

	// If the file was deleted externally and the baseline was also empty, nothing to do.
	if !currentExists && header.Baseline == "" {
		return nil
	}
	// If the file was deleted externally while baseline had content (or vice versa
	// when agent modified it), treat as conflict unless forced.
	if !currentExists {
		return ErrRevertConflict
	}

	lastAgentBytes, err := t.ReadBlob(lastAgentHash)
	if err != nil {
		return err
	}
	baselineBytes, err := t.ReadBlob(header.Baseline)
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

	if header.Baseline == "" && len(mergedLines) == 0 {
		return t.applyTargetHash(ctx, fsys, header, "")
	}

	mergedText := strings.Join(mergedLines, "\n")
	if len(mergedLines) > 0 && (strings.HasSuffix(string(baselineBytes), "\n") || strings.HasSuffix(string(currentBytes), "\n")) {
		mergedText += "\n"
	}
	mergedBytes := []byte(mergedText)
	mergedHash := sha256Hex(mergedBytes)

	if err := writeSandboxFile(ctx, fsys, header.Path, mergedBytes); err != nil {
		return err
	}

	add, del := diff.Stats(string(currentBytes), mergedText)
	return t.RecordChange(ctx, &std.FileChange{
		Path:          header.Path,
		Source:        std.SourceAgent,
		Tool:          "revert",
		Kind:          std.KindModified,
		BeforeHash:    currentHash,
		AfterHash:     mergedHash,
		BeforeContent: currentBytes,
		AfterContent:  mergedBytes,
		Additions:     add,
		Deletions:     del,
		IsBinary:      header.Binary,
	})
}

func (t *FileTracker) applyTargetHash(ctx context.Context, fsys sandbox.FS, header *JournalHeader, targetHash string) error {
	currentBytes, currentExists, err := readSandboxFile(ctx, fsys, header.Path)
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
		if err := fsys.Remove(ctx, header.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("session: remove file %q during revert: %w", header.Path, err)
		}
		_, del := diff.Stats(string(currentBytes), "")
		return t.RecordChange(ctx, &std.FileChange{
			Path:          header.Path,
			Source:        std.SourceAgent,
			Tool:          "revert",
			Kind:          std.KindDeleted,
			BeforeHash:    currentHash,
			AfterHash:     "",
			BeforeContent: currentBytes,
			Deletions:     del,
			IsBinary:      header.Binary,
		})
	}

	targetBytes, err := t.ReadBlob(targetHash)
	if err != nil {
		return err
	}

	if currentExists && currentHash == targetHash {
		return nil
	}

	if err := writeSandboxFile(ctx, fsys, header.Path, targetBytes); err != nil {
		return err
	}

	kind := std.KindModified
	if !currentExists {
		kind = std.KindCreated
	}
	add, del := diff.Stats(string(currentBytes), string(targetBytes))

	return t.RecordChange(ctx, &std.FileChange{
		Path:          header.Path,
		Source:        std.SourceAgent,
		Tool:          "revert",
		Kind:          kind,
		BeforeHash:    currentHash,
		AfterHash:     targetHash,
		BeforeContent: currentBytes,
		AfterContent:  targetBytes,
		Additions:     add,
		Deletions:     del,
		IsBinary:      header.Binary,
	})
}

func (t *FileTracker) journalPath(normPath string) string {
	return filepath.Join(t.changesDir, pathHash(normPath)+".jsonl")
}

func (t *FileTracker) writeBlobLocked(hash string, content []byte) error {
	blobPath := filepath.Join(t.blobsDir, filepath.Base(hash))
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(t.blobsDir, 0o700); err != nil {
		return fmt.Errorf("session: create blobs directory %q: %w", t.blobsDir, err)
	}
	if err := os.WriteFile(blobPath, content, 0o600); err != nil {
		return fmt.Errorf("session: write blob %q: %w", hash, err)
	}
	return nil
}

func (t *FileTracker) ensureHeaderLocked(normPath, baselineHash string, binary bool) error {
	jPath := t.journalPath(normPath)
	if _, err := os.Stat(jPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(t.changesDir, 0o700); err != nil {
		return fmt.Errorf("session: create changes directory %q: %w", t.changesDir, err)
	}

	header := JournalHeader{
		Path:     normPath,
		Baseline: baselineHash,
		Binary:   binary,
	}
	line, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("session: marshal journal header for %q: %w", normPath, err)
	}
	line = append(line, '\n')
	if err := os.WriteFile(jPath, line, 0o600); err != nil {
		return fmt.Errorf("session: write journal header %q: %w", jPath, err)
	}
	return nil
}

func (t *FileTracker) appendEntryLocked(normPath string, entry JournalEntry) error {
	jPath := t.journalPath(normPath)
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("session: marshal journal entry for %q: %w", normPath, err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(jPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("session: open journal %q for append: %w", jPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("session: append journal entry to %q: %w", jPath, err)
	}
	return nil
}

func readJournalFile(jPath string) (*JournalHeader, []JournalEntry, error) {
	f, err := os.Open(jPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, ErrJournalNotFound
		}
		return nil, nil, fmt.Errorf("session: open journal %q: %w", jPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, nil, fmt.Errorf("session: read journal header %q: %w", jPath, err)
		}
		return nil, nil, fmt.Errorf("%w: empty journal %q", ErrJournalNotFound, jPath)
	}

	var header JournalHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, nil, fmt.Errorf("session: unmarshal journal header %q: %w", jPath, err)
	}

	var entries []JournalEntry
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, nil, fmt.Errorf("session: unmarshal journal entry in %q: %w", jPath, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("session: scan journal %q: %w", jPath, err)
	}

	return &header, entries, nil
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
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" {
		return "."
	}
	return clean
}

func pathHash(normPath string) string {
	sum := sha256.Sum256([]byte(normPath))
	return fmt.Sprintf("%x", sum[:8])
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
