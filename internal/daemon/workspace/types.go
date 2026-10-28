package workspace

import (
	"context"
	"maps"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// Workspace represents an active TaskSmith workspace, encapsulating its 4-tier
// resource registry and isolated environment variables.
type Workspace interface {
	Initialize(ctx context.Context, opts InitOptions) error
	Reload(ctx context.Context) error
	RootDir() string
	CWD() string
	Name() string
	IsSynthetic() bool
	Warnings() []string
	Env() map[string]string
	SetEnv(key, value string) error
	DefaultAgentName() string
	SetAgentOverride(name string)
	Presets() []Provider
	ProviderPresets() []Provider
	Providers() []Provider
	GetProvider(name string) (*Provider, bool)
	Agents() []Agent
	GetAgent(name string) (*Agent, bool)
	Decisions() []Decision
	GetDecision(name string) (*Decision, bool)
	Skills() []Skill
	GetSkill(name string) (*Skill, bool)
	Commands() []Command
	GetCommand(name string) (*Command, bool)
	Tools() []Tool
	GetTool(name string) (*Tool, bool)
	Toolkits() []Toolkit
	GetToolkit(name string) (*Toolkit, bool)
	MCPs() []MCP
	GetMCP(name string) (*MCP, bool)
	Projects() []Project
	Policies() *Policies
	ResolveModels(role ModelRole, explicit ...string) []string
	RenderAgent(name string, args map[string]string, globals map[string]any) (string, error)
	RenderSkill(name string, args map[string]string, globals map[string]any) (string, error)
	RenderCommand(name string, args map[string]string, globals map[string]any) (string, error)
	RenderDecision(name string, inputs map[string]any, globals map[string]any) (string, error)
	RenderWorkspace(globals map[string]any) (string, error)
	AgentContext(name string) api.ContextBreakdown
	ModelLimits(rawID string) ModelLimits
}

// Namespace constants identifying the 4-tier resource resolution hierarchy.
const (
	NamespaceSystem    = warp.NamespaceSystem
	NamespaceUser      = warp.NamespaceUser
	NamespaceWorkspace = warp.NamespaceWorkspace
	NamespaceLocal     = warp.NamespaceLocal
)

// Builtin agent identifiers shipped in the system namespace.
const (
	AgentNameMain               = "main"
	AgentNameAgentCreator       = "agent-creator"
	AgentNameProjectInitializer = "project-initializer"
	AgentNameProviderManager    = "provider-manager"
	AgentNameSkillCreator       = "skill-creator"
	AgentNameTitleGenerator     = "title-generator"
	AgentNameToolCreator        = "tool-creator"
)

// ModelRole identifies the cognitive capability role for model resolution.
type ModelRole string

const (
	// RoleCompletion is the role for conversational and tool-calling completion models.
	RoleCompletion ModelRole = ModelRole(warp.ModelRoleCompletion)

	// RoleDecision is the role for probabilistic decision evaluation models.
	RoleDecision ModelRole = ModelRole(warp.ModelRoleDecision)

	// RoleEmbedding is the role for dense vector embedding generation models.
	RoleEmbedding ModelRole = ModelRole(warp.ModelRoleEmbedding)
)

// Metadata holds the common identifying and descriptive fields shared by all
// workspace resources.
type Metadata struct {
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace,omitempty"`
	QualifiedName string            `json:"qualified_name,omitempty"`
	DisplayName   string            `json:"display_name,omitempty"`
	Description   string            `json:"description,omitempty"`
	Directory     string            `json:"directory,omitempty"`
	Visibility    string            `json:"visibility,omitempty"`
	Version       string            `json:"version,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// Provider describes a model provider configuration and its model catalog.
type Provider struct {
	Metadata
	Type          string               `json:"type"`
	Endpoint      string               `json:"endpoint,omitempty"`
	DefaultModel  string               `json:"default_model,omitempty"`
	DefaultModels map[ModelRole]string `json:"default_models,omitempty"`
	Auth          *ProviderAuth        `json:"auth,omitempty"`
	Headers       map[string]string    `json:"headers,omitempty"`
	Config        map[string]any       `json:"config,omitempty"`
	Models        []ProviderModel      `json:"models,omitempty"`
}

// ProviderAuth defines how to authenticate with a model provider or remote MCP server.
type ProviderAuth struct {
	Type   string `json:"type,omitempty"`
	Header string `json:"header,omitempty"`
	Env    string `json:"env,omitempty"`
	File   string `json:"file,omitempty"`
}

// ProviderModel describes a specific model available from a provider.
type ProviderModel struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Label        string             `json:"label,omitempty"`
	Limits       ModelLimits        `json:"limits"`
	Capabilities *ModelCapabilities `json:"capabilities,omitempty"`
	Cost         *ModelCost         `json:"cost,omitempty"`
}

// ModelLimits defines token and vector dimension limits for a model.
type ModelLimits struct {
	Context    int `json:"context,omitempty"`
	Output     int `json:"output,omitempty"`
	Dimensions int `json:"dimensions,omitempty"`
}

// ModelCapabilities specifies the workload capabilities supported by a model.
type ModelCapabilities struct {
	Completion       *bool                `json:"completion,omitempty"`
	Decision         *bool                `json:"decision,omitempty"`
	Embedding        *bool                `json:"embedding,omitempty"`
	Dimensions       []int                `json:"dimensions,omitempty"`
	Attachment       *bool                `json:"attachment,omitempty"`
	Tools            *bool                `json:"tools,omitempty"`
	StructuredOutput *bool                `json:"structured_output,omitempty"`
	Temperature      *bool                `json:"temperature,omitempty"`
	Thinking         []ThinkingCapability `json:"thinking,omitempty"`
	Modalities       *ModelModalities     `json:"modalities,omitempty"`
}

// SupportsCompletion reports whether the model supports text/chat completion.
// Defaults to true when Capabilities or Completion is nil.
func (c *ModelCapabilities) SupportsCompletion() bool {
	if c == nil || c.Completion == nil {
		return true
	}
	return *c.Completion
}

// SupportsDecision reports whether the model supports probabilistic decision evaluation.
// Defaults to false when Capabilities or Decision is nil.
func (c *ModelCapabilities) SupportsDecision() bool {
	if c == nil || c.Decision == nil {
		return false
	}
	return *c.Decision
}

// SupportsEmbedding reports whether the model supports vector embedding generation.
// Defaults to false when Capabilities or Embedding is nil.
func (c *ModelCapabilities) SupportsEmbedding() bool {
	if c == nil || c.Embedding == nil {
		return false
	}
	return *c.Embedding
}

// ThinkingCapability describes a reasoning configuration strategy supported by a model.
type ThinkingCapability struct {
	Type            string   `json:"type"`
	IsDefault       bool     `json:"is_default,omitempty"`
	AllowedEfforts  []string `json:"allowed_efforts,omitempty"`
	MaxBudgetTokens int      `json:"max_budget_tokens,omitempty"`
	Default         any      `json:"default,omitempty"`
}

// ModelModalities defines supported input and output data modalities for a model.
type ModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// ModelCost defines token pricing and conditional pricing tiers for a model.
type ModelCost struct {
	Input      float64         `json:"input"`
	Output     float64         `json:"output,omitempty"`
	CacheRead  float64         `json:"cache_read,omitempty"`
	CacheWrite float64         `json:"cache_write,omitempty"`
	Tiers      []ModelCostTier `json:"tiers,omitempty"`
}

// ModelCostTier defines a conditional pricing tier for a model.
type ModelCostTier struct {
	Label         string  `json:"label"`
	ConditionType string  `json:"condition_type,omitempty"`
	ConditionSize int     `json:"condition_size,omitempty"`
	Input         float64 `json:"input"`
	Output        float64 `json:"output,omitempty"`
	CacheRead     float64 `json:"cache_read,omitempty"`
	CacheWrite    float64 `json:"cache_write,omitempty"`
}

// Agent describes an autonomous agent, its persona instructions, and its allowed capabilities.
type Agent struct {
	Metadata
	Extends      string               `json:"extends,omitempty"`
	Instructions string               `json:"instructions,omitempty"`
	Triggers     []string             `json:"triggers,omitempty"`
	Models       []string             `json:"models,omitempty"`
	Temperature  *float64             `json:"temperature,omitempty"`
	Thinking     map[string]any       `json:"thinking,omitempty"`
	Skills       []string             `json:"skills,omitempty"`
	Commands     []string             `json:"commands,omitempty"`
	Subagents    []string             `json:"subagents,omitempty"`
	Tools        []string             `json:"tools,omitempty"`
	Policies     *Policies            `json:"policies,omitempty"`
	Context      api.ContextBreakdown `json:"context"`
}

// DecisionQuestionType identifies the evaluation mode of a DecisionQuestion.
type DecisionQuestionType string

const (
	// DecisionQuestionPredicate evaluates a binary true/false condition.
	DecisionQuestionPredicate DecisionQuestionType = DecisionQuestionType(warp.DecisionQuestionPredicate)

	// DecisionQuestionChoice selects among discrete categorical options.
	DecisionQuestionChoice DecisionQuestionType = DecisionQuestionType(warp.DecisionQuestionChoice)

	// DecisionQuestionScore rates an input across ordered rubric levels.
	DecisionQuestionScore DecisionQuestionType = DecisionQuestionType(warp.DecisionQuestionScore)
)

// Decision describes a probabilistic decision evaluation resource.
type Decision struct {
	Metadata
	Instructions string             `json:"instructions,omitempty"`
	Models       []string           `json:"models,omitempty"`
	Hints        []string           `json:"hints,omitempty"`
	Questions    []DecisionQuestion `json:"questions,omitempty"`
}

// DecisionQuestion defines a single structured question within a Decision resource.
type DecisionQuestion struct {
	Name          string               `json:"name"`
	Type          DecisionQuestionType `json:"type"`
	Instructions  string               `json:"instructions,omitempty"`
	Threshold     *float64             `json:"threshold,omitempty"`
	MinConfidence *float64             `json:"min_confidence,omitempty"`
	Criteria      *DecisionCriteria    `json:"criteria,omitempty"`
	Options       []DecisionOption     `json:"options,omitempty"`
	Levels        []DecisionLevel      `json:"levels,omitempty"`
}

// DecisionCriteria defines explicit positive and negative criteria for a predicate question.
type DecisionCriteria struct {
	True  string `json:"true,omitempty"`
	False string `json:"false,omitempty"`
}

// DecisionOption defines a single categorical choice option.
type DecisionOption struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// DecisionLevel defines a single ordinal rubric level for a score question.
type DecisionLevel struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Skill describes a reusable domain expertise bundle.
type Skill struct {
	Metadata
	UseWhen      string   `json:"use_when,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
}

// Command describes a reusable user- or agent-invokable command prompt.
type Command struct {
	Metadata
	Instructions string   `json:"instructions,omitempty"`
	Models       []string `json:"models,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Hints        []string `json:"hints,omitempty"`
}

// Tool describes a declarative tool manifest.
type Tool struct {
	Metadata
	Instructions string            `json:"instructions,omitempty"`
	Command      []string          `json:"command,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	InputSchema  map[string]any    `json:"input_schema,omitempty"`
	OutputSchema map[string]any    `json:"output_schema,omitempty"`
	Annotations  *ToolAnnotation   `json:"annotations,omitempty"`
}

// ToolAnnotation defines the safety profile for a tool.
type ToolAnnotation struct {
	IsOpenWorld  bool   `json:"is_open_world,omitempty"`
	IsDangerous  bool   `json:"is_dangerous,omitempty"`
	IsReadOnly   bool   `json:"is_read_only,omitempty"`
	IsIdempotent bool   `json:"is_idempotent,omitempty"`
	UserHint     string `json:"user_hint,omitempty"`
}

// Toolkit describes a collection of referenced or inline tools.
type Toolkit struct {
	Metadata
	Tools []ToolkitTool `json:"tools,omitempty"`
}

// ToolkitTool represents a single tool entry in a Toolkit (either a reference or inline tool).
type ToolkitTool struct {
	Ref string `json:"ref,omitempty"`
	Tool
}

// MCP describes an MCP server configuration.
type MCP struct {
	Metadata
	Type        string                    `json:"type,omitempty"`
	Endpoint    string                    `json:"endpoint,omitempty"`
	Command     []string                  `json:"command,omitempty"`
	Env         map[string]string         `json:"env,omitempty"`
	Auth        *ProviderAuth             `json:"auth,omitempty"`
	Headers     map[string]string         `json:"headers,omitempty"`
	Annotations *ToolAnnotation           `json:"annotations,omitempty"`
	Policies    *MCPPolicies              `json:"policies,omitempty"`
	Overrides   map[string]ToolAnnotation `json:"overrides,omitempty"`
}

// MCPPolicies defines inclusion and exclusion rules for resources exposed by an MCP server.
type MCPPolicies struct {
	Tools     *ResourceFilter `json:"tools,omitempty"`
	Prompts   *ResourceFilter `json:"prompts,omitempty"`
	Resources *ResourceFilter `json:"resources,omitempty"`
}

// ResourceFilter defines glob inclusion and exclusion patterns.
type ResourceFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// Context describes a project's AGENTS.md instructions and referenced resources.
type Context struct {
	Metadata
	Instructions string   `json:"instructions,omitempty"`
	Resources    []string `json:"resources,omitempty"`
}

// Project describes a project directory registered within a workspace.
type Project struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	RootPath     string   `json:"root_path"`
	AbsPath      string   `json:"abs_path"`
	Instructions string   `json:"instructions,omitempty"`
	Context      *Context `json:"context,omitempty"`
}

