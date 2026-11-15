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
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// MCPCaller is the transport contract the tools package needs from an MCP
// manager. It is deliberately narrower than the workspace MCPService surface: a
// tool container only invokes tools and reads resources, never manages
// connections, so the MCP manager satisfies it through a small adapter without
// the tools package depending on the manager implementation.
//
// Implementations must be safe for concurrent use.
type MCPCaller interface {
	// CallMCPTool executes rawToolName on the named server and streams its
	// result. rawToolName is the server's own tool name, not the materialized
	// "<server>__<tool>" name the agent calls.
	CallMCPTool(ctx context.Context, serverName, rawToolName string, args map[string]any) (tool.ToolStream, error)
	// ReadMCPResource reads one resource URI from the named server.
	ReadMCPResource(ctx context.Context, serverName, uri string, allowProhibited bool) (api.MCPResourceResponse, error)
	// HasAllowedMCPResources reports whether any server exposes a resource that
	// spec.policies permits.
	HasAllowedMCPResources() bool
}

// MCPProhibitedError reports an MCP item that a server manifest's spec.policies
// prohibits. It is a hard block for the agent: the message names the manifest so
// the user can relax the policy, and the container surfaces it to the model as an
// error result.
type MCPProhibitedError struct {
	Kind         string // "tool" or "resource"
	Item         string
	Section      string // e.g. "spec.policies.tools"
	ManifestPath string
}

func (e *MCPProhibitedError) Error() string {
	msg := fmt.Sprintf("MCP %s %q is prohibited by %s", e.Kind, e.Item, e.Section)
	if path := strings.TrimSpace(e.ManifestPath); path != "" {
		msg += " in " + path
	}
	return msg
}

// mcpServerResolver looks up the effective definition of an MCP server so policy
// can be evaluated against the manifest currently on disk rather than the one that
// happened to be loaded when the tool was constructed.
type mcpServerResolver func(name string) (*warp.MCP, bool)

func (r mcpServerResolver) resolve(name string) (*warp.MCP, bool) {
	if r == nil {
		return nil, false
	}
	return r(name)
}

// mcpServerFor returns a resolver over a workspace catalog, or nil when the
// workspace carries no catalog.
func mcpServerFor(ws *workspace.Workspace) mcpServerResolver {
	if ws == nil {
		return nil
	}
	return func(name string) (*warp.MCP, bool) {
		catalog := ws.Catalog()
		if catalog == nil {
			return nil, false
		}
		return catalog.GetMCP(name)
	}
}

// portablePath renders path relative to the workspace root (or home) when the
// workspace can, so a policy error never leaks an absolute machine path.
func portablePath(ws *workspace.Workspace, path string) string {
	if ws == nil {
		return strings.TrimSpace(path)
	}
	return strings.TrimSpace(ws.PortablePath(path))
}

// mcpTool executes one tool discovered on an MCP server.
//
// It is the MCP counterpart of [customTool]: the definition, input schema, and
// safety profile come from the materialized warp.Tool the registry produced, whose
// annotations warp already resolved through the server manifest's override
// precedence. Nothing is inferred from argument names or values. Only the
// transport differs — validated arguments are sent to the server instead of a local
// process.
type mcpTool struct {
	def                 tool.Definition
	resolvedInputSchema *jsonschema.Resolved
	serverName          string
	rawName             string
	ws                  *workspace.Workspace
	caller              MCPCaller
	resolve             mcpServerResolver
}

func (t *mcpTool) Name() string {
	return t.def.Name
}

func (t *mcpTool) Definition() tool.Definition {
	return t.def
}

func (t *mcpTool) SetDeferLoading(deferLoading bool) {
	t.def.DeferLoading = deferLoading
}

// validateArgs checks args against the schema the server advertised. Failures come
// back as validation errors so the model can correct and retry rather than treat
// the tool as broken.
func (t *mcpTool) validateArgs(args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if t.resolvedInputSchema != nil {
		if err := t.resolvedInputSchema.Validate(args); err != nil {
			return nil, &tool.ValidationError{ToolName: t.def.Name, Err: err}
		}
	}
	return args, nil
}

