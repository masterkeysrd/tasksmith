package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

type customTool struct {
	def                 tool.Definition
	resolvedInputSchema *jsonschema.Resolved
	command             []string
	env                 map[string]string
	sandbox             sandbox.Sandbox
}

func (c *Container) registerCustomTools() error {
	if c.workspace == nil {
		return nil
	}

	wsTools := c.workspace.Tools()
	for _, wsTool := range wsTools {
		if len(wsTool.Command) == 0 {
			continue
		}
		if _, exists := c.tools[wsTool.Name]; exists {
			continue
		}

		ct, err := c.buildCustomTool(wsTool)
		if err != nil {
			return fmt.Errorf("tools: build custom tool %q: %w", wsTool.Name, err)
		}
		c.addTool(ct)
	}
	return nil
}

func (c *Container) buildCustomTool(wsTool workspace.Tool) (Tool, error) {
	title := wsTool.DisplayName
	if title == "" {
		title = wsTool.Name
	}

	desc := wsTool.Instructions
	if desc == "" {
		desc = wsTool.Description
	}
	if desc == "" {
		desc = fmt.Sprintf("Custom workspace tool %s", wsTool.Name)
	}

	var ann tool.Annotation
	if wsTool.Annotations != nil {
		ann = tool.Annotation{
			IsOpenWorld:  wsTool.Annotations.IsOpenWorld,
			IsDangerous:  wsTool.Annotations.IsDangerous,
			IsReadOnly:   wsTool.Annotations.IsReadOnly,
			IsIdempotent: wsTool.Annotations.IsIdempotent,
			UserHint:     wsTool.Annotations.UserHint,
		}
	}

	var inputSchema *jsonschema.Schema
	if len(wsTool.InputSchema) > 0 {
		if data, err := json.Marshal(wsTool.InputSchema); err == nil {
			var parsed jsonschema.Schema
			if err := json.Unmarshal(data, &parsed); err == nil {
				inputSchema = &parsed
			}
		}
	}
	if inputSchema == nil {
		inferred, err := jsonschema.For[map[string]any](nil)
		if err != nil {
			return nil, err
		}
		inputSchema = inferred
	}

	resolvedInput, err := inputSchema.Resolve(nil)
	if err != nil {
		fallback, fErr := jsonschema.For[map[string]any](nil)
		if fErr != nil {
			return nil, fErr
		}
		resolvedInput, err = fallback.Resolve(nil)
		if err != nil {
			return nil, err
		}
	}

	var outputSchema *jsonschema.Schema
	if len(wsTool.OutputSchema) > 0 {
		if data, err := json.Marshal(wsTool.OutputSchema); err == nil {
			var parsed jsonschema.Schema
			if err := json.Unmarshal(data, &parsed); err == nil {
				outputSchema = &parsed
			}
		}
	}
	if outputSchema == nil {
		outputSchema, _ = jsonschema.For[string](nil)
	}

	cmdSlice := make([]string, len(wsTool.Command))
	copy(cmdSlice, wsTool.Command)

	var envMap map[string]string
	if len(wsTool.Env) > 0 {
		envMap = maps.Clone(wsTool.Env)
	}

	return &customTool{
		def: tool.Definition{
			Name:         wsTool.Name,
			Title:        title,
			Description:  desc,
			Annotation:   ann,
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		},
		resolvedInputSchema: resolvedInput,
		command:             cmdSlice,
		env:                 envMap,
		sandbox:             c.sandbox,
	}, nil
}

func (ct *customTool) Name() string {
	return ct.def.Name
}

func (ct *customTool) Definition() tool.Definition {
	return ct.def
}

func (ct *customTool) validateArgs(args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if ct.resolvedInputSchema != nil {
		if err := ct.resolvedInputSchema.Validate(args); err != nil {
			return nil, &tool.ValidationError{ToolName: ct.def.Name, Err: err}
		}
	}
	return args, nil
}

func (ct *customTool) Prepare(ctx context.Context, args map[string]any) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := ct.validateArgs(args); err != nil {
		return nil, err
	}

	action := ActionExec
	safety := SafetyUnknown
	readOnly := false

	ann := ct.def.Annotation
	if ann.IsReadOnly {
		action = ActionRead
		readOnly = true
		if !ann.IsOpenWorld && !ann.IsDangerous {
			safety = SafetySafe
		}
	} else if ann.IsOpenWorld && !ann.IsDangerous {
		action = ActionNet
	}
	if ann.IsDangerous {
		safety = SafetyUnsafe
		readOnly = false
	}

	target := strings.Join(ct.command, " ")
	summary := fmt.Sprintf("Run custom tool: %s", ct.def.Name)
	if ann.UserHint != "" {
		if strings.Contains(ann.UserHint, "%s") {
			summary = fmt.Sprintf(ann.UserHint, target)
		} else {
			summary = ann.UserHint
		}
	}

	return &Plan{
		ToolName: ct.def.Name,
		Action:   action,
		Safety:   safety,
		Target:   target,
		Summary:  summary,
		ReadOnly: readOnly,
	}, nil
}

func (ct *customTool) Execute(ctx context.Context, call *message.ToolCall) (*message.Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		rawArgs map[string]any
		callID  string
		name    = ct.def.Name
	)
	if call != nil {
		rawArgs = call.Args
		callID = call.ID
		if call.Name != "" {
			name = call.Name
		}
	}

	args, err := ct.validateArgs(rawArgs)
	if err != nil {
		return nil, err
	}

	inputBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal custom tool args: %w", err)
	}

	cmd := sandbox.Command{
		Name:  ct.command[0],
		Args:  ct.command[1:],
		Env:   ct.env,
		Stdin: bytes.NewReader(inputBytes),
	}

	res, err := ct.sandbox.Proc().Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		errMsg := strings.TrimSpace(string(res.Stderr))
		if errMsg == "" {
			errMsg = strings.TrimSpace(string(res.Stdout))
		}
		if errMsg == "" {
			return nil, fmt.Errorf("custom tool %q exited with code %d", ct.def.Name, res.ExitCode)
		}
		return nil, errors.New(errMsg)
	}

	stdout := string(res.Stdout)
	return &message.Tool{
		ToolCallID:        callID,
		Name:              name,
		Content:           message.Content{&message.TextBlock{Text: stdout}},
		StructuredContent: stdout,
	}, nil
}