// Policies defines workspace- or agent-level security and tool policies.
type Policies struct {
	Tools *ToolPolicies `json:"tools,omitempty"`
}

// ToolPolicies defines restrictions for tool usage.
type ToolPolicies struct {
	AllowDangerous *bool    `json:"allow_dangerous,omitempty"`
	AllowOpenWorld *bool    `json:"allow_open_world,omitempty"`
	Include        []string `json:"include,omitempty"`
	Exclude        []string `json:"exclude,omitempty"`
}

// ─── Domain Conversions from Warp ───────────────────────────────────────────

func convertMetadata(b warp.BaseResource, reg *warp.Registry) Metadata {
	ns := b.Namespace
	if reg != nil && ns != "" && ns != NamespaceSystem && ns != NamespaceUser && ns != NamespaceWorkspace {
		if _, isProj := reg.GetProject(ns); isProj {
			ns = NamespaceLocal
		}
	}

	qn := b.QualifiedName()
	if ns == NamespaceLocal && b.Kind != "" && b.Metadata.Name != "" {
		qn = warp.MakeQualifiedName(NamespaceLocal, b.Kind, b.Metadata.Name)
	}

	var labels map[string]string
	var visibility, version string
	if len(b.Metadata.Labels) > 0 {
		labels = maps.Clone(b.Metadata.Labels)
		visibility = labels["visibility"]
		version = labels["version"]
	}
	if version == "" {
		version = b.APIVersion
	}

	return Metadata{
		Name:          b.Metadata.Name,
		Namespace:     ns,
		QualifiedName: qn,
		DisplayName:   b.Metadata.DisplayName,
		Description:   b.Metadata.Description,
		Directory:     b.Directory,
		Visibility:    visibility,
		Version:       version,
		Labels:        labels,
	}
}

