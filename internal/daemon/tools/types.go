package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
)

// Sentinel errors returned by the tools package.
var (
	ErrInvalidConfig    = errors.New("tools: invalid configuration")
	ErrToolNotFound     = errors.New("tools: tool not found")
	ErrPermissionDenied = errors.New("tools: permission denied")
)

// readOnlyExecKey tags a tool-execution context whose permission plan was
// authorized as read-only, so execution-capable tools (bash, command-backed
// custom tools) request a read-only kernel profile for the child process even
// when the sandbox itself is writable. This is defense-in-depth against
// shellguard or annotation misclassification: an auto-allowed "read-only"
// command physically cannot mutate disk under a native kernel sandbox.
type readOnlyExecKey struct{}

// unsandboxedExecKey tags a tool-execution context whose permission plan was
// explicitly approved to run outside the kernel sandbox (bypass_sandbox, an
// excludedCommands match, or a direct user shell command), so execution-
// capable tools skip the native wrapper for that single process.
type unsandboxedExecKey struct{}

// WithReadOnlyExecution marks ctx so tools that spawn child processes request
// a per-command read-only kernel profile. Passing false returns ctx unchanged.
func WithReadOnlyExecution(ctx context.Context, readOnly bool) context.Context {
	if !readOnly {
		return ctx
	}
	return context.WithValue(ctx, readOnlyExecKey{}, true)
}

// ReadOnlyExecutionFromContext reports whether ctx was tagged for read-only
// child-process execution.
func ReadOnlyExecutionFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(readOnlyExecKey{}).(bool)
	return v
}

// WithUnsandboxedExecution marks ctx so execution-capable tools skip the
// kernel sandbox wrapper for the child process of this tool call. Passing
// false returns ctx unchanged. The harness tags this only from a plan the
// permission engine approved as unsandboxed (bypass_sandbox, an
// excludedCommands match, or a direct user shell command).
func WithUnsandboxedExecution(ctx context.Context, unsandboxed bool) context.Context {
	if !unsandboxed {
		return ctx
	}
	return context.WithValue(ctx, unsandboxedExecKey{}, true)
}

// UnsandboxedExecutionFromContext reports whether ctx was tagged for
// unsandboxed child-process execution.
func UnsandboxedExecutionFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(unsandboxedExecKey{}).(bool)
	return v
}

type planReviewOutcomeKey struct{}

// PlanReviewOutcome carries the user's review decision when resuming an
// exit_plan_mode interrupt in the harness.
type PlanReviewOutcome struct {
	Approved bool
	Mode     api.PermissionMode
	Feedback string
}

// WithPlanReviewOutcome attaches the resolved plan review outcome to ctx.
func WithPlanReviewOutcome(ctx context.Context, outcome PlanReviewOutcome) context.Context {
	return context.WithValue(ctx, planReviewOutcomeKey{}, outcome)
}

// PlanReviewOutcomeFromContext retrieves the plan review outcome from ctx, if set.
func PlanReviewOutcomeFromContext(ctx context.Context) (PlanReviewOutcome, bool) {
	v, ok := ctx.Value(planReviewOutcomeKey{}).(PlanReviewOutcome)
	return v, ok
}

// AccessDecision represents the policy evaluation outcome.
type AccessDecision string

const (
	AccessAllow AccessDecision = "allow"
	AccessAsk   AccessDecision = "ask"
	AccessDeny  AccessDecision = "deny"
)

// Plan describes the pre-flight impact and unified diff preview of a validated
// tool invocation produced by [Tool.Prepare] or [Container.Prepare].
type Plan struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Action     api.ActionType  `json:"action"`
	Safety     api.SafetyLevel `json:"safety"`
	Target     string          `json:"target,omitempty"`
	// Paths holds the additional workspace-relative, slash-separated target paths
	// touched by the invocation (e.g. a bash command's CWD and file operands, or
	// the paths surfaced by a search/listing result). It is used by rule
	// matching to evaluate triggers.globs against every path in play, not just
	// the primary Target.
	Paths    []string `json:"paths,omitempty"`
	Summary  string   `json:"summary"`
	Diff     string   `json:"diff,omitempty"`
	ReadOnly bool     `json:"read_only,omitempty"`
	// Unsandboxed marks a plan whose child process runs directly on the host
	// instead of inside the kernel sandbox (bash bypass_sandbox or an
	// excludedCommands match). The harness tags their execution context
	// accordingly.
	Unsandboxed bool `json:"unsandboxed,omitempty"`
	// BypassSandbox distinguishes a model-requested bypass_sandbox plan from
	// a trusted excludedCommands match: bypass plans are isolated into the
	// bash_unsandboxed(...) rule namespace so regular bash(...) allow rules
	// can never auto-approve an unsandboxed retry, while excluded matches
	// flow through normal bash(...) rules and modes.
	BypassSandbox bool `json:"bypass_sandbox,omitempty"`
	// PlanReview carries the validated plan path and Markdown preview when
	// exit_plan_mode prepares a plan review interrupt.
	PlanReview *api.PlanReviewRequest `json:"-"`
}

