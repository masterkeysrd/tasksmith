package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	loommcp "github.com/masterkeysrd/loom/mcp"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// DefaultConnectTimeout bounds a single connection and discovery attempt.
const DefaultConnectTimeout = 30 * time.Second

// ErrServerNotFound reports that the workspace declares no MCP server with the
// requested name.
var ErrServerNotFound = errors.New("mcp server not found")

// ErrManagerClosed reports that the manager has been shut down with its workspace.
var ErrManagerClosed = errors.New("mcp manager is closed")

// Definer resolves the effective MCP definitions in a workspace.
type Definer interface {
	// MCPDef resolves the effective MCP definition for a short or qualified name.
	MCPDef(name string) (*warp.MCP, bool)
	// MCPDefinitions returns every effective MCP definition in scope.
	MCPDefinitions() []*warp.MCP
}

// Syncer receives discovered tools into the workspace registry and refreshes the
// catalog so tool lists, statuses, and token counts reflect live discovery.
type Syncer interface {
	// SyncMCPTools registers the discovered raw tools for one MCP server and
	// refreshes the workspace catalog. An empty slice clears the server's tools.
	SyncMCPTools(def *warp.MCP, rawTools []*warp.Tool) error
}

// Options configures a [Manager].
type Options struct {
	// Definer resolves MCP definitions. Required.
	Definer Definer
	// Syncer receives discovered tools. Optional; without it tools are still
	// discovered and reported, but the workspace registry is not updated.
	Syncer Syncer
	// ReloadWorkspace re-reads manifests from disk after a policy edit. Optional.
	ReloadWorkspace func(context.Context) error
	// Env is the workspace's isolated environment map, used to expand spec.env and
	// spec.headers placeholders and to resolve credentials.
	Env map[string]string
	// RenderOptions supplies workspace and project context for rendering commands
	// and endpoints.
	RenderOptions warp.MCPRenderOptions
	// ConnectTimeout bounds one connection and discovery attempt. Defaults to
	// [DefaultConnectTimeout].
	ConnectTimeout time.Duration
	// Credentials overrides the OAuth credential store, mainly for tests.
	Credentials *CredentialStore
	// AutoConnect starts background connections on [Manager.Sync]. When false,
	// servers connect only on demand.
	AutoConnect bool
	// OnDiscover runs after a server's discovered tools have been registered with the
	// workspace, which is the moment those tools become callable. A workspace whose
	// sessions are already open uses it to rebuild their tool containers; without it,
	// a server that finishes connecting after a session started would stay invisible
	// to that session. Optional.
	OnDiscover func()
}

// serverState holds one server's live runtime state.
type serverState struct {
	mu       sync.Mutex
	def      *warp.MCP
	client   *loommcp.Client
	provider *oauthProvider

	status  api.MCPStatus
	lastErr string
	authURL string

	tools     []api.MCPToolSummary
	prompts   []api.MCPPromptSummary
	resources []api.MCPResourceSummary

	// resyncing and resyncPending coalesce the list-change notifications a server
	// can emit in a burst, so a flurry of changes re-discovers once rather than
	// once per notification.
	resyncing     bool
	resyncPending bool

	discovered bool
	connecting bool
	closed     bool
}

// shutdown closes the server's connection and clears its discovery results.
func (s *serverState) shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	client := s.client
	s.client = nil
	s.tools = nil
	s.prompts = nil
	s.resources = nil
	s.discovered = false
	s.status = api.MCPStatusDisconnected
	s.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

// Manager owns the live connections for one workspace's MCP servers and satisfies
// the workspace MCPService contract.
type Manager struct {
	opts Options

	mu      sync.Mutex
	servers map[string]*serverState
	creds   *CredentialStore
	closed  error
	// runtime is the fingerprint of the environment and render options last
	// reconciled by [Manager.Sync]; a change means connected servers were built
	// from stale values and must reconnect.
	runtime string
}

