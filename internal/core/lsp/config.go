package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

type Config struct {
	Servers []lspx.ServerConfig `json:"servers"`
}

type LspSuggestion struct {
	Language   string   `json:"language"`
	ServerName string   `json:"server_name"`
	Command    []string `json:"command"`
}

var Presets = map[string]lspx.ServerConfig{
	"go": {
		Name:          "gopls",
		Command:       []string{"gopls"},
		FileTypes:     []string{"go"},
		ShareSessions: true,
	},
	"python": {
		Name:          "pyright",
		Command:       []string{"pyright-langserver", "--stdio"},
		FileTypes:     []string{"python"},
		ShareSessions: true,
	},
	"typescript": {
		Name:          "typescript-language-server",
		Command:       []string{"typescript-language-server", "--stdio"},
		FileTypes:     []string{"javascript", "typescript", "javascriptreact", "typescriptreact"},
		ShareSessions: true,
	},
	"rust": {
		Name:          "rust-analyzer",
		Command:       []string{"rust-analyzer"},
		FileTypes:     []string{"rust"},
		ShareSessions: true,
	},
	"c": {
		Name:          "clangd",
		Command:       []string{"clangd"},
		FileTypes:     []string{"c", "cpp", "objective-c", "objective-cpp"},
		ShareSessions: true,
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
			return &Config{Servers: []lspx.ServerConfig{}}, nil
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