func convertProviderAuth(in *warp.ProviderAuth) *ProviderAuth {
	if in == nil {
		return nil
	}
	return &ProviderAuth{
		Type:   in.Type,
		Header: in.Header,
		Env:    in.Env,
		File:   in.File,
	}
}

func convertProvider(in *warp.ModelProvider, reg *warp.Registry) Provider {
	if in == nil {
		return Provider{}
	}

	var defaultModels map[ModelRole]string
	if in.Spec.DefaultModels != nil {
		defaultModels = make(map[ModelRole]string)
		if in.Spec.DefaultModels.Completion != "" {
			defaultModels[RoleCompletion] = in.Spec.DefaultModels.Completion
		}
		if in.Spec.DefaultModels.Decision != "" {
			defaultModels[RoleDecision] = in.Spec.DefaultModels.Decision
		}
		if in.Spec.DefaultModels.Embedding != "" {
			defaultModels[RoleEmbedding] = in.Spec.DefaultModels.Embedding
		}
		if len(defaultModels) == 0 {
			defaultModels = nil
		}
	}

	var headers map[string]string
	if len(in.Spec.Headers) > 0 {
		headers = maps.Clone(in.Spec.Headers)
	}

	var cfg map[string]any
	if len(in.Spec.Config) > 0 {
		cfg = maps.Clone(in.Spec.Config)
	}

	var models []ProviderModel
	if len(in.Spec.Models) > 0 {
		models = make([]ProviderModel, len(in.Spec.Models))
		for i, m := range in.Spec.Models {
			models[i] = convertProviderModel(m)
		}
	}

	return Provider{
		Metadata:      convertMetadata(in.BaseResource, reg),
		Type:          in.Spec.Type,
		Endpoint:      in.Spec.Endpoint,
		DefaultModel:  in.Spec.DefaultModel,
		DefaultModels: defaultModels,
		Auth:          convertProviderAuth(in.Spec.Auth),
		Headers:       headers,
		Config:        cfg,
		Models:        models,
	}
}

