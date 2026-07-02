package autocomplete

import "context"

// Source defines the interface that all autocomplete completion sources must satisfy.
type Source interface {
	// Name returns the unique identifier of the source.
	Name() string

	// Query searches the source for autocomplete matches based on the query string.
	Query(ctx context.Context, query string) ([]Item, error)
}

// QueryReq represents the query and active sources requested for autocompletion.
type QueryReq struct {
	Query   string
	Sources []string
}
