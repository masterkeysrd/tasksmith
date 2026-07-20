package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/masterkeysrd/lspx"
)

type Manager struct {
	mu                   sync.Mutex
	client               *Client
	rootPath             string
	activeLangs          map[string]bool
	pendingSuggestions   map[string]LspSuggestion
	dismissedSuggestions map[string]bool
}

// NewManager creates a new LSP manager instance.
func NewManager() *Manager {
	return &Manager{
		activeLangs:          make(map[string]bool),
		pendingSuggestions:   make(map[string]LspSuggestion),
		dismissedSuggestions: make(map[string]bool),
	}
}

func detectWorkspaceLangs(rootPath string) []string {
	var langs []string
	seen := make(map[string]bool)

	// Scan the workspace directory (non-recursively, skipping heavy folders)
	_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if lang := GetLanguageID(path); lang != "" {
			if !seen[lang] {
				seen[lang] = true
				langs = append(langs, lang)
			}
		}
		return nil
	})
	return langs
}

func (m *Manager) ensureClientForLangsLocked(ctx context.Context, langs []string) error {
	needsRestart := false
	if m.client == nil {
		needsRestart = true
	} else if m.client.lspClient != nil {
		// Check if any desired language is missing from the active servers
		for _, lang := range langs {
			found := false
			for _, srv := range m.client.activeServers {
				for _, ft := range srv.FileTypes {
					if ft == lang {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				needsRestart = true
				break
			}
		}
	} else if len(langs) > 0 {
		// client was created as dummy/empty but now we have languages
		needsRestart = true
	}

	if !needsRestart {
		return nil
	}

	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}

	client, err := NewClientWithLangs(ctx, m.rootPath, langs)
	if err != nil {
		return err
	}
	m.client = client
	return nil
}

func (m *Manager) GetClient(ctx context.Context, rootPath string) (*Client, error) {
	m.mu.Lock()
	m.rootPath = rootPath

	if len(m.activeLangs) == 0 {
		for _, lang := range detectWorkspaceLangs(rootPath) {
			m.activeLangs[lang] = true
		}
	}

	var langs []string
	for l := range m.activeLangs {
		langs = append(langs, l)
	}

	err := m.ensureClientForLangsLocked(ctx, langs)
	client := m.client
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return client, nil
}

// RestartClient shuts down and recreates the client for the specified workspace path.
func (m *Manager) RestartClient(ctx context.Context, rootPath string) error {
	m.mu.Lock()
	m.rootPath = rootPath

	m.activeLangs = make(map[string]bool)
	for _, lang := range detectWorkspaceLangs(rootPath) {
		m.activeLangs[lang] = true
	}

	var langs []string
	for l := range m.activeLangs {
		langs = append(langs, l)
	}

	err := m.ensureClientForLangsLocked(ctx, langs)
	m.mu.Unlock()
	return err
}

// ServerStatus describes the state of a configured language server.
type ServerStatus struct {
	Name      string
	Command   []string
	FileTypes []string
	IsRunning bool
}

// GetStatus returns the status of all configured servers.
func (m *Manager) GetStatus() []ServerStatus {
	cfg, _ := LoadConfig()
	var statuses []ServerStatus

	m.mu.Lock()
	defer m.mu.Unlock()

	runningNames := make(map[string]bool)
	if m.client != nil {
		for _, s := range m.client.activeServers {
			runningNames[s.Name] = true
		}
	}

	if cfg != nil {
		for _, sc := range cfg.Servers {
			statuses = append(statuses, ServerStatus{
				Name:      sc.Name,
				Command:   sc.Command,
				FileTypes: sc.FileTypes,
				IsRunning: runningNames[sc.Name],
			})
		}
	}
	return statuses
}

// GetDiagnosticCounts retrieves summarized diagnostic counts for the specified path.
func (m *Manager) GetDiagnosticCounts(ctx context.Context, rootPath string) (errors, warnings, infos int, err error) {
	client, err := m.GetClient(ctx, rootPath)
	if err != nil {
		return 0, 0, 0, err
	}

	diags, err := client.GetDiagnostics(ctx, rootPath)
	if err != nil {
		return 0, 0, 0, err
	}

	for _, d := range diags {
		if d.Severity != nil {
			switch *d.Severity {
			case 1:
				errors++
			case 2:
				warnings++
			default:
				infos++
			}
		} else {
			infos++
		}
	}

	return errors, warnings, infos, nil
}

// CloseAll closes the active client.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}
}

