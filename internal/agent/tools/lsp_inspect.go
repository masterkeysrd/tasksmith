package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
)

var _ tool.TextContentProvider = LspDiagnosticsOutput{}

const (
	// MaxDocsChars is the maximum character budget for documentation text in the inline output.
	MaxDocsChars = 8000
	// MaxRefsPerTool is the maximum number of references returned in the inline output.
	MaxRefsPerTool = 10
	// MaxImplsPerTool is the maximum number of implementations returned in the inline output.
	MaxImplsPerTool = 10
)

// LspInspect performs a deep-dive inspection on a specific symbol.
func (h *ToolHandlers) LspInspect(ctx context.Context, in LspInspectArgs) (LspInspectOutput, error) {
	if h.LspManager == nil {
		return LspInspectOutput{}, fmt.Errorf("LSP manager is not initialized")
	}
	client, err := h.LspManager.GetClient(ctx, h.CWD)
	if err != nil {
		return LspInspectOutput{}, fmt.Errorf("failed to get LSP client: %w", err)
	}

	// Call resolver's ResolveSymbol (via ResolvePath)
	r := resolver.New(resolver.Config{
		Lsp:         h.LspManager,
		Cwd:         h.CWD,
		FileTracker: h.FileTracker,
		Storage:     h.Storage,
	})
	coords, err := r.ResolvePath(ctx, in.Query, resolver.TypeSymbol)
	if err != nil {
		if strings.Contains(err.Error(), "no symbols found") {
			return LspInspectOutput{TotalMatches: 0}, nil
		}
		return LspInspectOutput{}, err
	}

	res, err := r.LoadResource(ctx, coords, resolver.TypeSymbol)
	if err != nil {
		return LspInspectOutput{}, err
	}
	symRes, ok := res.(*resolver.ResolvedSymbol)
	if !ok {
		return LspInspectOutput{}, fmt.Errorf("expected ResolvedSymbol")
	}

	// Search for similar symbols using LSP
	symbols, err := client.Search(ctx, in.Query)
	if err != nil {
		return LspInspectOutput{}, err
	}

	// Prioritize exact matches (both case-sensitive and case-insensitive)
	sort.Slice(symbols, func(i, j int) bool {
		iExact := symbols[i].Name == in.Query
		jExact := symbols[j].Name == in.Query
		if iExact != jExact {
			return iExact
		}
		iLowerExact := strings.EqualFold(symbols[i].Name, in.Query)
		jLowerExact := strings.EqualFold(symbols[j].Name, in.Query)
		if iLowerExact != jLowerExact {
			return iLowerExact
		}
		return i < j
	})

	var result LspInspectOutputResult
	relPath, err := filepath.Rel(h.CWD, symRes.FilePath)
	if err != nil {
		relPath = symRes.FilePath
	}
	result = LspInspectOutputResult{
		Name:                 symRes.Name,
		Kind:                 symRes.Kind,
		DeclaredAt:           fmt.Sprintf("%s:%d", relPath, symRes.StartLine),
		TypeDefinedAt:        symRes.TypeDefinedAt,
		Signature:            symRes.Signature,
		Snippet:              symRes.Snippet,
		Docs:                 symRes.Docs,
		DocsTruncated:        symRes.DocsTruncated,
		References:           symRes.References,
		ReferencesTotal:      symRes.ReferencesTotal,
		Implementations:      symRes.Implementations,
		ImplementationsTotal: symRes.ImplementationsTotal,
		FullReportPath:       symRes.FullReportPath,
	}

	similarSymbols := make([]LspInspectOutputSimilarSymbolsItem, 0)
	for i := 1; i < len(symbols); i++ {
		if i > 20 {
			break
		}
		sym := symbols[i]
		var similarRelPath string
		if sym.Location.Location != nil {
			filePath := uriToPath(sym.Location.Location.URI)
			if rel, relErr := filepath.Rel(h.CWD, filePath); relErr == nil {
				similarRelPath = rel
			} else {
				similarRelPath = filePath
			}
			similarRelPath = fmt.Sprintf("%s:%d", similarRelPath, sym.Location.Location.Range.Start.Line+1)
		} else if sym.Location.LocationUriOnly != nil {
			similarRelPath = uriToPath(sym.Location.LocationUriOnly.URI)
		}
		similarSymbols = append(similarSymbols, LspInspectOutputSimilarSymbolsItem{
			Name:       sym.Name,
			Kind:       resolver.SymbolKindToString(uint32(sym.Kind)),
			DeclaredAt: similarRelPath,
		})
	}

	return LspInspectOutput{
		Result:         result,
		SimilarSymbols: similarSymbols,
		TotalMatches:   len(symbols),
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable summary instead of a raw JSON blob.
func (o LspInspectOutput) TextContent() string {
	if o.Result.Name == "" {
		return fmt.Sprintf("No symbols found matching the query (total: %d).", o.TotalMatches)
	}

	var sb strings.Builder

	// Primary match
	r := o.Result
	fmt.Fprintf(&sb, "## %s (%s)\n\n", r.Name, r.Kind)
	fmt.Fprintf(&sb, "**Declared at:** `%s`\n\n", r.DeclaredAt)
	if r.TypeDefinedAt != "" {
		fmt.Fprintf(&sb, "**Type Defined at:** `%s`\n\n", r.TypeDefinedAt)
	}
	if r.Signature != "" {
		sb.WriteString("```go\n")
		sb.WriteString(r.Signature)
		sb.WriteString("\n```\n\n")
	}
	if r.Snippet != "" {
		sb.WriteString("**Definition:**\n```go\n")
		sb.WriteString(r.Snippet)
		sb.WriteString("\n```\n\n")
	}
	if r.Docs != "" {
		sb.WriteString(r.Docs)
		if r.DocsTruncated {
			sb.WriteString("\n\n[Truncated — full report available at: `")
			sb.WriteString(r.FullReportPath)
			sb.WriteString("`]")
		}
		sb.WriteString("\n\n")
	}
	if len(r.References) > 0 {
		sb.WriteString("**References** (")
		fmt.Fprintf(&sb, "%d total", r.ReferencesTotal)
		sb.WriteString("):\n")
		for _, ref := range r.References {
			fmt.Fprintf(&sb, "- `%s`\n", ref)
		}
		sb.WriteString("\n")
	}
	if len(r.Implementations) > 0 {
		sb.WriteString("**Implementations** (")
		fmt.Fprintf(&sb, "%d total", r.ImplementationsTotal)
		sb.WriteString("):\n")
		for _, impl := range r.Implementations {
			fmt.Fprintf(&sb, "- `%s`\n", impl)
		}
		sb.WriteString("\n")
	}

	// Other matching symbols (up to 20)
	if len(o.SimilarSymbols) > 0 {
		sb.WriteString("\n---\n\n### Other matching symbols\n\n")
		for _, other := range o.SimilarSymbols {
			fmt.Fprintf(&sb, "* **%s** (%s) declared at `%s`\n", other.Name, other.Kind, other.DeclaredAt)
		}

		// 1 primary + len(o.SimilarSymbols)
		totalRendered := 1 + len(o.SimilarSymbols)
		if o.TotalMatches > totalRendered {
			remaining := o.TotalMatches - totalRendered
			fmt.Fprintf(&sb, "\n*... and %d more matching symbols. (Use a more specific query to inspect one of these).* \n", remaining)
		} else {
			sb.WriteString("\n*(Use a more specific query to inspect one of these).*\n")
		}
	}

	return sb.String()
}

// uriToPath converts a URI string to a filepath.
func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return filepath.FromSlash(uri[7:])
	}
	return uri
}
