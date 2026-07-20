package lsp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/lspx"
)

type Client struct {
	lspClient     *lspx.Client
	rootPath      string
	activeServers []lspx.ServerConfig

	diagnosticsMu sync.RWMutex
	diagnostics   map[string][]lspx.Diagnostic
}

// detectServers returns a list of installed servers.
func detectServers() []lspx.ServerConfig {
	var configs []lspx.ServerConfig

	// GOPLS
	if _, err := exec.LookPath("gopls"); err == nil {
		configs = append(configs, lspx.ServerConfig{
			Name:          "gopls",
			Command:       []string{"gopls"},
			FileTypes:     []string{"go"},
			ShareSessions: true,
		})
	}

	// Pyright or pylsp
	if _, err := exec.LookPath("pyright-langserver"); err == nil {
		configs = append(configs, lspx.ServerConfig{
			Name:          "pyright",
			Command:       []string{"pyright-langserver", "--stdio"},
			FileTypes:     []string{"python"},
			ShareSessions: true,
		})
	} else if _, err := exec.LookPath("pylsp"); err == nil {
		configs = append(configs, lspx.ServerConfig{
			Name:          "pylsp",
			Command:       []string{"pylsp"},
			FileTypes:     []string{"python"},
			ShareSessions: true,
		})
	}

	// Typescript / Javascript
	if _, err := exec.LookPath("typescript-language-server"); err == nil {
		configs = append(configs, lspx.ServerConfig{
			Name:          "typescript-language-server",
			Command:       []string{"typescript-language-server", "--stdio"},
			FileTypes:     []string{"javascript", "typescript", "javascriptreact", "typescriptreact"},
			ShareSessions: true,
		})
	}

	// Rust Analyzer
	if _, err := exec.LookPath("rust-analyzer"); err == nil {
		configs = append(configs, lspx.ServerConfig{
			Name:          "rust-analyzer",
			Command:       []string{"rust-analyzer"},
			FileTypes:     []string{"rust"},
			ShareSessions: true,
		})
	}

	// Clangd (C/C++)
	if _, err := exec.LookPath("clangd"); err == nil {
		configs = append(configs, lspx.ServerConfig{
			Name:          "clangd",
			Command:       []string{"clangd"},
			FileTypes:     []string{"c", "cpp", "objective-c", "objective-cpp"},
			ShareSessions: true,
		})
	}

	return configs
}

// pathToURI converts a filepath to a URI string.
func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	return "file://" + filepath.ToSlash(abs)
}

// uriToPath converts a URI string to a filepath.
func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return filepath.FromSlash(uri[7:])
	}
	return uri
}

// GetLanguageID maps file extensions to LSP LanguageIDs.
func GetLanguageID(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc", ".cxx":
		return "cpp"
	default:
		if len(ext) > 1 {
			return ext[1:]
		}
		return ""
	}
}

// findRootByMarkers walks up the directory hierarchy starting from path
// to locate a directory containing any of the specified marker files.
func findRootByMarkers(path string, markers []string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)

	if len(markers) == 0 {
		return abs
	}

	current := abs
	for {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return abs
}

// NewClient creates and initializes a multiplexed LSP client with all configured servers.
func NewClient(ctx context.Context, rootPath string) (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load LSP config: %w", err)
	}
	var langs []string
	for _, sc := range cfg.Servers {
		langs = append(langs, sc.FileTypes...)
	}
	return NewClientWithLangs(ctx, rootPath, langs)
}

// NewClientWithLangs creates and initializes a multiplexed LSP client with only the specified languages.
func NewClientWithLangs(ctx context.Context, rootPath string, langs []string) (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load LSP config: %w", err)
	}

	needed := make(map[string]bool)
	for _, l := range langs {
		needed[l] = true
	}

	var servers []lspx.ServerConfig
	var activeConfigs []ServerConfig
	for _, sc := range cfg.Servers {
		hasNeededLang := false
		for _, ft := range sc.FileTypes {
			if needed[ft] {
				hasNeededLang = true
				break
			}
		}
		if !hasNeededLang {
			continue
		}

		if len(sc.Command) > 0 {
			if _, err := exec.LookPath(sc.Command[0]); err == nil {
				servers = append(servers, sc.ToLspx())
				activeConfigs = append(activeConfigs, sc)
			}
		}
	}

	if len(servers) == 0 {
		return &Client{rootPath: rootPath}, nil
	}

	// Gather all root markers from active servers
	var markers []string
	seen := make(map[string]bool)
	for _, ac := range activeConfigs {
		for _, m := range ac.RootMarkers {
			if !seen[m] {
				seen[m] = true
				markers = append(markers, m)
			}
		}
	}

	canonicalRoot := findRootByMarkers(rootPath, markers)

	opts := lspx.ClientOptions{
		Servers: servers,
		RootURI: pathToURI(canonicalRoot),
		Aggregate: lspx.AggregateOptions{
			Diagnostics: true,
			Completions: true,
			References:  true,
		},
	}

	lspc, err := lspx.NewClient(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create lspx client: %w", err)
	}

	// Workaround for lspx routing bug on empty URI requests (like WorkspaceSymbol)
	// We register the empty string "" to map to the first available language server type.
	if len(servers) > 0 {
		var firstLang string
		if len(servers[0].FileTypes) > 0 {
			firstLang = servers[0].FileTypes[0]
		}
		if firstLang != "" {
			_ = lspc.DidOpen(ctx, &lspx.DidOpenTextDocumentParams{
				TextDocument: lspx.TextDocumentItem{
					URI:        "",
					LanguageID: lspx.LanguageKind(firstLang),
					Version:    1,
					Text:       "",
				},
			})
		}
	}

	c := &Client{
		lspClient:     lspc,
		rootPath:      canonicalRoot,
		activeServers: servers,
		diagnostics:   make(map[string][]lspx.Diagnostic),
	}

	// Capture diagnostics publish notifications
	lspc.OnNotification(lspx.MethodTextDocumentPublishDiagnostics, func(params *lspx.PublishDiagnosticsParams) {
		c.diagnosticsMu.Lock()
		defer c.diagnosticsMu.Unlock()
		pathKey := uriToPath(params.URI)
		c.diagnostics[pathKey] = params.Diagnostics
	})

	return c, nil
}