// New creates a manager for a workspace's declared MCP servers. With
// [Options.AutoConnect], servers begin connecting in the background so opening a
// workspace is never blocked by a slow or unreachable server.
func New(opts Options) (*Manager, error) {
	if opts.Definer == nil {
		return nil, errors.New("mcp: Definer is required")
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = DefaultConnectTimeout
	}
	creds := opts.Credentials
	if creds == nil {
		store, err := NewCredentialStore()
		if err != nil {
			return nil, fmt.Errorf("mcp: prepare credential store: %w", err)
		}
		creds = store
	}
	return &Manager{
		opts:    opts,
		servers: make(map[string]*serverState),
		creds:   creds,
	}, nil
}

// Sync reconciles managed servers with the workspace's current declarations: new
// servers start connecting, removed servers close, and changed definitions
// reconnect. Call it on workspace open and after every reload.
func (m *Manager) Sync(ctx context.Context) {
	if m == nil {
		return
	}
	defs := m.opts.Definer.MCPDefinitions()

	type pending struct {
		def *warp.MCP
	}
	var toStart []pending
	var toClose []*serverState
	// toRepublish holds servers whose connection is unchanged but whose tools may
	// have been lost when the workspace swapped its tool registry during a reload.
	var toRepublish []*serverState

	m.mu.Lock()
	if m.closed != nil {
		m.mu.Unlock()
		return
	}
	keep := make(map[string]struct{}, len(defs))
	runtimeChanged := m.rememberRuntimeLocked()
	for _, def := range defs {
		if def == nil || strings.TrimSpace(def.Metadata.Name) == "" {
			continue
		}
		name := def.Metadata.Name
		keep[name] = struct{}{}
		existing, ok := m.servers[name]
		if ok && !runtimeChanged && sameDefinition(existing.definition(), def) {
			existing.setDefinition(def)
			if republishEligible(existing) {
				toRepublish = append(toRepublish, existing)
			}
			continue
		}
		if ok {
			existing.mu.Lock()
			connecting := existing.connecting
			existing.mu.Unlock()
			if connecting {
				// A connect is already in flight with the values that were current
				// when it started; let it finish rather than stacking attempts.
				existing.setDefinition(def)
				continue
			}
			toClose = append(toClose, existing)
			delete(m.servers, name)
		}
		toStart = append(toStart, pending{def: def})
	}
	for name, state := range m.servers {
		if _, ok := keep[name]; !ok {
			toClose = append(toClose, state)
			delete(m.servers, name)
		}
	}
	autoConnect := m.opts.AutoConnect
	m.mu.Unlock()

	for _, state := range toClose {
		def := state.definition()
		state.shutdown()
		if def != nil {
			if err := m.publish(def, nil); err != nil {
				slog.Warn("mcp: unregister tools for disconnected server", "server", def.Metadata.Name, "error", err)
			}
		}
	}
	for _, state := range toRepublish {
		if err := m.republish(state); err != nil {
			slog.Warn("mcp: re-register tools after workspace reload", "error", err)
		}
	}
	for _, start := range toStart {
		state := m.state(start.def)
		if !autoConnect {
			continue
		}
		state.mu.Lock()
		if !state.closed && state.client == nil && state.status == api.MCPStatusDisconnected {
			state.status = api.MCPStatusConnecting
		}
		state.mu.Unlock()
		name := start.def.Metadata.Name
		go func(state *serverState, name string) {
			// The failure is also recorded on the state so listings can report it;
			// logging it here is what makes an unreachable server diagnosable from
			// daemon logs alone.
			if err := m.attempt(context.WithoutCancel(ctx), state); err != nil {
				slog.Warn("mcp: server connect failed", "server", name, "error", err)
			}
		}(state, name)
	}
}

// republish re-registers a connected server's discovered tools, which a workspace
// reload discards by replacing the tool registry.
func (m *Manager) republish(state *serverState) error {
	if m.opts.Syncer == nil {
		return nil
	}
	state.mu.Lock()
	def := state.def
	tools := slices.Clone(state.tools)
	connected := state.client != nil && state.discovered && !state.closed
	state.mu.Unlock()
	if !connected || def == nil {
		return nil
	}
	if err := m.publish(def, rawTools(tools)); err != nil {
		return err
	}
	m.notifyDiscovery()
	return nil
}

