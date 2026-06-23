package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

type InitializationOptions struct {
	ProjectName      string
	SelectedProvider string
	APIKey           string
	Endpoint         string
	DefaultModel     string
	Theme            string
	AuthorizedTools  map[string]bool
}

// Initialize coordinates the setup process for a new workspace.
func (w *Workspace) Initialize(ctx context.Context, opts InitializationOptions) error {
	// 1. Write sentinel file in the XDG workspace directory
	wsDir, err := xdg.WorkspaceDir(w.cwd)
	if err != nil {
		return fmt.Errorf("failed to get XDG workspace dir: %w", err)
	}
	if err := fsutil.EnsureDir(wsDir); err != nil {
		return fmt.Errorf("failed to ensure XDG workspace dir exists: %w", err)
	}

	sentinelPath := filepath.Join(wsDir, "setup.json")
	sentinelData := map[string]any{
		"version":        "1.0.0",
		"initialized_at": time.Now().Format(time.RFC3339),
		"workspace_path": w.cwd,
	}
	sentinelBytes, err := json.MarshalIndent(sentinelData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sentinel data: %w", err)
	}
	if err := os.WriteFile(sentinelPath, sentinelBytes, 0644); err != nil {
		return fmt.Errorf("failed to write sentinel file: %w", err)
	}

	// 2. Write theme configuration in XDG config directory
	cfgDir, err := xdg.SubConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get XDG config dir: %w", err)
	}
	if err := fsutil.EnsureDir(cfgDir); err != nil {
		return fmt.Errorf("failed to ensure XDG config dir exists: %w", err)
	}

	cfgPath := filepath.Join(cfgDir, "theme.json")
	cfgData := map[string]any{
		"theme": opts.Theme,
	}
	cfgBytes, err := json.MarshalIndent(cfgData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config data: %w", err)
	}
	if err := os.WriteFile(cfgPath, cfgBytes, 0644); err != nil {
		return fmt.Errorf("failed to write theme config file: %w", err)
	}

	var allowedTools []string
	for tool, allowed := range opts.AuthorizedTools {
		if allowed {
			allowedTools = append(allowedTools, tool)
		}
	}
	sort.Strings(allowedTools)

	var policies *warp.Policies
	if len(allowedTools) > 0 {
		policies = &warp.Policies{
			Tools: &warp.ToolPolicies{
				Include: allowedTools,
			},
		}
	}

	// 3. Create WORKSPACE.md in the workspace root using warp formatting
	wsDef := &warp.WorkspaceDef{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindWorkspace,
			Metadata: warp.Metadata{
				Name: opts.ProjectName,
			},
		},
		Spec: warp.WorkspaceDefSpec{
			Projects:        []string{"."},
			DefaultProvider: opts.SelectedProvider,
			Policies:        policies,
		},
	}
	wsData, err := warp.Format(wsDef)
	if err != nil {
		return fmt.Errorf("failed to format workspace definition: %w", err)
	}
	wsPath := filepath.Join(w.cwd, "WORKSPACE.md")
	if err := os.WriteFile(wsPath, wsData, 0644); err != nil {
		return fmt.Errorf("failed to write WORKSPACE.md: %w", err)
	}

	// 4. Create ./.agents/providers/<provider-name>.yaml in the workspace root
	providersDir := filepath.Join(w.cwd, ".agents", "providers")
	if err := os.MkdirAll(providersDir, 0755); err != nil {
		return fmt.Errorf("failed to create providers directory: %w", err)
	}

	// Let's build the model provider resource.
	// Find preset for base configuration (models/limits)
	var baseProvider *warp.ModelProvider
	for _, p := range w.ProvidersPresets() {
		if p.Metadata.Name == opts.SelectedProvider {
			baseProvider = p
			break
		}
	}

	var providerSpec warp.ModelProviderSpec
	if baseProvider != nil {
		providerSpec = baseProvider.Spec
	}

	// Override endpoint and default model if provided
	if opts.Endpoint != "" {
		providerSpec.Endpoint = opts.Endpoint
	}
	if opts.DefaultModel != "" {
		providerSpec.DefaultModel = opts.DefaultModel
	}

	providerRes := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: opts.SelectedProvider,
			},
		},
		Spec: providerSpec,
	}

	providerData, err := warp.Format(providerRes)
	if err != nil {
		return fmt.Errorf("failed to format provider configuration: %w", err)
	}
	providerPath := filepath.Join(providersDir, opts.SelectedProvider+".yaml")
	if err := os.WriteFile(providerPath, providerData, 0644); err != nil {
		return fmt.Errorf("failed to write provider yaml: %w", err)
	}

	// 5. Secrets Management: write API Key to local .env if provided
	if opts.APIKey != "" {
		envVarName := "API_KEY"
		if baseProvider != nil && baseProvider.Spec.Auth != nil {
			if baseProvider.Spec.Auth.Env != "" {
				envVarName = baseProvider.Spec.Auth.Env
			}
		}

		envPath := filepath.Join(w.cwd, ".env")
		envLine := fmt.Sprintf("%s=%s\n", envVarName, opts.APIKey)

		// Append or write .env file
		f, err := os.OpenFile(envPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to open .env file: %w", err)
		}
		if _, err := f.WriteString(envLine); err != nil {
			f.Close()
			return fmt.Errorf("failed to write to .env file: %w", err)
		}
		f.Close()

		// Try to ensure .env is ignored in .gitignore
		gitignorePath := filepath.Join(w.cwd, ".gitignore")
		ignoreContent, err := os.ReadFile(gitignorePath)
		hasEnv := false
		if err == nil {
			lines := strings.Split(string(ignoreContent), "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) == ".env" {
					hasEnv = true
					break
				}
			}
		}
		if !hasEnv {
			fGitignore, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				_, _ = fGitignore.WriteString("\n# TaskSmith secrets\n.env\n")
				fGitignore.Close()
			}
		}
	}

	return nil
}
