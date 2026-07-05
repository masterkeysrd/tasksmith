package filetrack

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	corefs "github.com/masterkeysrd/tasksmith/internal/core/fs"
	"github.com/masterkeysrd/tasksmith/internal/core/fuzzy"
)

type workspaceTracker struct {
	workspaceDir string
	watcher      *fsnotify.Watcher
	ignorer      corefs.Ignorer

	mu          sync.RWMutex
	files       map[string]bool            // Set of relative paths (using forward slashes) of all files in workspace
	dirs        map[string]bool            // Set of relative paths (using forward slashes) of all directories in workspace
	interests   map[string]map[string]bool // file -> set of sessionIDs
	subs        map[string]chan FileEvent  // sessionID -> event channel
	activeFiles []string                   // MRU active files list
	activeSet   map[string]bool            // Set of active files for fast lookup
	shortPaths  map[string]string          // full path -> shortest unique suffix

	done chan struct{}
	wg   sync.WaitGroup
}

// NewWorkspaceTracker creates a new WorkspaceTracker instance.
func NewWorkspaceTracker(workspaceDir string) WorkspaceTracker {
	ign, _ := corefs.NewIgnorer(workspaceDir)
	return &workspaceTracker{
		workspaceDir: workspaceDir,
		ignorer:      ign,
		files:        make(map[string]bool),
		dirs:         make(map[string]bool),
		interests:    make(map[string]map[string]bool),
		subs:         make(map[string]chan FileEvent),
		activeSet:    make(map[string]bool),
		shortPaths:   make(map[string]string),
		done:         make(chan struct{}),
	}
}

func (w *workspaceTracker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	w.watcher = watcher

	// Initial scan and watch directory tree
	if err := w.scanAndWatch(w.workspaceDir); err != nil {
		watcher.Close()
		return err
	}

	w.wg.Add(1)
	go w.watchLoop()

	return nil
}

func (w *workspaceTracker) scanAndWatch(root string) error {
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		name := filepath.Base(path)
		if w.ignorer != nil && w.ignorer.ShouldIgnore(name, path, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			if err := w.watcher.Add(path); err != nil {
				return fmt.Errorf("failed to watch directory %s: %w", path, err)
			}
			rel, err := filepath.Rel(w.workspaceDir, path)
			if err == nil && rel != "." && rel != "" {
				relNorm := filepath.ToSlash(rel)
				w.dirs[relNorm] = true
			}
		} else {
			rel, err := filepath.Rel(w.workspaceDir, path)
			if err == nil {
				relNorm := filepath.ToSlash(rel)
				w.files[relNorm] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// After initial scan, compute short paths for all files
	w.recomputeShortPathsLocked()
	return nil
}

// recomputeShortPathsLocked recomputes the shortest unique suffix for every
// file in the index. Must be called while w.mu is held.
func (w *workspaceTracker) recomputeShortPathsLocked() {
	// Collect all file paths into a slice
	var allFiles []string
	for path := range w.files {
		allFiles = append(allFiles, path)
	}

	// Clear existing short paths
	w.shortPaths = make(map[string]string)

	// Compute shortest unique suffix for each file
	for _, path := range allFiles {
		w.shortPaths[path] = ShortestUniqueSuffix(path, allFiles)
	}
}

func (w *workspaceTracker) watchLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			_ = err // Ignore or silently drop errors
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleFsEvent(event)
		}
	}
}

