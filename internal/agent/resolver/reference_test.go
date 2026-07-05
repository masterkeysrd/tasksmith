package resolver

import (
	"reflect"
	"testing"
)

func TestExtractReferences(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		tracked  []Reference
		wantRefs []Reference
	}{
		{
			name:    "single @file: reference",
			text:    "Refactor @file:internal/app/app.go please",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "internal/app/app.go", StartLine: 1, EndLine: 0, InsertText: "@file:internal/app/app.go", FromTracker: false},
			},
		},
		{
			name:    "single @sym: reference",
			text:    "Check @sym:Resolver.ResolveFile signature",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeSymbol, Value: "Resolver.ResolveFile", StartLine: 0, EndLine: 0, InsertText: "@sym:Resolver.ResolveFile", FromTracker: false},
			},
		},
		{
			name:    "single @skill: reference",
			text:    "Use @skill:golang conventions here",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeSkill, Value: "golang", StartLine: 0, EndLine: 0, InsertText: "@skill:golang", FromTracker: false},
			},
		},
		{
			name:    "multiple references in one text",
			text:    "Refactor @file:internal/app/app.go and @sym:NewResolver",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "internal/app/app.go", StartLine: 1, EndLine: 0, InsertText: "@file:internal/app/app.go", FromTracker: false},
				{Type: TypeSymbol, Value: "NewResolver", StartLine: 0, EndLine: 0, InsertText: "@sym:NewResolver", FromTracker: false},
			},
		},
		{
			name:    "all three prefix types",
			text:    "Check @file:main.go @sym:Main and @skill:go",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "main.go", StartLine: 1, EndLine: 0, InsertText: "@file:main.go", FromTracker: false},
				{Type: TypeSymbol, Value: "Main", StartLine: 0, EndLine: 0, InsertText: "@sym:Main", FromTracker: false},
				{Type: TypeSkill, Value: "go", StartLine: 0, EndLine: 0, InsertText: "@skill:go", FromTracker: false},
			},
		},
		{
			name:     "empty text",
			text:     "",
			tracked:  nil,
			wantRefs: nil,
		},
		{
			name:     "no prefixes in text",
			text:     "just plain text",
			tracked:  nil,
			wantRefs: nil,
		},
		{
			name: "skip already tracked references",
			text: "Refactor @file:internal/app/app.go",
			tracked: []Reference{
				{Type: TypeFile, Value: "internal/app/app.go", StartLine: 1, EndLine: 0, InsertText: "@file:internal/app/app.go", FromTracker: true},
			},
			wantRefs: nil,
		},
		{
			name: "extract new refs not in tracked set",
			text: "Check @file:internal/app/app.go and @file:internal/agent/agent.go",
			tracked: []Reference{
				{Type: TypeFile, Value: "internal/app/app.go", StartLine: 1, EndLine: 0, InsertText: "@file:internal/app/app.go", FromTracker: true},
			},
			wantRefs: []Reference{
				{Type: TypeFile, Value: "internal/agent/agent.go", StartLine: 1, EndLine: 0, InsertText: "@file:internal/agent/agent.go", FromTracker: false},
			},
		},
		{
			name:    "prefix with empty value is skipped",
			text:    "Check @file: and @file:valid.go",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "valid.go", StartLine: 1, EndLine: 0, InsertText: "@file:valid.go", FromTracker: false},
			},
		},
		{
			name:    "prefix at end of text",
			text:    "do something @file:main.go",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "main.go", StartLine: 1, EndLine: 0, InsertText: "@file:main.go", FromTracker: false},
			},
		},
		{
			name:    "prefix at start of text",
			text:    "@file:main.go refactor this",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "main.go", StartLine: 1, EndLine: 0, InsertText: "@file:main.go", FromTracker: false},
			},
		},
		{
			name:    "multiple same prefix different values",
			text:    "Compare @file:a.go with @file:b.go",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "a.go", StartLine: 1, EndLine: 0, InsertText: "@file:a.go", FromTracker: false},
				{Type: TypeFile, Value: "b.go", StartLine: 1, EndLine: 0, InsertText: "@file:b.go", FromTracker: false},
			},
		},
		{
			name:    "value with special characters",
			text:    "Open @file:internal/agent/resolver/reference_test.go",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "internal/agent/resolver/reference_test.go", StartLine: 1, EndLine: 0, InsertText: "@file:internal/agent/resolver/reference_test.go", FromTracker: false},
			},
		},
		{
			name:    "reference with line range anchor",
			text:    "Look at @file:main.go#L10-L20 please",
			tracked: nil,
			wantRefs: []Reference{
				{Type: TypeFile, Value: "main.go", StartLine: 10, EndLine: 20, InsertText: "@file:main.go#L10-L20", FromTracker: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractReferences(tt.text, tt.tracked)

			// Normalize nil vs empty slice
			if got == nil && len(tt.wantRefs) == 0 {
				got = nil
			}

			// Sort both slices for deterministic comparison
			sortRefs(got)
			sortRefs(tt.wantRefs)

			if len(got) != len(tt.wantRefs) {
				t.Errorf("ExtractReferences() got %d refs, want %d refs", len(got), len(tt.wantRefs))
				return
			}

			for i, g := range got {
				w := tt.wantRefs[i]
				if !reflect.DeepEqual(g, w) {
					t.Errorf("ExtractReferences()[%d] = %v, want %v", i, g, w)
				}
			}
		})
	}
}

func TestPrefixToSourceMap(t *testing.T) {
	m := PrefixToSourceMap()

	if len(m) != len(Prefixes) {
		t.Errorf("PrefixToSourceMap() got %d entries, want %d", len(m), len(Prefixes))
		return
	}

	for prefix, expectedRT := range Prefixes {
		got := m[prefix]
		if got != string(expectedRT) {
			t.Errorf("PrefixToSourceMap()[%q] = %q, want %q", prefix, got, string(expectedRT))
		}
	}
}

func TestIsPrefix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"@file: prefix", "@file:main.go", true},
		{"@sym: prefix", "@sym:Resolver", true},
		{"@skill: prefix", "@skill:golang", true},
		{"no prefix", "hello world", false},
		{"empty text", "", false},
		{"partial trigger", "@fi", false},
		{"just @", "@", false},
		{"@file without colon", "@file main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPrefix(tt.text)
			if got != tt.want {
				t.Errorf("IsPrefix(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func sortRefs(refs []Reference) {
	for i := 0; i < len(refs); i++ {
		for j := i + 1; j < len(refs); j++ {
			if refs[i].InsertText > refs[j].InsertText {
				refs[i], refs[j] = refs[j], refs[i]
			}
		}
	}
}