// checkAllowed enforces the server manifest's tool policy at the boundary the agent
// actually reaches. A prohibited tool is normally absent from the registry, so this
// exists for the window where a live container outlives a reload that tightened the
// policy.
func (t *mcpTool) checkAllowed() error {
	def, ok := t.resolve.resolve(t.serverName)
	if !ok || def == nil || def.AllowsTool(t.rawName) {
		return nil
	}
	return &MCPProhibitedError{
		Kind:         api.MCPAllowPolicyKindTool,
		Item:         t.rawName,
		Section:      "spec.policies.tools",
		ManifestPath: portablePath(t.ws, def.GetPath()),
	}
}

func (t *mcpTool) Prepare(ctx context.Context, args map[string]any) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := t.validateArgs(args); err != nil {
		return nil, err
	}
	if err := t.checkAllowed(); err != nil {
		return nil, err
	}

	// The target is "<server>/<tool>", so a granted rule reads for example
	// "notes__echo(notes/*)": approving one MCP tool never becomes a blanket
	// approval of a same-named tool on another server.
	target := t.serverName + "/" + t.rawName
	return customPlanFromAnnotation(t.def.Name, t.def.Annotation, target,
		fmt.Sprintf("Run MCP tool: %s", t.def.Name)), nil
}

func (t *mcpTool) Execute(ctx context.Context, call *message.ToolCall) (*message.Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		rawArgs map[string]any
		callID  string
		name    = t.def.Name
	)
	if call != nil {
		rawArgs = call.Args
		callID = call.ID
		if call.Name != "" {
			name = call.Name
		}
	}

	args, err := t.validateArgs(rawArgs)
	if err != nil {
		return nil, err
	}
	if err := t.checkAllowed(); err != nil {
		return nil, err
	}
	if t.caller == nil {
		return nil, fmt.Errorf("tools: MCP tool %q has no MCP runtime attached", t.def.Name)
	}

	stream, err := t.caller.CallMCPTool(ctx, t.serverName, t.rawName, args)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, fmt.Errorf("tools: MCP tool %q on server %q returned no result stream", t.def.Name, t.serverName)
	}

	content, structured, isError := aggregateMCPStream(ctx, stream)
	return &message.Tool{
		ToolCallID:        callID,
		Name:              name,
		Content:           content,
		StructuredContent: structured,
		IsError:           isError,
	}, nil
}

// aggregateMCPStream folds a server's streamed result into one tool message.
//
// Text is written through a [SpillWriter] so a large payload becomes a truncated
// preview pointing at the full file instead of flooding the context, while media
// blocks are passed through untouched — spilling them would discard the only thing
// the payload carries. A structured-only result is serialized as JSON so it still
// reaches the model.
func aggregateMCPStream(ctx context.Context, stream tool.ToolStream) (message.Content, any, bool) {
	w := NewSpillWriter(ctx)
	defer func() { _ = w.Close() }()

	var (
		media      message.Content
		structured any
		hasText    bool
		isError    bool
	)

	for chunk, streamErr := range stream {
		if streamErr != nil {
			// A transport failure after partial output is still a tool error, but
			// the container wraps returned errors into an IsError result, so
			// folding it into the text would hide the underlying cause.
			return message.Content{&message.TextBlock{Text: streamErr.Error()}}, nil, true
		}
		for _, block := range chunk.Content {
			switch typed := block.(type) {
			case *message.TextBlock:
				if typed.Text == "" {
					continue
				}
				hasText = true
				if _, err := w.Write([]byte(typed.Text)); err != nil {
					return message.Content{&message.TextBlock{Text: err.Error()}}, nil, true
				}
			case *message.ImageBlock, *message.AudioBlock, *message.VideoBlock, *message.DocumentBlock:
				media = append(media, typed)
			}
		}
		if chunk.StructuredContent != nil {
			structured = chunk.StructuredContent
		}
		if chunk.IsError {
			isError = true
		}
	}

	var blocks message.Content
	if text := w.String(); text != "" {
		blocks = append(blocks, &message.TextBlock{Text: text})
		hasText = true
	}
	if !hasText && structured != nil {
		if data, err := json.Marshal(structured); err == nil {
			blocks = append(blocks, &message.TextBlock{Text: string(data)})
		}
	}
	blocks = append(blocks, media...)
	if len(blocks) == 0 {
		if isError {
			blocks = message.Content{&message.TextBlock{Text: "MCP tool reported an error"}}
		} else {
			blocks = message.Content{&message.TextBlock{Text: "MCP tool returned no content"}}
		}
	}
	return blocks, structured, isError
}

