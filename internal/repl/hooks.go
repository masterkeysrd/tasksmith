package repl

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

func nextPermissionMode(current api.PermissionMode) api.PermissionMode {
	switch current {
	case api.ModeDefault:
		return api.ModeAcceptEdits
	case api.ModeAcceptEdits:
		return api.ModePlan
	case api.ModePlan:
		return api.ModeAsk
	case api.ModeAsk:
		return api.ModeAuto
	default:
		return api.ModeDefault
	}
}

// useLifecycleContext returns a Context that cancels when the calling component unmounts.
func useLifecycleContext() context.Context {
	ctxRef := kitex.UseRef[context.Context](nil)
	cancelRef := kitex.UseRef[context.CancelFunc](nil)

	if ctxRef.Current == nil {
		ctx, cancel := context.WithCancel(context.Background())
		ctxRef.Current = ctx
		cancelRef.Current = cancel
	}

	kitex.UseEffectCleanup(func() func() {
		return func() {
			if cancelRef.Current != nil {
				cancelRef.Current()
			}
		}
	}, []any{})

	return ctxRef.Current
}

// StreamState encapsulates in-flight assistant streaming text, reasoning, progress, and pending tools.
type StreamState struct {
	IsStreaming             bool
	StreamText              string
	StreamReasoning         string
	StreamReasoningDone     bool
	StreamReasoningDuration time.Duration
	PendingTools            []api.ToolCall
	CompletedTools          []api.ToolCall
	SpinnerText             string
	Start                   func(spinnerText string)
	Reset                   func()
}

