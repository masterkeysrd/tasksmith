package std

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/core/ripgrep"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

const (
	defaultSearchLimit = 50
	maxSearchLimit     = 200
	maxIndexEntries    = 100000
	debounceInterval   = 150 * time.Millisecond
)

// FileIndexOptions configures a workspace FileIndex.
type FileIndexOptions struct {
	Std       *Std
	AllowExec bool
	Lifecycle context.Context
}

// SearchOptions specifies parameters for fuzzy file searching and ranking.
type SearchOptions struct {
	Query         string
	Limit         int
	RecentPaths   []string
	IncludeHidden *bool // Defaults to true (excluding .git and .env secrets)
}

// FileIndex maintains an in-memory, thread-safe, watcher-backed file and directory index
// for fuzzy '@' context mentions in the workspace.
type FileIndex struct {
	std       *Std
	allowExec atomic.Bool

	mu      sync.RWMutex
	files   []api.WorkspaceFile
	byPath  map[string]api.WorkspaceFile
	indexed bool

	watcher   sandbox.Watcher
	cancel    context.CancelFunc
	closed    atomic.Bool
	refreshCh chan struct{}
}

// NewFileIndex constructs and initializes a new thread-safe FileIndex.
func NewFileIndex(opts FileIndexOptions) (*FileIndex, error) {
	if opts.Std == nil {
		return nil, errors.New("fileindex: std is required")
	}

	ctx := opts.Lifecycle
	if ctx == nil {
		ctx = context.Background()
	}
	idxCtx, cancel := context.WithCancel(ctx)

	idx := &FileIndex{
		std:       opts.Std,
		byPath:    make(map[string]api.WorkspaceFile),
		cancel:    cancel,
		refreshCh: make(chan struct{}, 1),
	}
	idx.allowExec.Store(opts.AllowExec)

	// Initial scan
	_ = idx.Refresh(idxCtx)

	// Start background watcher if available
	w, err := opts.Std.Watch(idxCtx, WatchRequest{Path: ".", Recursive: true})
	if err == nil {
		idx.watcher = w
		go idx.watchLoop(idxCtx)
	}

	return idx, nil
}

// Close terminates background watchers and index goroutines.
func (idx *FileIndex) Close() error {
	if !idx.closed.CompareAndSwap(false, true) {
		return nil
	}
	idx.cancel()
	if idx.watcher != nil {
		return idx.watcher.Close()
	}
	return nil
}

// SetAllowExec updates whether external discovery binaries (git, rg) may be spawned.
func (idx *FileIndex) SetAllowExec(allow bool) {
	idx.allowExec.Store(allow)
}

// Invalidate triggers an asynchronous refresh of the in-memory index.
func (idx *FileIndex) Invalidate() {
	select {
	case idx.refreshCh <- struct{}{}:
	default:
	}
}

func (idx *FileIndex) watchLoop(ctx context.Context) {
	timer := time.NewTimer(debounceInterval)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-idx.watcher.Events():
			if !ok {
				return
			}
			if timerActive {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			timer.Reset(debounceInterval)
			timerActive = true
		case _, ok := <-idx.watcher.Errors():
			if !ok {
				return
			}
		case <-timer.C:
			timerActive = false
			_ = idx.Refresh(ctx)
		case <-idx.refreshCh:
			_ = idx.Refresh(ctx)
		}
	}
}