// buildMCPTool constructs the [Tool] for one discovered MCP tool. The materialized
// warp.Tool supplies display metadata, schema, and the effective annotation; the
// workspace supplies the live MCP definition used for policy enforcement and the
// manifest path named in a policy rejection.
func buildMCPTool(wsTool *warp.Tool, ws *workspace.Workspace, caller MCPCaller) (Tool, error) {
	if wsTool == nil {
		return nil, errors.New("tools: MCP tool definition is nil")
	}
	serverName, rawName, ok := warp.ParseMCPToolName(wsTool.Metadata.Name)
	if !ok {
		return nil, fmt.Errorf("tools: MCP tool %q is not named <server>__<tool>", wsTool.Metadata.Name)
	}

	title := wsTool.Metadata.DisplayName
	if title == "" {
		title = serverName + ": " + rawName
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

	inputSchema, err := mcpJSONSchema(wsTool.Spec.InputSchema, func() (*jsonschema.Schema, error) {
		return jsonschema.For[map[string]any](nil)
	}, wsTool.Metadata.Name, "input")
	if err != nil {
		return nil, err
	}
	resolvedInput, err := inputSchema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("tools: resolve %s schema for MCP tool %q: %w", "input", wsTool.Metadata.Name, err)
	}

	// An MCP result is free-form content, so an absent output schema declares a
	// plain string, matching what the tool message actually carries.
	outputSchema, err := mcpJSONSchema(wsTool.Spec.OutputSchema, func() (*jsonschema.Schema, error) {
		return jsonschema.For[string](nil)
	}, wsTool.Metadata.Name, "output")
	if err != nil {
		return nil, err
	}

	desc := strings.TrimSpace(wsTool.Spec.Instructions)
	if desc == "" {
		desc = wsTool.Metadata.Description
	}
	if desc == "" {
		desc = fmt.Sprintf("Tool %s exposed by MCP server %s", rawName, serverName)
	}

	return &mcpTool{
		serverName:          serverName,
		rawName:             rawName,
		ws:                  ws,
		caller:              caller,
		resolve:             mcpServerFor(ws),
		resolvedInputSchema: resolvedInput,
		def: tool.Definition{
			Name:         wsTool.Metadata.Name,
			Title:        title,
			Description:  desc,
			Annotation:   ann,
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		},
	}, nil
}

// mcpJSONSchema converts a manifest-facing schema map into a typed schema, falling
// back to fallback when the map is absent or unparsable. A server advertising a
// schema the local resolver rejects still gets a callable tool with an open schema
// rather than disappearing from the catalog.
func mcpJSONSchema(raw map[string]any, fallback func() (*jsonschema.Schema, error), toolName, which string) (*jsonschema.Schema, error) {
	if len(raw) > 0 {
		data, err := json.Marshal(raw)
		if err == nil {
			var parsed jsonschema.Schema
			if err := json.Unmarshal(data, &parsed); err == nil {
				return &parsed, nil
			}
		}
	}
	schema, err := fallback()
	if err != nil {
		return nil, fmt.Errorf("tools: build fallback %s schema for MCP tool %q: %w", which, toolName, err)
	}
	return schema, nil
}

