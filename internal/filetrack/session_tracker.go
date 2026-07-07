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

type resourceData struct {
	ToolName   string     `json:"tool"`
	ChangeKind ChangeKind `json:"change_kind"`
	Additions  int        `json:"additions"`
	Deletions  int        `json:"deletions"`
}

type tracker struct {
	store         *resource.Store
	sessionID     string
	changesDir    string
	workspaceDir  string
	touched       map[string]bool
	mu            sync.Mutex
	globalTracker WorkspaceTracker

	expectedWrite map[string]string // path -> hash
	unsubChan     func()
}

// NewTracker creates a new session-scoped FileTracker instance.
func NewTracker(store *resource.Store, sessionID, changesDir, workspaceDir string, globalTracker WorkspaceTracker) FileTracker {
	t := &tracker{
		store:         store,
		sessionID:     sessionID,
		changesDir:    changesDir,
		workspaceDir:  workspaceDir,
		touched:       make(map[string]bool),
		globalTracker: globalTracker,
		expectedWrite: make(map[string]string),
	}

	if globalTracker != nil {
		globalTracker.RegisterSessionTracker(sessionID, t)
		ch, unsub := globalTracker.SubscribeSession(sessionID)
		t.unsubChan = unsub
		go t.watchFsChanges(ch)
	}

	return t
}

func normalizePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	path = strings.TrimPrefix(path, "./")
	return path
}

func (t *tracker) journalPath(relPath string) string {
	relPath = normalizePath(relPath)
	h := sha256.Sum256([]byte(relPath))
	return filepath.Join(t.changesDir, fmt.Sprintf("%x.jsonl", h[:8]))
}

func (t *tracker) lastPath(relPath string) string {
	relPath = normalizePath(relPath)
	h := sha256.Sum256([]byte(relPath))
	return filepath.Join(t.changesDir, fmt.Sprintf("%x.last", h[:8]))
}

func (t *tracker) Record(ctx context.Context, change Change, diff string, oldContent string) error {
	change.Path = normalizePath(change.Path)
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

	// Notify the global WorkspaceTracker if present
	if t.globalTracker != nil {
		t.globalTracker.RegisterInterest(t.sessionID, change.Path)
		t.globalTracker.NotifyTouch(change.Path, true)
		t.globalTracker.Publish(ctx, FileEvent{
			Path:      change.Path,
			Kind:      change.Kind,
			Hash:      hashVal,
			Source:    t.sessionID,
			Timestamp: now,
		})
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
				switch e.Op {
				case diff.OpInsert:
					additions++
				case diff.OpDelete:
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
	path = normalizePath(path)
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
		return false, nil
	}

	baseline := entries[0]
	if baseline.IsBinary {
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

	if bLines == nil {
		if cExists {
			return true, nil
		}
		return false, nil
	}

	if !cExists {
		if len(aLines) > 0 {
			return true, nil
		}
		return false, nil
	}

	_, hasConflict := diff.Merge3(bLines, aLines, cLines)
	return hasConflict, nil
}

func (t *tracker) RevertToBaseline(ctx context.Context, path string, force bool) error {
	path = normalizePath(path)
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
			conflict, err := t.checkConflictLocked(ctx, path)
			if err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("conflict")
			}

			aLines := splitLines(baseline.Content)

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

			cBytes, err := os.ReadFile(absPath)
			var cLines []string
			cExists := false
			if err == nil {
				cLines = splitLines(string(cBytes))
				cExists = true
			} else if !os.IsNotExist(err) {
				return err
			}

			var merged []string
			if bLines == nil {
				merged = nil
			} else if !cExists {
				merged = nil
			} else {
				merged, _ = diff.Merge3(bLines, aLines, cLines)
			}

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

	journalPath := t.journalPath(path)
	if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove journal file: %w", err)
	}

	lastPath := t.lastPath(path)
	if err := os.Remove(lastPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove .last file: %w", err)
	}

	if err := t.store.DeleteByKey(ctx, t.sessionID, "file_change", path); err != nil {
		return fmt.Errorf("failed to delete resource records: %w", err)
	}
	_ = t.store.DeleteByKey(ctx, t.sessionID, "file_hash", path)
	_ = t.store.DeleteByKey(ctx, t.sessionID, "file_read", path)

	delete(t.touched, path)

	return nil
}

func (t *tracker) CheckConflict(ctx context.Context, path string) (bool, error) {
	path = normalizePath(path)
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.checkConflictLocked(ctx, path)
}

func (t *tracker) RecordRead(ctx context.Context, path string) error {
	path = normalizePath(path)
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

	// Notify the global WorkspaceTracker if present
	if t.globalTracker != nil {
		t.globalTracker.RegisterInterest(t.sessionID, path)
		t.globalTracker.NotifyTouch(path, false)
	}

	return nil
}

