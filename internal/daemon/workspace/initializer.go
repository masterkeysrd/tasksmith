package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

// InitOptions configures the initialization of a workspace directory.
type InitOptions struct {
	ProjectName      string               `json:"project_name,omitempty"`
	Provider         string               `json:"provider,omitempty"`
	SelectedProvider string               `json:"selected_provider,omitempty"`
	APIKey           string               `json:"api_key,omitempty"`
	Endpoint         string               `json:"endpoint,omitempty"`
	DefaultModel     string               `json:"default_model,omitempty"`
	DefaultModels    map[ModelRole]string `json:"default_models,omitempty"`
	DefaultAgent     string               `json:"default_agent,omitempty"`
	AuthorizedTools  []string             `json:"authorized_tools,omitempty"`
	TrustOnly        bool                 `json:"trust_only,omitempty"`
	AuthType         string               `json:"auth_type,omitempty"`
	AuthEnv          string               `json:"auth_env,omitempty"`
	AuthFile         string               `json:"auth_file,omitempty"`
	AuthHeader       string               `json:"auth_header,omitempty"`
}

func (o InitOptions) providerName() string {
	if o.Provider != "" {
		return o.Provider
	}
	return o.SelectedProvider
}

// IsConfigured reports whether dir contains a WORKSPACE.md file or a .agents directory.
func IsConfigured(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	wsFile := filepath.Join(absDir, warp.WorkspaceFileName)
	if info, err := os.Stat(wsFile); err == nil && !info.IsDir() {
		return true
	}

	agentsDir := filepath.Join(absDir, warp.DefaultAgentsDir)
	if info, err := os.Stat(agentsDir); err == nil && info.IsDir() {
		return true
	}

	return false
}

// IsInitialized reports whether dir is already configured on disk (via WORKSPACE.md
// or .agents/) or has a setup.json sentinel in its XDG workspace data directory.
func IsInitialized(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	if IsConfigured(absDir) {
		return true
	}

	wsDir, err := xdg.WorkspaceDir(absDir)
	if err != nil {
		return false
	}

	sentinelPath := filepath.Join(wsDir, "setup.json")
	if info, err := os.Stat(sentinelPath); err == nil && !info.IsDir() {
		return true
	}

	return false
}

// CopyConfig copies WORKSPACE.md, .agents/ (recursively), and .env (if present)
// from srcDir into dstDir. It is used during bare-repository worktree promotion
// flows (C3a/C3b) to transfer workspace configuration into a newly created worktree.
func CopyConfig(srcDir, dstDir string) error {
	if strings.TrimSpace(srcDir) == "" || strings.TrimSpace(dstDir) == "" {
		return errors.New("workspace: source and destination directories must not be empty")
	}

	srcAbs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("workspace: resolve source dir: %w", err)
	}
	dstAbs, err := filepath.Abs(dstDir)
	if err != nil {
		return fmt.Errorf("workspace: resolve destination dir: %w", err)
	}

	if err := os.MkdirAll(dstAbs, 0o755); err != nil {
		return fmt.Errorf("workspace: create destination dir: %w", err)
	}

	// 1. Copy WORKSPACE.md if present
	srcWS := filepath.Join(srcAbs, warp.WorkspaceFileName)
	if info, err := os.Stat(srcWS); err == nil && !info.IsDir() {
		if err := copyFile(srcWS, filepath.Join(dstAbs, warp.WorkspaceFileName), info.Mode().Perm()); err != nil {
			return fmt.Errorf("workspace: copy WORKSPACE.md: %w", err)
		}
	}

	// 2. Copy .agents/ recursively if present
	srcAgents := filepath.Join(srcAbs, warp.DefaultAgentsDir)
	if info, err := os.Stat(srcAgents); err == nil && info.IsDir() {
		dstAgents := filepath.Join(dstAbs, warp.DefaultAgentsDir)
		if err := copyDirRecursive(srcAgents, dstAgents); err != nil {
			return fmt.Errorf("workspace: copy .agents: %w", err)
		}
	}

	// 3. Copy .env if present
	srcEnv := filepath.Join(srcAbs, ".env")
	if info, err := os.Stat(srcEnv); err == nil && !info.IsDir() {
		if err := copyFile(srcEnv, filepath.Join(dstAbs, ".env"), info.Mode().Perm()); err != nil {
			return fmt.Errorf("workspace: copy .env: %w", err)
		}
	}

	return nil
}

