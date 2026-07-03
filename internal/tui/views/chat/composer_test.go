package chat

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
)

func TestResourceTypeFromKind(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want resolver.ResourceType
	}{
		{"lowercase file", "file", resolver.TypeFile},
		{"uppercase FILE", "FILE", resolver.TypeFile},
		{"function", "function", resolver.TypeSymbol},
		{"struct", "struct", resolver.TypeSymbol},
		{"method", "method", resolver.TypeSymbol},
		{"variable", "variable", resolver.TypeSymbol},
		{"lowercase lsp", "lsp", resolver.TypeSymbol},
		{"uppercase LSP", "LSP", resolver.TypeSymbol},
		{"lowercase skill", "skill", resolver.TypeSkill},
		{"uppercase SKILL", "SKILL", resolver.TypeSkill},
		{"unknown kind defaults to file", "unknown", resolver.TypeFile},
		{"empty kind defaults to file", "", resolver.TypeFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resourceTypeFromKind(tt.kind)
			if got != tt.want {
				t.Errorf("resourceTypeFromKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
