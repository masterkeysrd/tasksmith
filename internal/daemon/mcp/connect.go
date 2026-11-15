package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	loommcp "github.com/masterkeysrd/loom/mcp"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// definition returns the definition the state is currently tracking.
func (s *serverState) definition() *warp.MCP {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.def
}

// setDefinition adopts a new definition for an unchanged server configuration and
// re-evaluates policy filters against any already-discovered items.
func (s *serverState) setDefinition(def *warp.MCP) {
	s.mu.Lock()
	s.def = def
	if def != nil {
		for i := range s.tools {
			s.tools[i].Allowed = def.AllowsTool(s.tools[i].RawName)
		}
		for i := range s.prompts {
			s.prompts[i].Allowed = def.AllowsPrompt(s.prompts[i].Name)
		}
		for i := range s.resources {
			s.resources[i].Allowed = def.AllowsResource(s.resources[i].URI)
		}
	}
	s.mu.Unlock()
}

// currentClient returns the live client, or nil when not connected.
func (s *serverState) currentClient() *loommcp.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.client
}

// view returns the server's status and pending authorization URL.
func (s *serverState) view() (api.MCPStatus, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.authURL
}

// viewError returns the server's status, pending authorization URL, and last
// failure reason.
func (s *serverState) viewError() (api.MCPStatus, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.authURL, s.lastErr
}

// snapshot renders the server's API presentation from its definition and live
// discovery results.
func (s *serverState) snapshot(def *warp.MCP) api.MCPServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	server := api.MCPServer{
		Name:        def.Metadata.Name,
		DisplayName: def.Metadata.DisplayName,
		Description: def.Metadata.Description,
		Status:      s.status,
		AuthURL:     s.authURL,
		Error:       s.lastErr,
		Transport:   transportName(def),
		Tools:       slices.Clone(s.tools),
		Prompts:     slices.Clone(s.prompts),
		Resources:   slices.Clone(s.resources),
	}
	if def.Spec.Auth != nil {
		server.AuthType = strings.TrimSpace(def.Spec.Auth.Type)
	}
	return server
}

// connectAndDiscover establishes a connection for def and records everything the
// server exposes, then publishes discovered tools to the workspace.
func (m *Manager) connectAndDiscover(ctx context.Context, state *serverState, def *warp.MCP, interactive ...bool) error {
	if def == nil {
		return fmt.Errorf("MCP definition is nil")
	}
	env, render := m.runtimeContext()
	cfg, provider, err := m.buildConfig(def, env, render)
	if err != nil {
		return err
	}
	if provider != nil && len(interactive) > 0 && interactive[0] {
		provider.interactive = true
	}
	// Record the provider before the handshake starts. An interactive flow blocks
	// inside the handshake until the browser calls back, and a URL pasted into the
	// terminal during that window has to reach the provider that is waiting.
	if provider != nil {
		state.mu.Lock()
		if !state.closed {
			state.provider = provider
		}
		state.mu.Unlock()
	}
	// A server that advertises listChanged re-syncs this workspace on its own, so
	// tools the server adds or drops mid-session become callable without a restart.
	// The SDK only subscribes when the server advertises the capability, so servers
	// that do not support it are unaffected.
	cfg.OnToolsChanged = func(ctx context.Context) { m.resync(ctx, state) }
	cfg.OnPromptsChanged = func(ctx context.Context) { m.resync(ctx, state) }
	cfg.OnResourcesChanged = func(ctx context.Context) { m.resync(ctx, state) }

	timeout := m.opts.ConnectTimeout
	if provider != nil && provider.interactive && authorizationTimeout > timeout {
		timeout = authorizationTimeout
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := loommcp.NewClient(cfg)
	if _, err := client.Info(connectCtx); err != nil {
		_ = client.Close()
		return fmt.Errorf("connect MCP server %q: %w", def.Metadata.Name, err)
	}

	tools, prompts, resources := discover(connectCtx, client, def)

	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		_ = client.Close()
		return ErrManagerClosed
	}
	previous := state.client
	state.client = client
	state.provider = provider
	state.tools = tools
	state.prompts = prompts
	state.resources = resources
	state.discovered = true
	state.status = api.MCPStatusConnected
	state.lastErr = ""
	if provider == nil || provider.pendingAuthURL() == "" {
		state.authURL = ""
	}
	state.mu.Unlock()

	if previous != nil {
		_ = previous.Close()
	}

	raw := rawTools(tools)
	if err := m.publish(def, raw); err != nil {
		return fmt.Errorf("publish tools for MCP server %q: %w", def.Metadata.Name, err)
	}
	return nil
}