// GetSuggestions returns all currently pending suggestions.
func (m *Manager) GetSuggestions() []LspSuggestion {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []LspSuggestion
	for _, s := range m.pendingSuggestions {
		result = append(result, s)
	}
	return result
}

// ActivateLanguage marks a language as active so its server is started.
func (m *Manager) ActivateLanguage(lang string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeLangs == nil {
		m.activeLangs = make(map[string]bool)
	}
	m.activeLangs[lang] = true
}

// DismissSuggestion removes a language suggestion and prevents it from showing again.
func (m *Manager) DismissSuggestion(lang string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pendingSuggestions != nil {
		delete(m.pendingSuggestions, lang)
	}
	if m.dismissedSuggestions == nil {
		m.dismissedSuggestions = make(map[string]bool)
	}
	m.dismissedSuggestions[lang] = true
}

func (m *Manager) addSuggestionLocked(lang string) {
	if m.pendingSuggestions == nil {
		m.pendingSuggestions = make(map[string]LspSuggestion)
	}
	if m.dismissedSuggestions == nil {
		m.dismissedSuggestions = make(map[string]bool)
	}

	if m.dismissedSuggestions[lang] {
		return
	}

	preset, ok := Presets[lang]
	if !ok {
		return
	}

	m.pendingSuggestions[lang] = LspSuggestion{
		Language:   lang,
		ServerName: preset.Name,
		Command:    preset.Command,
	}
}

// NotifyFileChanged handles notifying the LSP when a file's content is written or edited.
func (m *Manager) NotifyFileChanged(ctx context.Context, path string, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	absPath = filepath.Clean(absPath)

	langID := GetLanguageID(absPath)
	if langID == "" {
		return
	}

	if m.rootPath == "" {
		m.rootPath = filepath.Dir(absPath)
	}

	// Verify if configured
	cfg, err := LoadConfig()
	if err != nil {
		return
	}
	isConfigured := false
	for _, sc := range cfg.Servers {
		for _, ft := range sc.FileTypes {
			if ft == langID {
				isConfigured = true
				break
			}
		}
	}

	if !isConfigured {
		if _, ok := Presets[langID]; ok {
			m.addSuggestionLocked(langID)
		}
		return
	}

	// Ensure LSP is started for this language
	m.activeLangs[langID] = true
	var langs []string
	for l := range m.activeLangs {
		langs = append(langs, l)
	}
	if err := m.ensureClientForLangsLocked(ctx, langs); err != nil || m.client == nil || m.client.lspClient == nil {
		return
	}

	uri := pathToURI(absPath)
	if m.client.lspClient.IsOpened(uri) {
		_ = m.client.lspClient.DidChange(ctx, &lspx.DidChangeTextDocumentParams{
			TextDocument: lspx.VersionedTextDocumentIdentifier{
				URI:     uri,
				Version: 2,
			},
			ContentChanges: []lspx.TextDocumentContentChangeEvent{
				{
					TextDocumentContentChangeWholeDocument: &lspx.TextDocumentContentChangeWholeDocument{
						Text: content,
					},
				},
			},
		})
	} else {
		_ = m.client.lspClient.DidOpen(ctx, &lspx.DidOpenTextDocumentParams{
			TextDocument: lspx.TextDocumentItem{
				URI:        uri,
				LanguageID: lspx.LanguageKind(langID),
				Version:    1,
				Text:       content,
			},
		})
	}
}

// NotifyFileOpened registers file views with the LSP client.
func (m *Manager) NotifyFileOpened(ctx context.Context, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	absPath = filepath.Clean(absPath)

	langID := GetLanguageID(absPath)
	if langID == "" {
		return
	}

	if m.rootPath == "" {
		m.rootPath = filepath.Dir(absPath)
	}

	// Verify if configured
	cfg, err := LoadConfig()
	if err != nil {
		return
	}
	isConfigured := false
	for _, sc := range cfg.Servers {
		for _, ft := range sc.FileTypes {
			if ft == langID {
				isConfigured = true
				break
			}
		}
	}

	if !isConfigured {
		if _, ok := Presets[langID]; ok {
			m.addSuggestionLocked(langID)
		}
		return
	}

	// Ensure LSP is started for this language
	m.activeLangs[langID] = true
	var langs []string
	for l := range m.activeLangs {
		langs = append(langs, l)
	}
	if err := m.ensureClientForLangsLocked(ctx, langs); err != nil || m.client == nil {
		return
	}

	_ = m.client.EnsureOpened(ctx, absPath)
}
