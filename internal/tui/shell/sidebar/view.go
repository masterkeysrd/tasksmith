package sidebar

import (
	"context"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
)

// Props defines the query-backed shell sidebar component.
type Props struct{}

// View renders the left shell sidebar using workspace and session data.
var View = kitex.FC("ShellSidebar", func(props Props) kitex.Node {
	client := tuiapi.UseClient()
	windClient := wind.UseClient()
	activeSessionID := active.UseSessionID()

	wsCfg := queries.UseGetWorkspaceConfig()
	projects := queries.UseListProjects()
	agents := queries.UseListAgents()
	providers := queries.UseListProviders()
	sessions := queries.UseListSessions()
	sessionState := queries.UseGetSessionState(activeSessionID)

	currentTab, setCurrentTab := kitex.UseState(TabExplorer)
	expandedPaths, setExpandedPaths := kitex.UseState(map[string]bool{})
	switchingTo, setSwitchingTo := kitex.UseState("")

	// Clear the switching indicator once the session state query settles.
	kitex.UseEffect(func() {
		if !sessionState.IsLoading && !sessionState.IsFetching {
			setSwitchingTo("")
		}
	}, []any{sessionState.IsLoading, sessionState.IsFetching})

	data := Data{
		WorkspaceName:       "Workspace",
		DefaultProvider:     "—",
		ActiveSessionID:     activeSessionID,
		ActiveSessionStatus: "idle",
		SwitchingToID:       switchingTo(),
		AuthorizedTools:     map[string]bool{},
	}

	if wsCfg.Data != nil {
		if wsCfg.Data.Name != "" {
			data.WorkspaceName = wsCfg.Data.Name
		}
		if wsCfg.Data.CWD != "" {
			data.WorkspacePath = wsCfg.Data.CWD
		}
		if wsCfg.Data.DefaultProvider != "" {
			data.DefaultProvider = wsCfg.Data.DefaultProvider
		}
		data.AuthorizedTools = wsCfg.Data.AuthorizedTools
		data.IsConfigured = wsCfg.Data.IsConfigured
	}

	if sessionState.Data != nil && sessionState.Data.Status != "" {
		data.ActiveSessionStatus = sessionState.Data.Status
	}
	if projects.Data != nil {
		data.Projects = projects.Data.Projects
	}
	if agents.Data != nil {
		data.Agents = agents.Data.Agents
	}
	if providers.Data != nil {
		data.Providers = providers.Data.Providers
	}
	if sessions.Data != nil {
		data.Sessions = sessions.Data.Sessions
	}

	if data.WorkspacePath == "" && len(data.Projects) > 0 {
		data.WorkspacePath = data.Projects[0].Path
	}

	return Content(ContentProps{
		CurrentTab:    currentTab(),
		Data:          data,
		ExpandedPaths: expandedPaths(),
		OnSelectTab: func(tab Tab) {
			setCurrentTab(tab)
		},
		OnTogglePath: func(path string) {
			current := expandedPaths()
			next := make(map[string]bool, len(current)+1)
			for k, v := range current {
				next[k] = v
			}
			next[path] = next[path] == false
			setExpandedPaths(next)
		},
		OnSelectSession: func(id string) {
			setSwitchingTo(id)
			active.SetSessionID(id)
		},
		OnCreateSession: func() {
			promise.New(func(ctx context.Context) (string, error) {
				resp, err := client.CreateSession(ctx, api.CreateSessionRequest{Title: "New Chat"})
				if err != nil {
					return "", err
				}
				return resp.Session.ID, nil
			}).Then(func(id string) {
				windClient.InvalidateQueries(api.ListSessionsRequest{})
				active.SetSessionID(id)
			}, func(err error) {})
		},
		OnRenameSession: func(id, title string) {
			promise.New(func(ctx context.Context) (bool, error) {
				_, err := client.RenameSession(ctx, api.RenameSessionRequest{ID: id, Title: title})
				return err == nil, err
			}).Then(func(_ bool) {
				windClient.InvalidateQueries(api.ListSessionsRequest{})
			}, func(err error) {})
		},
		OnArchiveSession: func(id string) {
			promise.New(func(ctx context.Context) (bool, error) {
				_, err := client.ArchiveSession(ctx, api.ArchiveSessionRequest{ID: id})
				return err == nil, err
			}).Then(func(_ bool) {
				windClient.InvalidateQueries(api.ListSessionsRequest{})
				if id == activeSessionID {
					active.SetSessionID("")
				}
			}, func(err error) {})
		},
		OnDeleteSession: func(id string) {
			promise.New(func(ctx context.Context) (bool, error) {
				_, err := client.DeleteSession(ctx, api.DeleteSessionRequest{ID: id})
				return err == nil, err
			}).Then(func(_ bool) {
				windClient.InvalidateQueries(api.ListSessionsRequest{})
				if id == activeSessionID {
					active.SetSessionID("")
				}
			}, func(err error) {})
		},
	})
})