// registerMCPTools registers one [mcpTool] per tool the workspace attributes to an
// MCP server. It is the inverse filter of [container.registerCustomTools]:
// command-backed tools belong to the custom path, and system-namespace built-ins are
// constructed by the builtin factory, so both are skipped here.
func (c *container) registerMCPTools() error {
	if c.mcpCaller == nil || c.workspace == nil || c.workspace.Catalog() == nil {
		return nil
	}

	catalog := c.workspace.Catalog()
	for _, wsTool := range catalog.Tools() {
		if wsTool == nil || len(wsTool.Spec.Command) > 0 {
			continue
		}
		if server := catalog.MCPServerForTool(wsTool); server == nil {
			continue
		}
		if !nameAllowed(c.allowedTools, wsTool.Metadata.Name) {
			continue
		}
		if _, exists := c.tools[wsTool.Metadata.Name]; exists {
			continue
		}

		mt, err := buildMCPTool(wsTool, c.workspace, c.mcpCaller)
		if err != nil {
			return fmt.Errorf("tools: build MCP tool %q: %w", wsTool.Metadata.Name, err)
		}
		c.addTool(mt)
	}
	return nil
}

// nameAllowed reports whether name appears in an allowlist, compared
// case-insensitively the way custom tool registration compares names.
func nameAllowed(allowed []string, name string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, name) {
			return true
		}
	}
	return false
}

// CheckMCPResourceAllowed verifies that serverName does not prohibit uri under its
// current spec.policies.resources filter. It returns nil when ws or the server is
// unresolved so Prepare can defer unknown-server diagnostics to Execute while
// blocking prohibited resources before permission prompts.
func CheckMCPResourceAllowed(ws *workspace.Workspace, serverName, uri string) error {
	if ws == nil {
		return nil
	}
	catalog := ws.Catalog()
	if catalog == nil {
		return nil
	}
	def, ok := catalog.GetMCP(strings.TrimSpace(serverName))
	if !ok || def == nil {
		return nil
	}
	uri = strings.TrimSpace(uri)
	if !def.AllowsResource(uri) {
		return &MCPProhibitedError{
			Kind:         api.MCPAllowPolicyKindResource,
			Item:         uri,
			Section:      "spec.policies.resources",
			ManifestPath: portablePath(ws, def.GetPath()),
		}
	}
	return nil
}

// ReadMCPResource reads one resource URI from a named MCP server through ws's
// attached runtime. Unless allowProhibited is set — which only a user acting
// interactively may do, never the agent — a resource the server manifest's
// spec.policies.resources blocks is rejected with an [*MCPProhibitedError] naming
// the manifest. A missing runtime, unknown server, or blank argument is reported as
// a descriptive error.
func ReadMCPResource(ctx context.Context, ws *workspace.Workspace, caller MCPCaller, serverName, uri string, allowProhibited bool) (api.MCPResourceResponse, error) {
	if ws == nil {
		return api.MCPResourceResponse{}, errors.New("tools: no workspace is bound to the MCP runtime")
	}
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return api.MCPResourceResponse{}, fmt.Errorf("%w: MCP server name must not be empty", ErrInvalidConfig)
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return api.MCPResourceResponse{}, fmt.Errorf("%w: MCP resource URI must not be empty", ErrInvalidConfig)
	}

	catalog := ws.Catalog()
	if catalog == nil {
		return api.MCPResourceResponse{}, errors.New("tools: workspace catalog is not loaded")
	}
	def, ok := catalog.GetMCP(serverName)
	if !ok || def == nil {
		return api.MCPResourceResponse{}, fmt.Errorf("%w: MCP server %q is not configured in this workspace", ErrToolNotFound, serverName)
	}
	if !allowProhibited && !def.AllowsResource(uri) {
		return api.MCPResourceResponse{}, &MCPProhibitedError{
			Kind:         api.MCPAllowPolicyKindResource,
			Item:         uri,
			Section:      "spec.policies.resources",
			ManifestPath: portablePath(ws, def.GetPath()),
		}
	}
	if caller == nil {
		return api.MCPResourceResponse{}, fmt.Errorf("tools: no MCP runtime is attached to workspace %q", ws.RootDir())
	}
	return caller.ReadMCPResource(ctx, def.Metadata.Name, uri, allowProhibited)
}
