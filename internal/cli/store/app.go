package store

import (
	"slices"
	"sync"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
)

const (
	// DefaultHint is the standard shortcut hint shown on the status line.
	DefaultHint = "shift+tab cycle mode • /help commands"
	// CtrlCHint is the exit confirmation hint shown when Ctrl+C is first pressed.
	CtrlCHint = "Press Ctrl+C again to exit"
)

// DaemonStatus represents the connection health state between the REPL and Brain Daemon.
type DaemonStatus string

const (
	DaemonConnected    DaemonStatus = "connected"
	DaemonDisconnected DaemonStatus = "disconnected"
	DaemonRestarting   DaemonStatus = "restarting"
)

// AppState holds global application UI state for the REPL.
type AppState struct {
	CtrlCPending  bool
	ExitRequested bool
	Hint          string
	Version       string
	Workspace     string
	Warnings      *[]string
	Agents        *[]api.Agent
	Commands      *[]api.Command
	Skills        *[]api.Skill
	Rules         *[]api.Rule
	Tools         *[]api.Tool
	// MCPServers holds the last retrieved MCP server listing, including live
	// connection status and everything discovered on each server.
	MCPServers *[]api.MCPServer
	// ToolsAgent is the agent the cached Tools statuses were classified for.
	ToolsAgent string
	// ToolsFetched records that a tool listing has been retrieved at least once,
	// so an empty toolset is distinguishable from "never fetched" and does not
	// re-trigger a fetch on every render.
	ToolsFetched bool
	DefaultAgent string
	DaemonStatus DaemonStatus
}

var app = kites.Create(AppState{Hint: DefaultHint, DaemonStatus: DaemonConnected})

// UseApp subscribes a component to a comparable slice of AppState.
func UseApp[U comparable](selector func(AppState) U) U {
	return kites.Use(app, selector)
}

// GetApp returns the current AppState snapshot.
func GetApp() AppState {
	return app.Get()
}

// SetHeaderMeta updates the version and workspace metadata in AppState.
func SetHeaderMeta(version, workspace string) {
	app.Set(func(s AppState) AppState {
		s.Version = version
		s.Workspace = workspace
		return s
	})
}

// SetCtrlCPending updates the Ctrl+C exit confirmation state.
func SetCtrlCPending(pending bool) {
	app.Set(func(s AppState) AppState {
		s.CtrlCPending = pending
		return s
	})
}

// RequestExit requests the REPL application to terminate.
func RequestExit() {
	app.Set(func(s AppState) AppState {
		s.ExitRequested = true
		return s
	})
}

// SetHint updates the status line hint.
func SetHint(hint string) {
	app.Set(func(s AppState) AppState {
		s.Hint = hint
		return s
	})
}

// UseHint sets the status line hint while the calling component is mounted
// and restores the previous hint when the component unmounts.
func UseHint(hint string) {
	trail.UseLayoutEffectCleanup(func() func() {
		prev := GetApp().Hint
		SetHint(hint)
		return func() {
			if GetApp().Hint == hint {
				SetHint(prev)
			}
		}
	}, []any{hint})
}

// SetWorkspaceWarnings records the warnings the daemon reported for the workspace
// and refreshes what the header shows. These are the workspace's own problems —
// a provider missing an API key, a manifest that failed to parse — which stay true
// until the workspace is fetched again. MCP server health is deliberately not part
// of this: it changes while the session runs, and holding it in the header made the
// startup banner jump and keep stale state. See SetWorkspaceMCPServers.
func SetWorkspaceWarnings(warnings []string) {
	setHeaderWarnings(dedupeWarnings(warnings))
}

