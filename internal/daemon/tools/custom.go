package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// reTemplateAction matches an explicit Go template action ({{ ... }}) authored in
// a custom tool's spec.command element.
var reTemplateAction = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// customToolWait is how long a custom tool call blocks before its process is
// backgrounded, matching the built-in bash tool's synchronous wait window. A
// tool that needs longer keeps running as a killable background task and wakes
// the agent on completion instead of pinning the turn indefinitely. It is a
// variable so tests can exercise backgrounding without a real-minute wait.
var customToolWait = 60 * time.Second

// customTool executes a custom workspace tool declared as a warp.Tool carrying a
// spec.command. It is deliberately a black box: its security posture comes only
// from the manifest's declarative spec.annotations, and its arguments reach the
// process either as rendered command-line templates or as JSON on stdin.
type customTool struct {
	def                 tool.Definition
	resolvedInputSchema *jsonschema.Resolved
	command             []string
	env                 map[string]string
	runner              CommandRunner
	tasks               tasks.Store
	wsTool              *warp.Tool
	workspace           *workspace.Workspace
	agent               *warp.Agent
}

func (c *container) registerCustomTools() error {
	if c.workspace == nil || c.workspace.Catalog() == nil {
		return nil
	}

	wsTools := c.workspace.Catalog().Tools()
	for _, wsTool := range wsTools {
		if len(wsTool.Spec.Command) == 0 {
			continue
		}
		var allowed bool
		for _, a := range c.allowedTools {
			if strings.EqualFold(a, wsTool.Metadata.Name) {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}
		if _, exists := c.tools[wsTool.Metadata.Name]; exists {
			continue
		}

		ct, err := c.buildCustomTool(wsTool)
		if err != nil {
			return fmt.Errorf("tools: build custom tool %q: %w", wsTool.Metadata.Name, err)
		}
		c.addTool(ct)
	}
	return nil
}

func (c *container) buildCustomTool(wsTool *warp.Tool) (Tool, error) {
	title := wsTool.Metadata.DisplayName
	if title == "" {
		title = wsTool.Metadata.Name
	}

	var ann tool.Annotation
	if wsTool.Spec.Annotations != nil {
		ann = tool.Annotation{
			IsOpenWorld:  wsTool.Spec.Annotations.IsOpenWorld,
			IsDangerous:  wsTool.Spec.Annotations.IsDangerous,
			IsReadOnly:   wsTool.Spec.Annotations.IsReadOnly,
			IsIdempotent: wsTool.Spec.Annotations.IsIdempotent,
			UserHint:     wsTool.Spec.Annotations.UserHint,
		}
	}

	var inputSchema *jsonschema.Schema
	if len(wsTool.Spec.InputSchema) > 0 {
		if data, err := json.Marshal(wsTool.Spec.InputSchema); err == nil {
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
	if len(wsTool.Spec.OutputSchema) > 0 {
		if data, err := json.Marshal(wsTool.Spec.OutputSchema); err == nil {
			var parsed jsonschema.Schema
			if err := json.Unmarshal(data, &parsed); err == nil {
				outputSchema = &parsed
			}
		}
	}
	if outputSchema == nil {
		outputSchema, _ = jsonschema.For[string](nil)
	}

	cmdSlice := make([]string, len(wsTool.Spec.Command))
	copy(cmdSlice, wsTool.Spec.Command)

	var envMap map[string]string
	if len(wsTool.Spec.Env) > 0 {
		envMap = maps.Clone(wsTool.Spec.Env)
	}

	ct := &customTool{
		resolvedInputSchema: resolvedInput,
		command:             cmdSlice,
		env:                 envMap,
		runner:              c.commandRunner,
		tasks:               c.tasks,
		wsTool:              wsTool,
		workspace:           c.workspace,
		agent:               c.agent,
	}

	desc := ct.renderInstructions()
	if desc == "" {
		desc = wsTool.Metadata.Description
	}
	if desc == "" {
		desc = fmt.Sprintf("Custom workspace tool %s", wsTool.Metadata.Name)
	}

	ct.def = tool.Definition{
		Name:         wsTool.Metadata.Name,
		Title:        title,
		Description:  desc,
		Annotation:   ann,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
	}
	return ct, nil
}

func (ct *customTool) Name() string {
	return ct.def.Name
}

func (ct *customTool) Definition() tool.Definition {
	return ct.def
}

func (ct *customTool) SetDeferLoading(deferLoading bool) {
	ct.def.DeferLoading = deferLoading
}

// renderOptions builds the WARP render context from the active workspace, its
// project scope, the invoking agent, and the current call arguments under Input.
func (ct *customTool) renderOptions(args map[string]any) *warp.ToolRenderOptions {
	opts := &warp.ToolRenderOptions{Agent: ct.agent, Input: args}
	if ct.workspace != nil {
		if cat := ct.workspace.Catalog(); cat != nil {
			opts.Workspace = cat.WorkspaceSpec()
			opts.Project = cat.Project()
		}
	}
	return opts
}

// renderInstructions renders the manifest body so instruction templates can
// reference the workspace, project, and invoking agent. Instructions that
// interpolate per-call arguments are left raw, because the definition is built
// once per session rather than per call. A template failure degrades to the raw
// instructions rather than dropping the tool.
func (ct *customTool) renderInstructions() string {
	if ct.wsTool == nil {
		return ""
	}
	raw := strings.TrimSpace(ct.wsTool.Spec.Instructions)
	if raw == "" || !commandUsesTemplates([]string{raw}) {
		return raw
	}
	rendered, err := ct.wsTool.Render(ct.renderOptions(nil))
	if err != nil || strings.Contains(rendered, "<no value>") {
		return raw
	}
	return strings.TrimSpace(rendered)
}

// commandUsesTemplates reports whether the manifest authored an explicit Go
// template action in any command element. Shell-style $VAR references alone are
// not templates, so ordinary CLI scripts keep receiving their own environment.
func commandUsesTemplates(command []string) bool {
	return slices.ContainsFunc(command, func(part string) bool {
		return strings.Contains(part, "{{")
	})
}

// commandConsumesArgs reports whether a call argument is consumed as a command
// template parameter, either through {{.Input.<field>}} or through the top-level
// {{.<field>}} shorthand WARP exposes for the same values. When no argument is
// consumed, the arguments are piped as JSON on stdin instead.
func commandConsumesArgs(command []string, args map[string]any) bool {
	for _, part := range command {
		for _, action := range reTemplateAction.FindAllString(part, -1) {
			if strings.Contains(action, ".Input") {
				return true
			}
			for key := range args {
				if key != "" && strings.Contains(action, "."+key) {
					return true
				}
			}
		}
	}
	return false
}

// renderCommand resolves the argv to execute for args. It also reports whether an
// argument was consumed as a template parameter, which decides how the arguments
// are delivered to the process.
func (ct *customTool) renderCommand(args map[string]any) ([]string, bool, error) {
	if len(ct.command) == 0 {
		return nil, false, fmt.Errorf("tools: custom tool %q declares no spec.command", ct.def.Name)
	}

	consumesArgs := commandConsumesArgs(ct.command, args)
	if ct.wsTool == nil || !commandUsesTemplates(ct.command) {
		return slices.Clone(ct.command), consumesArgs, nil
	}

	rendered, err := ct.wsTool.RenderCommand(ct.renderOptions(args))
	if err != nil {
		return nil, false, fmt.Errorf("tools: render custom tool %q command: %w", ct.def.Name, err)
	}
	if len(rendered) == 0 || strings.TrimSpace(rendered[0]) == "" {
		return nil, false, fmt.Errorf("tools: custom tool %q rendered an empty executable", ct.def.Name)
	}
	// WARP renders a missing template value as a placeholder instead of failing.
	// Passing that placeholder through would hand the CLI a bogus literal, so it
	// is surfaced as a validation error the model can correct and retry.
	for _, part := range rendered {
		if strings.Contains(part, "<no value>") {
			return nil, false, &tool.ValidationError{
				ToolName: ct.def.Name,
				Err:      errors.New("command template referenced an argument that was not provided"),
			}
		}
	}
	return rendered, consumesArgs, nil
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
	validated, err := ct.validateArgs(args)
	if err != nil {
		return nil, err
	}

	argv, _, err := ct.renderCommand(validated)
	if err != nil {
		return nil, err
	}

	// The security posture is taken verbatim from the manifest annotations; no
	// path or risk is guessed from argument key names.
	return customPlanFromAnnotation(ct.def.Name, ct.def.Annotation, strings.Join(argv, " "),
		fmt.Sprintf("Run custom tool: %s", ct.def.Name)), nil
}

// PlanFromAnnotation derives a [Plan] from a manifest's safety annotations and the
// invocation's target. It is exported so built-in tools whose effect depends on an
// external server can share the exact mapping command-backed and MCP tools use,
// instead of restating the annotation precedence.
func PlanFromAnnotation(name string, ann tool.Annotation, target, defaultSummary string) *Plan {
	return customPlanFromAnnotation(name, ann, target, defaultSummary)
}

// customPlanFromAnnotation derives a [Plan] from a manifest's safety annotations.
// It is the single mapping shared by command-backed custom tools and tools
// discovered on MCP servers, so an annotation produces identical permission-plan
// semantics regardless of where the tool came from. Nothing is inferred from
// argument values: unannotated tools stay unknown-safety and non-read-only, which
// makes the permission gate ask rather than silently allow.
func customPlanFromAnnotation(name string, ann tool.Annotation, target, defaultSummary string) *Plan {
	action := api.ActionExec
	safety := api.SafetyUnknown
	readOnly := false

	switch {
	case ann.IsDangerous:
		action = api.ActionExec
		safety = api.SafetyUnsafe
	case ann.IsReadOnly:
		action = api.ActionRead
		readOnly = true
		if !ann.IsOpenWorld {
			safety = api.SafetySafe
		}
	case ann.IsOpenWorld:
		action = api.ActionNet
	}

	summary := defaultSummary
	if ann.UserHint != "" {
		if strings.Contains(ann.UserHint, "%s") {
			summary = fmt.Sprintf(ann.UserHint, target)
		} else {
			summary = ann.UserHint
		}
	}

	return &Plan{
		ToolName: name,
		Action:   action,
		Safety:   safety,
		Target:   target,
		Summary:  summary,
		ReadOnly: readOnly,
	}
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

	argv, consumesArgs, err := ct.renderCommand(args)
	if err != nil {
		return nil, err
	}

	// Arguments reach the process in exactly one way: as rendered command-line
	// arguments when the author embedded them in spec.command, or as the complete
	// JSON payload on stdin for plain CLI scripts.
	var stdin io.Reader
	if !consumesArgs {
		inputBytes, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("marshal custom tool args: %w", err)
		}
		stdin = bytes.NewReader(inputBytes)
	}

	// A tool whose manifest declares isReadOnly always runs under a read-only
	// kernel profile; the context tag additionally covers plans the permission
	// gate authorized as read-only under any mode.
	readOnlyExec := ReadOnlyExecutionFromContext(ctx) || ct.def.Annotation.IsReadOnly

	return ct.executeAsTask(ctx, callID, name, argv, stdin, readOnlyExec)
}

// executeAsTask runs the tool through the session's background task registry,
// the same pipeline the built-in bash tool uses: the call blocks for the wait
// window, and a still-running process is backgrounded instead of pinning the
// turn, remaining killable via the tasks tool and waking the agent with a
// completion notification. Execution goes through [CommandRunner.Exec], which
// synchronizes tracked-file mutations into the change journal afterwards.
func (ct *customTool) executeAsTask(ctx context.Context, callID, name string, argv []string, payload io.Reader, readOnly bool) (*message.Tool, error) {
	w := NewSpillWriter(ctx)
	defer func() { _ = w.Close() }()

	sw := NewSafeWriter(w)
	defer sw.Close()

	task := &tasks.Task{
		Kind:    "custom_tool",
		Title:   fmt.Sprintf("Custom tool: %s", ct.def.Name),
		Command: strings.Join(argv, " "),
		Wait:    customToolWait,
		Fn: func(taskCtx context.Context, out io.Writer, _ io.Reader) (int, error) {
			res, err := ct.runner.Exec(taskCtx, std.ExecRequest{
				Name:     argv[0],
				Args:     argv[1:],
				Env:      ct.env,
				Stdin:    payload,
				Output:   io.MultiWriter(out, sw),
				ReadOnly: readOnly,
			})
			if err != nil {
				return -1, err
			}
			return res.ExitCode, nil
		},
	}

	if err := ct.tasks.Add(task); err != nil {
		return nil, err
	}
	runErr := task.Run(ctx)
	sw.Close()

	if task.Backgrounded() {
		bgNotice := fmt.Sprintf("<background_task task_id=%q status=%q log_path=%q>\nCustom tool %q is still running and has been moved to the background. You do not need to poll this task's status — the system will automatically notify and wake you up when it finishes execution. Use the `tasks` tool to check intermediate logs or kill the task; unlike background bash commands, custom tools do not accept interactive input.\n</background_task>", task.ID, task.Status(), task.LogPath(), ct.def.Name)
		initial := strings.TrimSpace(w.String())
		outStr := bgNotice
		if initial != "" {
			outStr = initial + "\n\n" + bgNotice
		}
		return ct.toolResult(callID, name, outStr), nil
	}

	if runErr != nil {
		return nil, ct.runError(runErr, w)
	}
	if task.ExitCode() != 0 {
		return nil, ct.exitError(task.ExitCode(), w)
	}
	return ct.toolResult(callID, name, w.String()), nil
}

func (ct *customTool) toolResult(callID, name, out string) *message.Tool {
	return &message.Tool{
		ToolCallID:        callID,
		Name:              name,
		Content:           message.Content{&message.TextBlock{Text: out}},
		StructuredContent: out,
	}
}

func (ct *customTool) runError(runErr error, w SpillWriter) error {
	if msg := strings.TrimSpace(w.String()); msg != "" {
		return fmt.Errorf("custom tool %q: %w: %s", ct.def.Name, runErr, msg)
	}
	return fmt.Errorf("custom tool %q: %w", ct.def.Name, runErr)
}

func (ct *customTool) exitError(exitCode int, w SpillWriter) error {
	errMsg := strings.TrimSpace(w.String())
	if errMsg == "" {
		return fmt.Errorf("custom tool %q exited with code %d", ct.def.Name, exitCode)
	}
	return errors.New(errMsg)
}
