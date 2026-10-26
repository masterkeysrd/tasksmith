package components

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// maxHistoryEntries is the maximum number of entries kept in prompt history.
const maxHistoryEntries = 500

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
	Up         func(currentValue string) (string, bool)
	Down       func() (string, bool)
	Add        func(text string)
}

// usePromptHistory manages input prompt history navigation, draft preservation,
// and disk persistence of submitted prompts.
func usePromptHistory() PromptHistory {
	history, setHistory := kitex.UseState([]string(nil))
	historyIdx, setHistoryIdx := kitex.UseState(0)
	draft, setDraft := kitex.UseState("")

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
			return hist[newIdx], true
		} else if hIdx > 0 {
			newIdx := hIdx - 1
			setHistoryIdx(newIdx)
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
			return hist[newIdx], true
		} else if hIdx == len(hist)-1 {
			setHistoryIdx(len(hist))
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
		// Fail-safe: disk errors must not crash the REPL.
		_ = persistHistory(hist)
	}

	return PromptHistory{
		History:    history(),
		HistoryIdx: historyIdx(),
		Draft:      draft(),
		Up:         up,
		Down:       down,
		Add:        add,
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

	draft, setDraft := kitex.UseState(DraftEdit{})
	history := usePromptHistory()

	disabled := props.Disabled || hide
	auto := uicomponents.UseAutocomplete(uicomponents.AutocompleteOptions{
		Engine:   props.Autocomplete,
		Value:    draft().Value,
		Cursor:   draft().Cursor,
		Disabled: disabled,
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
	syncDraft := func() {
		if next := readDraft(); next != draft() {
			setDraft(next)
		}
	}
	replaceDraft := func(next DraftEdit) {
		if input, ok := inputRef.Current.(*element.TextAreaElement); ok {
			input.SetValue(next.Value)
			input.SetSelectionRange(next.Cursor, next.Cursor)
		}
		setDraft(next)
	}

	// First Ctrl+C clears the current input
	kitex.UseLayoutEffect(func() {
		if ctrlCPending {
			replaceDraft(DraftEdit{})
		}
	}, []any{ctrlCPending})

	handleKeyDown := func(e event.Event) {
		ke, ok := e.(*event.KeyEvent)
		if !ok || disabled {
			return
		}
		kitex.PostMacro(syncDraft)
		current := readDraft()
		isCurrent := current.Value == auto.Result.Input && current.Cursor == auto.Result.Cursor

		switch {
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
			if auto.Dismiss() {
				return
			}
			if props.OnCancel != nil {
				props.OnCancel()
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

		case ke.MatchString("up"):
			if isCurrent && auto.Up() {
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
			if prevVal, ok := history.Up(current.Value); ok {
				replaceDraft(DraftEdit{Value: prevVal, Cursor: len(prevVal)})
			}

		case ke.MatchString("down"):
			if isCurrent && auto.Down() {
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
				replaceDraft(DraftEdit{Value: nextVal, Cursor: len(nextVal)})
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

	border := style.SingleBorder().Top(true).Bottom(true).Left(false).Right(false)
	if scheme != nil && scheme.Color.Border.Primary != nil {
		border = border.Color(scheme.Color.Border.Primary)
	}

	composerStyle := style.S().
		Border(border).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignStart).
		Width(style.Percent(100))

	promptStyle := style.S().Bold(true).Margin(0, 1, 0, 0)
	if scheme != nil && scheme.Color.Icon.Primary != nil {
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
			}, trail.Text(">")),
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
		uicomponents.Autocomplete(uicomponents.AutocompleteProps{
			State: auto,
			Views: props.CompletionViews,
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