// publish registers discovered tools with the workspace registry and refreshes the
// workspace catalog.
func (m *Manager) publish(def *warp.MCP, raw []*warp.Tool) error {
	if m.opts.Syncer == nil {
		return nil
	}
	return m.opts.Syncer.SyncMCPTools(def, raw)
}

// resync re-lists what a server offers after it reports that a list changed. All
// three categories are re-listed regardless of which one changed, because the
// listings are cheap and a server is free to change more than one behind a single
// notification. A server can emit several notifications in quick succession, so
// concurrent re-lists coalesce into one trailing pass.
//
// The handler runs on the connection's notification goroutine, hence the separate
// goroutine here. A category whose re-list fails keeps its previously discovered
// items, so a transient error cannot silently remove working tools from the
// workspace.
func (m *Manager) resync(ctx context.Context, state *serverState) {
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return
	}
	if state.resyncing {
		state.resyncPending = true
		state.mu.Unlock()
		return
	}
	state.resyncing = true
	state.mu.Unlock()

	go m.resyncLoop(ctx, state)
}

// resyncLoop performs re-lists until the server stops reporting changes.
func (m *Manager) resyncLoop(ctx context.Context, state *serverState) {
	for {
		def := m.currentDefinition(state)
		client := state.currentClient()
		if def == nil || client == nil {
			state.mu.Lock()
			state.resyncing = false
			state.resyncPending = false
			state.mu.Unlock()
			return
		}

		listCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.opts.ConnectTimeout)
		changed, err := m.relist(listCtx, state, def, client)
		cancel()
		if err != nil {
			slog.Warn("mcp: re-discover after list change", "server", def.Metadata.Name, "error", err)
		} else if changed {
			slog.Debug("mcp: re-registered tools after list change", "server", def.Metadata.Name)
			// The server added or removed tools, so an open session's container is
			// now out of date with what the workspace can actually call.
			m.notifyDiscovery()
		}

		state.mu.Lock()
		if !state.resyncPending {
			state.resyncing = false
			state.mu.Unlock()
			return
		}
		state.resyncPending = false
		state.mu.Unlock()
	}
}

// currentDefinition returns the definition the state tracks, preferring the
// workspace's current one so a reload's policy edits apply to a re-list.
func (m *Manager) currentDefinition(state *serverState) *warp.MCP {
	tracked := state.definition()
	if tracked == nil || m.opts.Definer == nil {
		return tracked
	}
	if current, ok := m.opts.Definer.MCPDef(tracked.Metadata.Name); ok && current != nil {
		return current
	}
	return tracked
}

// relist re-lists all three categories, replacing only those that listed
// successfully, and re-publishes tools when the permitted set changed.
func (m *Manager) relist(ctx context.Context, state *serverState, def *warp.MCP, client *loommcp.Client) (bool, error) {
	tools, toolsErr := discoverTools(ctx, client, def)
	prompts, promptsErr := discoverPrompts(ctx, client, def)
	resources, resourcesErr := discoverResources(ctx, client, def)

	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return false, ErrManagerClosed
	}
	if toolsErr == nil {
		state.tools = tools
	}
	if promptsErr == nil {
		state.prompts = prompts
	}
	if resourcesErr == nil {
		state.resources = resources
	}
	published := slices.Clone(state.tools)
	state.mu.Unlock()

	if toolsErr != nil {
		return false, toolsErr
	}
	return true, m.publish(def, rawTools(published))
}

