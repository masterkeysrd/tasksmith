package command

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/flight"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerSubagentCommands() {
	r.Register(Command{
		Name:        "subagents",
		Aliases:     []string{"subagent"},
		Usage:       "/subagents",
		Description: "List and inspect subagents",
		Handler:     handleSubagents,
	})
}

func handleSubagents(_ []string) error {
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: SubagentsBrowser,
	})
	return nil
}

func init() {
	store.SetSubagentOpener(func(subagentID string) {
		store.EnqueueSlot(store.SlotItem{
			ID:   "subagent-viewer-" + subagentID,
			Kind: store.SlotKindPicker,
			Data: func(slotID string) trail.Node {
				return SubagentViewer(SubagentViewerProps{
					SubagentID: subagentID,
					OnClose: func() {
						store.RemoveSlot(slotID)
					},
				})
			},
		})
	})
}

func formatSubagentStatus(s api.SessionStatus) string {
	switch s {
	case api.StatusWaitingPermission:
		return "Waiting Permission"
	case api.StatusWaitingQuestion:
		return "Waiting Answer"
	case api.StatusWaitingPlanReview:
		return "Waiting Plan Review"
	default:
		return strings.ReplaceAll(string(s), "_", " ")
	}
}

// SubagentViewerProps defines the properties for the live SubagentViewer component.
type SubagentViewerProps struct {
	SubagentID string
	OnClose    func()
}

type subagentViewerStyles struct {
	styles.Common
	scheme *theme.Scheme
	header style.Style
	chat   style.Style
}

func newSubagentViewerStyles(s *theme.Scheme) subagentViewerStyles {
	common := styles.NewCommon(s)
	return subagentViewerStyles{
		Common: common,
		scheme: s,
		header: common.Row.
			Display(style.DisplayFlex).
			Width(style.Percent(100)).
			MarginBottom(1),
		chat: common.Column.
			MaxHeight(style.Cells(18)).
			OverflowY(style.OverflowAuto).
			ScrollbarY(true).
			Width(style.Percent(100)).
			MarginBottom(1),
	}
}

