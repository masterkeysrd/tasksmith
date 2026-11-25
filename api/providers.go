package api

// WorkspaceProvider describes one configured ModelProvider as the workspace
// resolves it across tiers. Unlike ProviderSummary (the built-in preset catalog
// used by auth flows), this reflects the declarative WARP manifests the daemon
// actually loaded: user-tier (~/.config/tasksmith/agents/providers/) and
// workspace-tier (<workspace-root>/.agents/providers/) files, including
// providers disabled with spec.enabled and workspace shadows that mute an
// inherited user provider.
type WorkspaceProvider struct {
	Name                 string               `json:"name"`
	DisplayName          string               `json:"display_name,omitempty"`
	Type                 string               `json:"type,omitempty"`
	Endpoint             string               `json:"endpoint,omitempty"`
	Scope                string               `json:"scope,omitempty"`    // "user", "workspace", "local", or "system"
	Path                 string               `json:"path,omitempty"`     // home-shortened manifest path of the owning file
	Enabled              bool                 `json:"enabled"`            // spec.enabled (true when omitted)
	Shadowed             bool                 `json:"shadowed,omitempty"` // a workspace-tier override mutes an inherited provider
	Status               string               `json:"status,omitempty"`   // "active", "disabled", "missing_auth", "local", "invalid_config"
	HasKey               bool                 `json:"has_key,omitempty"`
	AuthType             string               `json:"auth_type,omitempty"`
	AuthEnv              string               `json:"auth_env,omitempty"`
	Account              string               `json:"account,omitempty"`
	DefaultModel         string               `json:"default_model,omitempty"`
	Models               []ProviderModelInfo  `json:"models,omitempty"`
	Writable             bool                 `json:"writable,omitempty"`    // manifest can be patched by /providers
	Overridable          bool                 `json:"overridable,omitempty"` // a workspace shadow can mute an inherited provider
	SkippedPresetAliases []SkippedPresetAlias `json:"skipped_preset_aliases,omitempty"`
}

type SkippedPresetAlias struct {
	Alias             string `json:"alias"`
	ModelID           string `json:"model_id"`
	Owner             string `json:"owner,omitempty"`
	WorkspaceOverride bool   `json:"workspace_override,omitempty"`
}

type InitWorkspaceResponse struct {
	Status               string               `json:"status"`
	SkippedPresetAliases []SkippedPresetAlias `json:"skipped_preset_aliases,omitempty"`
}

// ProviderModelInfo is one model entry of a provider manifest, carrying the
// fields the /providers browser and alias editor read and write.
type ProviderModelInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Label   string   `json:"label,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	Context int      `json:"context,omitempty"`
	Output  int      `json:"output,omitempty"`
}

// SetProviderEnabledRequest patches spec.enabled on one provider manifest.
// When the owning manifest is not workspace-tier, disabling writes a
// workspace-tier shadow override instead of editing the inherited file.
type SetProviderEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// ProviderModelRequest is one model entry in an UpdateProviderModelsRequest.
type ProviderModelRequest struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Label   string   `json:"label,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	Context int      `json:"context,omitempty"`
	Output  int      `json:"output,omitempty"`
}

// UpdateProviderModelsRequest replaces spec.models on the owning manifest.
type UpdateProviderModelsRequest struct {
	Models       []ProviderModelRequest `json:"models"`
	DefaultModel string                 `json:"default_model,omitempty"`
	DefaultSet   bool                   `json:"default_set,omitempty"`
}

// CreateProviderRequest authors a new ModelProvider manifest in the user or
// workspace scope. Any supplied APIKey is persisted exclusively to the .env
// file for the scope (~/.config/tasksmith/.env or <workspace-root>/.env), never
// into the manifest itself.
type CreateProviderRequest struct {
	Scope        string                 `json:"scope"` // "user" or "workspace"
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Endpoint     string                 `json:"endpoint,omitempty"`
	APIKey       string                 `json:"api_key,omitempty"`
	AuthType     string                 `json:"auth_type,omitempty"`
	AuthEnv      string                 `json:"auth_env,omitempty"`
	DefaultModel string                 `json:"default_model,omitempty"`
	Models       []ProviderModelRequest `json:"models,omitempty"`
}

// AssignModelAliasesRequest declares the exact alias set the target model
// should own after the save. Aliases listed here are stripped from every other
// model across writable manifests (atomic move) before being added, so bare
// aliases stay 1-to-1. "default" is routed to the owning manifest's
// spec.defaultModels.completion; "@default" on the target model means keep.
type AssignModelAliasesRequest struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Aliases     []string `json:"aliases"`
	DefaultHere bool     `json:"default_here,omitempty"`
}

// AliasAssignment reports where one alias currently resolves.
type AliasAssignment struct {
	Alias      string   `json:"alias"`
	Provider   string   `json:"provider,omitempty"`
	Model      string   `json:"model,omitempty"`
	Duplicate  bool     `json:"duplicate,omitempty"`
	Duplicates []string `json:"duplicates,omitempty"` // extra "provider/model" owners when manual edits collided
	// WorkspaceDefault reports that WORKSPACE.md spec.modelAliases redirects
	// this bare alias, which shadows any per-model assignment.
	WorkspaceDefault bool `json:"workspace_default,omitempty"`
}

// AssignModelAliasesResponse confirms the state after an alias save: the
// provider's refreshed models plus the workspace-wide alias report.
type AssignModelAliasesResponse struct {
	Providers []WorkspaceProvider `json:"providers,omitempty"`
	Aliases   []AliasAssignment   `json:"aliases,omitempty"`
}

// ProviderCatalogEntry is one model from loom's built-in profile catalog
// (models.dev metadata) for a provider type, offered when managing a
// provider's model list.
type ProviderCatalogEntry struct {
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	ContextLimit int               `json:"context_limit,omitempty"`
	OutputLimit  int               `json:"output_limit,omitempty"`
	Capabilities ModelCapabilities `json:"capabilities"`
	Pricing      *ModelPricing     `json:"pricing,omitempty"`
	Listed       bool              `json:"listed,omitempty"` // already declared in the provider manifest
}