// Refresh re-scans the workspace filesystem and updates in-memory entries.
func (idx *FileIndex) Refresh(ctx context.Context) error {
	if idx.closed.Load() {
		return sandbox.ErrSandboxClosed
	}

	discovered, err := idx.discoverFiles(ctx)
	if err != nil {
		return err
	}

	// Build map and list of files + derived directories
	byPath := make(map[string]api.WorkspaceFile, len(discovered)*2)
	for _, p := range discovered {
		clean := filepath.ToSlash(filepath.Clean(p))
		if clean == "." || clean == "" {
			continue
		}

		// Ensure all parent directories exist
		parts := strings.Split(clean, "/")
		for i := 1; i < len(parts); i++ {
			dirPath := strings.Join(parts[:i], "/")
			if _, exists := byPath[dirPath]; !exists {
				parent := ""
				if i > 1 {
					parent = strings.Join(parts[:i-1], "/")
				}
				byPath[dirPath] = api.WorkspaceFile{
					Path:    dirPath,
					Name:    parts[i-1],
					Dir:     parent,
					IsDir:   true,
					Allowed: true,
				}
			}
		}

		parent := ""
		if len(parts) > 1 {
			parent = strings.Join(parts[:len(parts)-1], "/")
		}
		byPath[clean] = api.WorkspaceFile{
			Path:    clean,
			Name:    parts[len(parts)-1],
			Dir:     parent,
			IsDir:   false,
			Allowed: true,
		}
	}

	all := make([]api.WorkspaceFile, 0, len(byPath))
	for _, wf := range byPath {
		all = append(all, wf)
	}
	slices.SortFunc(all, func(a, b api.WorkspaceFile) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})

	// Compute ShortPath for every entry: shortest unique slash-delimited suffix.
	suffixCounts := make(map[string]int, len(all)*3)
	for _, wf := range all {
		parts := strings.Split(wf.Path, "/")
		for k := 1; k <= len(parts); k++ {
			cand := strings.Join(parts[len(parts)-k:], "/")
			if wf.IsDir {
				cand += "/"
			}
			suffixCounts[cand]++
		}
	}

	for i := range all {
		parts := strings.Split(all[i].Path, "/")
		var chosen string
		for k := 1; k <= len(parts); k++ {
			cand := strings.Join(parts[len(parts)-k:], "/")
			if all[i].IsDir {
				cand += "/"
			}
			if suffixCounts[cand] == 1 {
				chosen = cand
				break
			}
		}
		if chosen == "" {
			chosen = all[i].Path
			if all[i].IsDir && !strings.HasSuffix(chosen, "/") {
				chosen += "/"
			}
		}
		all[i].ShortPath = chosen
		byPath[all[i].Path] = all[i]
	}

	idx.mu.Lock()
	idx.files = all
	idx.byPath = byPath
	idx.indexed = true
	idx.mu.Unlock()

	return nil
}

func (idx *FileIndex) discoverFiles(ctx context.Context) ([]string, error) {
	if idx.allowExec.Load() {
		// 1. Try git ls-files
		if files, ok := idx.tryGitLsFiles(ctx); ok {
			return files, nil
		}
		// 2. Try rg --files
		if files, ok := idx.tryRgFiles(ctx); ok {
			return files, nil
		}
	}

	// 3. Pure-Go walk fallback (or default for restricted mode)
	return idx.pureGoWalk(ctx)
}

func (idx *FileIndex) tryGitLsFiles(ctx context.Context) ([]string, bool) {
	sb := idx.std.Sandbox()
	if sb == nil {
		return nil, false
	}

	res, err := sb.Proc().Exec(ctx, sandbox.Command{
		Name: "git",
		Args: []string{
			"--no-optional-locks",
			"-c", "core.fsmonitor=false",
			"-c", "safe.directory=*",
			"ls-files", "-z",
			"--cached", "--others", "--exclude-standard",
		},
	})
	if err != nil || res.ExitCode != 0 {
		return nil, false
	}

	rawPaths := bytes.Split(res.Stdout, []byte{0})
	candidates := make([]string, 0, len(rawPaths))
	for _, raw := range rawPaths {
		if len(raw) > 0 {
			candidates = append(candidates, filepath.ToSlash(string(raw)))
		}
	}

	return filterDiscoveredPaths(ctx, sb, candidates), true
}

func (idx *FileIndex) tryRgFiles(ctx context.Context) ([]string, bool) {
	sb := idx.std.Sandbox()
	if sb == nil || idx.std.rg == nil {
		return nil, false
	}

	candidates, err := idx.std.rg.Glob(ctx, ".", "", ripgrep.GlobOptions{IncludeHidden: true})
	if err != nil {
		return nil, false
	}

	return filterDiscoveredPaths(ctx, sb, candidates), true
}

