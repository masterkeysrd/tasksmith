package repl

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

// AppProps configures the root REPL trail functional component.
type AppProps struct {
	Client            *client.Client
	Workspace         string
	Version           string
	SessionID         string
	Agent             string
	Model             string
	Title             string
	InitialSess       *api.Session
	InitialMode       api.PermissionMode
	InitialModelLabel string
	InitialThinking   string
	Registry          *command.Registry
}

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

// App is the root trail functional component for the TaskSmith REPL.
var App = trail.FC("App", func(props AppProps) trail.Node {
	app := trail.UseApp()

	items, setItems := kitex.UseState([]ui.TranscriptItem(nil))
	state, setState := kitex.UseState(ui.StateIdle)
	session, setSession := kitex.UseState(props.InitialSess)
	mode, setMode := kitex.UseState(cmp.Or(props.InitialMode, api.ModeDefault))
	modelLabel, setModelLabel := kitex.UseState(props.InitialModelLabel)
	thinking, setThinking := kitex.UseState(props.InitialThinking)
	streamText, setStreamText := kitex.UseState("")
	streamReasoning, setStreamReasoning := kitex.UseState("")
	spinnerText, setSpinnerText := kitex.UseState("Thinking...")
	statusHint, setStatusHint := kitex.UseState("")
	pickerProps, setPickerProps := kitex.UseState(ui.PickerProps{})
	pendingPerms, setPendingPerms := kitex.UseState([]api.PermissionRequest(nil))

	// Initial startup setup: render banner header, load history and pending permissions
	kitex.UseLayoutEffect(func() {
		curr := items()
		if len(curr) == 0 {
			var initialItems []ui.TranscriptItem
			initialItems = append(initialItems, ui.HeaderItem(ui.HeaderProps{
				Version:  props.Version,
				Model:    modelLabel(),
				Thinking: thinking(),
				Path:     props.Workspace,
			}))

			sess := session()
			if sess != nil && props.Client != nil {
				hist, err := props.Client.History(context.Background(), sess.ID)
				if err == nil {
					for _, msg := range hist {
						initialItems = append(initialItems, ui.MessageItem(msg))
					}
				}
				perms, pErr := props.Client.PendingPermissions(context.Background(), sess.ID)
				if pErr == nil && len(perms) > 0 {
					setPendingPerms(perms)
					setState(ui.StateApproval)
				}
			}
			setItems(initialItems)
		}
	}, []any{})

	// Subscribe to real-time daemon SSE events
	kitex.UseEffectCleanup(func() func() {
		if props.Client == nil {
			return func() {}
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for ev, err := range props.Client.Subscribe(ctx, "") {
				if err != nil {
					return
				}
				curEv := ev
				kitex.PostMacro(func() {
					sess := session()
					if curEv.SessionID != "" && sess != nil && curEv.SessionID != sess.ID {
						return
					}

					switch curEv.Type {
					case api.EventMessageAdded:
						if curEv.Message != nil {
							setItems(append(items(), ui.MessageItem(*curEv.Message)))
							setStreamText("")
							setStreamReasoning("")
						}

					case api.EventChunk:
						if curEv.Chunk != nil {
							for _, b := range curEv.Chunk.Content {
								switch b.Type {
								case api.BlockReasoning:
									if b.Text != "" {
										setStreamReasoning(streamReasoning() + b.Text)
									}
								case api.BlockText:
									if b.Text != "" {
										setStreamText(streamText() + b.Text)
									}
								case api.BlockToolCall:
									if b.ToolCall != nil {
										setSpinnerText(fmt.Sprintf("Running %s...", b.ToolCall.Name))
									}
								}
							}
						}

					case api.EventPermissionRequested:
						if len(curEv.Permissions) > 0 {
							setPendingPerms(append(pendingPerms(), curEv.Permissions...))
							setState(ui.StateApproval)
						}

					case api.EventCompleted:
						setState(ui.StateIdle)
						setStreamText("")
						setStreamReasoning("")
						setSpinnerText("Thinking...")

					case api.EventError:
						if curEv.Error != "" {
							setItems(append(items(), ui.ErrorItem(curEv.Error)))
						}
						setState(ui.StateIdle)
					}
				})
			}
		}()
		return func() {
			cancel()
		}
	}, []any{})

	var handleCommandEvent func(evt command.Event)

	bindPickerCallbacks := func(p ui.PickerProps) ui.PickerProps {
		p.OnOpen = func() {
			setStatusHint("")
		}
		p.OnAction = func(action any, closePicker bool) {
			if closePicker {
				setState(ui.StateIdle)
			}
			if action != nil {
				handleCommandEvent(action)
			}
		}
		p.OnCancel = func() {
			setState(ui.StateIdle)
		}
		return p
	}

	refreshSessionPicker := func(activeSess *api.Session) {
		if props.Client == nil {
			return
		}
		sessions, err := props.Client.ListSessions(context.Background())
		kitex.PostMacro(func() {
			if err != nil {
				setItems(append(items(), ui.ErrorItem(err.Error())))
				return
			}
			if len(sessions) == 0 {
				setState(ui.StateIdle)
				return
			}
			var activeID string
			if activeSess != nil {
				activeID = activeSess.ID
			}
			p := bindPickerCallbacks(command.NewSessionPicker(sessions, activeID))
			setPickerProps(p)
			setState(ui.StatePicker)
		})
	}

	handleCommandEvent = func(evt command.Event) {
		if evt == nil {
			return
		}
		switch e := evt.(type) {
		case command.ExitMsg:
			if app != nil {
				app.Exit(nil)
			}

		case command.PrintMsg:
			setItems(append(items(), ui.NoticeItem(e.Text)))

		case command.ClearScreenMsg:
			setItems(append(items(), ui.HeaderItem(ui.HeaderProps{
				Version:  props.Version,
				Model:    modelLabel(),
				Thinking: thinking(),
				Path:     props.Workspace,
			})))

		case command.ChangeModeMsg:
			setMode(e.Mode)
			sess := session()
			if sess != nil && props.Client != nil {
				go props.Client.SetPermissionMode(context.Background(), sess.ID, e.Mode)
			}
			if !e.Silent {
				setItems(append(items(), ui.NoticeItem(fmt.Sprintf("Switched to %s mode.", e.Mode))))
			}

		case command.NewSessionMsg:
			targetModel := cmp.Or(strings.TrimSpace(e.Model), props.Model)
			mdlLabel, think := resolveModelAndThinking(context.Background(), props.Client, props.Agent, targetModel, nil)
			setSession(nil)
			setModelLabel(mdlLabel)
			setThinking(think)
			setMode(api.ModeDefault)
			setItems(append(items(), ui.HeaderItem(ui.HeaderProps{
				Version:  props.Version,
				Model:    mdlLabel,
				Thinking: think,
				Path:     props.Workspace,
			})))

		case command.SwitchSessionMsg:
			if props.Client != nil {
				go func() {
					switchedSess, err := props.Client.GetSession(context.Background(), e.SessionID)
					if err != nil {
						kitex.PostMacro(func() {
							setItems(append(items(), ui.ErrorItem(err.Error())))
						})
						return
					}
					hist, _ := props.Client.History(context.Background(), switchedSess.ID)
					m, _ := props.Client.PermissionMode(context.Background(), switchedSess.ID)
					mdlLabel, think := resolveModelAndThinking(context.Background(), props.Client, props.Agent, props.Model, switchedSess)
					kitex.PostMacro(func() {
						setSession(switchedSess)
						setModelLabel(mdlLabel)
						setThinking(think)
						if m != "" {
							setMode(m)
						}
						newItems := append(items(), ui.HeaderItem(ui.HeaderProps{
							Version:  props.Version,
							Model:    mdlLabel,
							Thinking: think,
							Path:     props.Workspace,
						}))
						for _, msg := range hist {
							newItems = append(newItems, ui.MessageItem(msg))
						}
						setItems(newItems)
					})
				}()
			}

		case command.RenameSessionMsg:
			if props.Client != nil {
				go func() {
					updated, err := props.Client.UpdateSession(context.Background(), e.SessionID, api.UpdateSessionRequest{
						Title: e.Title,
					})
					if err != nil {
						kitex.PostMacro(func() {
							setItems(append(items(), ui.ErrorItem(err.Error())))
						})
						return
					}
					activeSess := session()
					if activeSess != nil && activeSess.ID == e.SessionID && updated != nil {
						activeSess = updated
						kitex.PostMacro(func() {
							setSession(updated)
						})
					}
					refreshSessionPicker(activeSess)
				}()
			}

		case command.DeleteSessionMsg:
			if props.Client != nil {
				go func() {
					err := props.Client.DeleteSession(context.Background(), e.SessionID)
					if err != nil {
						kitex.PostMacro(func() {
							setItems(append(items(), ui.ErrorItem(err.Error())))
						})
						return
					}
					activeSess := session()
					if activeSess != nil && activeSess.ID == e.SessionID {
						activeSess = nil
						mdlLabel, think := resolveModelAndThinking(context.Background(), props.Client, props.Agent, props.Model, nil)
						kitex.PostMacro(func() {
							setSession(nil)
							setModelLabel(mdlLabel)
							setThinking(think)
						})
					}
					refreshSessionPicker(activeSess)
				}()
			}

		case command.ListChangesMsg:
			sess := session()
			if sess == nil {
				setItems(append(items(), ui.NoticeItem("No active session.")))
				return
			}
			if props.Client != nil {
				go func() {
					changes, err := props.Client.Changes(context.Background(), sess.ID)
					kitex.PostMacro(func() {
						if err != nil {
							setItems(append(items(), ui.ErrorItem(err.Error())))
							return
						}
						if len(changes) == 0 {
							setItems(append(items(), ui.NoticeItem("No tracked file changes.")))
							return
						}
						var rows []string
						for _, c := range changes {
							rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t+%d/-%d", c.ID, c.Kind, c.Path, c.Additions, c.Deletions))
						}
						setItems(append(items(), ui.NoticeItem(strings.Join(rows, "\n"))))
					})
				}()
			}

		case command.DiffChangeMsg:
			sess := session()
			if sess == nil {
				setItems(append(items(), ui.NoticeItem("No active session.")))
				return
			}
			if props.Client != nil {
				go func() {
					diff, err := props.Client.DiffChange(context.Background(), sess.ID, e.Path, e.EntryID)
					kitex.PostMacro(func() {
						if err != nil {
							setItems(append(items(), ui.ErrorItem(err.Error())))
							return
						}
						setItems(append(items(), ui.DiffItem(diff)))
					})
				}()
			}

		case command.RevertChangesMsg:
			sess := session()
			if sess == nil {
				setItems(append(items(), ui.NoticeItem("No active session.")))
				return
			}
			if props.Client != nil {
				go func() {
					err := props.Client.Revert(context.Background(), sess.ID, api.RevertRequest{
						Path:    e.Path,
						EntryID: e.EntryID,
					})
					kitex.PostMacro(func() {
						if err != nil {
							setItems(append(items(), ui.ErrorItem(err.Error())))
							return
						}
						setItems(append(items(), ui.NoticeItem("Reverted changes.")))
					})
				}()
			}

		case command.OpenPickerMsg:
			setPickerProps(bindPickerCallbacks(e.Picker))
			setState(ui.StatePicker)
		}
	}

	onSubmit := func(text string) {
		if strings.HasPrefix(text, "/") {
			reg := props.Registry
			if reg == nil {
				reg = command.NewRegistry()
			}
			env := command.Env{
				Mode:         mode(),
				Session:      session(),
				DefaultAgent: props.Agent,
				DefaultModel: props.Model,
				ListSessions: func() ([]api.Session, error) {
					if props.Client == nil {
						return nil, nil
					}
					return props.Client.ListSessions(context.Background())
				},
			}
			evt, err := reg.Execute(env, text)
			if err != nil {
				setItems(append(items(), ui.ErrorItem(err.Error())))
				return
			}
			handleCommandEvent(evt)
			return
		}

		// User prompt
		setItems(append(items(), ui.PromptItem(text)))
		setState(ui.StateStreaming)
		setSpinnerText("Thinking...")
		setStreamText("")
		setStreamReasoning("")

		if props.Client == nil {
			return
		}

		sess := session()
		if sess == nil {
			go func() {
				newSess, err := props.Client.CreateSession(context.Background(), api.CreateSessionRequest{
					Title: text,
					Agent: props.Agent,
					Model: props.Model,
				})
				if err != nil {
					kitex.PostMacro(func() {
						setItems(append(items(), ui.ErrorItem(err.Error())))
						setState(ui.StateIdle)
					})
					return
				}
				kitex.PostMacro(func() {
					setSession(newSess)
				})
				sendErr := props.Client.Send(context.Background(), newSess.ID, api.SendRequest{Content: text})
				if sendErr != nil {
					kitex.PostMacro(func() {
						setItems(append(items(), ui.ErrorItem(sendErr.Error())))
						setState(ui.StateIdle)
					})
				}
			}()
		} else {
			go func() {
				sendErr := props.Client.Send(context.Background(), sess.ID, api.SendRequest{Content: text})
				if sendErr != nil {
					kitex.PostMacro(func() {
						setItems(append(items(), ui.ErrorItem(sendErr.Error())))
						setState(ui.StateIdle)
					})
				}
			}()
		}
	}

	onDecision := func(req api.PermissionRequest, dec api.PermissionDecision) {
		setItems(append(items(), ui.DecisionItem(req, dec)))
		sess := session()
		if sess != nil && props.Client != nil {
			go props.Client.RespondPermission(context.Background(), sess.ID, dec)
		}
		curPerms := pendingPerms()
		if len(curPerms) > 0 {
			rem := curPerms[1:]
			setPendingPerms(rem)
			if len(rem) == 0 {
				setState(ui.StateStreaming)
			}
		} else {
			setState(ui.StateStreaming)
		}
	}

	onCycleMode := func() {
		next := nextPermissionMode(mode())
		setMode(next)
		sess := session()
		if sess != nil && props.Client != nil {
			go props.Client.SetPermissionMode(context.Background(), sess.ID, next)
		}
	}

	onCancel := func() {
		if state() == ui.StateStreaming {
			sess := session()
			if sess != nil && props.Client != nil {
				go props.Client.Cancel(context.Background(), sess.ID)
			}
			setState(ui.StateIdle)
			setStreamText("")
			setStreamReasoning("")
		}
	}

	suggestCommands := func(prefix string) []ui.Suggestion {
		reg := props.Registry
		if reg == nil {
			reg = command.NewRegistry()
		}
		cmds := reg.Suggest(prefix)
		var res []ui.Suggestion
		for _, c := range cmds {
			res = append(res, ui.Suggestion{
				Usage:       c.Usage,
				Description: c.Description,
			})
		}
		return res
	}

	suggestArgs := func(rawLine string) ([]ui.Option, bool) {
		reg := props.Registry
		if reg == nil {
			reg = command.NewRegistry()
		}
		env := command.Env{
			Mode:         mode(),
			Session:      session(),
			DefaultAgent: props.Agent,
			DefaultModel: props.Model,
			ListSessions: func() ([]api.Session, error) {
				if props.Client == nil {
					return nil, nil
				}
				return props.Client.ListSessions(context.Background())
			},
		}
		_, opts, ok := reg.SuggestArgs(env, rawLine)
		return opts, ok
	}

	var activeNodes []trail.Node

	// Streaming display
	if state() == ui.StateStreaming {
		if r := strings.TrimSpace(streamReasoning()); r != "" {
			activeNodes = append(activeNodes, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: theme.ReasoningStyle,
				}, trail.Text(fmt.Sprintf("[reasoning] %s", r))),
			))
		}
		if t := streamText(); t != "" {
			activeNodes = append(activeNodes, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: theme.AgentBadgeStyle,
				}, trail.Text("[assistant] ")),
				trail.Span(trail.SpanProps{
					Style: theme.AssistantMsgStyle,
				}, trail.Text(t)),
			))
		}
		activeNodes = append(activeNodes, ui.Spinner(ui.SpinnerProps{Text: spinnerText()}))
	}

	// Active modal
	switch state() {
	case ui.StatePicker:
		activeNodes = append(activeNodes, ui.Picker(pickerProps()))
	case ui.StateApproval:
		if len(pendingPerms()) > 0 {
			activeNodes = append(activeNodes, ui.Approval(ui.ApprovalProps{
				Request:    pendingPerms()[0],
				OnDecision: onDecision,
			}))
		}
	}

	// Composer (visible in idle and streaming so user can draft next prompt)
	if state() != ui.StatePicker && state() != ui.StateApproval {
		activeNodes = append(activeNodes, ui.Composer(ui.ComposerProps{
			Disabled:    false,
			Suggest:     suggestCommands,
			SuggestArgs: suggestArgs,
			OnSubmit:    onSubmit,
			OnCycleMode: onCycleMode,
			OnCancel:    onCancel,
		}))
	}

	// Global status bar (always visible at the bottom)
	activeNodes = append(activeNodes, ui.StatusBar(ui.StatusBarProps{
		State:    state(),
		Mode:     mode(),
		Model:    modelLabel(),
		Thinking: thinking(),
		Hint:     statusHint(),
	}))

	return trail.Fragment(
		ui.Transcript(items()),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)),
		}, activeNodes...),
	)
})