// republishEligible reports whether a server already holds tools that a workspace
// reload would have discarded.
func republishEligible(state *serverState) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.client != nil && state.discovered && !state.closed && len(state.tools) > 0
}

// rememberRuntimeLocked records the current runtime context and reports whether
// it changed since the last reconciliation. It must be called with m.mu held.
func (m *Manager) rememberRuntimeLocked() bool {
	fingerprint := runtimeFingerprint(m.opts.Env, m.opts.RenderOptions)
	changed := fingerprint != m.runtime
	m.runtime = fingerprint
	return changed
}

// runtimeFingerprint produces a stable digest of the values a connection is
// rendered from, so a reload that changes them can force a reconnect.
func runtimeFingerprint(env map[string]string, render warp.MCPRenderOptions) string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	slices.Sort(names)
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(env[name])
		builder.WriteByte('\n')
	}
	if render.Workspace != nil {
		builder.WriteString("workspace=")
		builder.WriteString(render.Workspace.RootPath)
		builder.WriteByte('\n')
	}
	if render.Project != nil {
		builder.WriteString("project=")
		builder.WriteString(render.Project.Name)
		builder.WriteByte('=')
		builder.WriteString(render.Project.AbsPath())
		builder.WriteByte('\n')
	}
	return builder.String()
}

// SetRuntimeContext updates the workspace-derived inputs a manager uses after a
// reload: the isolated environment map and the render context for commands and
// endpoints. Connected servers keep their current connection; values apply to the
// next connection and to what listing reports.
func (m *Manager) SetRuntimeContext(env map[string]string, render warp.MCPRenderOptions) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed != nil {
		return
	}
	m.opts.Env = maps.Clone(env)
	m.opts.RenderOptions = render
}

// runtimeContext returns the workspace-derived inputs used to render commands and
// endpoints and to expand placeholders. The returned environment map is a copy so
// a rebuild never observes a concurrent reload.
func (m *Manager) runtimeContext() (map[string]string, warp.MCPRenderOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.opts.Env), m.opts.RenderOptions
}

// state returns the live state for def, creating it as disconnected when new.
func (m *Manager) state(def *warp.MCP) *serverState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed != nil {
		return &serverState{def: def, status: api.MCPStatusDisconnected}
	}
	name := def.Metadata.Name
	if existing, ok := m.servers[name]; ok {
		return existing
	}
	state := &serverState{def: def, status: api.MCPStatusDisconnected}
	m.servers[name] = state
	return state
}

// takeState returns a fresh state for def registered with the manager, and the
// previous state if one existed so the caller can shut it down. A state is closed
// permanently by [serverState.shutdown], so reconnecting requires a new one rather
// than reusing the state that holds the live connection.
func (m *Manager) takeState(def *warp.MCP) (fresh *serverState, previous *serverState, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed != nil {
		return nil, nil, m.closed
	}
	name := def.Metadata.Name
	fresh = &serverState{def: def, status: api.MCPStatusDisconnected}
	if existing, ok := m.servers[name]; ok {
		previous = existing
	}
	m.servers[name] = fresh
	return fresh, previous, nil
}

// reconnect drops a server's current connection and connects again from the
// definition it now resolves to, which is what a restart or a policy edit needs.
func (m *Manager) reconnect(ctx context.Context, def *warp.MCP) error {
	fresh, previous, err := m.takeState(def)
	if err != nil {
		return err
	}
	if previous != nil {
		m.disconnect(def, previous)
	}
	return m.attempt(ctx, fresh)
}

// disconnect closes a server's connection and removes the tools it registered, so
// a disconnected server never leaves callable tools behind.
func (m *Manager) disconnect(def *warp.MCP, state *serverState) {
	state.shutdown()
	if def == nil {
		return
	}
	if err := m.publish(def, nil); err != nil {
		slog.Warn("mcp: unregister tools for disconnected server", "server", def.Metadata.Name, "error", err)
	}
}

