package autocomplete

import "context"

// Source defines the interface that all autocomplete completion sources must satisfy.
type Source interface {
	// Name returns the unique identifier of the source.
	Name() string

	// Query searches the source for autocomplete matches based on the QueryReq.
	Query(ctx context.Context, req QueryReq) ([]Item, error)
}

// QueryReq represents the query and active sources requested for autocompletion.
type QueryReq struct {
	Query     string
	Sources   []string
	SessionID string // Needed for session-scoped skill queries
}