// buildConfig renders the connection configuration for one server: transport,
// rendered command or endpoint, expanded environment and headers, static
// credentials, and the OAuth provider when the manifest asks for one.
func (m *Manager) buildConfig(def *warp.MCP, env map[string]string, render warp.MCPRenderOptions) (loommcp.Config, *oauthProvider, error) {
	var cfg loommcp.Config
	switch transportName(def) {
	case string(warp.MCPTransportStdio):
		command, err := renderCommand(def, render, env)
		if err != nil {
			return cfg, nil, fmt.Errorf("MCP server %q: render command: %w", def.Metadata.Name, err)
		}
		if len(command) == 0 {
			return cfg, nil, fmt.Errorf("MCP server %q declares no command for the stdio transport", def.Metadata.Name)
		}
		cfg.Transport = "stdio"
		cfg.Command = command[0]
		if len(command) > 1 {
			cfg.Args = command[1:]
		}
		cfg.Env = expandEnvMap(def.Spec.Env, env)
	case string(warp.MCPTransportSSE), string(warp.MCPTransportHTTP), string(warp.MCPTransportStreamableHTTP):
		endpoint, err := renderEndpoint(def, render)
		if err != nil {
			return cfg, nil, fmt.Errorf("MCP server %q: render endpoint: %w", def.Metadata.Name, err)
		}
		if strings.TrimSpace(endpoint) == "" {
			return cfg, nil, fmt.Errorf("MCP server %q declares no endpoint for the %s transport", def.Metadata.Name, def.Spec.Type)
		}
		cfg.Transport = "http"
		cfg.URL = endpoint
		// Server-Sent Events needs the persistent GET stream; the newer streamable
		// transports do not use one.
		cfg.DisableStandaloneSSE = def.Spec.Type != warp.MCPTransportSSE

		headers, err := staticAuthHeaders(def, env)
		if err != nil {
			return cfg, nil, err
		}
		cfg.Headers = headers

		var provider *oauthProvider
		if def.Spec.Auth != nil && isOAuthScheme(def.Spec.Auth.Type) {
			provider = &oauthProvider{
				name:     def.Metadata.Name,
				key:      serverKey(def.Metadata.Name, endpoint),
				scheme:   strings.ToLower(strings.TrimSpace(def.Spec.Auth.Type)),
				endpoint: endpoint,
				auth:     def.Spec.Auth,
				env:      env,
				store:    m.creds,
			}
			provider.events.onAuthorize = func(authorizeURL string) {
				state := m.state(def)
				state.mu.Lock()
				state.authURL = authorizeURL
				state.status = api.MCPStatusAuthRequired
				state.mu.Unlock()
			}
			cfg.Auth = provider
		}
		return cfg, provider, nil
	default:
		return cfg, nil, fmt.Errorf("MCP server %q declares unsupported transport %q", def.Metadata.Name, transportName(def))
	}
	return cfg, nil, nil
}

// discover reads the server's tools, prompts, and resources, marking each item
// with whether the manifest's policies permit it. Discovery failures for one
// category are recorded as empty rather than failing the whole connection, since
// servers legitimately support only some categories.
func discover(ctx context.Context, client *loommcp.Client, def *warp.MCP) ([]api.MCPToolSummary, []api.MCPPromptSummary, []api.MCPResourceSummary) {
	tools, _ := discoverTools(ctx, client, def)
	prompts, _ := discoverPrompts(ctx, client, def)
	resources, _ := discoverResources(ctx, client, def)
	return tools, prompts, resources
}

// discoverTools lists and converts the server's tools.
func discoverTools(ctx context.Context, client *loommcp.Client, def *warp.MCP) ([]api.MCPToolSummary, error) {
	discovered, err := client.Tools(ctx)
	if err != nil {
		return nil, err
	}
	var tools []api.MCPToolSummary
	for _, t := range discovered {
		if t == nil {
			continue
		}
		rawName := rawToolName(def.Metadata.Name, t.Definition.Name)
		tools = append(tools, api.MCPToolSummary{
			Name:         def.ToolName(rawName),
			RawName:      rawName,
			Description:  t.Definition.Description,
			Allowed:      def.AllowsTool(rawName),
			Annotations:  toolAnnotation(def.EffectiveAnnotation(rawName, loomAnnotation(t.Annotation))),
			InputSchema:  schemaMap(t.Definition.InputSchema),
			OutputSchema: schemaMap(t.Definition.OutputSchema),
		})
	}
	slices.SortFunc(tools, func(a, b api.MCPToolSummary) int { return strings.Compare(a.Name, b.Name) })
	return tools, nil
}

// discoverPrompts lists and converts the server's prompts.
func discoverPrompts(ctx context.Context, client *loommcp.Client, def *warp.MCP) ([]api.MCPPromptSummary, error) {
	discovered, err := client.Prompts(ctx)
	if err != nil {
		return nil, err
	}
	var prompts []api.MCPPromptSummary
	for _, p := range discovered {
		prompts = append(prompts, api.MCPPromptSummary{
			Name:        p.Name,
			Title:       p.Title,
			Description: p.Description,
			Arguments:   promptArguments(p.Arguments),
			Allowed:     def.AllowsPrompt(p.Name),
		})
	}
	slices.SortFunc(prompts, func(a, b api.MCPPromptSummary) int { return strings.Compare(a.Name, b.Name) })
	return prompts, nil
}

