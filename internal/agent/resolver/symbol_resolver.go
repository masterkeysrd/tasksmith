package resolver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
)

// Budget constants for symbol resolution (shared with LspInspect tool).
const (
	// MaxDocsChars is the maximum character budget for documentation text in the inline output.
	MaxDocsChars = 8000
	// MaxRefsPerTool is the maximum number of references returned in the inline output.
	MaxRefsPerTool = 10
	// MaxImplsPerTool is the maximum number of implementations returned in the inline output.
	MaxImplsPerTool = 10
	// MaxSimilarSymbols is the maximum number of similar symbols to gather.
	MaxSimilarSymbols = 20
)

// parseCoordinates extracts symbol coordinates from an autocomplete ID string.
// Returns path, line, character, kind string, name, and whether parsing succeeded.
func parseCoordinates(id string) (path string, line int, character int, kind string, name string, ok bool) {
	parts := strings.SplitN(id, ":", 5)
	if len(parts) < 5 {
		return "", 0, 0, "", "", false
	}

	path = parts[0]
	kind = parts[3]
	name = parts[4]

	var err error
	if _, err = fmt.Sscanf(parts[1], "%d", &line); err != nil {
		return "", 0, 0, "", "", false
	}
	if _, err = fmt.Sscanf(parts[2], "%d", &character); err != nil {
		return "", 0, 0, "", "", false
	}

	return path, line, character, kind, name, true
}

// ResolveSymbol resolves a symbol query to its coordinates.
// For autocomplete refs, it converts the path to absolute and reconstructs the coordinates.
// For manual refs, it queries LSP, picks the best match, and constructs its coordinates.
func (r *Resolver) ResolveSymbol(ctx context.Context, query string, fromTracker bool) (string, error) {
	if fromTracker {
		path, line, char, kind, name, ok := parseCoordinates(query)
		if ok {
			absPath := path
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(r.Cwd, absPath)
			}
			absPath, err := filepath.Abs(absPath)
			if err != nil {
				absPath = path
			}
			return fmt.Sprintf("%s:%d:%d:%s:%s", absPath, line, char, kind, name), nil
		}
	}

	// Pre-process manual query to strip trigger prefixes, separate kind suffixes, and isolate base symbol name
	queryClean := strings.TrimPrefix(query, "@sym:")
	queryClean = strings.TrimPrefix(queryClean, "sym:")

	var expectedKind string
	if idx := strings.LastIndex(queryClean, ":"); idx != -1 {
		suffix := queryClean[idx+1:]
		if !strings.ContainsAny(suffix, "/\\") {
			expectedKind = suffix
			queryClean = queryClean[:idx]
		}
	}

	baseName := queryClean
	var packagePart string
	if idx := strings.LastIndex(queryClean, "."); idx != -1 {
		baseName = queryClean[idx+1:]
		packagePart = queryClean[:idx]
		if pIdx := strings.LastIndex(packagePart, "."); pIdx != -1 {
			packagePart = packagePart[pIdx+1:]
		}
	} else if idx := strings.LastIndex(queryClean, "/"); idx != -1 {
		baseName = queryClean[idx+1:]
		packagePart = queryClean[:idx]
		if pIdx := strings.LastIndex(packagePart, "/"); pIdx != -1 {
			packagePart = packagePart[pIdx+1:]
		}
	}

	if r.Lsp == nil {
		return "", fmt.Errorf("LSP manager is not initialized")
	}

	client, err := r.Lsp.GetClient(ctx, r.Cwd)
	if err != nil {
		return "", fmt.Errorf("failed to get LSP client: %w", err)
	}

	// Manual ref: query LSP for symbol matches
	symbols, err := client.Search(ctx, queryClean)
	if err != nil {
		return "", fmt.Errorf("symbol search failed: %w", err)
	}

	// Fallback to baseName if no matches found with the qualified query (e.g., standard library or server limits)
	if len(symbols) == 0 && baseName != queryClean {
		symbols, err = client.Search(ctx, baseName)
		if err != nil {
			return "", fmt.Errorf("symbol search fallback failed: %w", err)
		}
	}

	if len(symbols) == 0 {
		return "", fmt.Errorf("no symbols found matching: %s", queryClean)
	}

	// Score and sort symbols to find the best match
	type scoredSymbol struct {
		sym   lspx.WorkspaceSymbol
		score int
	}
	var scored []scoredSymbol
	for _, sym := range symbols {
		score := 0
		if sym.Name == queryClean {
			score += 100
		} else if sym.Name == baseName {
			score += 50
		} else if strings.EqualFold(sym.Name, baseName) {
			score += 40
		}

		if expectedKind != "" {
			kindStr := SymbolKindToString(uint32(sym.Kind))
			if strings.EqualFold(kindStr, expectedKind) {
				score += 30
			}
		}

		if packagePart != "" {
			var uri string
			if sym.Location.Location != nil {
				uri = sym.Location.Location.URI
			} else if sym.Location.LocationUriOnly != nil {
				uri = sym.Location.LocationUriOnly.URI
			}
			uriLower := strings.ToLower(uri)
			pkgLower := strings.ToLower(packagePart)
			pathOnly := uriLower
			if _, after, ok := strings.Cut(uriLower, "://"); ok {
				pathOnly = after
			}
			segments := strings.Split(pathOnly, "/")
			var matchedPkg bool
			for sIdx, seg := range segments {
				if seg == pkgLower {
					matchedPkg = true
					break
				}
				if sIdx == len(segments)-1 {
					// Match exact file name or test file (e.g. message.go, message_test.go)
					if seg == pkgLower+".go" || seg == pkgLower+"_test.go" {
						matchedPkg = true
						break
					}
				}
			}
			if !matchedPkg {
				continue
			}
			score += 20
			if sym.ContainerName != nil && strings.Contains(strings.ToLower(*sym.ContainerName), pkgLower) {
				score += 20
			}
		}
		scored = append(scored, scoredSymbol{sym: sym, score: score})
	}

	if len(scored) == 0 {
		return "", fmt.Errorf("no symbols found matching: %s under package %s", baseName, packagePart)
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].sym.Name < scored[j].sym.Name
	})

	firstSym := scored[0].sym
	var docURI string
	var rangeVal lspx.Range

	if firstSym.Location.Location != nil {
		docURI = firstSym.Location.Location.URI
		rangeVal = firstSym.Location.Location.Range
	} else if firstSym.Location.LocationUriOnly != nil {
		docURI = firstSym.Location.LocationUriOnly.URI
	} else {
		return "", fmt.Errorf("no location found for symbol %s", query)
	}

	filePath := uriToPath(docURI)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	kindStr := SymbolKindToString(uint32(firstSym.Kind))
	return fmt.Sprintf("%s:%d:%d:%s:%s", absPath, rangeVal.Start.Line, rangeVal.Start.Character, kindStr, firstSym.Name), nil
}