func (w *workspaceTracker) handleFsEvent(event fsnotify.Event) {
	name := filepath.Base(event.Name)
	isDir := false
	info, errStat := os.Stat(event.Name)
	if errStat == nil {
		isDir = info.IsDir()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Dynamic reload: if a .gitignore file is updated, re-create the ignorer
	if name == ".gitignore" {
		ign, err := corefs.NewIgnorer(w.workspaceDir)
		if err == nil {
			w.ignorer = ign
		}
	}

	// Filter events using the standard filesystem ignorer
	if w.ignorer != nil && w.ignorer.ShouldIgnore(name, event.Name, isDir) {
		return
	}

	rel, err := filepath.Rel(w.workspaceDir, event.Name)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)

	var kind ChangeKind
	var hash string

	// Handle Create
	if event.Has(fsnotify.Create) {
		if errStat == nil {
			if isDir {
				_ = w.scanAndWatch(event.Name)
				w.dirs[rel] = true
				return
			} else {
				w.files[rel] = true
				// Ensure parent directory is also recorded
				parent := filepath.Dir(rel)
				if parent != "." && parent != "/" && parent != "" {
					w.dirs[parent] = true
				}
				kind = Created
				hash = w.computeHashLocked(event.Name)
				// Recompute short paths since a new file was added
				w.recomputeShortPathsLocked()
			}
		}
	}

	// Handle Write (Modify)
	if event.Has(fsnotify.Write) {
		if errStat == nil && !isDir {
			w.files[rel] = true
			kind = Modified
			if len(w.interests[rel]) > 0 {
				hash = w.computeHashLocked(event.Name)
			}
		}
	}

	// Handle Remove (Delete) or Rename
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		if w.dirs[rel] {
			delete(w.dirs, rel)
			prefix := rel + "/"
			for f := range w.files {
				if strings.HasPrefix(f, prefix) {
					delete(w.files, f)
				}
			}
			for d := range w.dirs {
				if strings.HasPrefix(d, prefix) {
					delete(w.dirs, d)
				}
			}
		} else {
			delete(w.files, rel)
		}
		_ = w.watcher.Remove(event.Name)
		kind = Deleted
		hash = "deleted"
		// Recompute short paths since files were removed/renamed
		w.recomputeShortPathsLocked()
	}

	if kind != "" {
		eventPayload := FileEvent{
			Path:      rel,
			Kind:      kind,
			Hash:      hash,
			Source:    "watcher",
			Timestamp: time.Now().UTC(),
		}
		w.publishLocked(context.Background(), eventPayload)
	}
}

func (w *workspaceTracker) computeHashLocked(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (w *workspaceTracker) publishLocked(_ context.Context, event FileEvent) {
	// Send to regular sessions matching registered interests
	sessionIDs := w.interests[event.Path]
	for sessionID := range sessionIDs {
		if ch, ok := w.subs[sessionID]; ok {
			select {
			case ch <- event:
			default:
			}
		}
	}

	// Send to wildcard subscribers (e.g. "wildcard:autocomplete")
	for id, ch := range w.subs {
		if strings.HasPrefix(id, "wildcard:") {
			select {
			case ch <- event:
			default:
			}
		}
	}
}

func (w *workspaceTracker) SubscribeSession(sessionID string) (<-chan FileEvent, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if oldCh, ok := w.subs[sessionID]; ok {
		close(oldCh)
	}

	ch := make(chan FileEvent, 100)
	w.subs[sessionID] = ch

	unsubscribe := func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if currCh, ok := w.subs[sessionID]; ok && currCh == ch {
			delete(w.subs, sessionID)
			close(ch)
		}

		if !strings.HasPrefix(sessionID, "wildcard:") {
			for path, sessions := range w.interests {
				if sessions[sessionID] {
					delete(sessions, sessionID)
					if len(sessions) == 0 {
						delete(w.interests, path)
					}
				}
			}
		}
	}

	return ch, unsubscribe
}

func (w *workspaceTracker) Publish(ctx context.Context, event FileEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.publishLocked(ctx, event)
}

func (w *workspaceTracker) RegisterInterest(sessionID, path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path = filepath.ToSlash(path)
	if w.interests[path] == nil {
		w.interests[path] = make(map[string]bool)
	}
	w.interests[path][sessionID] = true
}

func (w *workspaceTracker) UnregisterInterest(sessionID, path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path = filepath.ToSlash(path)
	if sessions := w.interests[path]; sessions != nil {
		delete(sessions, sessionID)
		if len(sessions) == 0 {
			delete(w.interests, path)
		}
	}
}

