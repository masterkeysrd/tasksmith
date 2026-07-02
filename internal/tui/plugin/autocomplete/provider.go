package autocomplete

import (
	"context"

	"github.com/masterkeysrd/kite/style"
)

// Item represents a single selectable option in the autocomplete menu.
type Item struct {
	ID          string      // Unique identifier (e.g., file path, command name)
	Label       string      // Main text displayed in the menu
	Sublabel    string      // Secondary detail (e.g. file directory, symbol signature)
	Badge       string      // Visual category tag (e.g., "FILE", "LSP", "CMD", "SKILL")
	BadgeStyle  style.Style // Visual styling for the category badge
	Kind        string      // Semantic kind (e.g. "file", "struct", "function", "module")
	InsertValue string      // Value injected into the text input upon selection
	Value       any         // Arbitrary metadata associated with this item
}

// Provider defines the interface that all completion sources must satisfy.
type Provider interface {
	// Name returns the identifier of the provider.
	Name() string

	// Query searches the provider's resource for matches based on the input query.
	// The query excludes trigger characters.
	Query(ctx context.Context, query string) ([]Item, error)
}