func filterDiscoveredPaths(ctx context.Context, sb sandbox.Sandbox, candidates []string) []string {
	out := make([]string, 0, len(candidates))
	for _, p := range candidates {
		if p == "" || isIgnoredFile(p) || !isSafeFileEntry(ctx, sb, p) {
			continue
		}
		out = append(out, p)
		if len(out) >= maxIndexEntries {
			break
		}
	}
	return out
}

func isSafeFileEntry(ctx context.Context, sb sandbox.Sandbox, p string) bool {
	info, err := sb.FS().Lstat(ctx, p)
	if err != nil {
		return false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		// Ensure symlink does not escape WorkDir.
		if _, err := sb.FS().Stat(ctx, p); err != nil {
			return false
		}
	}
	return true
}

func (idx *FileIndex) pureGoWalk(ctx context.Context) ([]string, error) {
	sb := idx.std.Sandbox()
	if sb == nil {
		return nil, sandbox.ErrSandboxClosed
	}

	var out []string
	queue := []string{"."}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		curr := queue[0]
		queue = queue[1:]

		entries, err := sb.FS().ReadDir(ctx, curr)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			name := entry.Name()
			child := name
			if curr != "." {
				child = filepath.ToSlash(filepath.Join(curr, name))
			}

			if isIgnoredFile(child) {
				continue
			}

			if entry.IsDir() {
				if slices.Contains(DefaultWatchIgnoreDirs, name) {
					continue
				}
				queue = append(queue, child)
				continue
			}

			if !isSafeFileEntry(ctx, sb, child) {
				continue
			}

			out = append(out, child)
			if len(out) >= maxIndexEntries {
				return out, nil
			}
		}
	}

	return out, nil
}

func isIgnoredFile(path string) bool {
	if guard.IsProtectedPath(path, "", ".git", ".bare") {
		return true
	}
	for part := range strings.SplitSeq(path, "/") {
		if guard.IsSecretEnvFile(part) {
			return true
		}
	}
	return false
}

// Search queries the file index using fuzzy matching and MRU ranking.
func (idx *FileIndex) Search(ctx context.Context, opts SearchOptions) ([]api.WorkspaceFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	idx.mu.RLock()
	candidates := make([]api.WorkspaceFile, len(idx.files))
	copy(candidates, idx.files)
	idx.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	} else if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	includeHidden := true
	if opts.IncludeHidden != nil {
		includeHidden = *opts.IncludeHidden
	}

	recentMap := make(map[string]int, len(opts.RecentPaths))
	for i, p := range opts.RecentPaths {
		norm := filepath.ToSlash(filepath.Clean(p))
		recentMap[norm] = i
	}

	query := strings.TrimSpace(opts.Query)
	type scoredItem struct {
		item  api.WorkspaceFile
		score int
	}

	var matched []scoredItem

	if query == "" {
		for _, f := range candidates {
			if !includeHidden && strings.HasPrefix(f.Name, ".") {
				continue
			}
			score := 1000 - pathDepth(f.Path)*10
			if idx, ok := recentMap[f.Path]; ok {
				score = 100000 - idx*100
			}
			matched = append(matched, scoredItem{item: f, score: score})
		}
	} else {
		smartCase := hasUpper(query)
		qNorm := query
		if !smartCase {
			qNorm = strings.ToLower(query)
		}
		qParts := strings.Split(qNorm, "/")

		for _, f := range candidates {
			if !includeHidden && strings.HasPrefix(f.Name, ".") {
				continue
			}

			pathNorm := f.Path
			nameNorm := f.Name
			if !smartCase {
				pathNorm = strings.ToLower(f.Path)
				nameNorm = strings.ToLower(f.Name)
			}

			score := calculateScore(f, pathNorm, nameNorm, qNorm, qParts)
			if score <= 0 {
				continue
			}

			// Boosts and tie-breakers
			if idx, ok := recentMap[f.Path]; ok {
				score += 500 - idx*10
			}

			if !strings.Contains(qNorm, "test") &&
				(strings.HasSuffix(f.Name, "_test.go") || strings.Contains(f.Path, "_test")) {
				score -= 200
			}

			score -= pathDepth(f.Path) * 10
			matched = append(matched, scoredItem{item: f, score: score})
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].item.Path < matched[j].item.Path
	})

	if len(matched) > limit {
		matched = matched[:limit]
	}

	result := make([]api.WorkspaceFile, len(matched))
	for i, sm := range matched {
		item := sm.item
		if _, ok := recentMap[item.Path]; ok {
			item.Recent = true
		}
		result[i] = item
	}

	return result, nil
}

