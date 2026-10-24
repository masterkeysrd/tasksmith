package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Sentinel errors returned by the tools package.
var (
	ErrInvalidConfig    = errors.New("tools: invalid configuration")
	ErrToolNotFound     = errors.New("tools: tool not found")
	ErrPermissionDenied = errors.New("tools: permission denied")
)

// ActionType classifies the fundamental category of a tool operation.
type ActionType = guard.ActionType

const (
	ActionRead    = guard.ActionRead
	ActionUnknown = guard.ActionUnknown
	ActionWrite   = guard.ActionWrite
	ActionDelete  = guard.ActionDelete
	ActionExec    = guard.ActionExec
	ActionNet     = guard.ActionNet
)

// SafetyLevel classifies the risk profile of a planned tool operation.
type SafetyLevel = guard.SafetyLevel

const (
	SafetySafe    = guard.SafetySafe
	SafetyOutside = guard.SafetyOutside
	SafetyUnknown = guard.SafetyUnknown
	SafetyUnsafe  = guard.SafetyUnsafe
)

// PermissionMode controls the session-wide approval behavior of a [Gate].
type PermissionMode string

const (
	ModeDefault     PermissionMode = "default"
	ModeAcceptEdits PermissionMode = "accept_edits"
	ModePlan        PermissionMode = "plan"
	ModeAsk         PermissionMode = "ask"
	ModeAuto        PermissionMode = "auto"
)

func (m PermissionMode) String() string {
	return string(m)
}

func (m PermissionMode) IsValid() bool {
	switch m {
	case ModeDefault, ModeAcceptEdits, ModePlan, ModeAsk, ModeAuto:
		return true
	default:
		return false
	}
}

// AccessDecision represents the policy evaluation outcome returned by [Gate.Evaluate].
type AccessDecision string

const (
	AccessAllow AccessDecision = "allow"
	AccessAsk   AccessDecision = "ask"
	AccessDeny  AccessDecision = "deny"
)

// PermissionDecisionKind represents the user's response to a [PermissionRequest].
type PermissionDecisionKind string

const (
	DecisionAllowOnce    PermissionDecisionKind = "allow_once"
	DecisionAllowSession PermissionDecisionKind = "allow_session"
	DecisionReject       PermissionDecisionKind = "reject"
)

// Plan describes the pre-flight impact and unified diff preview of a validated
// tool invocation produced by [Tool.Prepare] or [Container.Prepare].
type Plan struct {
	ToolCallID string      `json:"tool_call_id"`
	ToolName   string      `json:"tool_name"`
	Action     ActionType  `json:"action"`
	Safety     SafetyLevel `json:"safety"`
	Target     string      `json:"target,omitempty"`
	Summary    string      `json:"summary"`
	Diff       string      `json:"diff,omitempty"`
	ReadOnly   bool        `json:"read_only,omitempty"`
}

// PermissionRequest is the user-facing approval request minted by [Gate.Evaluate]
// when a [Plan] evaluates to [AccessAsk].
type PermissionRequest struct {
	ID            string      `json:"id"`
	ToolCallID    string      `json:"tool_call_id"`
	ToolName      string      `json:"tool_name"`
	Action        ActionType  `json:"action"`
	Safety        SafetyLevel `json:"safety"`
	Target        string      `json:"target,omitempty"`
	Summary       string      `json:"summary"`
	Diff          string      `json:"diff,omitempty"`
	GrantPatterns []string    `json:"grant_patterns,omitempty"`
}

// PermissionDecision captures the user's resolution of a [PermissionRequest].
type PermissionDecision struct {
	RequestID    string                 `json:"request_id"`
	ToolCallID   string                 `json:"tool_call_id"`
	Kind         PermissionDecisionKind `json:"kind"`
	GrantPattern string                 `json:"grant_pattern,omitempty"`
	Feedback     string                 `json:"feedback,omitempty"`
}

