package command

import (
	"cmp"
	"context"
	"fmt"
	"strings"

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

type tasksListRoute struct{}

type taskOutputRoute struct{ ID string }

// TasksBrowser renders an interactive tasks list and inline output viewer inside <Slot />.
var TasksBrowser = trail.FC("TasksBrowser", func(slotID string) trail.Node {
	showAll, setShowAll := kitex.UseState(false)
	return flight.Stack(flight.StackProps{
		InitialRoute: tasksListRoute{},
		RenderRoute: func(route flight.Route) trail.Node {
			if output, ok := route.(taskOutputRoute); ok {
				return taskDetail(output.ID)
			}
			return tasksList(tasksListProps{
				slotID: slotID, showAll: showAll(), toggle: func() { setShowAll(!showAll()) },
			})
		},
	})
})

var taskDetail = trail.FC("TaskDetail", func(id string) trail.Node {
	nav := flight.UseNavigation()
	tasks := store.UseTasks()
	for _, task := range tasks {
		if task.ID == id {
			return taskOutputViewer(taskOutputProps{task: task, onClose: nav.Pop})
		}
	}
	return missingTask(nav.Pop)
})

var missingTask = trail.FC("MissingTask", func(back func()) trail.Node {
	kitex.UseEffect(back, []any{})
	return trail.Box(trail.BoxProps{})
})

type taskOutputProps struct {
	task    api.Task
	onClose func()
}

type taskOutputStyles struct {
	command style.Style
	output  style.Style
}

func newTaskOutputStyles(s *theme.Scheme) taskOutputStyles {
	command := styles.NewCommon(s).Secondary.MarginBottom(1)
	return taskOutputStyles{command: command, output: command.WhiteSpace(style.WhiteSpacePre)}
}

var taskOutputViewer = trail.FC("TaskOutputViewer", func(props taskOutputProps) trail.Node {
	t := props.task
	sheet := styles.Use(newTaskOutputStyles)
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
				lines := strings.Split(acc+chunk, "\n")
				acc = strings.Join(lines[max(0, len(lines)-50):], "\n")
				setOutput(acc)
			}
		}()
		return cancel
	}, []any{t.ID})

	killLabel := "Kill task"
	if !taskCanKill(t.Status) {
		killLabel = ""
	}
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"esc", "escape"}, Label: "Return to tasks", OnPress: props.onClose},
		{Keys: []string{"x"}, Label: killLabel, OnPress: func() {
			if taskCanKill(t.Status) {
				setNotice("")
				_ = store.KillTask(t.ID)
			} else {
				setNotice(fmt.Sprintf("Task is already %s.", t.Status))
			}
		}},
	}, true)
	trail.UseInput(func(_ string, k event.KeyEvent) {
		if !k.MatchString("x") {
			setNotice("")
		}
	}, trail.InputOptions{IsActive: notice() != ""})

	text := output()
	if text == "" {
		text = "(no output)"
		if isStreaming() {
			text = "Waiting for output..."
		}
	}
	title := cmp.Or(taskLine(t.Title), "Task "+t.ID)
	cmd := taskLine(t.Command)
	return uicomponents.Panel(uicomponents.PanelProps{
		Title: fmt.Sprintf("%s (%s):", title, t.Status), Notice: notice(), Bindings: bindings,
	},
		kitex.If(cmd != "", func() trail.Node {
			return trail.Span(trail.SpanProps{Style: sheet.command}, trail.Text("$ "+cmd))
		}),
		trail.Span(trail.SpanProps{Style: sheet.output}, trail.Text(text)),
	)
})

func taskLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

func taskCanKill(status api.TaskStatus) bool {
	return status == api.TaskStatusRunning || status == api.TaskStatusPending
}

