package filetrack

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/core/fs"
	"github.com/masterkeysrd/tasksmith/internal/session/resource"
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

type resourceData struct {
	ToolName   string     `json:"tool"`
	ChangeKind ChangeKind `json:"change_kind"`
	Additions  int        `json:"additions"`
	Deletions  int        `json:"deletions"`
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

type tracker struct {
	store        *resource.Store
	sessionID    string
	changesDir   string
	workspaceDir string
	touched      map[string]bool
	mu           sync.Mutex
}

func NewTracker(store *resource.Store, sessionID, changesDir, workspaceDir string) FileTracker {
	return &tracker{
		store:        store,
		sessionID:    sessionID,
		changesDir:   changesDir,
		workspaceDir: workspaceDir,
		touched:      make(map[string]bool),
	}
}

func (t *tracker) journalPath(relPath string) string {
	h := sha256.Sum256([]byte(relPath))
	return filepath.Join(t.changesDir, fmt.Sprintf("%x.jsonl", h[:8]))
}

func (t *tracker) lastPath(relPath string) string {
	h := sha256.Sum256([]byte(relPath))
	return filepath.Join(t.changesDir, fmt.Sprintf("%x.last", h[:8]))
}

func (t *tracker) Record(ctx context.Context, change Change, diff string, oldContent string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := os.MkdirAll(t.changesDir, 0755); err != nil {
		return fmt.Errorf("failed to create changes directory: %w", err)
	}

	journalPath := t.journalPath(change.Path)

	// Check if touched
	touched := t.touched[change.Path]
	if !touched {
		if info, err := os.Stat(journalPath); err == nil && info.Size() > 0 {
			touched = true
			t.touched[change.Path] = true
		}
	}

	f, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open journal file: %w", err)
	}
	defer f.Close()

	now := time.Now().UTC()

	absPath := filepath.Join(t.workspaceDir, change.Path)
	isBinary := false
	if change.Kind != Deleted {
		mimeType := fs.DetectMIMEType(absPath)
		isBinary = fs.IsBinaryMIME(mimeType)
	}

	if !touched {
		baselineContent := ""
		if !isBinary {
			baselineContent = oldContent
		}
		baselineEntry := JournalEntry{
			Timestamp: now,
			Kind:      "baseline",
			Content:   baselineContent,
			IsBinary:  isBinary,
		}
		baselineBytes, err := json.Marshal(baselineEntry)
		if err != nil {
			return fmt.Errorf("failed to marshal baseline entry: %w", err)
		}
		if _, err := f.Write(append(baselineBytes, '\n')); err != nil {
			return fmt.Errorf("failed to write baseline entry: %w", err)
		}
		t.touched[change.Path] = true
	}

	changeEntry := JournalEntry{
		Timestamp: now,
		ToolName:  change.ToolName,
		Kind:      change.Kind,
		Additions: change.Additions,
		Deletions: change.Deletions,
		Diff:      diff,
	}
	changeBytes, err := json.Marshal(changeEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal change entry: %w", err)
	}
	if _, err := f.Write(append(changeBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write change entry: %w", err)
	}

	rd := resourceData{
		ToolName:   change.ToolName,
		ChangeKind: change.Kind,
		Additions:  change.Additions,
		Deletions:  change.Deletions,
	}
	rdBytes, err := json.Marshal(rd)
	if err != nil {
		return fmt.Errorf("failed to marshal resource data: %w", err)
	}

	res := resource.Resource{
		SessionID: t.sessionID,
		Kind:      "file_change",
		Key:       change.Path,
		Data:      string(rdBytes),
		CreatedAt: now,
	}
	if _, err := t.store.Insert(ctx, res); err != nil {
		return fmt.Errorf("failed to record resource metadata: %w", err)
	}

	// Compute and store file hash & .last file
	var hashVal string
	lastPath := t.lastPath(change.Path)
	if change.Kind == Deleted {
		hashVal = "deleted"
		_ = os.Remove(lastPath)
	} else {
		data, err := os.ReadFile(absPath)
		if err == nil {
			hashVal = fmt.Sprintf("%x", sha256.Sum256(data))
			if !isBinary {
				if err := os.WriteFile(lastPath, data, 0644); err != nil {
					return fmt.Errorf("failed to write .last file: %w", err)
				}
			}
		}
	}

	if hashVal != "" {
		_ = t.store.DeleteByKey(ctx, t.sessionID, "file_hash", change.Path)
		resHash := resource.Resource{
			SessionID: t.sessionID,
			Kind:      "file_hash",
			Key:       change.Path,
			Data:      hashVal,
			CreatedAt: now,
		}
		_, _ = t.store.Insert(ctx, resHash)
	}

	return nil
}

func (t *tracker) Summary(ctx context.Context) ([]FileSummary, error) {
	resources, err := t.store.Query(ctx, t.sessionID, "file_change")
	if err != nil {
		return nil, err
	}

	summaries := make(map[string]*FileSummary)
	for _, res := range resources {
		var rd resourceData
		if err := json.Unmarshal([]byte(res.Data), &rd); err != nil {
			continue
		}

		path := res.Key
		summary, exists := summaries[path]
		if !exists {
			summary = &FileSummary{
				Path:          path,
				Kind:          rd.ChangeKind,
				LastChangedAt: res.CreatedAt,
			}
			summaries[path] = summary
		}

		summary.TotalEdits++
		summary.LastChangedAt = res.CreatedAt

		if rd.ChangeKind == Deleted {
			summary.Kind = Deleted
		} else if summary.Kind != Created {
			summary.Kind = rd.ChangeKind
		}
	}

	var result []FileSummary
	for _, s := range summaries {
		baselineContent := ""
		entries, err := t.ReadJournal(ctx, s.Path)
		if err == nil && len(entries) > 0 && entries[0].Kind == "baseline" {
			baselineContent = entries[0].Content
		}

		currentContent := ""
		isBinary := false
		if s.Kind != Deleted {
			absPath := filepath.Join(t.workspaceDir, s.Path)
			mimeType := fs.DetectMIMEType(absPath)
			isBinary = fs.IsBinaryMIME(mimeType)
			if bytes, err := os.ReadFile(absPath); err == nil {
				currentContent = string(bytes)
			}
		}

		var additions, deletions int
		if !isBinary && len(currentContent) <= 1000000 && len(baselineContent) <= 1000000 {
			baselineLines := splitLines(baselineContent)
			currentLines := splitLines(currentContent)

			edits := diff.MyersDiff(baselineLines, currentLines)
			for _, e := range edits {
				if e.Op == diff.OpInsert {
					additions++
				} else if e.Op == diff.OpDelete {
					deletions++
				}
			}
		}

		s.NetAdditions = additions
		s.NetDeletions = deletions

		result = append(result, *s)
	}
	return result, nil
}

func (t *tracker) ReadJournal(ctx context.Context, path string) ([]JournalEntry, error) {
	journalPath := t.journalPath(path)
	file, err := os.Open(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var entries []JournalEntry
	scanner := bufio.NewScanner(file)
	// Allow scanning lines up to 16MB to support baseline entries of larger files
	const maxCapacity = 16 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func (t *tracker) checkConflictLocked(ctx context.Context, path string) (bool, error) {
	entries, err := t.ReadJournal(ctx, path)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 || entries[0].Kind != "baseline" {
		// Not tracked, so no conflict
		return false, nil
	}

	baseline := entries[0]
	if baseline.IsBinary {
		// Binary conflict detection based on SHA-256 hash.
		hashes, err := t.store.QueryByKey(ctx, t.sessionID, "file_hash", path)
		if err != nil {
			return false, err
		}
		var lastHash string
		if len(hashes) > 0 {
			var latestRes *resource.Resource
			for _, r := range hashes {
				if latestRes == nil || r.CreatedAt.After(latestRes.CreatedAt) {
					rCopy := r
					latestRes = &rCopy
				}
			}
			if latestRes != nil {
				lastHash = latestRes.Data
			}
		}

		absPath := filepath.Join(t.workspaceDir, path)
		data, err := os.ReadFile(absPath)
		var currentHash string
		if err == nil {
			currentHash = fmt.Sprintf("%x", sha256.Sum256(data))
		} else if os.IsNotExist(err) {
			currentHash = "deleted"
		} else {
			return false, err
		}

		return currentHash != lastHash, nil
	}

	aLines := splitLines(baseline.Content)

	// Get ancestor B (last agent content)
	var bLines []string
	lastPath := t.lastPath(path)
	bBytes, err := os.ReadFile(lastPath)
	if err == nil {
		bLines = splitLines(string(bBytes))
	} else if os.IsNotExist(err) {
		if len(entries) > 0 && entries[len(entries)-1].Kind == Deleted {
			bLines = nil
		} else {
			bLines = aLines
		}
	} else {
		return false, err
	}

	// Get current content C
	absPath := filepath.Join(t.workspaceDir, path)
	cBytes, err := os.ReadFile(absPath)
	var cLines []string
	cExists := false
	if err == nil {
		cLines = splitLines(string(cBytes))
		cExists = true
	} else if !os.IsNotExist(err) {
		return false, err
	}

	// If the file was deleted by agent (B is deleted)
	if bLines == nil {
		// If the file exists on disk (C exists), then C != B. Since B is deleted and C is not, it's a conflict!
		if cExists {
			return true, nil
		}
		// If both are deleted, no conflict
		return false, nil
	}

	// If B exists, but C is deleted (e.g. user deleted the file)
	if !cExists {
		// Left is A (baseline). Right is C (deleted).
		// If A was empty/deleted, and C is deleted, no conflict.
		// If A was not empty, and C is deleted, but A != B (agent edited it), then Left is A, Right is C (deleted).
		// Reverting agent's edit (B -> A) means restoring baseline. User deleted it (B -> C).
		// This conflicts if baseline had content!
		if len(aLines) > 0 {
			return true, nil
		}
		return false, nil
	}

	// Both B and C exist, perform line-level three-way merge
	_, hasConflict := diff.Merge3(bLines, aLines, cLines)
	return hasConflict, nil
}

func (t *tracker) RevertToBaseline(ctx context.Context, path string, force bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	entries, err := t.ReadJournal(ctx, path)
	if err != nil {
		return err
	}
	if len(entries) == 0 || entries[0].Kind != "baseline" {
		return fmt.Errorf("no baseline entry found for path: %s", path)
	}

	baseline := entries[0]
	absPath := filepath.Join(t.workspaceDir, path)

	if baseline.IsBinary {
		firstChangeIsCreation := false
		if len(entries) > 1 && entries[1].Kind == Created {
			firstChangeIsCreation = true
		}
		if !firstChangeIsCreation {
			return fmt.Errorf("cannot revert binary file: no backup available")
		}
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete created binary file on revert: %w", err)
		}
	} else {
		if force {
			// Just restore baseline directly, overwriting any manual edits.
			if baseline.Content == "" {
				if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to delete created file on revert: %w", err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
					return fmt.Errorf("failed to create directory for revert: %w", err)
				}
				if err := os.WriteFile(absPath, []byte(baseline.Content), 0644); err != nil {
					return fmt.Errorf("failed to restore baseline file content: %w", err)
				}
			}
		} else {
			// Non-forced revert: check conflict and perform three-way merge.
			conflict, err := t.checkConflictLocked(ctx, path)
			if err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("conflict")
			}

			aLines := splitLines(baseline.Content)

			// Get ancestor B (last agent content)
			var bLines []string
			lastPath := t.lastPath(path)
			bBytes, err := os.ReadFile(lastPath)
			if err == nil {
				bLines = splitLines(string(bBytes))
			} else if os.IsNotExist(err) {
				if len(entries) > 0 && entries[len(entries)-1].Kind == Deleted {
					bLines = nil
				} else {
					bLines = aLines
				}
			} else {
				return err
			}

			// Get current content C
			cBytes, err := os.ReadFile(absPath)
			var cLines []string
			cExists := false
			if err == nil {
				cLines = splitLines(string(cBytes))
				cExists = true
			} else if !os.IsNotExist(err) {
				return err
			}

			// Re-run the merge logic to get the merged content.
			// Note: since conflict is false, this is guaranteed to merge cleanly.
			var merged []string
			if bLines == nil {
				// both deleted
				merged = nil
			} else if !cExists {
				// A is empty, so merged is also empty
				merged = nil
			} else {
				merged, _ = diff.Merge3(bLines, aLines, cLines)
			}

			// Determine if we need a trailing newline.
			hasTrailing := false
			if len(cBytes) > 0 && cBytes[len(cBytes)-1] == '\n' {
				hasTrailing = true
			} else if len(baseline.Content) > 0 && baseline.Content[len(baseline.Content)-1] == '\n' {
				hasTrailing = true
			}

			var newContent string
			if len(merged) > 0 {
				newContent = strings.Join(merged, "\n")
				if hasTrailing {
					newContent += "\n"
				}
			}

			if newContent == "" {
				if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove file: %w", err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
					return fmt.Errorf("failed to create directory for revert: %w", err)
				}
				if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
					return fmt.Errorf("failed to write merged file: %w", err)
				}
			}
		}
	}

	// Clean up journal file
	journalPath := t.journalPath(path)
	if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove journal file: %w", err)
	}

	// Clean up .last file
	lastPath := t.lastPath(path)
	if err := os.Remove(lastPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove .last file: %w", err)
	}

	// Delete from resource store
	if err := t.store.DeleteByKey(ctx, t.sessionID, "file_change", path); err != nil {
		return fmt.Errorf("failed to delete resource records: %w", err)
	}
	_ = t.store.DeleteByKey(ctx, t.sessionID, "file_hash", path)
	_ = t.store.DeleteByKey(ctx, t.sessionID, "file_read", path)

	// Remove from touched map
	delete(t.touched, path)

	return nil
}