// discoverResources lists and converts the server's resources.
func discoverResources(ctx context.Context, client *loommcp.Client, def *warp.MCP) ([]api.MCPResourceSummary, error) {
	discovered, err := client.Resources(ctx)
	if err != nil {
		return nil, err
	}
	resources := make([]api.MCPResourceSummary, 0, len(discovered))
	for _, r := range discovered {
		resources = append(resources, api.MCPResourceSummary{
			Name:        r.Name,
			Title:       r.Title,
			URI:         r.URI,
			Description: r.Description,
			MIMEType:    r.MIMEType,
			Size:        r.Size,
			Allowed:     def.AllowsResource(r.URI),
		})
	}
	slices.SortFunc(resources, func(a, b api.MCPResourceSummary) int { return strings.Compare(a.URI, b.URI) })
	return resources, nil
}

// promptArguments converts discovered prompt arguments.
func promptArguments(args []loommcp.PromptArgument) []api.MCPPromptArgument {
	if len(args) == 0 {
		return nil
	}
	out := make([]api.MCPPromptArgument, 0, len(args))
	for _, a := range args {
		out = append(out, api.MCPPromptArgument{
			Name:        a.Name,
			Title:       a.Title,
			Description: a.Description,
			Required:    a.Required,
		})
	}
	return out
}

// rawToolName strips the server prefix from a discovered tool name.
func rawToolName(serverName, discoveredName string) string {
	name := strings.TrimSpace(discoveredName)
	if mcpName, raw, ok := warp.ParseMCPToolName(name); ok && mcpName == serverName {
		return raw
	}
	if after, ok := strings.CutPrefix(name, serverName+"__"); ok {
		return after
	}
	if after, ok := strings.CutPrefix(name, "mcp__"+serverName+"__"); ok {
		return after
	}
	return name
}

// rawTools converts discovered tool summaries into the raw definitions the
// workspace registry expects. Summaries already carry the materialized
// "<server>__<tool>" name, which registration accepts, applies policy filtering
// to, and resolves effective annotations from.
func rawTools(tools []api.MCPToolSummary) []*warp.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]*warp.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, &warp.Tool{
			BaseResource: warp.BaseResource{
				Kind: warp.KindTool,
				Metadata: warp.Metadata{
					Name:        t.Name,
					Description: t.Description,
				},
			},
			Spec: warp.ToolSpec{
				InputSchema:  t.InputSchema,
				OutputSchema: t.OutputSchema,
				Annotations:  warpAnnotation(t.Annotations),
			},
		})
	}
	return out
}

// loomAnnotation converts a client annotation into the wire annotation shape used
// for effective-annotation resolution. MCP omits annotations entirely for many
// servers, so a zero annotation stays nil and lets manifest defaults win.
func loomAnnotation(a tool.Annotation) *warp.ToolAnnotation {
	if a == (tool.Annotation{}) {
		return nil
	}
	return &warp.ToolAnnotation{
		IsOpenWorld:  a.IsOpenWorld,
		IsDangerous:  a.IsDangerous,
		IsReadOnly:   a.IsReadOnly,
		IsIdempotent: a.IsIdempotent,
		UserHint:     a.UserHint,
	}
}

// warpAnnotation converts an API annotation into the manifest annotation shape.
func warpAnnotation(a api.ToolAnnotation) *warp.ToolAnnotation {
	return &warp.ToolAnnotation{
		IsOpenWorld:  a.IsOpenWorld,
		IsDangerous:  a.IsDangerous,
		IsReadOnly:   a.IsReadOnly,
		IsIdempotent: a.IsIdempotent,
		UserHint:     a.UserHint,
	}
}

// toolAnnotation converts a manifest annotation into its API presentation.
func toolAnnotation(a *warp.ToolAnnotation) api.ToolAnnotation {
	if a == nil {
		return api.ToolAnnotation{}
	}
	return api.ToolAnnotation{
		IsOpenWorld:  a.IsOpenWorld,
		IsDangerous:  a.IsDangerous,
		IsReadOnly:   a.IsReadOnly,
		IsIdempotent: a.IsIdempotent,
		UserHint:     a.UserHint,
	}
}

// schemaMap converts a JSON schema pointer into the map form the API and registry
// carry.
func schemaMap(schema *jsonschema.Schema) map[string]any {
	if schema == nil {
		return nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
