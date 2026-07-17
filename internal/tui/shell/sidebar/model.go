package sidebar

import "github.com/masterkeysrd/tasksmith/internal/api"

// Tab identifies the active sidebar panel.
type Tab string

const (
	TabExplorer     Tab = "explorer"
	TabOrchestrator Tab = "orchestrator"
	TabSessions     Tab = "sessions"
	TabMetrics      Tab = "metrics"
)

// Data is the query-backed state rendered by the shell sidebar.
type Data struct {
	WorkspaceName       string
	WorkspacePath       string
	DefaultProvider     string
	ActiveSessionID     string
	ActiveSessionStatus string
	SwitchingToID       string
	IsConfigured        bool
	AuthorizedTools     map[string]bool
	Projects            []api.Project
	Agents              []api.Agent
	Providers           []api.Provider
	Sessions            []api.Session
	Todos               []api.Todo
	ChangedFiles        []api.FileChangeSummary
	IsGenerating        bool
	LastTurnMetrics     *api.SessionMetrics
}

// ContentProps configures the presentational sidebar content.
type ContentProps struct {
	CurrentTab       Tab
	Data             Data
	ExpandedPaths    map[string]bool
	OnSelectTab      func(Tab)
	OnTogglePath     func(string)
	OnSelectSession  func(string)
	OnCreateSession  func()
	OnRenameSession  func(id, title string)
	OnArchiveSession func(id string)
	OnDeleteSession  func(id string)
	OnCreateAgent    func()
	OnSelectFile     func(string)
}