func calculateScore(f api.WorkspaceFile, pathNorm, nameNorm, qNorm string, qParts []string) int {
	// 1. Exact matches
	if pathNorm == qNorm {
		return 10000
	}
	if nameNorm == qNorm {
		return 9000
	}

	// 2. Prefix matches
	if strings.HasPrefix(nameNorm, qNorm) {
		return 7000
	}
	if strings.HasPrefix(pathNorm, qNorm) {
		return 6000
	}

	// 3. Path segment prefix match
	if len(qParts) > 1 {
		pParts := strings.Split(pathNorm, "/")
		if matchSegmentPrefix(pParts, qParts) {
			return 5500
		}
	}

	// 4. Contiguous substring
	if strings.Contains(nameNorm, qNorm) {
		return 4000
	}
	if strings.Contains(pathNorm, qNorm) {
		return 3000
	}

	// 5. Fuzzy subsequence match
	if subScore, ok := matchSubsequence(f.Name, nameNorm, qNorm); ok {
		return 2000 + subScore
	}
	if subScore, ok := matchSubsequence(f.Path, pathNorm, qNorm); ok {
		return 1000 + subScore
	}

	return 0
}

func matchSegmentPrefix(pParts, qParts []string) bool {
	if len(pParts) < len(qParts) {
		return false
	}
	for i := 0; i <= len(pParts)-len(qParts); i++ {
		match := true
		for j := range qParts {
			if !strings.HasPrefix(pParts[i+j], qParts[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func matchSubsequence(origTarget, target, query string) (int, bool) {
	if len(query) == 0 {
		return 0, true
	}
	if len(target) < len(query) {
		return 0, false
	}

	origRunes := []rune(origTarget)
	tRunes := []rune(target)
	qRunes := []rune(query)
	if len(tRunes) < len(qRunes) {
		return 0, false
	}

	score := 0
	tIdx := 0
	qIdx := 0
	lastMatch := -1
	consecutive := 0

	for qIdx < len(qRunes) && tIdx < len(tRunes) {
		if tRunes[tIdx] == qRunes[qIdx] {
			score += 10
			// Word or camelCase boundary bonus
			isBoundary := tIdx == 0
			if !isBoundary {
				prev := tRunes[tIdx-1]
				if prev == '/' || prev == '_' || prev == '-' || prev == '.' || prev == ' ' {
					isBoundary = true
				} else if tIdx < len(origRunes) {
					prevOrig := origRunes[tIdx-1]
					currOrig := origRunes[tIdx]
					if (unicode.IsLower(prevOrig) && unicode.IsUpper(currOrig)) ||
						(unicode.IsDigit(prevOrig) && unicode.IsLetter(currOrig)) {
						isBoundary = true
					}
				}
			}
			if isBoundary {
				score += 50
			}
			// Consecutive bonus
			if lastMatch == tIdx-1 {
				consecutive++
				score += 20 * consecutive
			} else {
				consecutive = 0
				if lastMatch >= 0 {
					score -= (tIdx - lastMatch - 1) // Gap penalty
				}
			}
			lastMatch = tIdx
			qIdx++
		}
		tIdx++
	}

	if qIdx == len(qRunes) {
		return score, true
	}
	return 0, false
}

func pathDepth(path string) int {
	return strings.Count(path, "/")
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
