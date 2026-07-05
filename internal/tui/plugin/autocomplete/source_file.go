package autocomplete

import (
	"context"
	"path/filepath"
)

// FileSearchResult represents a match found by a file search query.
type FileSearchResult struct {
	Path      string
	ShortPath string // Pre-computed shortest unique suffix for autocomplete
	IsDir     bool
}

// FileSource implements the Source interface for file completions.
type FileSource struct {
	queryFn func(query string) []FileSearchResult
}

// NewFileSource instantiates a new FileSource with a query function.
func NewFileSource(queryFn func(query string) []FileSearchResult) *FileSource {
	return &FileSource{
		queryFn: queryFn,
	}
}

// Name returns the identifier for this source.
func (s *FileSource) Name() string {
	return "file"
}

// Query performs a file search using the provided query function and maps the results to Items.
func (s *FileSource) Query(ctx context.Context, req QueryReq) ([]Item, error) {
	results := s.queryFn(req.Query)
	var items []Item

	for _, r := range results {
		badge := "FILE "
		kind := "file"
		insertVal := "@file:" + r.ShortPath
		if r.IsDir {
			badge = "DIR  "
			kind = "directory"
			insertVal = "@file:" + r.ShortPath + "/"
		}

		items = append(items, Item{
			ID:          r.Path,
			Label:       filepath.Base(r.Path),
			Sublabel:    filepath.Dir(r.Path),
			Badge:       badge,
			Kind:        kind,
			InsertValue: insertVal,
		})
	}

	return items, nil
}
