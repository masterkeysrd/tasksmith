package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
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

	// Step 1: Search for the symbol using WorkspaceSymbol
	symbols, err := client.Search(ctx, in.Query)
	if err != nil {
		return LspInspectOutput{}, fmt.Errorf("symbol search failed: %w", err)
	}

	if len(symbols) == 0 {
		return LspInspectOutput{TotalMatches: 0}, nil
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

	// Deeply inspect only the first (best) match
	firstSym := symbols[0]
	firstItem, err := h.inspectSymbol(ctx, client, firstSym)
	if err == nil {
		result = firstItem
	} else {
		// Fallback: if deep inspection fails, still try to populate basic info
		var relPath string
		if firstSym.Location.Location != nil {
			filePath := uriToPath(firstSym.Location.Location.URI)
			if rel, relErr := filepath.Rel(h.CWD, filePath); relErr == nil {
				relPath = rel
			} else {
				relPath = filePath
			}
			relPath = fmt.Sprintf("%s:%d", relPath, firstSym.Location.Location.Range.Start.Line+1)
		} else if firstSym.Location.LocationUriOnly != nil {
			relPath = uriToPath(firstSym.Location.LocationUriOnly.URI)
		}
		result = LspInspectOutputResult{
			Name:       firstSym.Name,
			Kind:       symbolKindToString(uint32(firstSym.Kind)),
			DeclaredAt: relPath,
		}
	}

	similarSymbols := make([]LspInspectOutputSimilarSymbolsItem, 0)
	// For the next matches (up to 20 similar symbols), gather basic info from WorkspaceSymbol without deep inspection
	for i := 1; i < len(symbols); i++ {
		if i > 20 {
			break
		}
		sym := symbols[i]
		var relPath string
		if sym.Location.Location != nil {
			filePath := uriToPath(sym.Location.Location.URI)
			if rel, relErr := filepath.Rel(h.CWD, filePath); relErr == nil {
				relPath = rel
			} else {
				relPath = filePath
			}
			relPath = fmt.Sprintf("%s:%d", relPath, sym.Location.Location.Range.Start.Line+1)
		} else if sym.Location.LocationUriOnly != nil {
			relPath = uriToPath(sym.Location.LocationUriOnly.URI)
		}
		similarSymbols = append(similarSymbols, LspInspectOutputSimilarSymbolsItem{
			Name:       sym.Name,
			Kind:       symbolKindToString(uint32(sym.Kind)),
			DeclaredAt: relPath,
		})
	}

	return LspInspectOutput{
		Result:         result,
		SimilarSymbols: similarSymbols,
		TotalMatches:   len(symbols),
	}, nil
}

// uriToPath converts a URI string to a filepath.
func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return filepath.FromSlash(uri[7:])
	}
	return uri
}