func (t *tracker) IsKnown(ctx context.Context, path string) (bool, error) {
	path = normalizePath(path)
	t.mu.Lock()
	defer t.mu.Unlock()

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

func (t *tracker) ExpectWrite(path string, hash string) {
	path = normalizePath(path)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expectedWrite[path] = hash
}

func (t *tracker) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.globalTracker != nil {
		t.globalTracker.UnregisterSessionTracker(t.sessionID)
	}
	if t.unsubChan != nil {
		t.unsubChan()
		t.unsubChan = nil
	}
	return nil
}

func (t *tracker) watchFsChanges(ch <-chan FileEvent) {
	for event := range ch {
		if event.Source != "watcher" {
			continue
		}
		event.Path = normalizePath(event.Path)

		if event.Kind == Deleted {
			// Wait 200ms to let any atomic safe-write rename loops finish
			time.Sleep(200 * time.Millisecond)
			absPath := filepath.Join(t.workspaceDir, event.Path)
			if _, err := os.Stat(absPath); err == nil {
				continue
			}
		}

		ctx := context.Background()

		t.mu.Lock()
		expectedHash := t.expectedWrite[event.Path]
		t.mu.Unlock()

		hashes, err := t.store.QueryByKey(ctx, t.sessionID, "file_hash", event.Path)
		var lastHash string
		if err == nil && len(hashes) > 0 {
			var lastRes *resource.Resource
			for _, r := range hashes {
				if lastRes == nil || r.CreatedAt.After(lastRes.CreatedAt) {
					rCopy := r
					lastRes = &rCopy
				}
			}
			if lastRes != nil {
				lastHash = lastRes.Data
			}
		}

		if (expectedHash != "" && event.Hash == expectedHash) || (lastHash != "" && event.Hash == lastHash) {
			t.mu.Lock()
			if expectedHash != "" && event.Hash == expectedHash {
				delete(t.expectedWrite, event.Path)
			}
			t.mu.Unlock()
			continue
		}

		_ = t.recordExternalChange(ctx, event)
	}
}

func (t *tracker) recordExternalChange(ctx context.Context, event FileEvent) error {
	event.Path = normalizePath(event.Path)
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := os.MkdirAll(t.changesDir, 0755); err != nil {
		return err
	}

	journalPath := t.journalPath(event.Path)
	lastPath := t.lastPath(event.Path)
	absPath := filepath.Join(t.workspaceDir, event.Path)

	now := time.Now().UTC()

	touched := t.touched[event.Path]
	if !touched {
		if info, err := os.Stat(journalPath); err == nil && info.Size() > 0 {
			touched = true
			t.touched[event.Path] = true
		}
	}

	isBinary := false
	if event.Kind != Deleted {
		mimeType := fs.DetectMIMEType(absPath)
		isBinary = fs.IsBinaryMIME(mimeType)
	}

	var oldContent string
	if !isBinary {
		if oldBytes, err := os.ReadFile(lastPath); err == nil {
			oldContent = string(oldBytes)
		}
	}

	var newContent string
	if event.Kind != Deleted {
		if newBytes, err := os.ReadFile(absPath); err == nil {
			newContent = string(newBytes)
		}
	}

	f, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

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
		baselineBytes, _ := json.Marshal(baselineEntry)
		_, _ = f.Write(append(baselineBytes, '\n'))
		t.touched[event.Path] = true
	}

	var additions, deletions int
	var diffStr string
	if !isBinary && event.Kind != Deleted {
		diffStr = diff.FormatUnified(event.Path, event.Path, oldContent, newContent)
		for l := range strings.SplitSeq(diffStr, "\n") {
			if strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") {
				continue
			}
			if strings.HasPrefix(l, "+") {
				additions++
			} else if strings.HasPrefix(l, "-") {
				deletions++
			}
		}
	}

	toolName := "external"

	changeEntry := JournalEntry{
		Timestamp: now,
		ToolName:  toolName,
		Kind:      event.Kind,
		Additions: additions,
		Deletions: deletions,
		Diff:      diffStr,
	}
	changeBytes, _ := json.Marshal(changeEntry)
	_, _ = f.Write(append(changeBytes, '\n'))

	rd := resourceData{
		ToolName:   toolName,
		ChangeKind: event.Kind,
		Additions:  additions,
		Deletions:  deletions,
	}
	rdBytes, _ := json.Marshal(rd)
	res := resource.Resource{
		SessionID: t.sessionID,
		Kind:      "file_change",
		Key:       event.Path,
		Data:      string(rdBytes),
		CreatedAt: now,
	}
	_, _ = t.store.Insert(ctx, res)

	if event.Kind == Deleted {
		_ = os.Remove(lastPath)
	} else if !isBinary {
		_ = os.WriteFile(lastPath, []byte(newContent), 0644)
	}

	_ = t.store.DeleteByKey(ctx, t.sessionID, "file_hash", event.Path)
	resHash := resource.Resource{
		SessionID: t.sessionID,
		Kind:      "file_hash",
		Key:       event.Path,
		Data:      event.Hash,
		CreatedAt: now,
	}
	_, _ = t.store.Insert(ctx, resHash)

	return nil
}