// Close shuts down the client.
func (c *Client) Close() error {
	if c.lspClient == nil {
		return nil
	}
	return c.lspClient.Close()
}

// RawClient returns the underlying lspx client.
func (c *Client) RawClient() *lspx.Client {
	return c.lspClient
}

// EnsureOpened makes sure a file is registered with the LSP server.
func (c *Client) EnsureOpened(ctx context.Context, path string) error {
	if c.lspClient == nil {
		return nil
	}
	uri := pathToURI(path)
	if c.lspClient.IsOpened(uri) {
		return nil
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	langID := GetLanguageID(path)
	if langID == "" {
		return fmt.Errorf("unsupported file type: %s", path)
	}

	err = c.lspClient.DidOpen(ctx, &lspx.DidOpenTextDocumentParams{
		TextDocument: lspx.TextDocumentItem{
			URI:        uri,
			LanguageID: lspx.LanguageKind(langID),
			Version:    1,
			Text:       string(contentBytes),
		},
	})
	if err != nil {
		return fmt.Errorf("DidOpen failed: %w", err)
	}

	return nil
}

type Diagnostic struct {
	Path string
	lspx.Diagnostic
}

// GetDiagnostics retrieves diagnostics for a file or directory.
func (c *Client) GetDiagnostics(ctx context.Context, path string) ([]Diagnostic, error) {
	if c.lspClient == nil {
		return nil, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	absPath = filepath.Clean(absPath)

	isDir := false
	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		isDir = true
	}

	if !isDir {
		_ = c.EnsureOpened(ctx, absPath)

		// Wait briefly for asynchronous notification propagation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}

		c.diagnosticsMu.RLock()
		defer c.diagnosticsMu.RUnlock()

		diags := c.diagnostics[absPath]
		result := make([]Diagnostic, len(diags))
		for i, d := range diags {
			result[i] = Diagnostic{
				Path:       absPath,
				Diagnostic: d,
			}
		}
		return result, nil
	}

	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()

	var result []Diagnostic
	for docPath, diags := range c.diagnostics {
		rel, err := filepath.Rel(absPath, docPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			for _, d := range diags {
				result = append(result, Diagnostic{
					Path:       docPath,
					Diagnostic: d,
				})
			}
		}
	}
	return result, nil
}

// Search performs workspace-wide symbol searches using WorkspaceSymbol.
func (c *Client) Search(ctx context.Context, query string) ([]lspx.WorkspaceSymbol, error) {
	if c.lspClient == nil {
		return nil, nil
	}

	params := &lspx.WorkspaceSymbolParams{
		Query: query,
	}
	res, err := c.lspClient.WorkspaceSymbol(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("WorkspaceSymbol failed: %w", err)
	}

	if res == nil {
		return nil, nil
	}

	if res.ArrayOfWorkspaceSymbol != nil {
		return *res.ArrayOfWorkspaceSymbol, nil
	}

	if res.ArrayOfSymbolInformation != nil {
		symbols := make([]lspx.WorkspaceSymbol, len(*res.ArrayOfSymbolInformation))
		for i, sym := range *res.ArrayOfSymbolInformation {
			symbols[i] = lspx.WorkspaceSymbol{
				Name: sym.Name,
				Kind: sym.Kind,
				Location: lspx.WorkspaceSymbolLocation{
					Location: &lspx.Location{
						URI:   sym.Location.URI,
						Range: sym.Location.Range,
					},
				},
				ContainerName: sym.ContainerName,
			}
		}
		return symbols, nil
	}

	return nil, nil
}