// Close disconnects every server.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed != nil {
		m.mu.Unlock()
		return nil
	}
	m.closed = ErrManagerClosed
	states := make([]*serverState, 0, len(m.servers))
	for _, state := range m.servers {
		states = append(states, state)
	}
	clear(m.servers)
	m.mu.Unlock()

	for _, state := range states {
		state.shutdown()
	}
	return nil
}

// ListMCPs returns every declared server with live status and everything
// discovered on it, sorted by name. A server whose connection failed reports
// status error with the reason, and items blocked by spec.policies are listed with
// Allowed false so callers can show what a server offers and why an item is
// restricted.
func (m *Manager) ListMCPs(_ context.Context) []api.MCPServer {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	defs := m.opts.Definer.MCPDefinitions()
	out := make([]api.MCPServer, 0, len(defs))
	env, render := m.runtimeContext()

	m.mu.Lock()
	states := make(map[string]*serverState, len(m.servers))
	maps.Copy(states, m.servers)
	m.mu.Unlock()

	for _, def := range defs {
		if def == nil {
			continue
		}
		state, ok := states[def.Metadata.Name]
		if !ok {
			state = &serverState{def: def, status: api.MCPStatusDisconnected}
		}
		server := state.snapshot(def)
		server.Namespace, server.Scope = scopeOf(def)
		server.Path = def.GetPath()
		if command, err := renderCommand(def, render, env); err == nil {
			server.Command = command
		}
		if endpoint, err := renderEndpoint(def, render); err == nil {
			server.Endpoint = endpoint
		}
		out = append(out, server)
	}
	slices.SortFunc(out, func(a, b api.MCPServer) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// RestartMCP reconnects the named server, re-discovers its tools, prompts, and
// resources, and returns the refreshed state.
func (m *Manager) RestartMCP(ctx context.Context, name string) (api.MCPServer, error) {
	def, err := m.definition(name)
	if err != nil {
		return api.MCPServer{}, err
	}
	if err := m.reconnect(ctx, def); err != nil {
		return api.MCPServer{}, err
	}
	return m.serverSnapshot(def)
}

// AuthenticateMCP starts or resumes interactive authorization for the named
// server and reports the URL the user should open. A server already authorized
// with stored credentials reports its connected status and no URL. A server that
// authenticates with static or machine-to-machine credentials has no browser flow
// to open, so it answers with the credential the user must supply instead of a
// dead end.
func (m *Manager) AuthenticateMCP(ctx context.Context, name string) (api.MCPAuthResponse, error) {
	def, err := m.definition(name)
	if err != nil {
		return api.MCPAuthResponse{}, err
	}
	if def.Spec.Auth == nil || !isOAuthScheme(def.Spec.Auth.Type) {
		env, _ := m.runtimeContext()
		if instruction, ok := staticAuthInstruction(def, env); ok {
			return api.MCPAuthResponse{}, &api.APIError{
				StatusCode: http.StatusBadRequest,
				Code:       api.ErrCodeMCPAuthStatic,
				Message:    instruction,
			}
		}
		return api.MCPAuthResponse{}, fmt.Errorf("MCP server %q does not use an interactive authorization flow", def.Metadata.Name)
	}
	state := m.state(def)

	status, authorizeURL := state.view()
	if status == api.MCPStatusConnected {
		return api.MCPAuthResponse{Status: status}, nil
	}
	if authorizeURL == "" {
		attemptErr := m.startAndWaitForAuth(ctx, state)
		status, authorizeURL = state.view()
		if status == api.MCPStatusConnected {
			return api.MCPAuthResponse{Status: status}, nil
		}
		if attemptErr != nil && authorizeURL == "" {
			return api.MCPAuthResponse{Status: status}, attemptErr
		}
	}
	if authorizeURL == "" {
		return api.MCPAuthResponse{Status: status}, fmt.Errorf("MCP server %q did not report an authorization URL", def.Metadata.Name)
	}
	return api.MCPAuthResponse{AuthURL: authorizeURL, Status: api.MCPStatusAuthRequired}, nil
}

// SubmitMCPAuthCallback completes an in-flight interactive authorization with a
// redirect URL the user pasted from their browser. It exists for the case the
// loopback listener cannot serve: the browser is on another machine, a proxy or
// firewall blocks localhost, or the redirect page simply failed to load. The
// waiting flow is unchanged by this — it receives the code exactly as if the
// browser had reached the daemon.
func (m *Manager) SubmitMCPAuthCallback(_ context.Context, name, callbackURL string) error {
	def, err := m.definition(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	state, ok := m.servers[def.Metadata.Name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no authorization is in progress for %s; start one with /mcp auth %s", def.Metadata.Name, def.Metadata.Name)
	}
	state.mu.Lock()
	provider := state.provider
	state.mu.Unlock()
	if provider == nil {
		return fmt.Errorf("no authorization is in progress for %s; start one with /mcp auth %s", def.Metadata.Name, def.Metadata.Name)
	}
	return provider.submitCallbackURL(callbackURL)
}

// startAndWaitForAuth launches a background connection attempt for state and
// returns as soon as the OAuth provider publishes its authorization URL, the
// server finishes connecting without a browser flow, or the attempt fails. The
// background attempt remains alive while waiting for the browser callback.
func (m *Manager) startAndWaitForAuth(ctx context.Context, state *serverState) error {
	waitCtx, cancel := context.WithTimeout(ctx, m.opts.ConnectTimeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	// Wait for any non-interactive startup attempt to finish first so our
	// interactive attempt is not skipped by the state.connecting guard.
	for {
		status, authorizeURL, _ := state.viewError()
		if authorizeURL != "" || status == api.MCPStatusConnected {
			return nil
		}
		state.mu.Lock()
		connecting := state.connecting
		state.mu.Unlock()
		if !connecting {
			break
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- m.attemptWithOptions(context.WithoutCancel(ctx), state, true)
	}()

	var lastErr string
	for {
		status, authorizeURL, _ := state.viewError()
		if authorizeURL != "" || status == api.MCPStatusConnected {
			return nil
		}
		select {
		case err := <-errCh:
			status, authorizeURL, lastErr = state.viewError()
			if authorizeURL != "" || status == api.MCPStatusConnected {
				return nil
			}
			if err != nil {
				return err
			}
			state.mu.Lock()
			connecting := state.connecting
			state.mu.Unlock()
			if !connecting {
				if lastErr != "" {
					return errors.New(lastErr)
				}
				return nil
			}
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
			state.mu.Lock()
			connecting := state.connecting
			state.mu.Unlock()
			if !connecting {
				select {
				case err := <-errCh:
					if err != nil {
						return err
					}
				default:
				}
				status, authorizeURL, lastErr = state.viewError()
				if authorizeURL != "" || status == api.MCPStatusConnected {
					return nil
				}
				if lastErr != "" {
					return errors.New(lastErr)
				}
				return nil
			}
		}
	}
}

// GetMCPPrompt renders a prompt discovered on a server. Policy enforcement is the
// daemon's responsibility, so a prohibited prompt reaches this point only when a
// user explicitly authorized that single use.
func (m *Manager) GetMCPPrompt(ctx context.Context, name, prompt string, args map[string]string, _ bool) (api.MCPPromptResponse, error) {
	def, err := m.definition(name)
	if err != nil {
		return api.MCPPromptResponse{}, err
	}
	client, err := m.clientFor(ctx, def)
	if err != nil {
		return api.MCPPromptResponse{}, err
	}
	messages, err := client.GetPrompt(ctx, prompt, args)
	if err != nil {
		return api.MCPPromptResponse{}, fmt.Errorf("render MCP prompt %q from server %q: %w", prompt, def.Metadata.Name, err)
	}
	resp := api.MCPPromptResponse{Description: m.promptDescription(def, prompt)}
	for _, msg := range messages {
		resp.Messages = append(resp.Messages, api.MCPPromptMessage{
			Role: api.Role(msg.Role()),
			Text: msg.GetContent().Text(),
		})
	}
	return resp, nil
}

// ReadMCPResource reads one resource URI from a server.
func (m *Manager) ReadMCPResource(ctx context.Context, name string, req api.MCPResourceRequest) (api.MCPResourceResponse, error) {
	def, err := m.definition(name)
	if err != nil {
		return api.MCPResourceResponse{}, err
	}
	client, err := m.clientFor(ctx, def)
	if err != nil {
		return api.MCPResourceResponse{}, err
	}
	content, err := client.GetResources(ctx, []string{req.URI})
	if err != nil {
		return api.MCPResourceResponse{}, fmt.Errorf("read MCP resource %q from server %q: %w", req.URI, def.Metadata.Name, err)
	}
	resp := api.MCPResourceResponse{URI: req.URI, MIMEType: m.resourceMIMEType(def, req.URI)}
	var texts []string
	for _, block := range content {
		if textBlock, ok := block.(*message.TextBlock); ok && textBlock.Text != "" {
			texts = append(texts, textBlock.Text)
		}
	}
	resp.Text = strings.Join(texts, "\n")
	return resp, nil
}

// AllowPolicyItem permits one item in the named server's manifest, reloads the
// workspace so the new policy takes effect, reconnects the server, and returns the
// refreshed state.
func (m *Manager) AllowPolicyItem(ctx context.Context, name string, req api.MCPAllowPolicyRequest) (api.MCPServer, error) {
	def, err := m.definition(name)
	if err != nil {
		return api.MCPServer{}, err
	}
	if _, err := AllowPolicyItem(def, req.Kind, req.Value); err != nil {
		return api.MCPServer{}, err
	}
	if m.opts.ReloadWorkspace != nil {
		if err := m.opts.ReloadWorkspace(ctx); err != nil {
			return api.MCPServer{}, fmt.Errorf("reload workspace after policy update: %w", err)
		}
	}
	updated, err := m.definition(name)
	if err != nil {
		return api.MCPServer{}, err
	}
	state := m.state(updated)
	state.setDefinition(updated)
	if state.currentClient() != nil {
		if err := m.republish(state); err != nil {
			return api.MCPServer{}, err
		}
	} else if err := m.reconnect(ctx, updated); err != nil {
		return api.MCPServer{}, err
	}
	return m.serverSnapshot(updated)
}

// CallMCPTool executes a tool on a server and returns its streaming result. A tool
// prohibited by spec.policies.tools is rejected here as defense in depth, even
// though prohibited tools are never registered as callable agent tools.
func (m *Manager) CallMCPTool(ctx context.Context, serverName, toolName string, args map[string]any) (tool.ToolStream, error) {
	def, err := m.definition(serverName)
	if err != nil {
		return nil, err
	}
	if !def.AllowsTool(toolName) {
		return nil, fmt.Errorf("MCP tool %q is prohibited by spec.policies.tools in %s", toolName, def.GetPath())
	}
	client, err := m.clientFor(ctx, def)
	if err != nil {
		return nil, err
	}
	// The server only ever knows the raw wire name; the <server>__ prefix exists
	// solely to keep MCP tools unique inside the shared tool registry.
	wireName := toolName
	if serverName, rawName, ok := warp.ParseMCPToolName(toolName); ok && serverName == def.Metadata.Name {
		wireName = rawName
	}
	return client.CallTool(ctx, wireName, args)
}

// HasAllowedMCPResources reports whether any server exposes a resource that
// spec.policies permits, which decides whether the resource-reading built-in tool
// is worth loading.
func (m *Manager) HasAllowedMCPResources() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	states := make([]*serverState, 0, len(m.servers))
	for _, state := range m.servers {
		states = append(states, state)
	}
	m.mu.Unlock()

	for _, state := range states {
		state.mu.Lock()
		found := slices.ContainsFunc(state.resources, func(r api.MCPResourceSummary) bool { return r.Allowed })
		state.mu.Unlock()
		if found {
			return true
		}
	}
	return false
}

// definition resolves an MCP definition by name, mapping misses to
// [ErrServerNotFound].
func (m *Manager) definition(name string) (*warp.MCP, error) {
	if m == nil {
		return nil, ErrManagerClosed
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed != nil {
		return nil, closed
	}
	def, ok := m.opts.Definer.MCPDef(name)
	if !ok || def == nil {
		return nil, fmt.Errorf("%w: %s", ErrServerNotFound, strings.TrimSpace(name))
	}
	return def, nil
}

// clientFor returns a live client for def, connecting on demand when needed.
func (m *Manager) clientFor(ctx context.Context, def *warp.MCP) (*loommcp.Client, error) {
	state := m.state(def)
	if client := state.currentClient(); client != nil {
		return client, nil
	}
	if err := m.attempt(ctx, state); err != nil {
		return nil, err
	}
	client := state.currentClient()
	if client == nil {
		_, _, lastErr := state.viewError()
		if lastErr != "" {
			return nil, fmt.Errorf("MCP server %q is not connected: %s", def.Metadata.Name, lastErr)
		}
		return nil, fmt.Errorf("MCP server %q is not connected", def.Metadata.Name)
	}
	return client, nil
}

// attempt connects and discovers a server, recording the outcome on its state. A
// server already connecting is left alone so concurrent requests do not stack
// connection attempts.
func (m *Manager) attempt(ctx context.Context, state *serverState) error {
	return m.attemptWithOptions(ctx, state, false)
}

func (m *Manager) attemptWithOptions(ctx context.Context, state *serverState, interactive bool) error {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed != nil {
		return closed
	}
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return ErrManagerClosed
	}
	if state.connecting {
		state.mu.Unlock()
		return nil
	}
	state.connecting = true
	state.status = api.MCPStatusConnecting
	def := state.def
	state.mu.Unlock()

	err := m.connectAndDiscover(ctx, state, def, interactive)
	if err == nil {
		m.notifyDiscovery()
	}

	// Resolved before re-acquiring the state lock: failureStatus reads the state
	// itself, and locking it twice from the same goroutine would wedge the server.
	status := m.failureStatus(state, err)
	if err != nil && status == api.MCPStatusAuthRequired && !isAuthError(def, err) {
		env, _ := m.runtimeContext()
		if missing := unexpandedCredentialPlaceholders(def, env); len(missing) > 0 {
			err = fmt.Errorf("missing credential environment variable %q (%s): %w", placeholderVarName(missing[0]), missing[0], err)
		}
	}

	state.mu.Lock()
	state.connecting = false
	if err != nil {
		state.status = status
		state.lastErr = err.Error()
	}
	state.mu.Unlock()
	return err
}

// notifyDiscovery reports newly callable tools to the workspace. It never blocks a
// connect on session bookkeeping, so the notification runs on its own goroutine.
func (m *Manager) notifyDiscovery() {
	if m.opts.OnDiscover == nil {
		return
	}
	go m.opts.OnDiscover()
}

// failureStatus decides how a failed attempt should be reported: interactive
// authorization pending, an authentication the user still has to supply, or a
// plain connection failure. Anything that only a missing or rejected credential
// explains reports auth_required, because that is the status that makes the CLI
// offer the user something to do about it.
func (m *Manager) failureStatus(state *serverState, err error) api.MCPStatus {
	if err == nil {
		return api.MCPStatusError
	}
	state.mu.Lock()
	authURL := state.authURL
	def := state.def
	state.mu.Unlock()
	if authURL != "" {
		return api.MCPStatusAuthRequired
	}
	if isAuthError(def, err) {
		return api.MCPStatusAuthRequired
	}
	// A placeholder that never resolved means the credential variable is unset,
	// which only shows up as a failure once the connection is attempted.
	env, _ := m.runtimeContext()
	if unexpandedCredentialFailure(def, env) {
		return api.MCPStatusAuthRequired
	}
	return api.MCPStatusError
}

// serverSnapshot renders the current state of a server for an API response.
func (m *Manager) serverSnapshot(def *warp.MCP) (api.MCPServer, error) {
	if def == nil {
		return api.MCPServer{}, errors.New("mcp: definition is required")
	}
	m.mu.Lock()
	state, ok := m.servers[def.Metadata.Name]
	m.mu.Unlock()
	if !ok {
		// The server is declared but not tracked (for example, connections start on
		// demand), so report it from its manifest as disconnected.
		state = &serverState{def: def, status: api.MCPStatusDisconnected}
	}
	server := state.snapshot(def)
	server.Namespace, server.Scope = scopeOf(def)
	server.Path = def.GetPath()
	env, render := m.runtimeContext()
	if command, err := renderCommand(def, render, env); err == nil {
		server.Command = command
	}
	if endpoint, err := renderEndpoint(def, render); err == nil {
		server.Endpoint = endpoint
	}
	return server, nil
}

// promptDescription looks up a discovered prompt's description for rendering.
func (m *Manager) promptDescription(def *warp.MCP, prompt string) string {
	m.mu.Lock()
	state, ok := m.servers[def.Metadata.Name]
	m.mu.Unlock()
	if !ok {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, p := range state.prompts {
		if p.Name == prompt {
			return p.Description
		}
	}
	return ""
}

// resourceMIMEType looks up a discovered resource's media type for rendering.
func (m *Manager) resourceMIMEType(def *warp.MCP, uri string) string {
	m.mu.Lock()
	state, ok := m.servers[def.Metadata.Name]
	m.mu.Unlock()
	if !ok {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, r := range state.resources {
		if r.URI == uri {
			return r.MIMEType
		}
	}
	return ""
}

// renderCommand renders the server's stdio command with workspace context and
// expands placeholders from the workspace environment.
func renderCommand(def *warp.MCP, opts warp.MCPRenderOptions, env map[string]string) ([]string, error) {
	if def.Spec.Type != warp.MCPTransportStdio && len(def.Spec.Command) == 0 {
		return nil, nil
	}
	command, err := def.RenderCommand(&opts)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(command))
	for _, part := range command {
		out = append(out, expandPlaceholders(part, env))
	}
	return out, nil
}

// renderEndpoint renders the server's HTTP endpoint with workspace context.
func renderEndpoint(def *warp.MCP, opts warp.MCPRenderOptions) (string, error) {
	if def.Spec.Type == warp.MCPTransportStdio || strings.TrimSpace(def.Spec.Endpoint) == "" {
		return "", nil
	}
	return def.RenderEndpoint(&opts)
}

// transportName reports the declared transport, defaulting to stdio.
func transportName(def *warp.MCP) string {
	transport := strings.TrimSpace(string(def.Spec.Type))
	if transport == "" {
		return string(warp.MCPTransportStdio)
	}
	return transport
}

// scopeOf reports the namespace and display scope of a definition.
func scopeOf(def *warp.MCP) (namespace, scope string) {
	ns := strings.TrimSpace(def.Namespace)
	switch strings.ToLower(ns) {
	case warp.NamespaceSystem, "builtin", "":
		return warp.NamespaceSystem, "Builtin"
	case warp.NamespaceUser:
		return ns, "User"
	case warp.NamespaceWorkspace:
		return ns, "Workspace"
	default:
		return ns, "Project"
	}
}

// sameDefinition reports whether two definitions describe the same server
// configuration, which decides whether a reload must reconnect it.
func sameDefinition(a, b *warp.MCP) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Metadata.Name != b.Metadata.Name || transportName(a) != transportName(b) {
		return false
	}
	if !slices.Equal(a.Spec.Command, b.Spec.Command) {
		return false
	}
	if a.Spec.Endpoint != b.Spec.Endpoint {
		return false
	}
	if !maps.Equal(a.Spec.Env, b.Spec.Env) || !maps.Equal(a.Spec.Headers, b.Spec.Headers) {
		return false
	}
	return sameAuth(a.Spec.Auth, b.Spec.Auth) &&
		sameJSON(a.Spec.Annotations, b.Spec.Annotations) &&
		maps.Equal(a.Spec.Overrides, b.Spec.Overrides)
}

// sameAuth compares authentication configuration.
func sameAuth(a, b *warp.ProviderAuth) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// sameJSON compares two manifest sub-documents by their encoded form.
func sameJSON[T any](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(left) == string(right)
}
