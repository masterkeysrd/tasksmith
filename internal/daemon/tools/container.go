package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Config holds the mandatory dependencies required to construct a [Container].
type Config struct {
	Tools     []Tool
	Sandbox   sandbox.Sandbox
	Workspace *workspace.Workspace
	Logger    log.Interface
}

// Validate ensures that all required dependencies are non-nil. Zero silent
// fallbacks are permitted.
func (c Config) Validate() error {
	if c.Sandbox == nil {
		return fmt.Errorf("%w: sandbox is required", ErrInvalidConfig)
	}
	if c.Workspace == nil {
		return fmt.Errorf("%w: workspace is required", ErrInvalidConfig)
	}
	if c.Logger == nil {
		return fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	for i, t := range c.Tools {
		if t == nil {
			return fmt.Errorf("%w: tool at index %d cannot be nil", ErrInvalidConfig, i)
		}
	}
	return nil
}

// Container manages registered [Tool] instances, custom workspace command
// tools, two-phase Prepare/Execute orchestration, and the session permission [Gate].
type Container struct {
	sandbox   sandbox.Sandbox
	workspace *workspace.Workspace
	logger    log.Interface
	gate      *Gate
	order     []string
	tools     map[string]Tool
}

// NewContainer validates cfg and constructs a [Container] from cfg.Tools plus
// any custom command tools declared in cfg.Workspace.Tools().
func NewContainer(cfg Config) (*Container, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	c := &Container{
		sandbox:   cfg.Sandbox,
		workspace: cfg.Workspace,
		logger:    cfg.Logger.ForComponent("tools"),
		gate:      NewGate(cfg.Workspace),
		tools:     make(map[string]Tool, len(cfg.Tools)),
	}

	for _, t := range cfg.Tools {
		if wb, ok := t.(workspaceBinder); ok {
			if err := wb.bindWorkspace(cfg.Workspace); err != nil {
				return nil, err
			}
		}
		c.addTool(t)
	}

	if err := c.registerCustomTools(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Container) addTool(t Tool) {
	name := t.Name()
	if _, exists := c.tools[name]; !exists {
		c.order = append(c.order, name)
	}
	c.tools[name] = t
	c.gate.registerAnnotation(name, t.Definition().Annotation)
}

// Gate returns the permission and policy [Gate] associated with c.
func (c *Container) Gate() *Gate {
	return c.gate
}

// Definitions returns the LLM tool definitions for all registered tools in
// registration order.
func (c *Container) Definitions() []tool.Definition {
	defs := make([]tool.Definition, 0, len(c.order))
	for _, name := range c.order {
		if t, ok := c.tools[name]; ok {
			defs = append(defs, t.Definition())
		}
	}
	return defs
}

// List returns all registered [Tool] instances in registration order.
func (c *Container) List() []Tool {
	list := make([]Tool, 0, len(c.order))
	for _, name := range c.order {
		if t, ok := c.tools[name]; ok {
			list = append(list, t)
		}
	}
	return list
}

// Get retrieves a registered [Tool] by name.
func (c *Container) Get(name string) (Tool, bool) {
	t, ok := c.tools[name]
	return t, ok
}

// Mode returns the Gate's active [PermissionMode].
func (c *Container) Mode() PermissionMode {
	return c.gate.Mode()
}

// SetMode sets the Gate's active [PermissionMode].
func (c *Container) SetMode(mode PermissionMode) {
	c.gate.SetMode(mode)
}

// AddGrant adds a session-scoped [Grant] to the Gate.
func (c *Container) AddGrant(grant Grant) {
	c.gate.AddGrant(grant)
}

// Grants returns a defensive copy of all active session grants in the Gate.
func (c *Container) Grants() []Grant {
	return c.gate.Grants()
}

// ClearGrants clears all active session grants in the Gate.
func (c *Container) ClearGrants() {
	c.gate.ClearGrants()
}

// Prepare validates the tool call and delegates pre-flight analysis to the
// target [Tool.Prepare] implementation.
func (c *Container) Prepare(ctx context.Context, tc *message.ToolCall) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tc == nil {
		return nil, fmt.Errorf("%w: tool call is nil", ErrToolNotFound)
	}

	t, ok := c.tools[tc.Name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, tc.Name)
	}

	plan, err := t.Prepare(ctx, tc.Args)
	if err != nil {
		return nil, err
	}
	if plan != nil {
		plan.ToolCallID = tc.ID
		if plan.ToolName == "" {
			plan.ToolName = tc.Name
		}
	}
	return plan, nil
}

// Evaluate delegates plan evaluation to [Gate.Evaluate].
func (c *Container) Evaluate(plan *Plan) (AccessDecision, *PermissionRequest, error) {
	return c.gate.Evaluate(plan)
}

// Execute invokes the target [Tool.Execute], serializing concurrent mutations
// to the same file path, wrapping any validation or execution error into a
// non-fatal [*message.Tool] with IsError: true, and assigning a single UUIDv7
// message ID.
func (c *Container) Execute(ctx context.Context, tc *message.ToolCall) (*message.Tool, error) {
	if tc == nil {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("tools: generate tool message id: %w", err)
		}
		resp := &message.Tool{
			IsError: true,
			Content: message.Content{&message.TextBlock{Text: "tools: tool call is nil"}},
		}
		resp.ID = id.String()
		return resp, nil
	}

	t, ok := c.tools[tc.Name]
	var resp *message.Tool
	if !ok {
		err := fmt.Errorf("%w: %s", ErrToolNotFound, tc.Name)
		resp = &message.Tool{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			IsError:    true,
			Content:    message.Content{&message.TextBlock{Text: err.Error()}},
		}
	} else {
		if targetPath, shouldLock := c.mutatingPathForTool(t, tc.Args); shouldLock {
			unlock := c.gate.lockPath(targetPath)
			defer unlock()
		}

		var execErr error
		resp, execErr = t.Execute(ctx, tc)
		if execErr != nil {
			resp = &message.Tool{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				IsError:    true,
				Content:    message.Content{&message.TextBlock{Text: execErr.Error()}},
			}
		} else if resp == nil {
			resp = &message.Tool{
				ToolCallID: tc.ID,
				Name:       tc.Name,
			}
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("tools: generate tool message id: %w", err)
	}
	resp.ID = id.String()

	return resp, nil
}

func (c *Container) mutatingPathForTool(t Tool, args map[string]any) (string, bool) {
	if t.Definition().Annotation.IsReadOnly || args == nil {
		return "", false
	}
	raw, ok := args["path"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false
	}
	if rel, err := c.sandbox.FS().ResolvePath(raw); err == nil {
		return rel, true
	}
	return strings.TrimSpace(raw), true
}
