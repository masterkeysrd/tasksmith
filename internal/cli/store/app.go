package store

import (
	"context"
	"slices"
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/vcs"
)

const (
	// DefaultHint is the standard shortcut hint shown on the status line.
	DefaultHint = "Shift+Tab mode · /help"
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
	Branch        string
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
	ToolsFetched    bool
	DefaultAgent    string
	DaemonStatus    DaemonStatus
	SandboxWarnings *[]string
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var branch string
	if info, err := vcs.Inspect(ctx, workspace); err == nil && info.Type != vcs.RepoTypeNone {
		branch = info.Branch
		if info.Detached {
			branch = "HEAD@" + info.Commit[:7]
		}
	}

	app.Set(func(s AppState) AppState {
		s.Version = version
		s.Workspace = workspace
		s.Branch = branch
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

// SetWorkspaceWarnings updates the warning view and the sandbox-only header warnings.
func SetWorkspaceWarnings(warnings []string, sandboxWarnings ...string) {
	app.Set(func(s AppState) AppState {
		s.Warnings = warningSnapshot(s.Warnings, warnings)
		s.SandboxWarnings = warningSnapshot(s.SandboxWarnings, sandboxWarnings)
		return s
	})
}

func warningSnapshot(previous *[]string, warnings []string) *[]string {
	if len(warnings) == 0 {
		return nil
	}
	unique := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if !slices.Contains(unique, warning) {
			unique = append(unique, warning)
		}
	}
	if previous != nil && slices.Equal(*previous, unique) {
		return previous
	}
	return &unique
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

// SetWorkspaceMCPServers replaces the cached listing used by the warnings view and MCP tools.
func SetWorkspaceMCPServers(servers []api.MCPServer) {
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
	app.Set(func(s AppState) AppState {
		return AppState{
			Hint:            DefaultHint,
			Version:         s.Version,
			Workspace:       s.Workspace,
			Branch:          s.Branch,
			Warnings:        s.Warnings,
			SandboxWarnings: s.SandboxWarnings,
			DaemonStatus:    DaemonConnected,
		}
	})
}
