package repl

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	replcomponents "github.com/masterkeysrd/tasksmith/internal/repl/components"
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

// PermissionQueue manages pending permission approval requests and decision dispatch.
type PermissionQueue struct {
	Count  func() int
	Active func() (api.PermissionRequest, bool)
	Decide func(req api.PermissionRequest, dec api.PermissionDecision)
}

// usePermissionQueue provides queueing and dispatch for interactive tool approvals.
func usePermissionQueue(
	c *client.Client,
	getSession func() *api.Session,
	requests []api.PermissionRequest,
	onDecisionLogged func(req api.PermissionRequest, dec api.PermissionDecision),
	onError func(error),
) PermissionQueue {
	var sessionID string
	if sess := getSession(); sess != nil {
		sessionID = sess.ID
	}
	type decisionKey struct {
		SessionID string
		RequestID string
	}
	type decisionInput struct {
		Key      decisionKey
		Request  api.PermissionRequest
		Decision api.PermissionDecision
	}
	decided, setDecided := kitex.UseState(map[decisionKey]bool(nil))
	mutation := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, input decisionInput) (struct{}, error) {
		return struct{}{}, c.RespondPermission(ctx, input.Key.SessionID, input.Decision)
	}), wind.MutationOptions[decisionInput, struct{}]{
		OnSuccess: func(_ struct{}, input decisionInput, _ wind.MutationContext) {
			if sess := getSession(); sess != nil && sess.ID == input.Key.SessionID {
				onDecisionLogged(input.Request, input.Decision)
			}
		},
		OnError: func(err error, input decisionInput, _ wind.MutationContext) {
			next := maps.Clone(decided())
			delete(next, input.Key)
			setDecided(next)
			if sess := getSession(); sess != nil && sess.ID == input.Key.SessionID {
				onError(err)
			}
		},
	})
	pending := func() []api.PermissionRequest {
		sess := getSession()
		if sess == nil || sess.ID != sessionID {
			return nil
		}
		var pending []api.PermissionRequest
		for _, request := range requests {
			if !decided()[decisionKey{SessionID: sess.ID, RequestID: request.ID}] {
				pending = append(pending, request)
			}
		}
		return pending
	}
	decide := func(req api.PermissionRequest, dec api.PermissionDecision) {
		sess := getSession()
		if sess == nil || sess.ID != sessionID {
			return
		}
		key := decisionKey{SessionID: sess.ID, RequestID: req.ID}
		if decided()[key] {
			return
		}
		next := maps.Clone(decided())
		if next == nil {
			next = make(map[decisionKey]bool)
		}
		next[key] = true
		setDecided(next)
		dec.RequestID, dec.ToolCallID = req.ID, req.ToolCallID
		mutation.Mutate(decisionInput{Key: key, Request: req, Decision: dec})
	}
	return PermissionQueue{
		Count: func() int { return len(pending()) },
		Active: func() (api.PermissionRequest, bool) {
			requests := pending()
			if len(requests) == 0 {
				return api.PermissionRequest{}, false
			}
			return requests[0], true
		},
		Decide: decide,
	}
}

// TranscriptView is the data rendered by the Transcript component. Key
// changes whenever the transcript restarts.
type TranscriptView struct {
	Key     string
	Entries []replcomponents.TranscriptEntry
}

// REPLController aggregates all REPL state, command execution, and session management.
type REPLController struct {
	Transcript   TranscriptView
	Mode         func() api.PermissionMode
	ModelLabel   func() string
	Thinking     func() string
	StatusHint   func() string
	ActivePicker func() ui.PickerProps
	IsPickerOpen func() bool
	State        func() ui.State
	Conversation uihooks.Conversation
	IsStreaming  bool
	Perms        PermissionQueue
	Submit       func(text string)
	CycleMode    func()
	Cancel       func()
}