// loadResourceSymbol loads symbol details and snippet from coordinates.
func (r *Resolver) loadResourceSymbol(ctx context.Context, coords string) (ResolvedResource, error) {
	filePath, line, char, kind, name, ok := parseCoordinates(coords)
	if !ok {
		return nil, fmt.Errorf("failed to parse symbol coordinates: %s", coords)
	}

	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("symbol file not found: %s", filePath)
	}

	relPath, err := filepath.Rel(r.Cwd, filePath)
	if err != nil {
		relPath = filePath
	}

	var client *lsp.Client
	if r.Lsp != nil {
		client, _ = r.Lsp.GetClient(ctx, r.Cwd)
	}

	var docs string
	var references []string
	var implementations []string
	var typeDefinedAt string

	if client != nil {
		if r.Lsp != nil {
			r.Lsp.NotifyFileOpened(ctx, filePath)
		}
		// Build position for LSP calls
		pos := lspx.Position{
			Line:      uint32(line),
			Character: uint32(char),
		}
		docURI := pathToURI(filePath)
		textDoc := lspx.TextDocumentIdentifier{URI: docURI}

		// Gather hover documentation
		hover, err := client.RawClient().Hover(ctx, &lspx.HoverParams{
			TextDocument: textDoc,
			Position:     pos,
		})
		if err == nil && hover != nil {
			docs = extractHoverContent(hover)
		}

		// Gather references
		refParams := lspx.ReferenceParams{
			TextDocument: textDoc,
			Position:     pos,
			Context:      lspx.ReferenceContext{IncludeDeclaration: true},
		}
		locations, err := client.RawClient().References(ctx, &refParams)
		if err == nil {
			for _, loc := range locations {
				lpath := uriToPath(loc.URI)
				rel, relErr := filepath.Rel(r.Cwd, lpath)
				if relErr != nil {
					rel = lpath
				}
				references = append(references, fmt.Sprintf("%s:%d:%d", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
			}
			sortStrings(references)
		}

		// Gather implementations
		implParams := lspx.ImplementationParams{
			TextDocument: textDoc,
			Position:     pos,
		}
		implResult, err := client.RawClient().Implementation(ctx, &implParams)
		if err == nil && implResult != nil {
			implementations = extractLocationsFromResult(implResult, r.Cwd)
			sortStrings(implementations)
		}

		// Gather type definition
		typeDefParams := lspx.TypeDefinitionParams{
			TextDocument: textDoc,
			Position:     pos,
		}
		typeDefResult, err := client.RawClient().TypeDefinition(ctx, &typeDefParams)
		if err == nil && typeDefResult != nil {
			locs := extractLocationsFromResult(typeDefResult, r.Cwd)
			if len(locs) > 0 {
				typeDefinedAt = locs[0]
			}
		}
	}

	// Extract signature from hover docs
	signature := name
	if docs != "" {
		signature = extractSignature(docs)
	}

	// Read definition snippet from file
	snippet, startLineBound, endLineBound, err := r.readSymbolSnippet(filePath, line, line)
	if err != nil {
		snippet = fmt.Sprintf("// Error reading symbol: %v", err)
	}

	// Get diagnostics for this symbol's line range
	var diags []lsp.Diagnostic
	if client != nil {
		if fileDiags, err := client.GetDiagnostics(ctx, filePath); err == nil {
			diags = filterDiagnosticsInRange(fileDiags, startLineBound, endLineBound)
		}
	}

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
		reportFilename := fmt.Sprintf("lsp_inspect_%s_%s.md", toolCallID, sanitizeFilename(name))
		if r.Storage != nil {
			storagePath := filepath.Join("lsp_inspect", reportFilename)
			fullReport := buildFullReport(name, kind, relPath, typeDefinedAt, signature, docs, references, implementations)
			cachedPath, errSave := r.Storage.Save(ctx, storagePath, strings.NewReader(fullReport))
			if errSave == nil && cachedPath != "" {
				fullReportPath = cachedPath
			}
		}
	}

	// Inline phase: build compact output with budget limits
	inline := buildInlineOutput(docs, references, implementations)

	return &ResolvedSymbol{
		Name:                 name,
		Kind:                 kind,
		Signature:            signature,
		TypeDefinedAt:        typeDefinedAt,
		Container:            "",
		FilePath:             filePath,
		StartLine:            startLineBound,
		EndLine:              endLineBound,
		Snippet:              snippet,
		Diagnostics:          diags,
		Docs:                 inline.docs,
		DocsTruncated:        inline.docsTruncated,
		References:           inline.references,
		ReferencesTotal:      refsCount,
		Implementations:      inline.implementations,
		ImplementationsTotal: implsCount,
		FullReportPath:       fullReportPath,
	}, nil
}

