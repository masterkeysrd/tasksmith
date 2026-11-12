package components

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/mentions"
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

var composerAutocompleteBindings = []uicomponents.KeyBinding{
	{Key: "↑/↓", Label: "Navigate"},
	{Key: "Ctrl+P/Ctrl+N", Label: "Navigate"},
	{Key: "Tab", Label: "Select"},
	{Key: "Esc", Label: "Dismiss"},
}

// ComposerProps defines the properties for the Composer functional component.
type ComposerProps struct {
	Disabled        bool
	OnSubmit        func(text string, attachments []api.Attachment)
	OnCycleMode     func()
	OnCancel        func()
	Autocomplete    *autocomplete.Engine
	CompletionViews map[string]uicomponents.AutocompleteView
}

// DraftEdit holds the current draft value and cursor offset.
type DraftEdit = uicomponents.AutocompleteEdit

// HistoryEntry holds a prompt's text and its structured attachments.
type HistoryEntry struct {
	Text        string           `json:"text"`
	Attachments []api.Attachment `json:"attachments,omitempty"`
}

// UnmarshalJSON unmarshals either a rich HistoryEntry object or a legacy JSON string.
func (h *HistoryEntry) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		h.Text = s
		h.Attachments = nil
		return nil
	}
	type rawEntry HistoryEntry
	var raw rawEntry
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return err
	}
	*h = HistoryEntry(raw)
	return nil
}

// PromptHistory holds state and operations for traversing input prompt history.
type PromptHistory struct {
	History    []HistoryEntry
	HistoryIdx int
	Draft      HistoryEntry
	Browsing   func() bool
	Up         func(current HistoryEntry) (HistoryEntry, bool)
	Down       func() (HistoryEntry, bool)
	Add        func(entry HistoryEntry)
	Cancel     func() HistoryEntry
	Exit       func(entry HistoryEntry)
}