func (w *workspaceTracker) NotifyTouch(path string, isWrite bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path = filepath.ToSlash(path)
	w.activeSet[path] = true

	newActive := []string{path}
	for _, p := range w.activeFiles {
		if p != path {
			newActive = append(newActive, p)
		}
	}

	if len(newActive) > 50 {
		removed := newActive[50:]
		for _, rp := range removed {
			delete(w.activeSet, rp)
		}
		newActive = newActive[:50]
	}
	w.activeFiles = newActive
}

func (w *workspaceTracker) ActiveFiles() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	out := make([]string, len(w.activeFiles))
	copy(out, w.activeFiles)
	return out
}

func (w *workspaceTracker) Search(query string) []SearchResult {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if query == "" {
		// Return all active, inactive files, and directories in priority order
		var results []SearchResult
		for _, p := range w.activeFiles {
			results = append(results, SearchResult{Path: p, ShortPath: w.shortPaths[p], IsDir: false})
		}
		for path := range w.files {
			if !w.activeSet[path] {
				results = append(results, SearchResult{Path: path, ShortPath: w.shortPaths[path], IsDir: false})
			}
		}
		for path := range w.dirs {
			results = append(results, SearchResult{Path: path, IsDir: true})
		}
		return results
	}

	type scoredResult struct {
		res   SearchResult
		score int
	}
	var matches []scoredResult

	// Match files using fuzzy subsequence matcher
	for path := range w.files {
		if matched, score := fuzzy.Match(path, query); matched {
			// Boost open files
			if w.activeSet[path] {
				score += 50
			}
			matches = append(matches, scoredResult{
				res:   SearchResult{Path: path, ShortPath: w.shortPaths[path], IsDir: false},
				score: score,
			})
		}
	}

	// Match directories (with a trailing slash appended during matching)
	for path := range w.dirs {
		dirPath := path
		if !strings.HasSuffix(dirPath, "/") {
			dirPath += "/"
		}
		if matched, score := fuzzy.Match(dirPath, query); matched {
			matches = append(matches, scoredResult{
				res:   SearchResult{Path: path, IsDir: true},
				score: score - 5, // Slightly penalize directories vs direct files
			})
		}
	}

	// Sort matches by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	results := make([]SearchResult, 0, len(matches))
	for _, m := range matches {
		results = append(results, m.res)
	}
	return results
}

func (w *workspaceTracker) IsDir(path string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.dirs[path]
}

// ShortestUniqueSuffix computes the minimum path suffix that uniquely
// identifies a file within the index. It starts with the basename and
// progressively adds parent directories until the suffix is unique
// among all provided paths.
func ShortestUniqueSuffix(fullPath string, allPaths []string) string {
	basename := filepath.Base(fullPath)
	dir := filepath.Dir(fullPath)

	// Check if basename alone is unique
	basenameCount := 0
	for _, p := range allPaths {
		if filepath.Base(p) == basename {
			basenameCount++
		}
	}
	if basenameCount == 1 {
		return basename
	}

	// Build suffix by walking up from the file's directory
	parts := strings.Split(dir, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		suffix := strings.Join(parts[i:], "/") + "/" + basename
		matchCount := 0
		for _, p := range allPaths {
			if p == suffix || strings.HasSuffix(p, "/"+suffix) {
				matchCount++
			}
		}
		if matchCount == 1 {
			return suffix
		}
	}

	// Fallback: return the full path
	return fullPath
}

func (w *workspaceTracker) Stop() error {
	w.mu.Lock()
	select {
	case <-w.done:
		w.mu.Unlock()
		return nil
	default:
		close(w.done)
	}

	if w.watcher != nil {
		_ = w.watcher.Close()
	}
	w.mu.Unlock()

	w.wg.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ch := range w.subs {
		close(ch)
	}
	w.subs = make(map[string]chan FileEvent)

	return nil
}
