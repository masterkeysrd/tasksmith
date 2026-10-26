package command

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerTaskCommands() {
	r.Register(Command{
		Name:        "tasks",
		Usage:       "/tasks",
		Description: "List and manage background tasks",
		Handler:     handleTasks,
	})
}

func handleTasks(_ []string) error {
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: TasksBrowser,
	})
	return nil
}

type taskOutputProps struct {
	task    api.Task
	onClose func()
}

var taskOutputViewer = trail.FC("TaskOutputViewer", func(props taskOutputProps) trail.Node {
	allTasks := store.UseTasks()
	t := props.task
	for i := range allTasks {
		if allTasks[i].ID == props.task.ID {
			t = allTasks[i]
			break
		}
	}
	output, setOutput := kitex.UseState("")
	isStreaming, setIsStreaming := kitex.UseState(true)
	notice, setNotice := kitex.UseState("")

	kitex.UseEffectCleanup(func() func() {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			defer setIsStreaming(false)
			var acc string
			for chunk, err := range store.StreamTaskOutput(ctx, t.ID) {
				if err != nil {
					break
				}
				acc += chunk
				lines := strings.Split(acc, "\n")
				if len(lines) > 50 {
					acc = strings.Join(lines[len(lines)-50:], "\n")
				}
				setOutput(acc)
			}
		}()
		return cancel
	}, []any{t.ID})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		if notice() != "" {
			setNotice("")
		}
		if k.MatchString("esc", "escape") {
			if props.onClose != nil {
				props.onClose()
			}
		} else if k.MatchString("x") {
			if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
				_ = store.KillTask(t.ID)
			} else {
				setNotice(fmt.Sprintf("Task is already %s.", t.Status))
			}
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false)
	if scheme != nil && scheme.Color.Border.Primary != nil {
		border = border.Color(scheme.Color.Border.Primary)
	}

	text := output()
	if text == "" {
		if isStreaming() {
			text = "Waiting for output..."
		} else {
			text = "(no output)"
		}
	} else {
		lines := strings.Split(text, "\n")
		if len(lines) > 50 {
			text = strings.Join(lines[len(lines)-50:], "\n")
		}
	}

	title := strings.TrimSpace(strings.ReplaceAll(t.Title, "\n", " "))
	if title == "" {
		title = "Task " + t.ID
	}
	cmd := strings.TrimSpace(strings.ReplaceAll(t.Command, "\n", " "))

	headerText := fmt.Sprintf("%s (%s):", title, t.Status)
	headerStyle := style.S().Bold(true)
	if scheme != nil && scheme.Color.Text.Primary != nil {
		headerStyle = headerStyle.Foreground(scheme.Color.Text.Primary)
	}

	cmdStyle := style.S().Margin(0, 0, 1, 0)
	if scheme != nil && scheme.Color.Text.Secondary != nil {
		cmdStyle = cmdStyle.Foreground(scheme.Color.Text.Secondary)
	}

	outputStyle := style.S().
		WhiteSpace(style.WhiteSpacePre).
		Margin(0, 0, 1, 0)
	if scheme != nil && scheme.Color.Text.Secondary != nil {
		outputStyle = outputStyle.Foreground(scheme.Color.Text.Secondary)
	}

	bindings := []components.KeyBinding{
		{Key: "esc", Label: "Return to tasks"},
	}
	if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
		bindings = append(bindings, components.KeyBinding{Key: "x", Label: "Kill task"})
	}

	children := []trail.Node{
		trail.Span(trail.SpanProps{Style: headerStyle}, trail.Text(headerText)),
	}
	if cmd != "" {
		children = append(children, trail.Span(trail.SpanProps{Style: cmdStyle}, trail.Text("$ "+cmd)))
	}
	children = append(children,
		trail.Span(trail.SpanProps{Style: outputStyle}, trail.Text(text)),
	)
	if n := notice(); n != "" {
		noticeStyle := style.S().Margin(0, 0, 1, 0)
		if scheme != nil && scheme.Color.Text.Secondary != nil {
			noticeStyle = noticeStyle.Foreground(scheme.Color.Text.Secondary)
		}
		children = append(children, trail.Span(trail.SpanProps{Style: noticeStyle}, trail.Text(n)))
	}
	children = append(children,
		components.KeyboardHint(components.KeyboardHintProps{
			Bindings: bindings,
		}),
	)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

type emptyTasksViewProps struct {
	slotID      string
	title       string
	emptyMsg    string
	toggleLabel string
	hasAllTasks bool
	onToggle    func()
}