// usePromptHistory manages input prompt history navigation, draft preservation,
// and disk persistence of submitted prompts.
func usePromptHistory() PromptHistory {
	history, setHistory := kitex.UseState([]HistoryEntry(nil))
	historyIdx, setHistoryIdx := kitex.UseState(0)
	draft, setDraft := kitex.UseState(HistoryEntry{})
	browsing, setBrowsing := kitex.UseState(false)

	// Load persisted history from disk on first mount.
	kitex.UseLayoutEffect(func() {
		if entries := loadHistoryFromFile(); entries != nil {
			setHistory(entries)
			setHistoryIdx(len(entries))
		}
	}, []any{})

	up := func(current HistoryEntry) (HistoryEntry, bool) {
		hist := history()
		if len(hist) == 0 {
			return HistoryEntry{}, false
		}
		hIdx := historyIdx()
		if hIdx >= len(hist) {
			setDraft(current)
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
		return HistoryEntry{}, false
	}

	down := func() (HistoryEntry, bool) {
		hist := history()
		if len(hist) == 0 {
			return HistoryEntry{}, false
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
		return HistoryEntry{}, false
	}

	add := func(entry HistoryEntry) {
		hist := history()
		// Skip consecutive duplicates (by text).
		if len(hist) > 0 && hist[len(hist)-1].Text == entry.Text {
			setHistoryIdx(len(hist))
			setDraft(HistoryEntry{})
			setBrowsing(false)
			return
		}
		hist = append(hist, entry)
		// Cap at maxHistoryEntries (keep most recent).
		if len(hist) > maxHistoryEntries {
			hist = hist[len(hist)-maxHistoryEntries:]
		}
		setHistory(hist)
		setHistoryIdx(len(hist))
		setDraft(HistoryEntry{})
		setBrowsing(false)
		// Fail-safe: disk errors must not crash the REPL.
		_ = persistHistory(hist)
	}

	// cancel abandons browsing and restores the pre-navigation draft.
	cancel := func() HistoryEntry {
		restored := draft()
		setHistoryIdx(len(history()))
		setBrowsing(false)
		return restored
	}

	// exit leaves browsing mode after active editing, adopting the edited
	// entry as the new working draft.
	exit := func(entry HistoryEntry) {
		setHistoryIdx(len(history()))
		setDraft(entry)
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
func loadHistoryFromFile() []HistoryEntry {
	path, err := xdg.HistoryFilePath("repl.jsonl")
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // gracefully handle missing file
	}
	var entries []HistoryEntry
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry HistoryEntry
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
// Each entry is encoded as a JSON object on its own line.
func persistHistory(entries []HistoryEntry) error {
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
	attachments, setAttachments := kitex.UseState([]api.Attachment(nil))
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

	recordCandidate := func(idx int) []api.Attachment {
		currentAtts := attachments()
		if idx < 0 || idx >= len(auto.Result.Candidates) {
			return currentAtts
		}
		newAtt, ok := candidateAttachment(auto.Result.Candidates[idx])
		if !ok {
			return currentAtts
		}
		next := slices.Clone(currentAtts)
		if attIdx := slices.IndexFunc(next, func(a api.Attachment) bool { return a.Ref == newAtt.Ref }); attIdx >= 0 {
			next[attIdx] = newAtt
		} else {
			next = append(next, newAtt)
		}
		setAttachments(next)
		return next
	}

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
			history.Exit(HistoryEntry{Text: next.Value, Attachments: slices.Clone(attachments())})
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
	recall := func(entry HistoryEntry) {
		setAttachments(slices.Clone(entry.Attachments))
		value := entry.Text
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
			setAttachments(nil)
			if bashMode() {
				setBashMode(false)
				store.SetHint(store.DefaultHint)
			}
			if history.Browsing() {
				history.Exit(HistoryEntry{})
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
					candIdx := auto.Selected
					if edit, err := auto.Accept(); err == nil {
						recordCandidate(candIdx)
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
			entryToPass := HistoryEntry{
				Text:        valToPass,
				Attachments: slices.Clone(attachments()),
			}
			if prevEntry, ok := history.Up(entryToPass); ok {
				recall(prevEntry)
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
			if nextEntry, ok := history.Down(); ok {
				recall(nextEntry)
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
				setAttachments(nil)
				setBashMode(false)
				store.SetHint(store.DefaultHint)
				if text != "" {
					history.Add(HistoryEntry{Text: "!" + text})
					if props.OnSubmit != nil {
						props.OnSubmit("!"+text, nil)
					}
				}
				return
			}
			activeAtts := attachments()
			if isCurrent && auto.IsOpen {
				if auto.IsLoading || auto.Error != nil || len(auto.Result.Candidates) == 0 {
					return
				}
				if !selectionMatchesQuery(auto) {
					candIdx := auto.Selected
					if edit, err := auto.Accept(); err == nil {
						recordCandidate(candIdx)
						replaceDraft(edit)
					}
					return
				}
				activeAtts = recordCandidate(auto.Selected)
			}

			text := strings.TrimSpace(current.Value)
			reconciled := mentions.Resolve(text, activeAtts, nil)
			replaceDraft(DraftEdit{})
			setAttachments(nil)
			if text != "" {
				history.Add(HistoryEntry{Text: text, Attachments: reconciled})
				if props.OnSubmit != nil {
					props.OnSubmit(text, reconciled)
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
			State:    auto,
			Views:    props.CompletionViews,
			Bindings: composerAutocompleteBindings,
		}),
	)
})

func candidateAttachment(cand autocomplete.Candidate) (api.Attachment, bool) {
	switch data := cand.Data.(type) {
	case api.WorkspaceFile:
		target := data.Path
		if data.IsDir && !strings.HasSuffix(target, "/") {
			target += "/"
		}
		return api.Attachment{
			Kind:   api.AttachmentFile,
			Ref:    "@" + strings.TrimSpace(cand.InsertText),
			Target: target,
		}, true
	case api.Rule:
		return api.Attachment{
			Kind:   api.AttachmentRule,
			Ref:    "@rule:" + data.Name,
			Target: data.Name,
		}, true
	case api.Skill:
		return api.Attachment{
			Kind:   api.AttachmentSkill,
			Ref:    "@skill:" + data.Name,
			Target: data.Name,
		}, true
	default:
		return api.Attachment{}, false
	}
}

func selectionMatchesQuery(auto uicomponents.AutocompleteState) bool {
	candidates := auto.Result.Candidates
	if auto.Selected < 0 || auto.Selected >= len(candidates) || auto.Result.Query == "" {
		return false
	}
	candidate := candidates[auto.Selected]
	if auto.Result.Source == "files" {
		raw, _ := strings.CutPrefix(auto.Result.Query, "file:")
		raw = strings.Trim(raw, "\"'")
		if raw == "" {
			return false
		}
		if wf, ok := candidate.Data.(api.WorkspaceFile); ok {
			if wf.ShortPath != "" && raw == wf.ShortPath {
				return true
			}
			target := wf.Path
			if wf.IsDir && !strings.HasSuffix(target, "/") {
				target += "/"
			}
			if raw == target || raw == wf.Path {
				return true
			}
		}
		return raw == candidate.Value || strings.TrimSuffix(raw, "/") == strings.TrimSuffix(candidate.Value, "/")
	}
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