// useSSEStream subscribes to the daemon's real-time SSE stream and manages live text chunk state.
func useSSEStream(
	ctx context.Context,
	c *client.Client,
	getSession func() *api.Session,
	onItem func(trail.Node),
	onPermission func([]api.PermissionRequest),
	onError func(string),
	onCompleted func(),
) StreamState {
	isStreaming, setIsStreaming := kitex.UseState(false)
	streamText, setStreamText := kitex.UseState("")
	streamReasoning, setStreamReasoning := kitex.UseState("")
	streamReasoningDone, setStreamReasoningDone := kitex.UseState(false)
	streamReasoningDuration, setStreamReasoningDuration := kitex.UseState(time.Duration(0))
	pendingTools, setPendingTools := kitex.UseState([]api.ToolCall(nil))
	completedTools, setCompletedTools := kitex.UseState([]api.ToolCall(nil))
	spinnerText, setSpinnerText := kitex.UseState("Thinking...")
	reasoningStartRef := kitex.UseRef(time.Time{})

	flushCompletedTools := func() {
		curr := completedTools()
		if len(curr) > 0 {
			groups := ui.GroupToolCalls(curr)
			for _, grp := range groups {
				if onItem != nil {
					onItem(ui.ToolGroup(ui.ToolGroupProps{
						Calls:   grp,
						Running: false,
					}))
				}
			}
			setCompletedTools(nil)
		}
	}

	kitex.UseEffectCleanup(func() func() {
		if c == nil {
			return func() {}
		}
		subCtx, subCancel := context.WithCancel(ctx)
		go func() {
			for ev, err := range c.Subscribe(subCtx, "") {
				if err != nil {
					return
				}
				curEv := ev
				kitex.PostMacro(func() {
					sess := getSession()
					if curEv.SessionID != "" && sess != nil && curEv.SessionID != sess.ID {
						return
					}

					switch curEv.Type {
					case api.EventMessageAdded:
						if curEv.Message != nil {
							if curEv.Message.Role == api.RoleUser {
								// User prompt was already appended optimistically in submit
								return
							}

							switch curEv.Message.Role {
							case api.RoleAssistant:
								var reasoningDuration time.Duration
								if !reasoningStartRef.Current.IsZero() {
									if streamReasoningDone() && streamReasoningDuration() > 0 {
										reasoningDuration = streamReasoningDuration()
									} else {
										reasoningDuration = time.Since(reasoningStartRef.Current)
									}
									reasoningStartRef.Current = time.Time{}
								}

								var toolCalls []api.ToolCall
								var nonToolBlocks []api.Block
								for _, b := range curEv.Message.Content {
									if b.Type == api.BlockToolCall && b.ToolCall != nil {
										toolCalls = append(toolCalls, *b.ToolCall)
									} else {
										nonToolBlocks = append(nonToolBlocks, b)
									}
								}

								if len(nonToolBlocks) > 0 {
									flushCompletedTools()
									if onItem != nil {
										msg := *curEv.Message
										msg.Content = nonToolBlocks
										onItem(ui.Message(ui.MessageProps{
											Message:           msg,
											ReasoningDuration: reasoningDuration,
										}))
									}
								}

								if len(toolCalls) > 0 {
									setPendingTools(append(pendingTools(), toolCalls...))
									setSpinnerText(fmt.Sprintf("Running %s...", toolCalls[len(toolCalls)-1].Name))
								}

								setStreamText("")
								setStreamReasoning("")
								setStreamReasoningDone(false)
								setStreamReasoningDuration(0)

							case api.RoleTool:
								toolCallID := curEv.Message.ToolCallID
								curPending := pendingTools()
								matchedIdx := -1
								var matchedCall api.ToolCall
								for i, pt := range curPending {
									if pt.ID == toolCallID {
										matchedIdx = i
										matchedCall = pt
										break
									}
								}

								errText := ""
								if curEv.Message.IsError {
									errText = ui.ExtractTextBlocks(curEv.Message.Content)
									if errText == "" {
										errText = "tool execution failed"
									}
								}

								if matchedIdx >= 0 {
									newPending := make([]api.ToolCall, 0, len(curPending)-1)
									newPending = append(newPending, curPending[:matchedIdx]...)
									newPending = append(newPending, curPending[matchedIdx+1:]...)
									setPendingTools(newPending)
									if len(newPending) == 0 {
										setSpinnerText("Thinking...")
									}

									if curEv.Message.IsError {
										flushCompletedTools()
										if onItem != nil {
											onItem(ui.ToolCall(ui.ToolCallProps{
												ToolCall:  matchedCall,
												Running:   false,
												ErrorText: errText,
											}))
										}
									} else {
										setCompletedTools(append(completedTools(), matchedCall))
									}
								} else if curEv.Message.IsError {
									flushCompletedTools()
									if onItem != nil {
										onItem(ui.ToolError(ui.ToolErrorProps{
											ErrorText: errText,
										}))
									}
								}
								// If not in pending and not an error, do NOT dump raw tool output

							default:
								flushCompletedTools()
								if onItem != nil {
									onItem(ui.Message(ui.MessageProps{Message: *curEv.Message}))
								}
							}
						}

					case api.EventChunk:
						if curEv.Chunk != nil {
							for _, b := range curEv.Chunk.Content {
								switch b.Type {
								case api.BlockReasoning:
									if b.Text != "" {
										if len(completedTools()) > 0 && len(pendingTools()) == 0 {
											flushCompletedTools()
										}
										if reasoningStartRef.Current.IsZero() {
											reasoningStartRef.Current = time.Now()
											setStreamReasoningDone(false)
											setStreamReasoningDuration(0)
										}
										setStreamReasoning(streamReasoning() + b.Text)
										setSpinnerText("Thinking...")
									}
								case api.BlockText:
									if b.Text != "" {
										if len(completedTools()) > 0 && len(pendingTools()) == 0 {
											flushCompletedTools()
										}
										if !reasoningStartRef.Current.IsZero() && !streamReasoningDone() {
											setStreamReasoningDuration(time.Since(reasoningStartRef.Current))
											setStreamReasoningDone(true)
										}
										setStreamText(streamText() + b.Text)
									}
								case api.BlockToolCall:
									if !reasoningStartRef.Current.IsZero() && !streamReasoningDone() {
										setStreamReasoningDuration(time.Since(reasoningStartRef.Current))
										setStreamReasoningDone(true)
									}
									// Do not set SpinnerText here to "Running <tool>..."!
								}
							}
						}

					case api.EventPermissionRequested:
						if len(curEv.Permissions) > 0 && onPermission != nil {
							onPermission(curEv.Permissions)
						}

					case api.EventCompleted:
						flushCompletedTools()
						setIsStreaming(false)
						setStreamText("")
						setStreamReasoning("")
						setStreamReasoningDone(false)
						setStreamReasoningDuration(0)
						setPendingTools(nil)
						setCompletedTools(nil)
						reasoningStartRef.Current = time.Time{}
						setSpinnerText("Thinking...")
						if onCompleted != nil {
							onCompleted()
						}

					case api.EventError:
						flushCompletedTools()
						setIsStreaming(false)
						setPendingTools(nil)
						setCompletedTools(nil)
						setStreamReasoningDone(false)
						setStreamReasoningDuration(0)
						reasoningStartRef.Current = time.Time{}
						if curEv.Error != "" && onError != nil {
							onError(curEv.Error)
						}
					}
				})
			}
		}()
		return func() {
			subCancel()
		}
	}, []any{c})

	start := func(spinnerTextVal string) {
		setIsStreaming(true)
		setStreamText("")
		setStreamReasoning("")
		setStreamReasoningDone(false)
		setStreamReasoningDuration(0)
		setPendingTools(nil)
		setCompletedTools(nil)
		reasoningStartRef.Current = time.Time{}
		if spinnerTextVal != "" {
			setSpinnerText(spinnerTextVal)
		} else {
			setSpinnerText("Thinking...")
		}
	}

	reset := func() {
		flushCompletedTools()
		setIsStreaming(false)
		setStreamText("")
		setStreamReasoning("")
		setStreamReasoningDone(false)
		setStreamReasoningDuration(0)
		setPendingTools(nil)
		setCompletedTools(nil)
		reasoningStartRef.Current = time.Time{}
		setSpinnerText("Thinking...")
	}

	return StreamState{
		IsStreaming:             isStreaming(),
		StreamText:              streamText(),
		StreamReasoning:         streamReasoning(),
		StreamReasoningDone:     streamReasoningDone(),
		StreamReasoningDuration: streamReasoningDuration(),
		PendingTools:            pendingTools(),
		CompletedTools:          completedTools(),
		SpinnerText:             spinnerText(),
		Start:                   start,
		Reset:                   reset,
	}
}