func dedupeWarnings(warnings []string) []string {
	merged := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if !slices.Contains(merged, warning) {
			merged = append(merged, warning)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// setHeaderWarnings writes the composed warnings into AppState.
func setHeaderWarnings(warnings []string) {
	app.Set(func(s AppState) AppState {
		if len(warnings) == 0 {
			if s.Warnings == nil {
				return s
			}
			s.Warnings = nil
			return s
		}
		if s.Warnings != nil && slices.Equal(*s.Warnings, warnings) {
			return s
		}
		w := make([]string, len(warnings))
		copy(w, warnings)
		s.Warnings = &w
		return s
	})
}

// SetWorkspaceAgents updates the workspace agents in AppState.
func SetWorkspaceAgents(agents []api.Agent) {
	app.Set(func(s AppState) AppState {
		if len(agents) == 0 {
			if s.Agents == nil {
				return s
			}
			s.Agents = nil
			return s
		}
		a := slices.Clone(agents)
		s.Agents = &a
		return s
	})
}

// SetWorkspaceCommands updates the workspace commands in AppState.
func SetWorkspaceCommands(commands []api.Command) {
	app.Set(func(s AppState) AppState {
		if len(commands) == 0 {
			if s.Commands == nil {
				return s
			}
			s.Commands = nil
			return s
		}
		c := slices.Clone(commands)
		s.Commands = &c
		return s
	})
}

// SetWorkspaceSkills updates the workspace skills in AppState.
func SetWorkspaceSkills(skills []api.Skill) {
	app.Set(func(s AppState) AppState {
		if len(skills) == 0 {
			if s.Skills == nil {
				return s
			}
			s.Skills = nil
			return s
		}
		sk := slices.Clone(skills)
		s.Skills = &sk
		return s
	})
}

// SetWorkspaceRules updates the workspace rules in AppState.
func SetWorkspaceRules(rules []api.Rule) {
	app.Set(func(s AppState) AppState {
		if len(rules) == 0 {
			if s.Rules == nil {
				return s
			}
			s.Rules = nil
			return s
		}
		rl := slices.Clone(rules)
		s.Rules = &rl
		return s
	})
}

// SetWorkspaceMCPServers updates the cached MCP server listing in AppState. The
// listing carries live connection status, so it is replaced wholesale rather than
// merged; an empty listing clears the cache so a later fetch re-runs.
//
// A server that has settled into needing authentication or failing to connect is
// announced once in the conversation timeline rather than pinned to the header: the
// listing is polled, so the header would jitter, and a warning that disappears when
// the server connects cannot be read afterwards. Each distinct problem is announced
// once, and a server whose problem changes — a new failure reason, or an
// authentication request that turns into a connection error — is announced again.
func SetWorkspaceMCPServers(servers []api.MCPServer) {
	setWorkspaceMCPServersState(servers)
	announceMCPServerWarnings(servers, true)
}

// SetWorkspaceMCPServersSilent updates the cached MCP server listing and marks any
// current server warnings as already seen without appending a timeline warning. Use
// this after an explicit user action (such as /mcp auth or /mcp restart) that
// already writes its own outcome to the timeline so the same failure is not
// reported twice.
func SetWorkspaceMCPServersSilent(servers []api.MCPServer) {
	setWorkspaceMCPServersState(servers)
	announceMCPServerWarnings(servers, false)
}

func setWorkspaceMCPServersState(servers []api.MCPServer) {
	app.Set(func(s AppState) AppState {
		if len(servers) == 0 {
			if s.MCPServers == nil {
				return s
			}
			s.MCPServers = nil
			return s
		}
		sm := slices.Clone(servers)
		s.MCPServers = &sm
		return s
	})
}

// mcpWarned tracks which MCP server problems the timeline has already announced,
// keyed by server name. A connected or still-connecting server drops its entry, so
// a server that breaks again is reported again.
var mcpWarned sync.Map

// announceMCPServerWarnings writes a timeline warning for every server that has
// settled into an unhealthy state and has not been announced with this exact
// problem yet. Servers that are no longer in the listing are forgotten, so a
// re-added server reporting the same problem is reported again.
func announceMCPServerWarnings(servers []api.MCPServer, emit bool) {
	present := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		present[server.Name] = struct{}{}
		warning := api.MCPServerWarning(server)
		if warning == "" {
			mcpWarned.Delete(server.Name)
			continue
		}
		if previous, ok := mcpWarned.Load(server.Name); ok && previous == warning {
			continue
		}
		mcpWarned.Store(server.Name, warning)
		if emit {
			AppendWarning(warning)
		}
	}
	mcpWarned.Range(func(name, _ any) bool {
		if _, ok := present[name.(string)]; !ok {
			mcpWarned.Delete(name)
		}
		return true
	})
}

// ClearMCPWarningAnnouncements forgets which MCP server problems were announced.
// Starting a session in a different workspace, or fetching a listing that is now
// empty, means the next unhealthy reading deserves to be said again.
func ClearMCPWarningAnnouncements() {
	mcpWarned.Range(func(name, _ any) bool {
		mcpWarned.Delete(name)
		return true
	})
}

// SetDefaultAgent records the workspace's default agent name in AppState. It is
// the agent used for tool status classification when the session has none.
func SetDefaultAgent(agent string) {
	app.Set(func(s AppState) AppState {
		if s.DefaultAgent == agent {
			return s
		}
		s.DefaultAgent = agent
		return s
	})
}

// SetWorkspaceTools updates the workspace tools in AppState along with the name
// of the agent the tool statuses were classified for.
func SetWorkspaceTools(tools []api.Tool, agent string) {
	app.Set(func(s AppState) AppState {
		s.ToolsFetched = true
		if len(tools) == 0 {
			if s.Tools == nil && s.ToolsAgent == agent {
				return s
			}
			s.Tools = nil
			s.ToolsAgent = agent
			return s
		}
		tl := slices.Clone(tools)
		s.Tools = &tl
		s.ToolsAgent = agent
		return s
	})
}

// SetDaemonStatus updates the daemon connection status in AppState.
func SetDaemonStatus(status DaemonStatus) {
	app.Set(func(s AppState) AppState {
		s.DaemonStatus = status
		return s
	})
}

// UseDaemonStatus subscribes a component to AppState.DaemonStatus.
func UseDaemonStatus() DaemonStatus {
	return UseApp(func(s AppState) DaemonStatus {
		return s.DaemonStatus
	})
}

// ResetApp resets AppState to its default values while preserving version, workspace, and warnings.
func ResetApp() {
	// The cached MCP listing is dropped below, so forget what the timeline already
	// announced about it; the next listing deserves to speak for itself.
	ClearMCPWarningAnnouncements()
	app.Set(func(s AppState) AppState {
		return AppState{
			Hint:         DefaultHint,
			Version:      s.Version,
			Workspace:    s.Workspace,
			Warnings:     s.Warnings,
			DaemonStatus: DaemonConnected,
		}
	})
}