func taskOption(t api.Task) uicomponents.Option {
	started := "--:--:--"
	if !t.StartedAt.IsZero() {
		started = t.StartedAt.Local().Format("15:04:05")
	}
	title, cmd := taskLine(t.Title), taskLine(t.Command)
	label := fmt.Sprintf("[%s] %s", started, cmp.Or(title, cmd, t.ID))
	if title != "" && cmd != "" && title != cmd {
		label += " (" + cmd + ")"
	}
	tone := uicomponents.ToneMuted
	switch t.Status {
	case api.TaskStatusCompleted:
		tone = uicomponents.ToneSuccess
	case api.TaskStatusFailed:
		tone = uicomponents.ToneError
	case api.TaskStatusPending:
		tone = uicomponents.ToneWarning
	case api.TaskStatusRunning:
		tone = uicomponents.ToneAccent
	}
	return uicomponents.Option{
		Value: t.ID, Label: label,
		Cells:     []string{label, string(t.Status), format.DurationMilis(t.DurationMs)},
		CellNodes: []trail.Node{nil, uicomponents.Badge(uicomponents.BadgeProps{Label: string(t.Status), Tone: tone})},
	}
}

type tasksListProps struct {
	slotID  string
	showAll bool
	toggle  func()
}

var tasksList = trail.FC("TasksList", func(props tasksListProps) trail.Node {
	nav := flight.UseNavigation()
	tasks := store.UseTasks()
	title, toggleLabel := "Background Tasks", "Show all"
	if props.showAll {
		title, toggleLabel = "Background Tasks (all)", "Hide completed"
	}
	var opts []uicomponents.Option
	statuses := make(map[string]api.TaskStatus, len(tasks))
	hasKillable := false
	for _, task := range tasks {
		active := taskCanKill(task.Status)
		if props.showAll || active {
			opts = append(opts, taskOption(task))
			statuses[task.ID] = task.Status
			hasKillable = hasKillable || active
		}
	}
	closeList := func() { store.RemoveSlot(props.slotID) }
	emptyBindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"a"}, Label: toggleLabel, Disabled: len(tasks) == 0 && !props.showAll, OnPress: props.toggle},
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: closeList},
	}, len(opts) == 0)
	if len(opts) == 0 {
		msg := "  No background tasks."
		if len(tasks) > 0 {
			msg = "  No running background tasks."
		}
		return uicomponents.Panel(uicomponents.PanelProps{Title: title, Notice: msg, Bindings: emptyBindings})
	}

	picker := components.NewPicker(title, opts, func(opt uicomponents.Option) {
		nav.Push(taskOutputRoute{ID: opt.Value})
	})
	picker.Filterable = false
	picker.Columns = []uicomponents.Column{{}, {Width: 14}, {Width: 14, Align: style.TextAlignRight}}
	picker.Bindings = []uicomponents.KeyBinding{{Key: "↑/↓", Label: "Select"}, {Key: "enter", Label: "View output"}}
	if hasKillable {
		picker.Bindings = append(picker.Bindings, uicomponents.KeyBinding{Key: "x", Label: "Kill"})
	}
	picker.Bindings = append(picker.Bindings,
		uicomponents.KeyBinding{Key: "a", Label: toggleLabel}, uicomponents.KeyBinding{Key: "esc", Label: "Close"},
	)
	picker.Keys = map[string]func(uicomponents.Option){"a": func(uicomponents.Option) { props.toggle() }}
	picker.OnCancel = func() {
		closeList()
		store.AppendCommandOutput("/tasks", "Exited /tasks command")
	}
	picker.ConfirmActions = map[string]components.ConfirmAction{
		"x": {
			DisabledReason: func(opt uicomponents.Option) string {
				if status := statuses[opt.Value]; !taskCanKill(status) {
					return fmt.Sprintf("Task is already %s.", status)
				}
				return ""
			},
			Prompt: func(opt uicomponents.Option) string { return "Kill background task " + opt.Label + "?" },
			OnConfirm: func(opt uicomponents.Option) {
				if taskCanKill(statuses[opt.Value]) {
					_ = store.KillTask(opt.Value)
				}
			},
		},
	}
	return components.Picker(picker)
})
