package autocomplete

import (
	"context"
	"strings"
)

// DummyProvider implements Provider to return mockup autocompletions for testing.
type DummyProvider struct {
	Items []Item
}

// NewDummyProvider instantiates a populated DummyProvider.
func NewDummyProvider() *DummyProvider {
	return &DummyProvider{
		Items: []Item{
			{ID: "1", Label: "main.go", Sublabel: "cmd/tasksmith/", Badge: "FILE", Kind: "file", InsertValue: "@file:main.go "},
			{ID: "2", Label: "app.go", Sublabel: "internal/tui/", Badge: "FILE", Kind: "file", InsertValue: "@file:app.go "},
			{ID: "3", Label: "view.go", Sublabel: "internal/tui/views/chat/", Badge: "FILE", Kind: "file", InsertValue: "@file:view.go "},
			{ID: "4", Label: "UseCommand", Sublabel: "func(string)", Badge: "LSP", Kind: "function", InsertValue: "@sym:UseCommand "},
			{ID: "5", Label: "ComposerProps", Sublabel: "struct", Badge: "LSP", Kind: "struct", InsertValue: "@sym:ComposerProps "},
			{ID: "6", Label: "agent-tooling", Sublabel: "agent core skills", Badge: "SKILL", Kind: "module", InsertValue: "@skill:agent-tooling "},
			{ID: "7", Label: "/goal", Sublabel: "Run autonomous task", Badge: "CMD", Kind: "constant", InsertValue: "/goal "},
			{ID: "8", Label: "/grill-me", Sublabel: "Align on a plan", Badge: "CMD", Kind: "constant", InsertValue: "/grill-me "},
		},
	}
}

func (p *DummyProvider) Name() string {
	return "dummy"
}

func (p *DummyProvider) Query(ctx context.Context, query string) ([]Item, error) {
	if query == "" {
		return p.Items, nil
	}
	var res []Item
	q := strings.ToLower(query)
	for _, item := range p.Items {
		if strings.Contains(strings.ToLower(item.Label), q) || strings.Contains(strings.ToLower(item.Sublabel), q) {
			res = append(res, item)
		}
	}
	return res, nil
}