func convertProviderModel(in warp.ProviderModel) ProviderModel {
	var caps *ModelCapabilities
	if in.Capabilities != nil {
		c := in.Capabilities
		caps = &ModelCapabilities{
			Completion:       cloneBoolPtr(c.Completion),
			Decision:         cloneBoolPtr(c.Decision),
			Embedding:        cloneBoolPtr(c.Embedding),
			Dimensions:       cloneSlice(c.Dimensions),
			Attachment:       cloneBoolPtr(c.Attachment),
			Tools:            cloneBoolPtr(c.Tools),
			StructuredOutput: cloneBoolPtr(c.StructuredOutput),
			Temperature:      cloneBoolPtr(c.Temperature),
		}
		if len(c.Thinking) > 0 {
			caps.Thinking = make([]ThinkingCapability, len(c.Thinking))
			for i, t := range c.Thinking {
				caps.Thinking[i] = ThinkingCapability{
					Type:            t.Type,
					IsDefault:       t.IsDefault,
					AllowedEfforts:  cloneSlice(t.AllowedEfforts),
					MaxBudgetTokens: t.MaxBudgetTokens,
					Default:         t.Default,
				}
			}
		}
		if c.Modalities != nil {
			caps.Modalities = &ModelModalities{
				Input:  cloneSlice(c.Modalities.Input),
				Output: cloneSlice(c.Modalities.Output),
			}
		}
	}

	var cost *ModelCost
	if in.Cost != nil {
		cost = &ModelCost{
			Input:      in.Cost.Input,
			Output:     in.Cost.Output,
			CacheRead:  in.Cost.CacheRead,
			CacheWrite: in.Cost.CacheWrite,
		}
		if len(in.Cost.Tiers) > 0 {
			cost.Tiers = make([]ModelCostTier, len(in.Cost.Tiers))
			for i, t := range in.Cost.Tiers {
				cost.Tiers[i] = ModelCostTier{
					Label:         t.Label,
					ConditionType: t.Tier.Type,
					ConditionSize: t.Tier.Size,
					Input:         t.Input,
					Output:        t.Output,
					CacheRead:     t.CacheRead,
					CacheWrite:    t.CacheWrite,
				}
			}
		}
	}

	return ProviderModel{
		ID:    in.ID,
		Name:  in.Name,
		Label: in.Label,
		Limits: ModelLimits{
			Context:    in.Limits.Context,
			Output:     in.Limits.Output,
			Dimensions: in.Limits.Dimensions,
		},
		Capabilities: caps,
		Cost:         cost,
	}
}