// SubagentViewer renders a live, non-static, navigable chat view of a subagent session.
var SubagentViewer = trail.FC("SubagentViewer", func(props SubagentViewerProps) trail.Node {
	sheet := styles.Use(newSubagentViewerStyles)
	allSubs := store.UseSubagents()
	var sub *api.Session
	for i := range allSubs {
		if allSubs[i].ID == props.SubagentID {
			sub = &allSubs[i]
			break
		}
	}

	subSession, setSubSession := kitex.UseState[*api.Session](nil)
	if sub == nil && subSession() != nil {
		sub = subSession()
	}

	conv := kitex.UseMemo(func() *store.SubagentConversation {
		return store.NewSubagentConversation(props.SubagentID)
	}, []any{props.SubagentID})

	rev, setRev := kitex.UseState(0)
	revCounter := kitex.UseMemo(func() *atomic.Int64 {
		return &atomic.Int64{}
	}, []any{props.SubagentID})
	bumpRev := func() {
		setRev(int(revCounter.Add(1)))
	}
	scroll := uicomponents.UseScroll(uicomponents.ScrollOptions{
		FollowTail: true,
		ExtraDeps:  []any{rev()},
	})

	kitex.UseEffectCleanup(func() func() {
		ctx, cancel := context.WithCancel(context.Background())
		c := store.Client()
		if c == nil {
			return cancel
		}
		go func() {
			if loaded, err := c.GetSession(ctx, props.SubagentID); err == nil && loaded != nil {
				setSubSession(loaded)
				if tl, err := c.Timeline(ctx, props.SubagentID); err == nil {
					conv.LoadTimelineSnapshot(loaded, tl, loaded.PendingQuestions)
				}
				if len(loaded.PendingPermissions) > 0 {
					conv.SeedPermissions(loaded.PendingPermissions)
				}
				conv.ReconcileStreamStatus(loaded)
				bumpRev()
			}
			for ev, err := range c.Watch(ctx, "", api.SessionStreamTopic(props.SubagentID)) {
				if err != nil {
					break
				}
				conv.ApplyEvent(ev)
				bumpRev()
			}
		}()
		return cancel
	}, []any{props.SubagentID})

	var perms []api.PermissionRequest
	var questions []api.QuestionRequest
	if sub != nil {
		perms = sub.PendingPermissions
		questions = sub.PendingQuestions
	}
	if len(perms) == 0 {
		perms = conv.PendingPermissions()
	}
	if len(questions) == 0 {
		questions = conv.PendingQuestions()
	}
	reviews := conv.PendingPlanReviews()
	if sub != nil && len(sub.PendingPlanReviews) > 0 {
		reviews = sub.PendingPlanReviews
	}
	planReviewActive := len(reviews) > 0 && len(perms) == 0 && len(questions) == 0
	hasPrompt := len(perms) > 0 || len(questions) > 0 || len(reviews) > 0

	pageLabel := "Page"
	if hasPrompt {
		pageLabel = "Scroll chat"
	}
	pageKeys := []string{"pgup", "ctrl+u", "b", "ctrl+b", "pgdown", "ctrl+d", "f", "ctrl+f"}
	trail.UseInput(func(input string, k event.KeyEvent) {
		if !hasPrompt || k.MatchString(pageKeys...) {
			scroll.HandleKey(input, k)
		}
	}, trail.InputOptions{IsActive: !planReviewActive})

	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Hint: "↑/↓ or j/k", Label: "Scroll", Disabled: hasPrompt},
		{Keys: pageKeys, Hint: "pgup/pgdn", Label: pageLabel},
		{Hint: "g/G", Label: "Top/Bottom", Disabled: hasPrompt},
		{
			Keys: []string{"x"}, Label: "Kill subagent",
			Disabled: hasPrompt || sub == nil || sub.Status == api.StatusIdle,
			OnPress:  func() { _ = store.KillSubagent(props.SubagentID) },
		},
		{Keys: []string{"esc", "escape"}, Label: "Return", OnPress: props.OnClose},
	}, !planReviewActive)

	agentName := "subagent"
	title := props.SubagentID
	status := api.StatusExecuting
	isWorktree := false

	if sub != nil {
		if sub.Config.Agent != "" {
			agentName = sub.Config.Agent
		}
		if strings.TrimSpace(sub.Title) != "" {
			title = strings.TrimSpace(strings.ReplaceAll(sub.Title, "\n", " "))
		}
		if sub.Status != "" {
			status = sub.Status
		}
		if sub.WorkDir != "" {
			mainSess := store.GetSession().Session
			if mainSess != nil && mainSess.WorkDir != "" && sub.WorkDir != mainSess.WorkDir {
				isWorktree = true
			}
		}
	}
	agentColor := components.SubagentColor(sheet.scheme, agentName)

	headerChildren := []trail.Node{
		trail.Span(trail.SpanProps{
			Style: sheet.Title.Foreground(agentColor),
		}, trail.Text("@"+agentName)),
	}
	if sub != nil {
		headerChildren = append(headerChildren, components.SubagentModelBadge(sheet.scheme, *sub))
	}
	headerChildren = append(headerChildren, trail.Span(trail.SpanProps{
		Style: sheet.Title,
	}, trail.Text(" · "+title+" ("+formatSubagentStatus(status)+")")))
	if isWorktree {
		headerChildren = append(headerChildren, trail.Span(trail.SpanProps{
			Style: sheet.Secondary,
		}, trail.Text(" · worktree")))
	}
	if !scroll.AutoScroll && scroll.Pos > 0 {
		headerChildren = append(headerChildren, trail.Span(trail.SpanProps{
			Style: sheet.Secondary,
		}, trail.Text(" [scrolled up]")))
	}

	headerNode := trail.Box(trail.BoxProps{
		Style: sheet.header,
	}, headerChildren...)

	chatChildren := make([]trail.Node, 0)
	for _, msg := range conv.Messages() {
		chatChildren = append(chatChildren, components.Message(components.MessageProps{Message: msg}))
	}

	if conv.IsStreaming() && status != api.StatusIdle && status != api.StatusWaitingPermission && status != api.StatusWaitingQuestion && status != api.StatusWaitingPlanReview {
		startedAt, input, output := conv.TurnStats()
		todos := conv.Todos()
		activeText := ""
		for _, it := range todos {
			if it.Status == "in_progress" {
				activeText = it.Content
				break
			}
		}
		spinnerProps := components.SpinnerProps{
			Status:     conv.SessionStatus(),
			ActiveText: activeText,
			StartedAt:  startedAt,
			Input:      input,
			Output:     output,
		}
		chatChildren = append(chatChildren, components.Spinner(spinnerProps))
	}

	chatBox := trail.Box(trail.BoxProps{
		Ref:   scroll.Ref,
		Style: sheet.chat,
	}, chatChildren...)

	var promptNode trail.Node
	if len(perms) > 0 {
		promptNode = components.PermissionPrompt(components.PermissionPromptProps{
			Request: perms[0],
			OnDecision: func(req api.PermissionRequest, dec api.PermissionDecision) {
				conv.RemovePermission(req.ID)
				store.DecideSubagentPermission(props.SubagentID, req, dec)
			},
		})
	} else if len(questions) > 0 {
		promptNode = components.QuestionPrompt(components.QuestionPromptProps{
			Request: questions[0],
			OnSubmit: func(req api.QuestionRequest, resp api.QuestionResponse, done func(error)) {
				conv.RemoveQuestion(req.ID)
				store.SubmitSubagentQuestion(props.SubagentID, req, resp, done)
			},
		})
	} else if len(reviews) > 0 {
		promptNode = components.PlanReviewPrompt(components.PlanReviewPromptProps{
			Request: reviews[0],
			OnSubmit: func(req api.PlanReviewRequest, dec api.PlanReviewDecision, done func(error)) {
				store.SubmitSubagentPlanReview(props.SubagentID, req, dec, func(err error) {
					if err == nil {
						conv.RemovePlanReview(req.ID)
						if cur := subSession(); cur != nil && len(cur.PendingPlanReviews) > 0 {
							cp := *cur
							cp.PendingPlanReviews = slices.DeleteFunc(slices.Clone(cp.PendingPlanReviews), func(p api.PlanReviewRequest) bool {
								return p.ID == req.ID
							})
							setSubSession(&cp)
						}
						bumpRev()
					}
					done(err)
				})
			},
		})
	}

	return uicomponents.Panel(uicomponents.PanelProps{
		Header: headerNode, Bindings: bindings,
	}, chatBox, promptNode)
})