// PermissionQueue manages pending permission approval requests and decision dispatch.
type PermissionQueue struct {
	Count  func() int
	Active func() (api.PermissionRequest, bool)
	Queue  func(perms []api.PermissionRequest)
	Decide func(req api.PermissionRequest, dec api.PermissionDecision)
}

// usePermissionQueue provides queueing and dispatch for interactive tool approvals.
func usePermissionQueue(
	ctx context.Context,
	c *client.Client,
	getSession func() *api.Session,
	onDecisionLogged func(req api.PermissionRequest, dec api.PermissionDecision),
	onQueueEmpty func(),
) PermissionQueue {
	pendingPerms, setPendingPerms := kitex.UseState([]api.PermissionRequest(nil))

	queue := func(perms []api.PermissionRequest) {
		setPendingPerms(append(pendingPerms(), perms...))
	}

	active := func() (api.PermissionRequest, bool) {
		p := pendingPerms()
		if len(p) == 0 {
			return api.PermissionRequest{}, false
		}
		return p[0], true
	}

	count := func() int {
		return len(pendingPerms())
	}

	decide := func(req api.PermissionRequest, dec api.PermissionDecision) {
		if onDecisionLogged != nil {
			onDecisionLogged(req, dec)
		}
		sess := getSession()
		if sess != nil && c != nil {
			go c.RespondPermission(ctx, sess.ID, dec)
		}
		curPerms := pendingPerms()
		if len(curPerms) > 0 {
			rem := curPerms[1:]
			setPendingPerms(rem)
			if len(rem) == 0 && onQueueEmpty != nil {
				onQueueEmpty()
			}
		} else if onQueueEmpty != nil {
			onQueueEmpty()
		}
	}

	return PermissionQueue{
		Count:  count,
		Active: active,
		Queue:  queue,
		Decide: decide,
	}
}

// REPLController aggregates all REPL state, command execution, and session management.
type REPLController struct {
	Items        func() []trail.Node
	Mode         func() api.PermissionMode
	ModelLabel   func() string
	Thinking     func() string
	StatusHint   func() string
	ActivePicker func() ui.PickerProps
	IsPickerOpen func() bool
	State        func() ui.State
	Stream       StreamState
	Perms        PermissionQueue
	Submit       func(text string)
	CycleMode    func()
	Cancel       func()
}

