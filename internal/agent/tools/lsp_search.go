package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/loom/message"
)

// LspSearch searches using LSP.
func (h *ToolHandlers) LspSearch(ctx context.Context, in LspSearchArgs) (LspSearchOutput, error) {
	if h.LspManager == nil {
		return LspSearchOutput{}, fmt.Errorf("LSP manager is not initialized")
	}
	client, err := h.LspManager.GetClient(ctx, h.CWD)
	if err != nil {
		return LspSearchOutput{}, fmt.Errorf("failed to get LSP client: %w", err)
	}

	results, err := client.Search(ctx, in.Query)
	if err != nil {
		return LspSearchOutput{}, err
	}

	outputResults := make([]LspSearchOutputResultsItem, len(results))
	for i, sym := range results {
		var docURI string
		var rangeVal LspSearchOutputResultsItemRange

		if sym.Location.Location != nil {
			docURI = sym.Location.Location.URI
			rangeVal = LspSearchOutputResultsItemRange{
				Start: LspSearchOutputResultsItemRangeStart{
					Line:      int(sym.Location.Location.Range.Start.Line),
					Character: int(sym.Location.Location.Range.Start.Character),
				},
				End: LspSearchOutputResultsItemRangeEnd{
					Line:      int(sym.Location.Location.Range.End.Line),
					Character: int(sym.Location.Location.Range.End.Character),
				},
			}
		} else if sym.Location.LocationUriOnly != nil {
			docURI = sym.Location.LocationUriOnly.URI
		}

		var filePath string
		if strings.HasPrefix(docURI, "file://") {
			filePath = filepath.FromSlash(docURI[7:])
		} else {
			filePath = docURI
		}

		relPath, err := filepath.Rel(h.CWD, filePath)
		if err != nil {
			relPath = filePath
		}

		var containerName string
		if sym.ContainerName != nil {
			containerName = *sym.ContainerName
		}

		kindStr := fmt.Sprintf("Kind(%d)", sym.Kind)
		switch sym.Kind {
		case 1:
			kindStr = "File"
		case 2:
			kindStr = "Module"
		case 3:
			kindStr = "Namespace"
		case 4:
			kindStr = "Package"
		case 5:
			kindStr = "Class"
		case 6:
			kindStr = "Method"
		case 7:
			kindStr = "Property"
		case 8:
			kindStr = "Field"
		case 9:
			kindStr = "Constructor"
		case 10:
			kindStr = "Enum"
		case 11:
			kindStr = "Interface"
		case 12:
			kindStr = "Function"
		case 13:
			kindStr = "Variable"
		case 14:
			kindStr = "Constant"
		case 15:
			kindStr = "String"
		case 16:
			kindStr = "Number"
		case 17:
			kindStr = "Boolean"
		case 18:
			kindStr = "Array"
		case 19:
			kindStr = "Object"
		case 20:
			kindStr = "Key"
		case 21:
			kindStr = "Null"
		case 22:
			kindStr = "EnumMember"
		case 23:
			kindStr = "Struct"
		case 24:
			kindStr = "Event"
		case 25:
			kindStr = "Operator"
		case 26:
			kindStr = "TypeParameter"
		}

		outputResults[i] = LspSearchOutputResultsItem{
			Name:          sym.Name,
			Kind:          kindStr,
			Path:          relPath,
			ContainerName: containerName,
			Range:         rangeVal,
		}
	}

	return LspSearchOutput{Results: outputResults}, nil
}

// TextContent implements the loom tool.TextContentProvider interface.
func (o LspSearchOutput) TextContent() string {
	if len(o.Results) == 0 {
		return "No symbols found matching the query."
	}

	const maxChars = 8000
	var sb strings.Builder
	renderedCount := 0
	truncated := false

	for _, r := range o.Results {
		containerPart := ""
		if r.ContainerName != "" {
			containerPart = fmt.Sprintf(" in %s", r.ContainerName)
		}
		line := fmt.Sprintf("- %s (%s)%s at %s:%d:%d\n",
			r.Name,
			r.Kind,
			containerPart,
			r.Path,
			r.Range.Start.Line+1,
			r.Range.Start.Character+1,
		)

		if sb.Len()+len(line) > maxChars {
			truncated = true
			break
		}
		sb.WriteString(line)
		renderedCount++
	}

	if truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: Results truncated to conserve tokens. Showing first %d of %d symbols found. Use a more specific query to narrow down the search.]",
			renderedCount,
			len(o.Results),
		)
	}
	return sb.String()
}

// ToolContent implements the loom tool.ContentProvider interface.
func (o LspSearchOutput) ToolContent() message.Content {
	return message.Content{
		&message.TextBlock{
			Text: o.TextContent(),
		},
	}
}
