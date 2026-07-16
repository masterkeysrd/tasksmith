package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

type ServerConfig struct {
	Name                  string            `json:"name"`
	Command               []string          `json:"command"`
	FileTypes             []string          `json:"filetypes"`
	Env                   map[string]string `json:"env,omitempty"`
	ShareSessions         bool              `json:"share_sessions"`
	RootMarkers           []string          `json:"root_markers,omitempty"`
	InitializationOptions any               `json:"initialization_options,omitempty"`
}

func (s ServerConfig) ToLspx() lspx.ServerConfig {
	return lspx.ServerConfig{
		Name:                  s.Name,
		Command:               s.Command,
		FileTypes:             s.FileTypes,
		Env:                   s.Env,
		ShareSessions:         s.ShareSessions,
		InitializationOptions: s.InitializationOptions,
	}
}

type Config struct {
	Servers []ServerConfig `json:"servers"`
}

type LspSuggestion struct {
	Language   string   `json:"language"`
	ServerName string   `json:"server_name"`
	Command    []string `json:"command"`
}

var Presets = map[string]ServerConfig{
	"go": {
		Name:          "gopls",
		Command:       []string{"gopls"},
		FileTypes:     []string{"go"},
		ShareSessions: true,
		RootMarkers:   []string{"go.work", "go.mod", ".git"},
		InitializationOptions: map[string]any{
			"gopls": map[string]any{
				"staticcheck": true,
				"directoryFilters": []string{
					"-.git",
					"-.vscode",
					"-.idea",
					"-.vscode-test",
					"-node_modules",
				},
				"analyses": map[string]any{
					"nilness":      true,
					"unusedparams": true,
					"unusedwrite":  true,
					"useany":       true,
					"asign":        true,
				},
			},
		},
	},
	"python": {
		Name:          "pyright",
		Command:       []string{"pyright-langserver", "--stdio"},
		FileTypes:     []string{"python"},
		ShareSessions: true,
		RootMarkers:   []string{"pyproject.toml", "setup.py", "requirements.txt", ".git"},
	},
	"typescript": {
		Name:    "typescript-language-server",
		Command: []string{"typescript-language-server", "--stdio"},
		FileTypes: []string{
			"javascript",
			"javascriptreact",
			"javascript.jsx",
			"typescript",
			"typescriptreact",
			"typescript.tsx",
		},
		ShareSessions: true,
		RootMarkers:   []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"},
		InitializationOptions: map[string]any{
			"hostInfo": "neovim",
		},
	},
	"rust": {
		Name:          "rust-analyzer",
		Command:       []string{"rust-analyzer"},
		FileTypes:     []string{"rust"},
		ShareSessions: true,
		RootMarkers:   []string{"Cargo.toml", ".git"},
	},
	"c": {
		Name:          "clangd",
		Command:       []string{"clangd"},
		FileTypes:     []string{"c", "cpp", "objective-c", "objective-cpp"},
		ShareSessions: true,
		RootMarkers:   []string{"Makefile", "CMakeLists.txt", "compile_commands.json", ".git"},
	},
}

// ConfigPath returns the absolute path to the user's lsp.config file.
func ConfigPath() (string, error) {
	dir, err := xdg.SubConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lsp.config"), nil
}

// LoadConfig reads the lsp.config JSON file. If it doesn't exist, it returns an empty Config.
func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Servers: []ServerConfig{}}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveConfig writes the lsp.config JSON file.
func SaveConfig(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
