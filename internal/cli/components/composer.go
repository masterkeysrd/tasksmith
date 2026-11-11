package components

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// maxHistoryEntries is the maximum number of entries kept in prompt history.
const maxHistoryEntries = 500

// BashModeHint is the status line hint shown when interactive bash mode is active.
const BashModeHint = "activated bash mode · esc to cancel"

// HistoryModeHint is the status line hint shown while browsing prompt history.
const HistoryModeHint = "prompt history · enter to send · esc to cancel"

// ComposerProps defines the properties for the Composer functional component.
type ComposerProps struct {
	Disabled        bool
	OnSubmit        func(text string)
	OnCycleMode     func()
	OnCancel        func()
	Autocomplete    *autocomplete.Engine
	CompletionViews map[string]uicomponents.AutocompleteView
}

// DraftEdit holds the current draft value and cursor offset.
type DraftEdit = uicomponents.AutocompleteEdit

// PromptHistory holds state and operations for traversing input prompt history.
type PromptHistory struct {
	History    []string
	HistoryIdx int
	Draft      string
	Browsing   func() bool
	Up         func(currentValue string) (string, bool)
	Down       func() (string, bool)
	Add        func(text string)
	Cancel     func() string
	Exit       func(value string)
}

// usePromptHistory manages input prompt history navigation, draft preservation,
// and disk persistence of submitted prompts.
func usePromptHistory() PromptHistory {
	history, setHistory := kitex.UseState([]string(nil))
	historyIdx, setHistoryIdx := kitex.UseState(0)
	draft, setDraft := kitex.UseState("")
	browsing, setBrowsing := kitex.UseState(false)

	// Load persisted history from disk on first mount.
	kitex.UseLayoutEffect(func() {
		if entries := loadHistoryFromFile(); entries != nil {
			setHistory(entries)
			setHistoryIdx(len(entries))
		}
	}, []any{})

	up := func(currentValue string) (string, bool) {
		hist := history()
		if len(hist) == 0 {
			return "", false
		}
		hIdx := historyIdx()
		if hIdx >= len(hist) {
			setDraft(currentValue)
			newIdx := len(hist) - 1
			setHistoryIdx(newIdx)
			setBrowsing(true)
			return hist[newIdx], true
		} else if hIdx > 0 {
			newIdx := hIdx - 1
			setHistoryIdx(newIdx)
			setBrowsing(true)
			return hist[newIdx], true
		}
		return "", false
	}

	down := func() (string, bool) {
		hist := history()
		if len(hist) == 0 {
			return "", false
		}
		hIdx := historyIdx()
		if hIdx < len(hist)-1 {
			newIdx := hIdx + 1
			setHistoryIdx(newIdx)
			setBrowsing(true)
			return hist[newIdx], true
		} else if hIdx == len(hist)-1 {
			setHistoryIdx(len(hist))
			setBrowsing(false)
			return draft(), true
		}
		return "", false
	}

	add := func(text string) {
		hist := history()
		// Skip consecutive duplicates.
		if len(hist) > 0 && hist[len(hist)-1] == text {
			setHistoryIdx(len(hist))
			setDraft("")
			setBrowsing(false)
			return
		}
		hist = append(hist, text)
		// Cap at maxHistoryEntries (keep most recent).
		if len(hist) > maxHistoryEntries {
			hist = hist[len(hist)-maxHistoryEntries:]
		}
		setHistory(hist)
		setHistoryIdx(len(hist))
		setDraft("")
		setBrowsing(false)
		// Fail-safe: disk errors must not crash the REPL.
		_ = persistHistory(hist)
	}

	// cancel abandons browsing and restores the pre-navigation draft.
	cancel := func() string {
		restored := draft()
		setHistoryIdx(len(history()))
		setBrowsing(false)
		return restored
	}

	// exit leaves browsing mode after active editing, adopting the edited
	// text as the new working draft.
	exit := func(value string) {
		setHistoryIdx(len(history()))
		setDraft(value)
		setBrowsing(false)
	}

	return PromptHistory{
		History:    history(),
		HistoryIdx: historyIdx(),
		Draft:      draft(),
		Browsing:   browsing,
		Up:         up,
		Down:       down,
		Add:        add,
		Cancel:     cancel,
		Exit:       exit,
	}
}

