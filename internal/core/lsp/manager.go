package lsp

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/masterkeysrd/lspx"
)

type Manager struct {
	mu                   sync.Mutex
	client               *Client
	pendingSuggestions   map[string]LspSuggestion
	dismissedSuggestions map[string]bool
}

// NewManager creates a new LSP manager instance.
func NewManager() *Manager {
	return &Manager{
		pendingSuggestions:   make(map[string]LspSuggestion),
		dismissedSuggestions: make(map[string]bool),
	}
}

func (m *Manager) GetClient(ctx context.Context, rootPath string) (*Client, error) {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()

	if client != nil {
		return client, nil
	}

	newClient, err := NewClient(ctx, rootPath)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == nil {
		m.client = newClient
	} else {
		// Another goroutine initialized it while we were creating one
		_ = newClient.Close()
	}
	return m.client, nil
}

// RestartClient shuts down and recreates the client for the specified workspace path.
func (m *Manager) RestartClient(ctx context.Context, rootPath string) error {
	client, err := NewClient(ctx, rootPath)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		_ = m.client.Close()
	}
	m.client = client
	return nil
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

	langID := getLanguageID(absPath)
	if langID == "" {
		return
	}

	if m.client == nil {
		if _, ok := Presets[langID]; ok {
			m.addSuggestionLocked(langID)
		}
		return
	}

	// Verify if configured
	cfg, err := LoadConfig()
	if err != nil {
		return
	}
	hasLang := false
	for _, sc := range cfg.Servers {
		for _, ft := range sc.FileTypes {
			if ft == langID {
				hasLang = true
				break
			}
		}
	}

	if !hasLang {
		if _, ok := Presets[langID]; ok {
			m.addSuggestionLocked(langID)
		}
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

	langID := getLanguageID(absPath)
	if langID == "" {
		return
	}

	if m.client == nil {
		if _, ok := Presets[langID]; ok {
			m.addSuggestionLocked(langID)
		}
		return
	}

	// Verify if configured
	cfg, err := LoadConfig()
	if err != nil {
		return
	}
	hasLang := false
	for _, sc := range cfg.Servers {
		for _, ft := range sc.FileTypes {
			if ft == langID {
				hasLang = true
				break
			}
		}
	}

	if !hasLang {
		if _, ok := Presets[langID]; ok {
			m.addSuggestionLocked(langID)
		}
		return
	}

	_ = m.client.EnsureOpened(ctx, absPath)
}