type emptySubagentsViewProps struct {
	slotID          string
	title           string
	emptyMsg        string
	toggleLabel     string
	hasAllSubagents bool
	onToggle        func()
}

var emptySubagentsView = trail.FC("EmptySubagentsView", func(props emptySubagentsViewProps) trail.Node {
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"a"}, Label: props.toggleLabel, Disabled: !props.hasAllSubagents, OnPress: props.onToggle},
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: func() { store.RemoveSlot(props.slotID) }},
	}, true)
	return uicomponents.Panel(uicomponents.PanelProps{Title: props.title, Notice: props.emptyMsg, Bindings: bindings})
})

type subagentsListRoute struct{}

type subagentDetailRoute struct{ ID string }

type subagentsListProps struct {
	slotID   string
	showAll  bool
	onToggle func()
}

// SubagentsBrowser renders an interactive subagents list and inline live viewer inside <Slot />.
var SubagentsBrowser = trail.FC("SubagentsBrowser", func(slotID string) trail.Node {
	showAll, setShowAll := kitex.UseState(false)
	return flight.Stack(flight.StackProps{
		InitialRoute: subagentsListRoute{},
		RenderRoute: func(route flight.Route) trail.Node {
			if detail, ok := route.(subagentDetailRoute); ok {
				return subagentDetailView(detail.ID)
			}
			return subagentsList(subagentsListProps{
				slotID: slotID, showAll: showAll(), onToggle: func() { setShowAll(!showAll()) },
			})
		},
	})
})

var subagentDetailView = trail.FC("SubagentDetailView", func(id string) trail.Node {
	nav := flight.UseNavigation()
	return SubagentViewer(SubagentViewerProps{SubagentID: id, OnClose: nav.Pop})
})