var emptyTasksView = trail.FC("EmptyTasksView", func(props emptyTasksViewProps) trail.Node {
	trail.UseInput(func(_ string, k event.KeyEvent) {
		if k.MatchString("esc", "escape") {
			store.RemoveSlot(props.slotID)
		} else if k.MatchString("a") {
			if props.onToggle != nil {
				props.onToggle()
			}
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false)
	if scheme != nil && scheme.Color.Border.Primary != nil {
		border = border.Color(scheme.Color.Border.Primary)
	}
	boxStyle := style.S().
		Border(border).
		Width(style.Percent(100)).
		FlexDirection(style.FlexColumn)
	titleStyle := style.S().Bold(true).Margin(0, 0, 1, 0)
	emptyStyle := style.S().Margin(0, 0, 1, 0)

	if scheme != nil {
		if scheme.Color.Text.Primary != nil {
			titleStyle = titleStyle.Foreground(scheme.Color.Text.Primary)
		}
		if scheme.Color.Text.Secondary != nil {
			emptyStyle = emptyStyle.Foreground(scheme.Color.Text.Secondary)
		}
	}

	var bindings []components.KeyBinding
	if props.hasAllTasks {
		bindings = append(bindings, components.KeyBinding{Key: "a", Label: props.toggleLabel})
	}
	bindings = append(bindings, components.KeyBinding{Key: "esc", Label: "Close"})

	return trail.Box(trail.BoxProps{Style: boxStyle},
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(props.title)),
		trail.Span(trail.SpanProps{Style: emptyStyle}, trail.Text(props.emptyMsg)),
		components.KeyboardHint(components.KeyboardHintProps{
			Bindings: bindings,
		}),
	)
})

// TasksBrowser renders an interactive tasks list and inline output viewer inside <Slot />.
var TasksBrowser = trail.FC("TasksBrowser", func(slotID string) trail.Node {
	showAll, setShowAll := kitex.UseState(false)
	allTasks := store.UseTasks()

	selectedTaskID, setSelectedTaskID := kitex.UseState("")

	var visibleTasks []api.Task
	if showAll() {
		visibleTasks = allTasks
	} else {
		for _, t := range allTasks {
			if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
				visibleTasks = append(visibleTasks, t)
			}
		}
	}

	toggleLabel := "Show all"
	if showAll() {
		toggleLabel = "Hide completed"
	}

	title := "Background Tasks"
	if showAll() {
		title = "Background Tasks (all)"
	}

	if selID := selectedTaskID(); selID != "" {
		var sel *api.Task
		for i := range allTasks {
			if allTasks[i].ID == selID {
				sel = &allTasks[i]
				break
			}
		}
		if sel != nil {
			return taskOutputViewer(taskOutputProps{
				task: *sel,
				onClose: func() {
					setSelectedTaskID("")
				},
			})
		}
	}

	if len(visibleTasks) == 0 {
		emptyMsg := "  No background tasks."
		if !showAll() && len(allTasks) > 0 {
			emptyMsg = "  No running background tasks."
		}
		return emptyTasksView(emptyTasksViewProps{
			slotID:      slotID,
			title:       title,
			emptyMsg:    emptyMsg,
			toggleLabel: toggleLabel,
			hasAllTasks: len(allTasks) > 0 || showAll(),
			onToggle: func() {
				if len(allTasks) > 0 || showAll() {
					setShowAll(!showAll())
				}
			},
		})
	}

	opts := make([]uicomponents.Option, len(visibleTasks))
	for i, t := range visibleTasks {
		timeStr := "--:--:--"
		if !t.StartedAt.IsZero() {
			timeStr = t.StartedAt.Local().Format("15:04:05")
		}

		titleStr := strings.TrimSpace(strings.ReplaceAll(t.Title, "\n", " "))
		cmdStr := strings.TrimSpace(strings.ReplaceAll(t.Command, "\n", " "))

		var label string
		if titleStr != "" && cmdStr != "" && titleStr != cmdStr {
			label = fmt.Sprintf("[%s] %s (%s)", timeStr, titleStr, cmdStr)
		} else {
			label = fmt.Sprintf("[%s] %s", timeStr, cmp.Or(titleStr, cmdStr, t.ID))
		}

		dur := fmt.Sprintf("%dms", t.DurationMs)
		if t.DurationMs >= 1000 {
			dur = fmt.Sprintf("%.1fs", float64(t.DurationMs)/1000.0)
		}
		opts[i] = uicomponents.Option{
			Value: t.ID,
			Label: label,
			Cells: []string{label, string(t.Status), dur},
		}
	}

	var hasKillable bool
	for _, t := range visibleTasks {
		if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
			hasKillable = true
			break
		}
	}

	bindings := []components.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "View output"},
	}
	if hasKillable {
		bindings = append(bindings, components.KeyBinding{Key: "x", Label: "Kill"})
	}
	bindings = append(bindings,
		components.KeyBinding{Key: "a", Label: toggleLabel},
		components.KeyBinding{Key: "esc", Label: "Close"},
	)

	pickerProps := components.NewPicker(title, opts, func(opt uicomponents.Option) {
		setSelectedTaskID(opt.Value)
	})
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0},
		{Width: 14, Align: style.TextAlignLeft},
		{Width: 14, Align: style.TextAlignRight},
	}
	pickerProps.Filterable = false
	pickerProps.Bindings = bindings
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"a": func(_ uicomponents.Option) {
			setShowAll(!showAll())
		},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/tasks", "Exited /tasks command")
	}
	pickerProps.ConfirmActions = map[string]components.ConfirmAction{
		"x": {
			DisabledReason: func(opt uicomponents.Option) string {
				for _, t := range visibleTasks {
					if t.ID == opt.Value {
						if t.Status != api.TaskStatusRunning && t.Status != api.TaskStatusPending {
							return fmt.Sprintf("Task is already %s.", t.Status)
						}
						return ""
					}
				}
				return ""
			},
			Prompt: func(opt uicomponents.Option) string {
				for _, t := range visibleTasks {
					if t.ID == opt.Value {
						if t.Status != api.TaskStatusRunning && t.Status != api.TaskStatusPending {
							return ""
						}
						return fmt.Sprintf("Kill background task %s?", opt.Label)
					}
				}
				return ""
			},
			OnConfirm: func(opt uicomponents.Option) {
				for _, t := range visibleTasks {
					if t.ID == opt.Value {
						if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
							_ = store.KillTask(opt.Value)
						}
						break
					}
				}
			},
		},
	}

	return components.Picker(pickerProps)
})