// Tool is an executable LLM-callable tool with two-phase Prepare and Execute
// lifecycle hooks.
type Tool interface {
	Name() string
	Definition() tool.Definition
	Prepare(ctx context.Context, args map[string]any) (*Plan, error)
	Execute(ctx context.Context, call *message.ToolCall) (*message.Tool, error)
	// SetDeferLoading marks the tool for deferred loading. Deferred tools are
	// withheld from the initial model context until discovered via tool_search.
	SetDeferLoading(deferLoading bool)
}

// Handler is implemented by strongly typed tools with two-phase Prepare and
// Execute methods.
type Handler[In, Out any] interface {
	Prepare(ctx context.Context, in In) (*Plan, error)
	Execute(ctx context.Context, in In) (Out, error)
}

// Spec defines a strongly typed [Tool] backed by Go input and output types.
type Spec[In, Out any] struct {
	Name         string
	Title        string
	Description  string
	Annotation   tool.Annotation
	DeferLoading bool
	InputSchema  map[string]any
	Prepare      func(ctx context.Context, in In) (*Plan, error)
	Execute      func(ctx context.Context, in In) (Out, error)
}

// Metadata provides display and permission metadata for a tool.
type Metadata struct {
	Title        string
	Description  string
	Annotation   tool.Annotation
	DeferLoading bool
	InputSchema  map[string]any
}

type typedTool[In, Out any] struct {
	def                 tool.Definition
	resolvedInputSchema *jsonschema.Resolved
	prepare             func(ctx context.Context, in In) (*Plan, error)
	execute             func(ctx context.Context, in In) (Out, error)
}

// NewHandler constructs a [Tool] from a typed [Handler] and explicit [Metadata],
// inferring its JSON schemas from In and Out.
func NewHandler[In, Out any](name string, meta Metadata, h Handler[In, Out]) Tool {
	t, err := New(Spec[In, Out]{
		Name:         name,
		Title:        meta.Title,
		Description:  meta.Description,
		Annotation:   meta.Annotation,
		DeferLoading: meta.DeferLoading,
		InputSchema:  meta.InputSchema,
		Prepare:      h.Prepare,
		Execute:      h.Execute,
	})
	if err != nil {
		panic(err)
	}
	return t
}

// New constructs a [Tool] from spec, inferring and resolving its JSON schemas
// from In and Out at construction time.
func New[In, Out any](spec Spec[In, Out]) (Tool, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: tool name is required", ErrInvalidConfig)
	}
	if spec.Execute == nil {
		return nil, fmt.Errorf("%w: tool %q execute handler is required", ErrInvalidConfig, name)
	}

	inputSchema, err := jsonschema.For[In](nil)
	if err != nil {
		return nil, fmt.Errorf("tools: infer input schema for %q: %w", name, err)
	}
	outputSchema, err := jsonschema.For[Out](nil)
	if err != nil {
		return nil, fmt.Errorf("tools: infer output schema for %q: %w", name, err)
	}
	resolvedInput, err := inputSchema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("tools: resolve input schema for %q: %w", name, err)
	}
	if len(spec.InputSchema) > 0 {
		inputSchema = inputSchema.CloneSchemas()
		overlaySchemaEnums(inputSchema, spec.InputSchema)
	}

	title := spec.Title
	if title == "" {
		title = name
	}

	return &typedTool[In, Out]{
		def: tool.Definition{
			Name:         name,
			Title:        title,
			Description:  spec.Description,
			Annotation:   spec.Annotation,
			DeferLoading: spec.DeferLoading,
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		},
		resolvedInputSchema: resolvedInput,
		prepare:             spec.Prepare,
		execute:             spec.Execute,
	}, nil
}

func (t *typedTool[In, Out]) Name() string {
	return t.def.Name
}

func (t *typedTool[In, Out]) Definition() tool.Definition {
	return t.def
}