var subagentsList = trail.FC("SubagentsList", func(props subagentsListProps) trail.Node {
	nav := flight.UseNavigation()
	scheme := theme.UseTheme()
	allSubs := store.UseSubagents()

	var visibleSubs []api.Session
	if props.showAll {
		visibleSubs = allSubs
	} else {
		for _, s := range allSubs {
			if s.Status != "" && s.Status != api.StatusIdle {
				visibleSubs = append(visibleSubs, s)
			}
		}
	}

	toggleLabel := "Show all"
	if props.showAll {
		toggleLabel = "Hide idle"
	}

	title := "Subagents"
	if props.showAll {
		title = "Subagents (all)"
	}

	if len(visibleSubs) == 0 {
		emptyMsg := "  No subagents."
		if !props.showAll && len(allSubs) > 0 {
			emptyMsg = "  No running subagents."
		}
		return emptySubagentsView(emptySubagentsViewProps{
			slotID:          props.slotID,
			title:           title,
			emptyMsg:        emptyMsg,
			toggleLabel:     toggleLabel,
			hasAllSubagents: len(allSubs) > 0 || props.showAll,
			onToggle:        props.onToggle,
		})
	}

	mainSess := store.GetSession().Session
	opts := make([]uicomponents.Option, len(visibleSubs))
	for i, s := range visibleSubs {
		timeStr := "--:--:--"
		if !s.CreatedAt.IsZero() {
			timeStr = s.CreatedAt.Local().Format("15:04:05")
		}

		agentName := cmp.Or(s.Config.Agent, "subagent")
		taskTitle := strings.TrimSpace(strings.ReplaceAll(s.Title, "\n", " "))
		if taskTitle == "" {
			taskTitle = s.ID
		}

		label := fmt.Sprintf("[%s] @%s · %s", timeStr, agentName, taskTitle)
		if s.WorkDir != "" && mainSess != nil && mainSess.WorkDir != "" && s.WorkDir != mainSess.WorkDir {
			label += " (worktree)"
		}

		var durStr string
		if s.CreatedAt.IsZero() {
			durStr = "-"
		} else if s.Status != api.StatusIdle {
			durStr = format.Duration(time.Since(s.CreatedAt))
		} else if !s.UpdatedAt.IsZero() {
			durStr = format.Duration(s.UpdatedAt.Sub(s.CreatedAt))
		} else {
			durStr = "-"
		}

		opts[i] = uicomponents.Option{
			Value: s.ID,
			Label: label,
			Cells: []string{label, formatSubagentStatus(s.Status), durStr},
			Icon:  components.SubagentModelBadge(scheme, s),
		}
	}

	var hasKillable bool
	for _, s := range visibleSubs {
		if s.Status != api.StatusIdle {
			hasKillable = true
			break
		}
	}

	bindings := []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Inspect"},
	}
	if hasKillable {
		bindings = append(bindings, uicomponents.KeyBinding{Key: "x", Label: "Kill"})
	}
	bindings = append(bindings,
		uicomponents.KeyBinding{Key: "a", Label: toggleLabel},
		uicomponents.KeyBinding{Key: "esc", Label: "Close"},
	)

	pickerProps := components.NewPicker(title, opts, func(opt uicomponents.Option) {
		nav.Push(subagentDetailRoute{ID: opt.Value})
	})
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0},
		{Width: 20, Align: style.TextAlignLeft},
		{Width: 14, Align: style.TextAlignRight},
	}
	pickerProps.Filterable = false
	pickerProps.Bindings = bindings
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"a": func(_ uicomponents.Option) {
			props.onToggle()
		},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(props.slotID)
		store.AppendCommandOutput("/subagents", "Exited /subagents command")
	}
	pickerProps.ConfirmActions = map[string]components.ConfirmAction{
		"x": {
			DisabledReason: func(opt uicomponents.Option) string {
				for _, s := range visibleSubs {
					if s.ID == opt.Value {
						if s.Status == api.StatusIdle {
							return "Subagent is already idle."
						}
						return ""
					}
				}
				return ""
			},
			Prompt: func(opt uicomponents.Option) string {
				for _, s := range visibleSubs {
					if s.ID == opt.Value {
						if s.Status == api.StatusIdle {
							return ""
						}
						return fmt.Sprintf("Kill subagent %s?", opt.Label)
					}
				}
				return ""
			},
			OnConfirm: func(opt uicomponents.Option) {
				_ = store.KillSubagent(opt.Value)
			},
		},
	}

	return components.Picker(pickerProps)
})
