package sidebar

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
)

// Props defines the query-backed shell sidebar component.
type Props struct{}

// View renders the left shell sidebar using workspace and session data.
var View = kitex.FC("ShellSidebar", func(props Props) kitex.Node {
	client := tuiapi.UseClient()
	windClient := wind.UseClient()
	activeSessionID := active.UseSessionID()
	c := useColors()

	wsCfg := queries.UseGetWorkspaceConfig()
	projects := queries.UseListProjects()
	agents := queries.UseListAgents()
	providers := queries.UseListProviders()
	sessions := queries.UseListSessions()
	sessionState := queries.UseGetSessionState(activeSessionID)
	fileChanges := queries.UseGetFileChanges(activeSessionID)

	currentTab, setCurrentTab := kitex.UseState(TabExplorer)
	expandedPaths, setExpandedPaths := kitex.UseState(map[string]bool{})
	switchingTo, setSwitchingTo := kitex.UseState("")
	selectedFile, setSelectedFile := kitex.UseState("")
	diffContent, setDiffContent := kitex.UseState("")
	loadingDiff, setLoadingDiff := kitex.UseState(false)
	showForceConfirm, setShowForceConfirm := kitex.UseState(false)
	split, setSplit := kitex.UseState(false)

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

	if sessions.Data != nil {
		data.Sessions = sessions.Data.Sessions
		for _, s := range sessions.Data.Sessions {
			if s.ID == activeSessionID {
				data.LastTurnMetrics = s.LastTurnMetrics
				break
			}
		}
	}

	if sessionState.Data != nil {
		if sessionState.Data.Status != "" {
			data.ActiveSessionStatus = sessionState.Data.Status
		}
		data.Todos = sessionState.Data.Todos
		data.IsGenerating = sessionState.Data.IsGenerating
		if sessionState.Data.LastTurnMetrics != nil {
			data.LastTurnMetrics = sessionState.Data.LastTurnMetrics
		}
	}

	if fileChanges.Data != nil {
		data.ChangedFiles = fileChanges.Data.Changes
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

	if data.WorkspacePath == "" && len(data.Projects) > 0 {
		data.WorkspacePath = data.Projects[0].Path
	}

	return kitex.Fragment(
		Content(ContentProps{
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
			OnSelectFile: func(path string) {
				setSelectedFile(path)
				setLoadingDiff(true)
				setDiffContent("")
				promise.New(func(ctx context.Context) (string, error) {
					resp, err := client.GetFileJournal(ctx, api.GetFileJournalRequest{
						SessionID: activeSessionID,
						Path:      path,
					})
					if err != nil {
						return "", err
					}
					var sb strings.Builder
					for _, entry := range resp.Entries {
						if entry.Kind == "baseline" {
							continue
						}
						if entry.Diff != "" {
							fmt.Fprintf(&sb, "--- [%s] by tool '%s' ---\n%s\n", entry.Timestamp.Format("15:04:05"), entry.ToolName, entry.Diff)
						} else if entry.Kind == "created" {
							fmt.Fprintf(&sb, "--- [%s] created ---\n(New file created in session)\n", entry.Timestamp.Format("15:04:05"))
						} else if entry.Kind == "deleted" {
							fmt.Fprintf(&sb, "--- [%s] deleted ---\n(File deleted in session)\n", entry.Timestamp.Format("15:04:05"))
						}
					}
					return sb.String(), nil
				}).Then(func(diff string) {
					setDiffContent(diff)
					setLoadingDiff(false)
				}, func(err error) {
					setDiffContent(fmt.Sprintf("Failed to load diff: %v", err))
					setLoadingDiff(false)
				})
			},
		}),
		components.Modal(components.ModalProps{
			IsOpen: selectedFile() != "",
			Title:  kitex.Text(fmt.Sprintf("Diff - %s", selectedFile())),
			OnClose: func() {
				setSelectedFile("")
				setDiffContent("")
				setLoadingDiff(false)
				setShowForceConfirm(false)
			},
			HeaderActions: kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonBase,
					OnClick: func() {
						setSplit(!split())
					},
				}, kitex.IfElse(split(), kitex.Text("Show Unified"), kitex.Text("Show Split"))),
				kitex.If(!showForceConfirm(), func() kitex.Node {
					return components.Button(components.ButtonProps{
						Variant: components.ButtonSolid,
						Color:   components.ButtonError,
						OnClick: func() {
							promise.New(func(ctx context.Context) (string, error) {
								resp, err := client.RevertFile(ctx, api.RevertFileRequest{
									SessionID: activeSessionID,
									Path:      selectedFile(),
									Force:     false,
								})
								if err != nil {
									return "", err
								}
								if !resp.Success {
									return "", fmt.Errorf("%s", resp.Error)
								}
								return "success", nil
							}).Then(func(status string) {
								setSelectedFile("")
								setDiffContent("")
								setLoadingDiff(false)
								setShowForceConfirm(false)
								windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: activeSessionID})
							}, func(err error) {
								if err.Error() == "conflict" {
									setShowForceConfirm(true)
								} else {
									setDiffContent("Error: " + err.Error())
								}
							})
						},
					}, kitex.Text("Revert Changes"))
				}),
			),
		},
			kitex.If(selectedFile() != "", func() kitex.Node {
				if showForceConfirm() {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1).Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(c.warning).Bold(true),
						}, kitex.Text("WARNING: MANUAL CHANGES DETECTED")),
						kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(c.text),
						}, kitex.Text("This file has been modified manually since the agent last edited it. Reverting will permanently discard your manual changes.")),
						kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).MarginTop(1),
						},
							components.Button(components.ButtonProps{
								Variant: components.ButtonSolid,
								Color:   components.ButtonError,
								OnClick: func() {
									promise.New(func(ctx context.Context) (bool, error) {
										resp, err := client.RevertFile(ctx, api.RevertFileRequest{
											SessionID: activeSessionID,
											Path:      selectedFile(),
											Force:     true,
										})
										return resp.Success, err
									}).Then(func(success bool) {
										setSelectedFile("")
										setDiffContent("")
										setLoadingDiff(false)
										setShowForceConfirm(false)
										windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: activeSessionID})
									}, func(err error) {})
								},
							}, kitex.Text("Discard & Revert (Force)")),
							components.Button(components.ButtonProps{
								Variant: components.ButtonText,
								Color:   components.ButtonBase,
								OnClick: func() {
									setShowForceConfirm(false)
								},
							}, kitex.Text("Cancel")),
						),
					)
				}
				if loadingDiff() {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1),
					}, kitex.Text("Loading diff..."))
				}
				if diffContent() == "" {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1),
					}, kitex.Text("No diff details available."))
				}
				ext := filepath.Ext(selectedFile())
				lang := strings.TrimPrefix(ext, ".")
				return components.DiffBlock(components.DiffBlockProps{
					Diff:  diffContent(),
					Lang:  lang,
					Split: split(),
				})
			}),
		),
	)
})
