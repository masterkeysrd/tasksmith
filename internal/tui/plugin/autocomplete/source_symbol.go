package autocomplete

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// SymbolSearchResult represents a symbol match from an LSP search query.
type SymbolSearchResult struct {
	Name          string
	Kind          string
	Path          string // relative path in workspace
	StartLine     int
	StartChar     int
	ContainerName string
}

// SymbolSource implements the Source interface for symbol completions via LSP.
type SymbolSource struct {
	queryFn func(query string) []SymbolSearchResult
}

// NewSymbolSource instantiates a new SymbolSource with a query function.
func NewSymbolSource(queryFn func(query string) []SymbolSearchResult) *SymbolSource {
	return &SymbolSource{
		queryFn: queryFn,
	}
}

// Name returns the identifier for this source.
func (s *SymbolSource) Name() string {
	return "symbol"
}

// Query performs an LSP symbol search using the provided query function and maps results to Items.
func (s *SymbolSource) Query(ctx context.Context, query string) ([]Item, error) {
	results := s.queryFn(query)
	var items []Item

	for _, r := range results {
		displayName := r.Name
		if idx := strings.LastIndex(r.Name, "/"); idx != -1 {
			displayName = r.Name[idx+1:]
		}

		// Encode full coordinates for autocomplete ID: path:line:char:kind:name
		id := fmt.Sprintf("%s:%d:%d:%s:%s", r.Path, r.StartLine, r.StartChar, r.Kind, displayName)

		// Insert value encodes the kind so markdown inline pill parser can extract it
		insertVal := "@sym:" + displayName + ":" + r.Kind

		sublabel := cleanPackagePath(r.Path)

		items = append(items, Item{
			ID:          id,
			Label:       displayName,
			Sublabel:    sublabel,
			Badge:       abbreviateKind(r.Kind),
			Kind:        r.Kind,
			InsertValue: insertVal,
		})
	}

	return items, nil
}

func abbreviateKind(kind string) string {
	switch strings.ToLower(kind) {
	case "function":
		return "FUNC "
	case "method":
		return "METH "
	case "variable":
		return "VAR  "
	case "constant":
		return "CONST"
	case "class":
		return "CLASS"
	case "interface":
		return "INTF "
	case "struct":
		return "STRT "
	case "field":
		return "FIELD"
	case "property":
		return "PROP "
	case "package":
		return "PKG  "
	case "module":
		return "MOD  "
	case "namespace":
		return "NS   "
	case "enum":
		return "ENUM "
	case "typeparameter":
		return "TYPE "
	default:
		if len(kind) > 5 {
			return strings.ToUpper(kind[:5])
		}
		res := strings.ToUpper(kind)
		for len(res) < 5 {
			res += " "
		}
		return res
	}
}

func cleanPackagePath(path string) string {
	path = filepath.ToSlash(path)

	// Go package mod cache
	if idx := strings.Index(path, "go/pkg/mod/"); idx != -1 {
		sub := path[idx+len("go/pkg/mod/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		if atIdx := strings.Index(sub, "@"); atIdx != -1 {
			afterAt := sub[atIdx:]
			if nextSlash := strings.Index(afterAt, "/"); nextSlash != -1 {
				sub = sub[:atIdx] + afterAt[nextSlash:]
			} else {
				sub = sub[:atIdx]
			}
		}
		return sub
	}

	// Go standard library
	if idx := strings.LastIndex(path, "/src/"); idx != -1 {
		sub := path[idx+len("/src/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			return sub[:lastSlash]
		}
		return sub
	}

	// Node/TypeScript node_modules
	if idx := strings.Index(path, "node_modules/"); idx != -1 {
		sub := path[idx+len("node_modules/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		if strings.HasPrefix(sub, "@") {
			parts := strings.Split(sub, "/")
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		} else {
			parts := strings.Split(sub, "/")
			if len(parts) >= 1 {
				return parts[0]
			}
		}
		return sub
	}

	// Python site-packages & dist-packages
	if idx := strings.Index(path, "site-packages/"); idx != -1 {
		sub := path[idx+len("site-packages/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		parts := strings.Split(sub, "/")
		if len(parts) >= 1 {
			return parts[0]
		}
		return sub
	}
	if idx := strings.Index(path, "dist-packages/"); idx != -1 {
		sub := path[idx+len("dist-packages/"):]
		if lastSlash := strings.LastIndex(sub, "/"); lastSlash != -1 {
			sub = sub[:lastSlash]
		}
		parts := strings.Split(sub, "/")
		if len(parts) >= 1 {
			return parts[0]
		}
		return sub
	}

	// Fallback to directory name
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