// useREPLController coordinates reactive hooks, slash command execution, and session switching.
func useREPLController(props AppProps, app trail.AppHandle) REPLController {
	ctx := useLifecycleContext()

	items, setItems := kitex.UseState([]trail.Node(nil))
	session, setSession := kitex.UseState(props.InitialSess)
	sessionVersion := kitex.UseRef(0)
	selectSession := func(sess *api.Session) {
		sessionVersion.Current++
		setSession(sess)
	}
	mode, setMode := kitex.UseState(cmp.Or(props.InitialMode, api.ModeDefault))
	modelLabel, setModelLabel := kitex.UseState(props.InitialModelLabel)
	thinking, setThinking := kitex.UseState(props.InitialThinking)
	statusHint, setStatusHint := kitex.UseState("")
	pickerProps, setPickerProps := kitex.UseState(ui.PickerProps{})
	isPickerOpen, setIsPickerOpen := kitex.UseState(false)
	isSessionPickerOpen, setIsSessionPickerOpen := kitex.UseState(false)
	sessionMutationPending := kitex.UseRef(false)

	sessions := uihooks.UseSessions(wind.Options{Enabled: isSessionPickerOpen()})

	appendItem := func(n trail.Node) {
		setItems(append(items(), n))
	}

	renameSession := uihooks.UseRenameSession(wind.MutationOptions[uihooks.RenameSessionInput, *api.Session]{
		OnSuccess: func(updated *api.Session, input uihooks.RenameSessionInput, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			if active := session(); active != nil && active.ID == input.SessionID {
				selectSession(updated)
			}
		},
		OnError: func(err error, _ uihooks.RenameSessionInput, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
		},
	})
	type modelAndThinking struct {
		model    string
		thinking string
	}
	resolveDefaults := promise.Wrap(func(ctx context.Context) (modelAndThinking, error) {
		model, thinking := resolveModelAndThinking(ctx, props.Client, props.Agent, props.Model, nil)
		return modelAndThinking{model: model, thinking: thinking}, nil
	})
	deleteSession := uihooks.UseDeleteSession(wind.MutationOptions[string, struct{}]{
		OnSuccess: func(_ struct{}, sessionID string, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			if active := session(); active == nil || active.ID != sessionID {
				return
			}
			selectSession(nil)
			version := sessionVersion.Current
			resolveDefaults(ctx).Then(func(defaults modelAndThinking) {
				if ctx.Err() != nil {
					return
				}
				if sessionVersion.Current == version {
					setModelLabel(defaults.model)
					setThinking(defaults.thinking)
				}
			}, func(err error) {
				if ctx.Err() == nil {
					appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
				}
			})
		},
		OnError: func(err error, _ string, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
		},
	})

	kitex.UseEffect(func() {
		if !isSessionPickerOpen() || sessions.IsLoading || sessions.IsFetching {
			return
		}
		if sessions.IsError {
			appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: sessions.Error.Error()}))
		} else if len(sessions.Data) == 0 {
			appendItem(ui.CommandOutput(ui.CommandOutputProps{
				Command: "/sessions",
				Output:  "No sessions found.",
			}))
		} else {
			return
		}
		setIsSessionPickerOpen(false)
		setIsPickerOpen(false)
	}, []any{isSessionPickerOpen(), sessions.IsLoading, sessions.IsFetching, sessions.IsError, sessions.Error, sessions.Data})

	var stream StreamState
	var perms PermissionQueue

	stream = useSSEStream(
		ctx,
		props.Client,
		session,
		appendItem,
		func(newPerms []api.PermissionRequest) {
			perms.Queue(newPerms)
		},
		func(errText string) {
			appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: errText}))
		},
		func() {},
	)

	perms = usePermissionQueue(
		ctx,
		props.Client,
		session,
		func(req api.PermissionRequest, dec api.PermissionDecision) {
			appendItem(ui.DecisionSummary(ui.DecisionSummaryProps{
				Request:  req,
				Decision: dec,
			}))
		},
		func() {},
	)

	// Initial startup setup: render banner header, load history and pending permissions
	kitex.UseLayoutEffect(func() {
		curr := items()
		if len(curr) == 0 {
			var initialItems []trail.Node
			initialItems = append(initialItems, ui.Header(ui.HeaderProps{
				Version:  props.Version,
				Model:    modelLabel(),
				Thinking: thinking(),
				Path:     props.Workspace,
			}))

			sess := session()
			if sess != nil && props.Client != nil {
				hist, err := props.Client.History(ctx, sess.ID)
				if err == nil {
					initialItems = append(initialItems, ui.RenderHistoryMessages(hist)...)
				}
				permsList, pErr := props.Client.PendingPermissions(ctx, sess.ID)
				if pErr == nil && len(permsList) > 0 {
					perms.Queue(permsList)
				}
			}
			setItems(initialItems)
		}
	}, []any{})

	bindPickerCallbacks := func(p ui.PickerProps) ui.PickerProps {
		p.OnOpen = func() {
			setStatusHint("")
		}
		origSelect := p.OnSelect
		p.OnSelect = func(opt components.Option) {
			setIsPickerOpen(false)
			setIsSessionPickerOpen(false)
			if origSelect != nil {
				origSelect(opt)
			}
		}
		origCancel := p.OnCancel
		p.OnCancel = func() {
			setIsPickerOpen(false)
			setIsSessionPickerOpen(false)
			if origCancel != nil {
				origCancel()
			}
		}
		return p
	}

	var makeEnv func() command.Env

	beginSessionMutation := func() bool {
		if sessionMutationPending.Current {
			setStatusHint("A session update is already in progress.")
			return false
		}
		sessionMutationPending.Current = true
		setStatusHint("")
		return true
	}

	makeEnv = func() command.Env {
		return command.Env{
			Mode:         mode(),
			Session:      session(),
			DefaultAgent: props.Agent,
			DefaultModel: props.Model,
			AppendNode:   appendItem,
			OpenPicker: func(p ui.PickerProps) {
				setIsSessionPickerOpen(false)
				setPickerProps(bindPickerCallbacks(p))
				setIsPickerOpen(true)
			},
			SetMode: func(m api.PermissionMode) {
				setMode(m)
				sess := session()
				if sess != nil && props.Client != nil {
					go props.Client.SetPermissionMode(ctx, sess.ID, m)
				}
			},
			NewSession: func(model string) {
				targetModel := cmp.Or(strings.TrimSpace(model), props.Model)
				mdlLabel, think := resolveModelAndThinking(ctx, props.Client, props.Agent, targetModel, nil)
				selectSession(nil)
				setModelLabel(mdlLabel)
				setThinking(think)
				setMode(api.ModeDefault)
				appendItem(ui.Header(ui.HeaderProps{
					Version:  props.Version,
					Model:    mdlLabel,
					Thinking: think,
					Path:     props.Workspace,
				}))
			},
			SwitchSession: func(sessionID string) {
				if props.Client != nil {
					go func() {
						switchedSess, err := props.Client.GetSession(ctx, sessionID)
						if err != nil {
							kitex.PostMacro(func() {
								appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
							})
							return
						}
						hist, _ := props.Client.History(ctx, switchedSess.ID)
						m, _ := props.Client.PermissionMode(ctx, switchedSess.ID)
						mdlLabel, think := resolveModelAndThinking(ctx, props.Client, props.Agent, props.Model, switchedSess)
						kitex.PostMacro(func() {
							selectSession(switchedSess)
							setModelLabel(mdlLabel)
							setThinking(think)
							if m != "" {
								setMode(m)
							}
							newItems := append(items(), ui.Header(ui.HeaderProps{
								Version:  props.Version,
								Model:    mdlLabel,
								Thinking: think,
								Path:     props.Workspace,
							}))
							newItems = append(newItems, ui.RenderHistoryMessages(hist)...)
							setItems(newItems)
						})
					}()
				}
			},
			RenameSession: func(sessionID, title string) {
				if beginSessionMutation() {
					renameSession.Mutate(uihooks.RenameSessionInput{SessionID: sessionID, Title: title})
				}
			},
			DeleteSession: func(sessionID string) {
				if beginSessionMutation() {
					deleteSession.Mutate(sessionID)
				}
			},
			BrowseSessions: func() {
				sessions.Refetch()
				setIsSessionPickerOpen(true)
				setIsPickerOpen(true)
			},
			ListChanges: func() {
				sess := session()
				if sess == nil {
					appendItem(ui.Notice(ui.NoticeProps{Text: "No active session."}))
					return
				}
				if props.Client != nil {
					go func() {
						changes, err := props.Client.Changes(ctx, sess.ID)
						kitex.PostMacro(func() {
							if err != nil {
								appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
								return
							}
							appendItem(ui.ChangesList(ui.ChangesListProps{Changes: changes}))
						})
					}()
				}
			},
			DiffChange: func(path, entryID string) {
				sess := session()
				if sess == nil {
					appendItem(ui.Notice(ui.NoticeProps{Text: "No active session."}))
					return
				}
				if props.Client != nil {
					go func() {
						diff, err := props.Client.DiffChange(ctx, sess.ID, path, entryID)
						kitex.PostMacro(func() {
							if err != nil {
								appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
								return
							}
							appendItem(ui.Diff(ui.DiffProps{Text: diff}))
						})
					}()
				}
			},
			RevertChanges: func(path, entryID string) {
				sess := session()
				if sess == nil {
					appendItem(ui.CommandOutput(ui.CommandOutputProps{
						Command: "/revert",
						Output:  "No active session.",
					}))
					return
				}
				if props.Client != nil {
					go func() {
						err := props.Client.Revert(ctx, sess.ID, api.RevertRequest{
							Path:    path,
							EntryID: entryID,
						})
						kitex.PostMacro(func() {
							if err != nil {
								appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
								return
							}
							cmdStr := "/revert"
							if path != "" {
								cmdStr += " " + path
								if entryID != "" {
									cmdStr += " " + entryID
								}
							}
							appendItem(ui.CommandOutput(ui.CommandOutputProps{
								Command: cmdStr,
								Output:  "Reverted changes.",
							}))
						})
					}()
				}
			},
			ClearScreen: func() {
				appendItem(ui.Header(ui.HeaderProps{
					Version:  props.Version,
					Model:    modelLabel(),
					Thinking: thinking(),
					Path:     props.Workspace,
				}))
			},
			Exit: func() {
				if app != nil {
					app.Exit(nil)
				}
			},
		}
	}

	activePicker := func() ui.PickerProps {
		if !isSessionPickerOpen() {
			return pickerProps()
		}
		var activeID string
		if active := session(); active != nil {
			activeID = active.ID
		}
		p := bindPickerCallbacks(command.NewSessionPicker(makeEnv(), sessions.Data, activeID))
		if sessions.IsLoading || sessions.IsFetching {
			p.Footer = "Loading sessions..."
		} else if renameSession.IsPending {
			p.Footer = "Renaming session..."
		} else if deleteSession.IsPending {
			p.Footer = "Deleting session..."
		}
		return p
	}

	submit := func(text string) {
		if strings.HasPrefix(text, "/") {
			reg := props.Registry
			if reg == nil {
				reg = command.NewRegistry()
			}
			err := reg.Execute(makeEnv(), text)
			if err != nil {
				appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
			}
			return
		}

		// User prompt
		appendItem(ui.UserPrompt(ui.UserPromptProps{Text: text}))
		stream.Start("Thinking...")

		if props.Client == nil {
			return
		}

		sess := session()
		if sess == nil {
			go func() {
				newSess, err := props.Client.CreateSession(ctx, api.CreateSessionRequest{
					Title: text,
					Agent: props.Agent,
					Model: props.Model,
				})
				if err != nil {
					kitex.PostMacro(func() {
						appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: err.Error()}))
						stream.Reset()
					})
					return
				}
				kitex.PostMacro(func() {
					selectSession(newSess)
				})
				sendErr := props.Client.Send(ctx, newSess.ID, api.SendRequest{Content: text})
				if sendErr != nil {
					kitex.PostMacro(func() {
						appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: sendErr.Error()}))
						stream.Reset()
					})
				}
			}()
		} else {
			go func() {
				sendErr := props.Client.Send(ctx, sess.ID, api.SendRequest{Content: text})
				if sendErr != nil {
					kitex.PostMacro(func() {
						appendItem(ui.ErrorMessage(ui.ErrorMessageProps{Text: sendErr.Error()}))
						stream.Reset()
					})
				}
			}()
		}
	}

	cycleMode := func() {
		next := nextPermissionMode(mode())
		setMode(next)
		sess := session()
		if sess != nil && props.Client != nil {
			go props.Client.SetPermissionMode(ctx, sess.ID, next)
		}
	}

	cancel := func() {
		if stream.IsStreaming {
			sess := session()
			if sess != nil && props.Client != nil {
				go props.Client.Cancel(ctx, sess.ID)
			}
			stream.Reset()
		}
	}

	computedState := func() ui.State {
		if isPickerOpen() {
			return ui.StatePicker
		}
		if perms.Count() > 0 {
			return ui.StateApproval
		}
		if stream.IsStreaming {
			return ui.StateStreaming
		}
		return ui.StateIdle
	}

	return REPLController{
		Items:        items,
		Mode:         mode,
		ModelLabel:   modelLabel,
		Thinking:     thinking,
		StatusHint:   statusHint,
		ActivePicker: activePicker,
		IsPickerOpen: isPickerOpen,
		State:        computedState,
		Stream:       stream,
		Perms:        perms,
		Submit:       submit,
		CycleMode:    cycleMode,
		Cancel:       cancel,
	}
}