func (t *typedTool[In, Out]) SetDeferLoading(deferLoading bool) {
	t.def.DeferLoading = deferLoading
}

func (t *typedTool[In, Out]) decodeAndValidate(args map[string]any) (In, error) {
	var zero In
	if args == nil {
		args = map[string]any{}
	}
	if err := t.resolvedInputSchema.Validate(args); err != nil {
		return zero, &tool.ValidationError{ToolName: t.def.Name, Err: err}
	}

	data, err := json.Marshal(args)
	if err != nil {
		return zero, &tool.ValidationError{ToolName: t.def.Name, Err: err}
	}

	var in In
	if err := json.Unmarshal(data, &in); err != nil {
		return zero, &tool.ValidationError{ToolName: t.def.Name, Err: err}
	}

	if v, ok := any(in).(tool.Validator); ok {
		if err := v.Validate(); err != nil {
			return zero, &tool.ValidationError{ToolName: t.def.Name, Err: err}
		}
	} else if v, ok := any(&in).(tool.Validator); ok {
		if err := v.Validate(); err != nil {
			return zero, &tool.ValidationError{ToolName: t.def.Name, Err: err}
		}
	}

	return in, nil
}

func (t *typedTool[In, Out]) Prepare(ctx context.Context, args map[string]any) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	in, err := t.decodeAndValidate(args)
	if err != nil {
		return nil, err
	}

	if t.prepare != nil {
		plan, err := t.prepare(ctx, in)
		if err != nil {
			return nil, err
		}
		if plan != nil && plan.ToolName == "" {
			plan.ToolName = t.def.Name
		}
		return plan, nil
	}

	action := api.ActionExec
	safety := api.SafetyUnknown
	if t.def.Annotation.IsReadOnly {
		action = api.ActionRead
		if !t.def.Annotation.IsOpenWorld && !t.def.Annotation.IsDangerous {
			safety = api.SafetySafe
		}
	} else if t.def.Annotation.IsOpenWorld && !t.def.Annotation.IsDangerous {
		action = api.ActionNet
	}
	if t.def.Annotation.IsDangerous {
		safety = api.SafetyUnsafe
	}

	return &Plan{
		ToolName: t.def.Name,
		Action:   action,
		Safety:   safety,
		Summary:  t.def.Title,
		ReadOnly: t.def.Annotation.IsReadOnly && !t.def.Annotation.IsDangerous,
	}, nil
}

func (t *typedTool[In, Out]) Execute(ctx context.Context, call *message.ToolCall) (*message.Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		args   map[string]any
		callID string
		name   = t.def.Name
	)
	if call != nil {
		args = call.Args
		callID = call.ID
		if call.Name != "" {
			name = call.Name
		}
	}

	in, err := t.decodeAndValidate(args)
	if err != nil {
		return nil, err
	}

	out, err := t.execute(ctx, in)
	if err != nil {
		return nil, err
	}

	var content message.Content
	if cp, ok := any(out).(tool.ContentProvider); ok {
		content = cp.ToolContent()
	} else if tp, ok := any(out).(tool.TextContentProvider); ok {
		content = message.Content{&message.TextBlock{Text: tp.TextContent()}}
	} else if s, ok := any(out).(string); ok {
		content = message.Content{&message.TextBlock{Text: s}}
	}

	return &message.Tool{
		ToolCallID:        callID,
		Name:              name,
		Content:           content,
		StructuredContent: out,
	}, nil
}

// overlaySchemaEnums recursively copies enum constraints from a raw WARP
// manifest inputSchema map onto a struct-inferred [jsonschema.Schema] so
// constrained-decoding LLM providers receive exact enum values.
func overlaySchemaEnums(dst *jsonschema.Schema, raw map[string]any) {
	if dst == nil || len(raw) == 0 {
		return
	}
	switch v := raw["enum"].(type) {
	case []any:
		if len(v) > 0 {
			dst.Enum = slices.Clone(v)
		}
	case []string:
		if len(v) > 0 {
			enums := make([]any, len(v))
			for i, s := range v {
				enums[i] = s
			}
			dst.Enum = enums
		}
	}
	if rawProps, ok := raw["properties"].(map[string]any); ok && len(dst.Properties) > 0 {
		for name, propSchema := range dst.Properties {
			if rawProp, ok := rawProps[name].(map[string]any); ok {
				overlaySchemaEnums(propSchema, rawProp)
			}
		}
	}
	if rawItems, ok := raw["items"].(map[string]any); ok && dst.Items != nil {
		overlaySchemaEnums(dst.Items, rawItems)
	}
}