// inspectSymbol performs deep inspection on a single symbol.
func (h *ToolHandlers) inspectSymbol(ctx context.Context, client *lsp.Client, sym lspx.WorkspaceSymbol) (LspInspectOutputResult, error) {
	var docURI string
	var rangeVal lspx.Range

	if sym.Location.Location != nil {
		docURI = sym.Location.Location.URI
		rangeVal = sym.Location.Location.Range
	} else if sym.Location.LocationUriOnly != nil {
		docURI = sym.Location.LocationUriOnly.URI
	} else {
		return LspInspectOutputResult{}, fmt.Errorf("no location found for symbol %s", sym.Name)
	}

	filePath := uriToPath(docURI)
	_, err := os.Stat(filePath)
	if err != nil {
		return LspInspectOutputResult{}, fmt.Errorf("symbol file not found: %s", filePath)
	}
	relPath, err := filepath.Rel(h.CWD, filePath)
	if err != nil {
		relPath = filePath
	}

	// Build position for further LSP calls
	pos := lspx.Position{
		Line:      rangeVal.Start.Line,
		Character: rangeVal.Start.Character,
	}
	textDoc := lspx.TextDocumentIdentifier{URI: docURI}

	// Gather all LSP data
	var docs string
	hover, err := client.RawClient().Hover(ctx, &lspx.HoverParams{
		TextDocument: textDoc,
		Position:     pos,
	})
	if err == nil && hover != nil {
		docs = extractHoverContent(hover)
	}

	var references []string
	refParams := lspx.ReferenceParams{
		TextDocument: textDoc,
		Position:     pos,
		Context:      lspx.ReferenceContext{IncludeDeclaration: true},
	}
	locations, err := client.RawClient().References(ctx, &refParams)
	if err == nil {
		for _, loc := range locations {
			lpath := uriToPath(loc.URI)
			rel, relErr := filepath.Rel(h.CWD, lpath)
			if relErr != nil {
				rel = lpath
			}
			references = append(references, fmt.Sprintf("%s:%d:%d", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
		}
		sortStrings(references)
	}

	var implementations []string
	implParams := lspx.ImplementationParams{
		TextDocument: textDoc,
		Position:     pos,
	}
	implResult, err := client.RawClient().Implementation(ctx, &implParams)
	if err == nil && implResult != nil {
		implementations = extractLocationsFromResult(implResult, h.CWD)
		sortStrings(implementations)
	}

	var typeDefinedAt string
	typeDefParams := lspx.TypeDefinitionParams{
		TextDocument: textDoc,
		Position:     pos,
	}
	typeDefResult, err := client.RawClient().TypeDefinition(ctx, &typeDefParams)
	if err == nil && typeDefResult != nil {
		locs := extractLocationsFromResult(typeDefResult, h.CWD)
		if len(locs) > 0 {
			typeDefinedAt = locs[0]
		}
	}

	// Build signature from hover or use symbol name
	signature := sym.Name
	if docs != "" {
		signature = extractSignature(docs)
	}

	// Determine kind string
	kindStr := symbolKindToString(uint32(sym.Kind))

	// Counter phase: check if full report is needed
	docsLen := len(docs)
	refsCount := len(references)
	implsCount := len(implementations)
	needFullReport := docsLen > MaxDocsChars || refsCount > MaxRefsPerTool || implsCount > MaxImplsPerTool

	// Report phase: save full report to storage if needed
	var fullReportPath string
	if needFullReport {
		toolCallID, _ := ctx.Value("tool_call_id").(string)
		if toolCallID == "" {
			toolCallID = "unknown"
		}
		reportFilename := fmt.Sprintf("lsp_inspect_%s_%s.md", toolCallID, sanitizeFilename(sym.Name))
		if h.Storage != nil {
			storagePath := filepath.Join("lsp_inspect", reportFilename)
			fullReport := buildFullReport(sym.Name, kindStr, relPath, typeDefinedAt, signature, docs, references, implementations)
			cachedPath, errSave := h.Storage.Save(ctx, storagePath, strings.NewReader(fullReport))
			if errSave == nil && cachedPath != "" {
				fullReportPath = cachedPath
			}
		}
	}

	// Inline phase: build compact output
	inline := buildInlineOutput(docs, references, implementations)

	return LspInspectOutputResult{
		Name:                 sym.Name,
		Kind:                 kindStr,
		DeclaredAt:           fmt.Sprintf("%s:%d", relPath, rangeVal.Start.Line+1),
		TypeDefinedAt:        typeDefinedAt,
		Signature:            signature,
		Docs:                 inline.docs,
		DocsTruncated:        inline.docsTruncated,
		References:           inline.references,
		ReferencesTotal:      refsCount,
		Implementations:      inline.implementations,
		ImplementationsTotal: implsCount,
		FullReportPath:       fullReportPath,
	}, nil
}

type inlineResult struct {
	docs            string
	docsTruncated   bool
	references      []string
	implementations []string
}

func buildInlineOutput(docs string, references, implementations []string) inlineResult {
	// Docs: plain text, truncated to budget
	docsText := docs
	docsTruncated := false
	if len(docs) > MaxDocsChars {
		docsTruncated = true
		docsText = docs[:MaxDocsChars]
	}

	// References: hard cap
	refLimit := minInt(len(references), MaxRefsPerTool)
	cappedRefs := make([]string, refLimit)
	copy(cappedRefs, references[:refLimit])

	// Implementations: hard cap
	implLimit := minInt(len(implementations), MaxImplsPerTool)
	cappedImpls := make([]string, implLimit)
	copy(cappedImpls, implementations[:implLimit])

	return inlineResult{
		docs:            docsText,
		docsTruncated:   docsTruncated,
		references:      cappedRefs,
		implementations: cappedImpls,
	}
}

func buildFullReport(name, kind, declaredAt, typeDefinedAt, signature, docs string, references, implementations []string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s (%s)\n\n", name, kind)
	sb.WriteString("```go\n")
	sb.WriteString(signature)
	sb.WriteString("\n```\n\n")
	fmt.Fprintf(&sb, "**Declared at:** `%s`\n\n", declaredAt)
	if typeDefinedAt != "" {
		fmt.Fprintf(&sb, "**Type Defined at:** `%s`\n\n", typeDefinedAt)
	}

	if docs != "" {
		sb.WriteString("## Documentation\n\n")
		sb.WriteString(docs)
		sb.WriteString("\n\n")
	}

	fmt.Fprintf(&sb, "## References (%d total)\n\n", len(references))
	if len(references) == 0 {
		sb.WriteString("No references found.\n\n")
	} else {
		for i, ref := range references {
			fmt.Fprintf(&sb, "%d. `%s`\n", i+1, ref)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "## Implementations (%d total)\n\n", len(implementations))
	if len(implementations) == 0 {
		sb.WriteString("No implementations found.\n\n")
	} else {
		for i, impl := range implementations {
			fmt.Fprintf(&sb, "%d. `%s`\n", i+1, impl)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func extractHoverContent(hover *lspx.Hover) string {
	if hover == nil {
		return ""
	}
	contents := hover.Contents
	if contents.MarkupContent != nil {
		return contents.MarkupContent.Value
	}
	if contents.MarkedString != nil && contents.MarkedString.String != nil {
		return *contents.MarkedString.String
	}
	if contents.MarkedString != nil && contents.MarkedString.WithLanguage != nil {
		return fmt.Sprintf("%s\n%s\n", contents.MarkedString.WithLanguage.Language, contents.MarkedString.WithLanguage.Value)
	}
	if contents.ArrayOfMarkedString != nil {
		var parts []string
		for _, ms := range *contents.ArrayOfMarkedString {
			if ms.String != nil {
				parts = append(parts, *ms.String)
			} else if ms.WithLanguage != nil {
				parts = append(parts, fmt.Sprintf("%s\n%s", ms.WithLanguage.Language, ms.WithLanguage.Value))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func extractSignature(docs string) string {
	lines := strings.Split(docs, "\n")
	var sigLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "##") {
			continue
		}
		if strings.Contains(line, "func ") || strings.Contains(line, "type ") ||
			strings.Contains(line, "const ") || strings.Contains(line, "var ") ||
			strings.Contains(line, "interface") || strings.Contains(line, "struct") {
			sigLines = append(sigLines, line)
		}
		if len(sigLines) >= 3 {
			break
		}
	}
	if len(sigLines) > 0 {
		return strings.Join(sigLines, "\n")
	}
	return ""
}

func extractLocationsFromResult(result any, cwd string) []string {
	var locs []lspx.Location

	switch r := result.(type) {
	case *lspx.ImplementationResult:
		if r.Definition != nil && r.Definition.Location != nil {
			locs = append(locs, *r.Definition.Location)
		}
		if r.ArrayOfDefinitionLink != nil {
			for _, dl := range *r.ArrayOfDefinitionLink {
				locs = append(locs, lspx.Location{
					URI:   dl.TargetURI,
					Range: dl.TargetRange,
				})
			}
		}
	case *lspx.TypeDefinitionResult:
		if r.Definition != nil && r.Definition.Location != nil {
			locs = append(locs, *r.Definition.Location)
		}
		if r.ArrayOfDefinitionLink != nil {
			for _, dl := range *r.ArrayOfDefinitionLink {
				locs = append(locs, lspx.Location{
					URI:   dl.TargetURI,
					Range: dl.TargetRange,
				})
			}
		}
	}

	var resultStr []string
	for _, loc := range locs {
		lpath := uriToPath(loc.URI)
		rel, err := filepath.Rel(cwd, lpath)
		if err != nil {
			rel = lpath
		}
		resultStr = append(resultStr, fmt.Sprintf("%s:%d:%d", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
	}
	return resultStr
}

func symbolKindToString(kind uint32) string {
	switch kind {
	case 1:
		return "File"
	case 2:
		return "Module"
	case 3:
		return "Namespace"
	case 4:
		return "Package"
	case 5:
		return "Class"
	case 6:
		return "Method"
	case 7:
		return "Property"
	case 8:
		return "Field"
	case 9:
		return "Constructor"
	case 10:
		return "Enum"
	case 11:
		return "Interface"
	case 12:
		return "Function"
	case 13:
		return "Variable"
	case 14:
		return "Constant"
	case 15:
		return "String"
	case 16:
		return "Number"
	case 17:
		return "Boolean"
	case 18:
		return "Array"
	case 19:
		return "Object"
	case 20:
		return "Key"
	case 21:
		return "Null"
	case 22:
		return "EnumMember"
	case 23:
		return "Struct"
	case 24:
		return "Event"
	case 25:
		return "Operator"
	case 26:
		return "TypeParameter"
	default:
		return fmt.Sprintf("Kind(%d)", kind)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func sanitizeFilename(name string) string {
	return strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_").Replace(name)
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
			sb.WriteString("- `" + ref + "`\n")
		}
		sb.WriteString("\n")
	}
	if len(r.Implementations) > 0 {
		sb.WriteString("**Implementations** (")
		fmt.Fprintf(&sb, "%d total", r.ImplementationsTotal)
		sb.WriteString("):\n")
		for _, impl := range r.Implementations {
			sb.WriteString("- `" + impl + "`\n")
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