// Initialize initializes the workspace directory at dir according to opts.
// It records a setup.json sentinel in the XDG workspace data directory, writes
// any provided APIKey into <dir>/.env (ensuring .env is listed in .gitignore),
// and—unless opts.TrustOnly is true—writes WORKSPACE.md and .agents/providers/<provider>.yaml.
func Initialize(ctx context.Context, dir string, opts InitOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("workspace: directory path must not be empty")
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("workspace: resolve directory %q: %w", dir, err)
	}

	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("workspace: ensure directory %q: %w", absDir, err)
	}

	// 1. Write setup.json sentinel in XDG workspace directory
	wsDir, err := xdg.WorkspaceDir(absDir)
	if err != nil {
		return fmt.Errorf("workspace: get XDG workspace dir: %w", err)
	}
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return fmt.Errorf("workspace: create XDG workspace dir: %w", err)
	}

	sentinelPath := filepath.Join(wsDir, "setup.json")
	sentinelData := map[string]any{
		"version":        "1.0.0",
		"initialized_at": time.Now().UTC().Format(time.RFC3339),
		"workspace_path": absDir,
	}
	sentinelBytes, err := json.MarshalIndent(sentinelData, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal setup.json: %w", err)
	}
	if err := os.WriteFile(sentinelPath, append(sentinelBytes, '\n'), 0o644); err != nil {
		return fmt.Errorf("workspace: write setup.json: %w", err)
	}

	provName := opts.providerName()
	baseProvider := findWarpPreset(provName)

	// 2. Persist API key in <dir>/.env and ensure .env is in <dir>/.gitignore
	if opts.APIKey != "" {
		envVarName := "API_KEY"
		if opts.AuthEnv != "" {
			envVarName = opts.AuthEnv
		} else if baseProvider != nil && baseProvider.Spec.Auth != nil && baseProvider.Spec.Auth.Env != "" {
			envVarName = baseProvider.Spec.Auth.Env
		}

		envPath := filepath.Join(absDir, ".env")
		if err := env.SetInFile(envPath, envVarName, opts.APIKey); err != nil {
			return fmt.Errorf("workspace: write .env: %w", err)
		}

		if err := ensureGitignoreEntry(absDir, ".env"); err != nil {
			return fmt.Errorf("workspace: update .gitignore: %w", err)
		}
	}

	// 3. If TrustOnly is set, skip writing WORKSPACE.md and .agents/providers/
	if opts.TrustOnly {
		return nil
	}

	// 4. Write WORKSPACE.md
	projectName := opts.ProjectName
	if projectName == "" {
		projectName = filepath.Base(absDir)
	}

	var policies *warp.Policies
	if len(opts.AuthorizedTools) > 0 {
		allowed := cloneSlice(opts.AuthorizedTools)
		sort.Strings(allowed)
		policies = &warp.Policies{
			Tools: &warp.ToolPolicies{
				Include: allowed,
			},
		}
	}

	var defaultModels *warp.ModelDefaults
	if len(opts.DefaultModels) > 0 {
		defaultModels = &warp.ModelDefaults{
			Completion: opts.DefaultModels[RoleCompletion],
			Decision:   opts.DefaultModels[RoleDecision],
			Embedding:  opts.DefaultModels[RoleEmbedding],
		}
	}

	wsDef := &warp.WorkspaceDef{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindWorkspace,
			Metadata: warp.Metadata{
				Name: projectName,
			},
		},
		Spec: warp.WorkspaceDefSpec{
			Projects:        []string{"."},
			DefaultProvider: provName,
			DefaultModel:    opts.DefaultModel,
			DefaultModels:   defaultModels,
			DefaultAgent:    opts.DefaultAgent,
			Policies:        policies,
		},
	}

	wsData, err := warp.Format(wsDef)
	if err != nil {
		return fmt.Errorf("workspace: format WORKSPACE.md: %w", err)
	}
	wsPath := filepath.Join(absDir, warp.WorkspaceFileName)
	if err := os.WriteFile(wsPath, wsData, 0o644); err != nil {
		return fmt.Errorf("workspace: write WORKSPACE.md: %w", err)
	}

	// 5. Write .agents/providers/<provider>.yaml when a provider is specified
	if provName != "" {
		providersDir := filepath.Join(absDir, warp.DefaultAgentsDir, "providers")
		if err := os.MkdirAll(providersDir, 0o755); err != nil {
			return fmt.Errorf("workspace: create providers directory: %w", err)
		}

		var providerSpec warp.ModelProviderSpec
		if baseProvider != nil {
			providerSpec = *baseProvider.Spec.DeepCopy()
		} else {
			providerSpec.Type = provName
		}

		if opts.Endpoint != "" {
			providerSpec.Endpoint = opts.Endpoint
		}
		if opts.DefaultModel != "" {
			providerSpec.DefaultModel = opts.DefaultModel
		}
		if defaultModels != nil {
			providerSpec.DefaultModels = defaultModels.DeepCopy()
		}

		if opts.AuthType != "" {
			authConf := &warp.ProviderAuth{
				Type: opts.AuthType,
			}
			if opts.AuthType == "api-key" && opts.AuthHeader != "" {
				authConf.Header = opts.AuthHeader
			}
			if opts.AuthFile != "" {
				authConf.File = opts.AuthFile
			} else if opts.AuthEnv != "" {
				authConf.Env = opts.AuthEnv
			} else if baseProvider != nil && baseProvider.Spec.Auth != nil && baseProvider.Spec.Auth.Env != "" {
				authConf.Env = baseProvider.Spec.Auth.Env
			} else {
				authConf.Env = "API_KEY"
			}
			providerSpec.Auth = authConf
		} else if baseProvider != nil && baseProvider.Spec.Auth != nil {
			authConf := baseProvider.Spec.Auth.DeepCopy()
			if opts.APIKey != "" && authConf.Env == "" {
				authConf.Env = "API_KEY"
			}
			providerSpec.Auth = authConf
		} else if opts.APIKey != "" || opts.AuthEnv != "" {
			envKey := opts.AuthEnv
			if envKey == "" {
				envKey = "API_KEY"
			}
			providerSpec.Auth = &warp.ProviderAuth{
				Type: "bearer",
				Env:  envKey,
			}
		}

		providerRes := &warp.ModelProvider{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindModelProvider,
				Metadata: warp.Metadata{
					Name: provName,
				},
			},
			Spec: providerSpec,
		}

		providerData, err := warp.Format(providerRes)
		if err != nil {
			return fmt.Errorf("workspace: format provider manifest: %w", err)
		}
		providerPath := filepath.Join(providersDir, provName+".yaml")
		if err := os.WriteFile(providerPath, providerData, 0o644); err != nil {
			return fmt.Errorf("workspace: write provider manifest: %w", err)
		}
	}

	return nil
}

// Init initializes the workspace directory at dir according to opts and opens it.
func Init(ctx context.Context, dir string, opts InitOptions, openOpts ...Option) (Workspace, error) {
	if err := Initialize(ctx, dir, opts); err != nil {
		return nil, err
	}
	return Open(ctx, dir, openOpts...)
}

// Initialize initializes the workspace's root directory and reloads its registry and environment.
func (w *workspace) Initialize(ctx context.Context, opts InitOptions) error {
	if err := Initialize(ctx, w.RootDir(), opts); err != nil {
		return err
	}
	return w.Reload(ctx)
}

func ensureGitignoreEntry(dir, entry string) error {
	gitignorePath := filepath.Join(dir, ".gitignore")
	existing, err := os.ReadFile(gitignorePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	for line := range strings.SplitSeq(string(existing), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if existing[len(existing)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString("# TaskSmith secrets\n")
	b.WriteString(entry)
	b.WriteByte('\n')

	return os.WriteFile(gitignorePath, []byte(b.String()), 0o644)
}

func copyFile(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
}

func copyDirRecursive(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}