// Grant represents an active session-scoped permission rule stored in [Gate].
type Grant struct {
	ToolName string     `json:"tool_name"`
	Action   ActionType `json:"action,omitempty"`
	Pattern  string     `json:"pattern"` // e.g. "*", "internal/foo.go", "internal/**", "go test*"
}

// Tool is an executable LLM-callable tool with two-phase Prepare and Execute
// lifecycle hooks.
type Tool interface {
	Name() string
	Definition() tool.Definition
	Prepare(ctx context.Context, args map[string]any) (*Plan, error)
	Execute(ctx context.Context, call *message.ToolCall) (*message.Tool, error)
}

// Handler is implemented by strongly typed tools with two-phase Prepare and
// Execute methods.
type Handler[In, Out any] interface {
	Prepare(ctx context.Context, in In) (*Plan, error)
	Execute(ctx context.Context, in In) (Out, error)
}

// Spec defines a strongly typed [Tool] backed by Go input and output types.
type Spec[In, Out any] struct {
	Name        string
	Title       string
	Description string
	Annotation  tool.Annotation
	Prepare     func(ctx context.Context, in In) (*Plan, error)
	Execute     func(ctx context.Context, in In) (Out, error)
}

type workspaceBinder interface {
	bindWorkspace(ws workspace.Workspace) error
}

type typedTool[In, Out any] struct {
	def                  tool.Definition
	resolvedInputSchema  *jsonschema.Resolved
	prepare              func(ctx context.Context, in In) (*Plan, error)
	execute              func(ctx context.Context, in In) (Out, error)
	requireWorkspaceMeta bool
}

// NewHandler constructs a [Tool] from a typed [Handler], inferring its JSON
// schemas from In and Out. Display metadata and annotations are bound from the
// workspace when the tool is registered in a [Container].
func NewHandler[In, Out any](name string, h Handler[In, Out]) Tool {
	t, err := New(Spec[In, Out]{
		Name:    name,
		Prepare: h.Prepare,
		Execute: h.Execute,
	})
	if err != nil {
		panic(err)
	}
	if tt, ok := t.(*typedTool[In, Out]); ok {
		tt.requireWorkspaceMeta = true
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

func (t *typedTool[In, Out]) bindWorkspace(ws workspace.Workspace) error {
	if ws == nil {
		return nil
	}
	wsTool, ok := ws.GetTool(t.def.Name)
	if !ok {
		if t.requireWorkspaceMeta {
			return fmt.Errorf("%w: tool %q not found in workspace", ErrToolNotFound, t.def.Name)
		}
		return nil
	}
	if wsTool.DisplayName != "" {
		t.def.Title = wsTool.DisplayName
	} else if wsTool.Name != "" {
		t.def.Title = wsTool.Name
	}
	if wsTool.Instructions != "" {
		t.def.Description = wsTool.Instructions
	} else if wsTool.Description != "" {
		t.def.Description = wsTool.Description
	}
	if wsTool.Annotations != nil {
		t.def.Annotation = tool.Annotation{
			IsOpenWorld:  wsTool.Annotations.IsOpenWorld,
			IsDangerous:  wsTool.Annotations.IsDangerous,
			IsReadOnly:   wsTool.Annotations.IsReadOnly,
			IsIdempotent: wsTool.Annotations.IsIdempotent,
			UserHint:     wsTool.Annotations.UserHint,
		}
	}
	return nil
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

	action := ActionExec
	safety := SafetyUnknown
	if t.def.Annotation.IsReadOnly {
		action = ActionRead
		if !t.def.Annotation.IsOpenWorld && !t.def.Annotation.IsDangerous {
			safety = SafetySafe
		}
	} else if t.def.Annotation.IsOpenWorld && !t.def.Annotation.IsDangerous {
		action = ActionNet
	}
	if t.def.Annotation.IsDangerous {
		safety = SafetyUnsafe
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