func convertAgent(in *warp.Agent, resolved *warp.ResolvedAgent, reg *warp.Registry) Agent {
	if in == nil {
		return Agent{}
	}
	var temp *float64
	if in.Spec.Temperature != nil {
		v := *in.Spec.Temperature
		temp = &v
	}
	var thinking map[string]any
	if len(in.Spec.Thinking) > 0 {
		thinking = maps.Clone(in.Spec.Thinking)
	}

	var skills, commands, subagents, tools []string
	if resolved != nil {
		for _, s := range resolved.Skills {
			skills = append(skills, s.Metadata.Name)
		}
		for _, c := range resolved.Commands {
			commands = append(commands, c.Metadata.Name)
		}
		for _, sa := range resolved.Subagents {
			subagents = append(subagents, sa.Metadata.Name)
		}
		for _, t := range resolved.Tools {
			tools = append(tools, t.Metadata.Name)
		}
	} else {
		if in.Spec.Skills != nil {
			skills = cloneSlice(*in.Spec.Skills)
		}
		if in.Spec.Commands != nil {
			commands = cloneSlice(*in.Spec.Commands)
		}
		if in.Spec.Subagents != nil {
			subagents = cloneSlice(*in.Spec.Subagents)
		}
	}

	return Agent{
		Metadata:     convertMetadata(in.BaseResource, reg),
		Extends:      in.Spec.Extends,
		Instructions: in.Spec.Instructions,
		Triggers:     cloneSlice(in.Spec.Triggers),
		Models:       cloneSlice(in.Spec.Models),
		Temperature:  temp,
		Thinking:     thinking,
		Skills:       skills,
		Commands:     commands,
		Subagents:    subagents,
		Tools:        tools,
		Policies:     convertPolicies(in.Spec.Policies),
	}
}