func (t *tracker) CheckConflict(ctx context.Context, path string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.checkConflictLocked(ctx, path)
}

func (t *tracker) RecordRead(ctx context.Context, path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	absPath := filepath.Join(t.workspaceDir, path)
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	hashVal := fmt.Sprintf("%x", sha256.Sum256(data))

	// Delete older file_read resources to avoid piling up
	_ = t.store.DeleteByKey(ctx, t.sessionID, "file_read", path)

	res := resource.Resource{
		SessionID: t.sessionID,
		Kind:      "file_read",
		Key:       path,
		Data:      hashVal,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := t.store.Insert(ctx, res); err != nil {
		return fmt.Errorf("failed to record file read: %w", err)
	}
	return nil
}

func (t *tracker) IsKnown(ctx context.Context, path string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Query file_hash and file_read
	hashes, err := t.store.QueryByKey(ctx, t.sessionID, "file_hash", path)
	if err != nil {
		return false, err
	}
	reads, err := t.store.QueryByKey(ctx, t.sessionID, "file_read", path)
	if err != nil {
		return false, err
	}

	var lastRes *resource.Resource
	for _, r := range hashes {
		if lastRes == nil || r.CreatedAt.After(lastRes.CreatedAt) {
			rCopy := r
			lastRes = &rCopy
		}
	}
	for _, r := range reads {
		if lastRes == nil || r.CreatedAt.After(lastRes.CreatedAt) {
			rCopy := r
			lastRes = &rCopy
		}
	}

	if lastRes == nil {
		// Agent has never read or written this file in this session
		return false, nil
	}

	lastHash := lastRes.Data

	absPath := filepath.Join(t.workspaceDir, path)
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return lastHash == "deleted", nil
		}
		return false, err
	}

	currentHash := fmt.Sprintf("%x", sha256.Sum256(data))
	return currentHash == lastHash, nil
}

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