// loadSymbolContent loads the definition snippet and diagnostics for a symbol.

// readSymbolSnippet reads a window of lines around the symbol definition.
// Reads 5 lines before and 20 lines after the symbol's range.
func (r *Resolver) readSymbolSnippet(filePath string, startLine, endLine int) (string, int, int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, 0, err
	}
	defer file.Close()

	// Calculate window bounds
	windowStart := max(startLine-5, 1)
	windowEnd := endLine + 20

	var lines []string
	scanner := newScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum < windowStart {
			continue
		}
		if lineNum > windowEnd {
			break
		}
		lines = append(lines, fmt.Sprintf("%d | %s", lineNum, scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		return "", 0, 0, err
	}

	return strings.Join(lines, "\n"), windowStart, windowEnd, nil
}

// filterDiagnosticsInRange returns only diagnostics that fall within the specified line range.
func filterDiagnosticsInRange(diags []lsp.Diagnostic, startLine, endLine int) []lsp.Diagnostic {
	var filtered []lsp.Diagnostic
	for _, d := range diags {
		diagLine := int(d.Range.Start.Line)
		if diagLine >= startLine && diagLine <= endLine {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

// uriToPath converts a URI string to a filepath.
func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return filepath.FromSlash(uri[7:])
	}
	return uri
}

// pathToURI converts a filepath to a URI string.
func pathToURI(path string) string {
	return "file://" + filepath.ToSlash(path)
}

// extractHoverContent extracts markdown content from an LSP hover response.
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

// extractSignature extracts function/type signatures from hover documentation.
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

// extractLocationsFromResult extracts location strings from LSP implementation/type definition results.
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

// scanScanner wraps bufio.Scanner for cleaner code.
type scanScanner struct {
	*bufio.Scanner
}

func newScanner(r *os.File) *scanScanner {
	return &scanScanner{bufio.NewScanner(r)}
}

// inlineResult holds the budget-limited inline output data.
type inlineResult struct {
	docs            string
	docsTruncated   bool
	references      []string
	implementations []string
}

// buildInlineOutput applies budget limits to the inline output.
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

// buildFullReport creates a full markdown report for symbols exceeding budget limits.
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

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// minInt returns the minimum of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sanitizeFilename sanitizes a string for use in filenames.
func sanitizeFilename(name string) string {
	return strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_").Replace(name)
}