func convertDecision(in *warp.Decision, reg *warp.Registry) Decision {
	if in == nil {
		return Decision{}
	}
	var questions []DecisionQuestion
	if len(in.Spec.Questions) > 0 {
		questions = make([]DecisionQuestion, len(in.Spec.Questions))
		for i, q := range in.Spec.Questions {
			var threshold, minConf *float64
			if q.Threshold != nil {
				v := *q.Threshold
				threshold = &v
			}
			if q.MinConfidence != nil {
				v := *q.MinConfidence
				minConf = &v
			}
			var criteria *DecisionCriteria
			if q.Criteria != nil {
				criteria = &DecisionCriteria{
					True:  q.Criteria.True,
					False: q.Criteria.False,
				}
			}
			var opts []DecisionOption
			if len(q.Options) > 0 {
				opts = make([]DecisionOption, len(q.Options))
				for j, o := range q.Options {
					opts[j] = DecisionOption{
						Value:       o.Value,
						Description: o.Description,
					}
				}
			}
			var levels []DecisionLevel
			if len(q.Levels) > 0 {
				levels = make([]DecisionLevel, len(q.Levels))
				for j, l := range q.Levels {
					levels[j] = DecisionLevel{
						Label:       l.Label,
						Description: l.Description,
					}
				}
			}
			questions[i] = DecisionQuestion{
				Name:          q.Name,
				Type:          DecisionQuestionType(q.Type),
				Instructions:  q.Instructions,
				Threshold:     threshold,
				MinConfidence: minConf,
				Criteria:      criteria,
				Options:       opts,
				Levels:        levels,
			}
		}
	}

	return Decision{
		Metadata:     convertMetadata(in.BaseResource, reg),
		Instructions: in.Spec.Instructions,
		Models:       cloneSlice(in.Spec.Models),
		Hints:        cloneSlice(in.Spec.Hints),
		Questions:    questions,
	}
}

func convertSkill(in *warp.Skill, reg *warp.Registry) Skill {
	if in == nil {
		return Skill{}
	}
	return Skill{
		Metadata:     convertMetadata(in.BaseResource, reg),
		UseWhen:      in.Spec.UseWhen,
		Keywords:     cloneSlice(in.Spec.Keywords),
		Instructions: in.Spec.Instructions,
	}
}

func convertCommand(in *warp.Command, reg *warp.Registry) Command {
	if in == nil {
		return Command{}
	}
	return Command{
		Metadata:     convertMetadata(in.BaseResource, reg),
		Instructions: in.Spec.Instructions,
		Models:       cloneSlice(in.Spec.Models),
		Tools:        cloneSlice(in.Spec.Tools),
		Hints:        cloneSlice(in.Spec.Hints),
	}
}

func convertToolAnnotation(in *warp.ToolAnnotation) *ToolAnnotation {
	if in == nil {
		return nil
	}
	return &ToolAnnotation{
		IsOpenWorld:  in.IsOpenWorld,
		IsDangerous:  in.IsDangerous,
		IsReadOnly:   in.IsReadOnly,
		IsIdempotent: in.IsIdempotent,
		UserHint:     in.UserHint,
	}
}

func convertTool(in *warp.Tool, reg *warp.Registry) Tool {
	if in == nil {
		return Tool{}
	}
	meta := convertMetadata(in.BaseResource, reg)
	if meta.Description == "" && in.Spec.Description != "" {
		meta.Description = in.Spec.Description
	}
	var envMap map[string]string
	if len(in.Spec.Env) > 0 {
		envMap = maps.Clone(in.Spec.Env)
	}
	var inputSchema, outputSchema map[string]any
	if len(in.Spec.InputSchema) > 0 {
		inputSchema = maps.Clone(in.Spec.InputSchema)
	}
	if len(in.Spec.OutputSchema) > 0 {
		outputSchema = maps.Clone(in.Spec.OutputSchema)
	}
	return Tool{
		Metadata:     meta,
		Instructions: in.Spec.Instructions,
		Command:      cloneSlice(in.Spec.Command),
		Env:          envMap,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Annotations:  convertToolAnnotation(in.Spec.Annotations),
	}
}