// useREPLController coordinates reactive hooks, slash command execution, and session switching.
func useREPLController(props AppProps, app trail.AppHandle) REPLController {
	ctx := useLifecycleContext()

	// entries is the append-only transcript. lastMessageID is the high-water
	// mark of conversation messages already appended; message IDs are
	// time-sortable (UUIDv7) and arrive in order, so anything not greater than
	// it has been printed.
	entries, setEntries := kitex.UseState([]replcomponents.TranscriptEntry{
		replcomponents.HeaderEntry(ui.HeaderProps{
			Version:  props.Version,
			Model:    props.InitialModelLabel,
			Thinking: props.InitialThinking,
			Path:     props.Workspace,
		}),
	})
	lastMessageID := kitex.UseRef("")
	latestMessageID := kitex.UseRef("")
	epoch, setEpoch := kitex.UseState(0)
	session, setSession := kitex.UseState(props.InitialSess)
	sessionVersion := kitex.UseRef(0)
	type sendState struct {
		Version int
		Pending bool
	}
	sending, setSending := kitex.UseState(sendState{})
	// restartTranscript starts a new printed transcript with a fresh header.
	// skipThrough is the last message ID that is already on screen (used by
	// /clear); messages up to it are not printed again.
	restartTranscript := func(skipThrough, model, thinking string) {
		lastMessageID.Current = skipThrough
		setEntries([]replcomponents.TranscriptEntry{replcomponents.HeaderEntry(ui.HeaderProps{
			Version: props.Version, Model: model, Thinking: thinking, Path: props.Workspace,
		})})
		setEpoch(epoch() + 1)
	}
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

	var sessionID string
	if sess := session(); sess != nil {
		sessionID = sess.ID
	}
	conversation := uihooks.UseConversation(sessionID)
	if n := len(conversation.History); n > 0 {
		latestMessageID.Current = conversation.History[n-1].SourceID
	} else {
		latestMessageID.Current = ""
	}
	isStreaming := func() bool {
		return (sending().Pending && sending().Version == sessionVersion.Current) ||
			conversation.Stream.Status == uihooks.StreamRunning ||
			conversation.Stream.Status == uihooks.StreamWaitingPermission
	}
	sessions := uihooks.UseSessions(wind.Options{Enabled: isSessionPickerOpen()})

	appendItem := func(e replcomponents.TranscriptEntry) {
		setEntries(append(slices.Clone(entries()), e))
	}

	kitex.UseLayoutEffect(func() {
		var added []replcomponents.TranscriptEntry
		for _, message := range conversation.History {
			if message.SourceID > lastMessageID.Current {
				added = append(added, replcomponents.MessageEntry(message))
				lastMessageID.Current = message.SourceID
			}
		}
		if len(added) > 0 {
			setEntries(append(slices.Clone(entries()), added...))
		}
	}, []any{sessionID, epoch(), len(conversation.History)})

	renameSession := uihooks.UseRenameSession(wind.MutationOptions[uihooks.RenameSessionInput, *api.Session]{
		OnSuccess: func(updated *api.Session, input uihooks.RenameSessionInput, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			if active := session(); active != nil && active.ID == input.SessionID {
				setSession(updated)
			}
		},
		OnError: func(err error, _ uihooks.RenameSessionInput, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
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
					restartTranscript("", defaults.model, defaults.thinking)
				}
			}, func(err error) {
				if ctx.Err() == nil {
					appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
				}
			})
		},
		OnError: func(err error, _ string, _ wind.MutationContext) {
			sessionMutationPending.Current = false
			setStatusHint("")
			appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
		},
	})

	kitex.UseEffect(func() {
		if !isSessionPickerOpen() || sessions.IsLoading || sessions.IsFetching {
			return
		}
		if sessions.IsError {
			appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: sessions.Error.Error()}))
		} else if len(sessions.Data) == 0 {
			appendItem(replcomponents.CommandEntry(ui.CommandOutputProps{
				Command: "/sessions",
				Output:  "No sessions found.",
			}))
		} else {
			return
		}
		setIsSessionPickerOpen(false)
		setIsPickerOpen(false)
	}, []any{isSessionPickerOpen(), sessions.IsLoading, sessions.IsFetching, sessions.IsError, sessions.Error, sessions.Data})

	perms := usePermissionQueue(
		props.Client,
		session,
		conversation.Permissions,
		func(req api.PermissionRequest, dec api.PermissionDecision) {
			appendItem(replcomponents.DecisionEntry(ui.DecisionSummaryProps{
				Request:  req,
				Decision: dec,
			}))
		},
		func(err error) {
			appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
		},
	)

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
			AppendOutput: func(output ui.CommandOutputProps) {
				appendItem(replcomponents.CommandEntry(output))
			},
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
				restartTranscript("", mdlLabel, think)
			},
			SwitchSession: func(sessionID string) {
				if props.Client != nil {
					sessionVersion.Current++
					version := sessionVersion.Current
					go func() {
						switchedSess, err := props.Client.GetSession(ctx, sessionID)
						if err != nil {
							kitex.PostMacro(func() {
								if ctx.Err() == nil && sessionVersion.Current == version {
									appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
								}
							})
							return
						}
						m, _ := props.Client.PermissionMode(ctx, switchedSess.ID)
						mdlLabel, think := resolveModelAndThinking(ctx, props.Client, props.Agent, props.Model, switchedSess)
						kitex.PostMacro(func() {
							if ctx.Err() != nil || sessionVersion.Current != version {
								return
							}
							selectSession(switchedSess)
							setModelLabel(mdlLabel)
							setThinking(think)
							if m != "" {
								setMode(m)
							}
							restartTranscript("", mdlLabel, think)
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
					appendItem(replcomponents.NoticeEntry(ui.NoticeProps{Text: "No active session."}))
					return
				}
				if props.Client != nil {
					go func() {
						changes, err := props.Client.Changes(ctx, sess.ID)
						kitex.PostMacro(func() {
							if err != nil {
								appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
								return
							}
							appendItem(replcomponents.ChangesEntry(ui.ChangesListProps{Changes: changes}))
						})
					}()
				}
			},
			DiffChange: func(path, entryID string) {
				sess := session()
				if sess == nil {
					appendItem(replcomponents.NoticeEntry(ui.NoticeProps{Text: "No active session."}))
					return
				}
				if props.Client != nil {
					go func() {
						diff, err := props.Client.DiffChange(ctx, sess.ID, path, entryID)
						kitex.PostMacro(func() {
							if err != nil {
								appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
								return
							}
							appendItem(replcomponents.DiffEntry(ui.DiffProps{Text: diff}))
						})
					}()
				}
			},
			RevertChanges: func(path, entryID string) {
				sess := session()
				if sess == nil {
					appendItem(replcomponents.CommandEntry(ui.CommandOutputProps{
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
								appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
								return
							}
							cmdStr := "/revert"
							if path != "" {
								cmdStr += " " + path
								if entryID != "" {
									cmdStr += " " + entryID
								}
							}
							appendItem(replcomponents.CommandEntry(ui.CommandOutputProps{
								Command: cmdStr,
								Output:  "Reverted changes.",
							}))
						})
					}()
				}
			},
			ClearScreen: func() {
				restartTranscript(latestMessageID.Current, modelLabel(), thinking())
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

	send := func(sessionID, text string, version int) {
		go func() {
			err := props.Client.Send(ctx, sessionID, api.SendRequest{Content: text})
			kitex.PostMacro(func() {
				if ctx.Err() != nil || sessionVersion.Current != version {
					return
				}
				setSending(sendState{})
				if err != nil {
					appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
				}
			})
		}()
	}

	submit := func(text string) {
		if strings.HasPrefix(text, "/") {
			reg := props.Registry
			if reg == nil {
				reg = command.NewRegistry()
			}
			err := reg.Execute(makeEnv(), text)
			if err != nil {
				appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
			}
			return
		}

		if props.Client == nil {
			appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: "Daemon client is unavailable."}))
			return
		}
		if isStreaming() {
			appendItem(replcomponents.NoticeEntry(ui.NoticeProps{Text: "A response is already running."}))
			return
		}
		if conversation.Error != nil {
			conversation.Refetch()
		}
		version := sessionVersion.Current
		setSending(sendState{Version: version, Pending: true})

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
						if ctx.Err() != nil || sessionVersion.Current != version {
							return
						}
						appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
						setSending(sendState{})
					})
					return
				}
				kitex.PostMacro(func() {
					if ctx.Err() != nil || sessionVersion.Current != version {
						return
					}
					selectSession(newSess)
					setSending(sendState{Version: sessionVersion.Current, Pending: true})
					send(newSess.ID, text, sessionVersion.Current)
				})
			}()
		} else {
			send(sess.ID, text, version)
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
		if isStreaming() {
			sess := session()
			if sess != nil && props.Client != nil {
				go func() {
					if err := props.Client.Cancel(ctx, sess.ID); err != nil {
						kitex.PostMacro(func() {
							if active := session(); ctx.Err() == nil && active != nil && active.ID == sess.ID {
								appendItem(replcomponents.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
							}
						})
					}
				}()
			} else {
				sessionVersion.Current++
			}
			setSending(sendState{})
		}
	}

	computedState := func() ui.State {
		if isPickerOpen() {
			return ui.StatePicker
		}
		if perms.Count() > 0 {
			return ui.StateApproval
		}
		if isStreaming() {
			return ui.StateStreaming
		}
		return ui.StateIdle
	}

	transcript := TranscriptView{Key: strconv.Itoa(epoch()), Entries: entries()}

	return REPLController{
		Transcript:   transcript,
		Mode:         mode,
		ModelLabel:   modelLabel,
		Thinking:     thinking,
		StatusHint:   statusHint,
		ActivePicker: activePicker,
		IsPickerOpen: isPickerOpen,
		State:        computedState,
		Conversation: conversation,
		IsStreaming:  isStreaming(),
		Perms:        perms,
		Submit:       submit,
		CycleMode:    cycleMode,
		Cancel:       cancel,
	}
}