// loadHistoryFromFile reads persisted prompt history from disk.
// Returns nil on any error (including missing file).
func loadHistoryFromFile() []string {
	path, err := xdg.HistoryFilePath("repl.jsonl")
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // gracefully handle missing file
	}
	var entries []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry string
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	return entries
}

// persistHistory writes the given entries to the history file in JSONL format.
// Each entry is encoded as a JSON string on its own line.
func persistHistory(entries []string) error {
	path, err := xdg.HistoryFilePath("repl.jsonl")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, entry := range entries {
		b, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		b = append(b, '\n')
		if _, err := f.Write(b); err != nil {
			return err
		}
	}
	return nil
}

// Composer is a trail functional component managing multi-line user prompt drafting,
// autocomplete, prompt history navigation, and mode cycling.
var Composer = trail.FC("Composer", func(props ComposerProps) trail.Node {
	hide := store.UseHideComposer()
	ctrlCPending := store.UseApp(func(s store.AppState) bool {
		return s.CtrlCPending
	})
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	draft, setDraft := kitex.UseState(DraftEdit{})
	bashMode, setBashMode := kitex.UseState(false)
	history := usePromptHistory()

	disabled := props.Disabled || hide
	browsing := history.Browsing()
	auto := uicomponents.UseAutocomplete(uicomponents.AutocompleteOptions{
		Engine:   props.Autocomplete,
		Value:    draft().Value,
		Cursor:   draft().Cursor,
		Disabled: disabled || bashMode() || browsing,
	})

	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  !disabled,
	})

	kitex.UseEffect(func() {
		if inputRef.Current != nil {
			if !disabled {
				inputRef.Current.Focus()
			} else {
				inputRef.Current.Blur()
			}
		}
	}, []any{disabled})

	readDraft := func() DraftEdit {
		if input, ok := inputRef.Current.(*element.TextAreaElement); ok {
			return DraftEdit{Value: input.TextContent(), Cursor: input.Buffer().ByteOffset()}
		}
		return draft()
	}
	navValue := kitex.UseRef("")
	syncDraft := func() {
		next := readDraft()
		if history.Browsing() && next.Value != navValue.Current {
			history.Exit(next.Value)
		}
		if next != draft() {
			setDraft(next)
		}
	}
	replaceDraft := func(next DraftEdit) {
		if input, ok := inputRef.Current.(*element.TextAreaElement); ok {
			input.SetValue(next.Value)
			input.SetSelectionRange(next.Cursor, next.Cursor)
		}
		navValue.Current = next.Value
		setDraft(next)
	}
	// recall renders a history entry, entering or leaving bash mode to match
	// the entry's "!" prefix. The hint follows via the browsing effect.
	recall := func(value string) {
		if strings.HasPrefix(value, "!") {
			if !bashMode() {
				setBashMode(true)
			}
			stripped := strings.TrimPrefix(value, "!")
			replaceDraft(DraftEdit{Value: stripped, Cursor: len(stripped)})
			return
		}
		if bashMode() {
			setBashMode(false)
		}
		replaceDraft(DraftEdit{Value: value, Cursor: len(value)})
	}

	// First Ctrl+C clears the current input
	kitex.UseLayoutEffect(func() {
		if ctrlCPending {
			replaceDraft(DraftEdit{})
			if bashMode() {
				setBashMode(false)
				store.SetHint(store.DefaultHint)
			}
			if history.Browsing() {
				history.Exit("")
			}
		}
	}, []any{ctrlCPending})

	// Entering history browsing shows the history hint; leaving it restores
	// the default hint unless another mode (bash) owns it. Bash mode keeps
	// its own hint even while browsing. The first run is skipped so mounting
	// never overwrites a slot view's hint.
	wasBrowsing := kitex.UseRef(false)
	kitex.UseEffect(func() {
		prev := wasBrowsing.Current
		wasBrowsing.Current = browsing
		switch {
		case bashMode():
			store.SetHint(BashModeHint)
		case browsing:
			store.SetHint(HistoryModeHint)
		case prev:
			store.SetHint(store.DefaultHint)
		}
	}, []any{browsing, bashMode()})

	handleKeyDown := func(e event.Event) {
		ke, ok := e.(*event.KeyEvent)
		if !ok || disabled {
			return
		}
		kitex.PostMacro(syncDraft)
		current := readDraft()
		isCurrent := current.Value == auto.Result.Input && current.Cursor == auto.Result.Cursor

		if !bashMode() && current.Value == "" && (ke.Text == "!" || ke.MatchString("!")) {
			e.PreventDefault()
			e.StopPropagation()
			setBashMode(true)
			store.SetHint(BashModeHint)
			return
		}

		switch {
		case ke.MatchString("ctrl+y"):
			waiting := store.GetWaitingSubagents()
			if len(waiting) > 0 && len(waiting[0].PendingPermissions) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				store.DecideSubagentPermission(waiting[0].ID, waiting[0].PendingPermissions[0], api.PermissionDecision{Kind: api.DecisionAllowOnce})
				return
			}

		case ke.MatchString("ctrl+g"):
			waiting := store.GetWaitingSubagents()
			if len(waiting) > 0 {
				e.PreventDefault()
				e.StopPropagation()
				store.OpenSubagent(waiting[0].ID)
				return
			}

		case ke.MatchString("shift+tab"):
			e.PreventDefault()
			e.StopPropagation()
			if props.OnCycleMode != nil {
				props.OnCycleMode()
			} else {
				store.CycleMode()
			}

		case ke.MatchString("esc", "escape"):
			e.PreventDefault()
			e.StopPropagation()
			if bashMode() {
				setBashMode(false)
				store.SetHint(store.DefaultHint)
				return
			}
			if browsing {
				recall(history.Cancel())
				return
			}
			if auto.Dismiss() {
				return
			}
			if props.OnCancel != nil {
				props.OnCancel()
			}

		case ke.MatchString("backspace"):
			if bashMode() && current.Value == "" {
				e.PreventDefault()
				e.StopPropagation()
				setBashMode(false)
				store.SetHint(store.DefaultHint)
				return
			}

		case ke.MatchString("tab"):
			if isCurrent && auto.IsOpen {
				e.PreventDefault()
				e.StopPropagation()
				if !auto.IsLoading && auto.Error == nil && len(auto.Result.Candidates) > 0 {
					if edit, err := auto.Accept(); err == nil {
						replaceDraft(edit)
					}
				}
			}

		case ke.MatchString("up", "ctrl+p"):
			if !browsing && isCurrent && auto.Up() {
				e.PreventDefault()
				e.StopPropagation()
				return
			}
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				if txa.CursorState().Y > 0 {
					return
				}
			}
			e.PreventDefault()
			e.StopPropagation()
			valToPass := current.Value
			if bashMode() && current.Value != "" {
				valToPass = "!" + current.Value
			}
			if prevVal, ok := history.Up(valToPass); ok {
				recall(prevVal)
			}

		case ke.MatchString("down", "ctrl+n"):
			if !browsing && isCurrent && auto.Down() {
				e.PreventDefault()
				e.StopPropagation()
				return
			}
			if txa, ok := inputRef.Current.(*element.TextAreaElement); ok {
				lineCount := strings.Count(txa.TextContent(), "\n") + 1
				if txa.CursorState().Y < lineCount-1 {
					return
				}
			}
			e.PreventDefault()
			e.StopPropagation()
			if nextVal, ok := history.Down(); ok {
				recall(nextVal)
			}

		case ke.MatchString("shift+enter", "alt+enter"):
			e.PreventDefault()
			e.StopPropagation()
			start, end := current.Cursor, current.Cursor
			if input, ok := inputRef.Current.(*element.TextAreaElement); ok && input.SelectedText() != "" {
				start, end = input.SelectionRange()
			}
			replaceDraft(DraftEdit{
				Value:  current.Value[:start] + "\n" + current.Value[end:],
				Cursor: start + 1,
			})

		case ke.MatchString("enter"):
			e.PreventDefault()
			e.StopPropagation()
			if bashMode() {
				text := strings.TrimSpace(current.Value)
				replaceDraft(DraftEdit{})
				setBashMode(false)
				store.SetHint(store.DefaultHint)
				if text != "" {
					history.Add("!" + text)
					if props.OnSubmit != nil {
						props.OnSubmit("!" + text)
					}
				}
				return
			}
			if isCurrent && auto.IsOpen {
				if auto.IsLoading || auto.Error != nil || len(auto.Result.Candidates) == 0 {
					return
				}
				if !selectionMatchesQuery(auto) {
					if edit, err := auto.Accept(); err == nil {
						replaceDraft(edit)
					}
					return
				}
			}

			text := strings.TrimSpace(current.Value)
			replaceDraft(DraftEdit{})
			if text != "" {
				history.Add(text)
				if props.OnSubmit != nil {
					props.OnSubmit(text)
				}
			}
		}
	}

	keyHandler := kitex.UseRef[event.Listener](nil)
	keyHandler.Current = handleKeyDown
	kitex.UseEffectCleanup(func() func() {
		if input := inputRef.Current; input != nil {
			selection := input.AddEventListener(event.EventSelectionChange, func(event.Event) { syncDraft() })
			keys := input.AddEventListener(event.EventKeyDown, func(e event.Event) { keyHandler.Current(e) }, event.Capture())
			mouse := input.AddEventListener(event.EventMouseDown, func(event.Event) { kitex.PostMacro(syncDraft) })
			drag := input.AddEventListener(event.EventDrag, func(event.Event) { kitex.PostMacro(syncDraft) })
			return func() {
				selection.Cancel()
				keys.Cancel()
				mouse.Cancel()
				drag.Cancel()
			}
		}
		return nil
	}, []any{})

	borderCol := scheme.Color.Border.Focus
	if disabled {
		borderCol = scheme.Color.Border.Default
	}
	border := style.SingleBorder().Top(true).Bottom(!browsing).Left(false).Right(false).Color(borderCol)

	composerStyle := style.S().
		Border(border).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignStart).
		Width(style.Percent(100))

	promptStyle := style.S().Bold(true).Margin(0, 1, 0, 0)
	promptIndicator := ">"
	if bashMode() {
		promptIndicator = "!"
		promptStyle = promptStyle.Foreground(scheme.Color.Mode.Bash)
	} else {
		promptStyle = promptStyle.Foreground(scheme.Color.Icon.Primary)
	}

	outerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100))
	if hide {
		outerStyle = outerStyle.Display(style.DisplayNone)
	}

	return trail.Box(trail.BoxProps{
		Style: outerStyle,
	},
		trail.Box(trail.BoxProps{
			Style: composerStyle,
		},
			trail.Span(trail.SpanProps{
				Style: promptStyle,
			}, trail.Text(promptIndicator)),
			kitex.TextArea(kitex.TextAreaProps{
				Ref:      inputRef,
				Value:    draft().Value,
				Disabled: disabled,
				Style: style.S().
					Flex(1).
					Width(style.Percent(100)).
					Height(style.Content).
					MinHeight(style.Cells(1)).
					Scrollbar(style.Scrollbar{Y: style.Some(false)}),
				OnChange: func(event.Event) { syncDraft() },
			}),
		),
		kitex.If(browsing, func() trail.Node {
			label := fmt.Sprintf(" history (%d/%d) ", history.HistoryIdx+1, len(history.History))
			width := winSize.Width
			if width <= 0 {
				width = 40
			}
			side := max(1, (width-len(label))/2)
			rule := strings.Repeat("─", side) + label + strings.Repeat("─", side)
			return trail.Box(trail.BoxProps{
				Style: style.S().Width(style.Percent(100)),
			}, trail.Span(trail.SpanProps{
				Style: style.S().Foreground(borderCol).WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(rule)))
		}),
		uicomponents.Autocomplete(uicomponents.AutocompleteProps{
			State: auto,
			Views: props.CompletionViews,
			Bindings: []uicomponents.KeyBinding{
				{Key: "↑/↓", Label: "Navigate"},
				{Key: "Ctrl+P/Ctrl+N", Label: "Navigate"},
				{Key: "Tab", Label: "Select"},
				{Key: "Esc", Label: "Dismiss"},
			},
		}),
	)
})

func selectionMatchesQuery(auto uicomponents.AutocompleteState) bool {
	candidates := auto.Result.Candidates
	if auto.Selected < 0 || auto.Selected >= len(candidates) || auto.Result.Query == "" {
		return false
	}
	candidate := candidates[auto.Selected]
	if strings.EqualFold(auto.Result.Query, candidate.Value) {
		return true
	}
	for _, alias := range candidate.Aliases {
		if strings.EqualFold(auto.Result.Query, alias) {
			return true
		}
	}
	return false
}