func convertToolkit(in *warp.Toolkit, reg *warp.Registry) Toolkit {
	if in == nil {
		return Toolkit{}
	}
	var tools []ToolkitTool
	if len(in.Spec.Tools) > 0 {
		tools = make([]ToolkitTool, len(in.Spec.Tools))
		for i, item := range in.Spec.Tools {
			var envMap map[string]string
			if len(item.Env) > 0 {
				envMap = maps.Clone(item.Env)
			}
			var inputSchema, outputSchema map[string]any
			if len(item.InputSchema) > 0 {
				inputSchema = maps.Clone(item.InputSchema)
			}
			if len(item.OutputSchema) > 0 {
				outputSchema = maps.Clone(item.OutputSchema)
			}
			tools[i] = ToolkitTool{
				Ref: item.Ref,
				Tool: Tool{
					Metadata: Metadata{
						Name:        item.Name,
						Namespace:   convertMetadata(in.BaseResource, reg).Namespace,
						Description: item.Description,
						Directory:   in.Directory,
					},
					Instructions: item.Instructions,
					Command:      cloneSlice(item.Command),
					Env:          envMap,
					InputSchema:  inputSchema,
					OutputSchema: outputSchema,
					Annotations:  convertToolAnnotation(item.Annotations),
				},
			}
		}
	}
	return Toolkit{
		Metadata: convertMetadata(in.BaseResource, reg),
		Tools:    tools,
	}
}

func convertResourceFilter(in *warp.ResourceFilter) *ResourceFilter {
	if in == nil {
		return nil
	}
	return &ResourceFilter{
		Include: cloneSlice(in.Include),
		Exclude: cloneSlice(in.Exclude),
	}
}

func convertMCP(in *warp.MCP, reg *warp.Registry) MCP {
	if in == nil {
		return MCP{}
	}
	var envMap, headers map[string]string
	if len(in.Spec.Env) > 0 {
		envMap = maps.Clone(in.Spec.Env)
	}
	if len(in.Spec.Headers) > 0 {
		headers = maps.Clone(in.Spec.Headers)
	}
	var policies *MCPPolicies
	if in.Spec.Policies != nil {
		policies = &MCPPolicies{
			Tools:     convertResourceFilter(in.Spec.Policies.Tools),
			Prompts:   convertResourceFilter(in.Spec.Policies.Prompts),
			Resources: convertResourceFilter(in.Spec.Policies.Resources),
		}
	}
	var overrides map[string]ToolAnnotation
	if len(in.Spec.Overrides) > 0 {
		overrides = make(map[string]ToolAnnotation, len(in.Spec.Overrides))
		for k, v := range in.Spec.Overrides {
			overrides[k] = *convertToolAnnotation(&v)
		}
	}
	return MCP{
		Metadata:    convertMetadata(in.BaseResource, reg),
		Type:        in.Spec.Type,
		Endpoint:    in.Spec.Endpoint,
		Command:     cloneSlice(in.Spec.Command),
		Env:         envMap,
		Auth:        convertProviderAuth(in.Spec.Auth),
		Headers:     headers,
		Annotations: convertToolAnnotation(in.Spec.Annotations),
		Policies:    policies,
		Overrides:   overrides,
	}
}

func convertContext(in *warp.Context, reg *warp.Registry) *Context {
	if in == nil {
		return nil
	}
	return &Context{
		Metadata:     convertMetadata(in.BaseResource, reg),
		Instructions: in.Spec.Instructions,
		Resources:    cloneSlice(in.Spec.Resources),
	}
}

func convertProject(in *warp.Project, reg *warp.Registry) Project {
	if in == nil {
		return Project{}
	}
	var instructions string
	if in.Context != nil {
		instructions = in.Context.Spec.Instructions
	}
	return Project{
		Name:         in.Name,
		Path:         in.Path,
		RootPath:     in.RootPath,
		AbsPath:      in.AbsPath(),
		Instructions: instructions,
		Context:      convertContext(in.Context, reg),
	}
}

func convertPolicies(in *warp.Policies) *Policies {
	if in == nil {
		return nil
	}
	var tools *ToolPolicies
	if in.Tools != nil {
		tools = &ToolPolicies{
			AllowDangerous: cloneBoolPtr(in.Tools.AllowDangerous),
			AllowOpenWorld: cloneBoolPtr(in.Tools.AllowOpenWorld),
			Include:        cloneSlice(in.Tools.Include),
			Exclude:        cloneSlice(in.Tools.Exclude),
		}
	}
	return &Policies{
		Tools: tools,
	}
}

func cloneBoolPtr(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

func cloneSlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}
