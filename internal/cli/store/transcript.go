package store

import (
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
)

// TranscriptKind identifies the kind of item in the transcript scrollback.
type TranscriptKind string

const (
	TranscriptKindHeader   TranscriptKind = "header"
	TranscriptKindTimeline TranscriptKind = "timeline"
	TranscriptKindNotice   TranscriptKind = "notice"
	TranscriptKindError    TranscriptKind = "error"
	TranscriptKindCommand  TranscriptKind = "command"
	TranscriptKindMessage  TranscriptKind = "message"
)

// HeaderData holds the data for the startup banner.
type HeaderData struct {
	Version   string
	Model     string
	Thinking  string
	Workspace string
	Warnings  []string
}

// NoticeData holds a local informational notice.
type NoticeData struct {
	Text string
}

// ErrorData holds a local or turn error message.
type ErrorData struct {
	Title   string
	Message string
}

// CommandData holds the output of a local slash command.
type CommandData struct {
	Command string
	Output  string
}

// TranscriptItem represents a single finalized item in the transcript scrollback.
type TranscriptItem struct {
	ID   string
	Kind TranscriptKind
	Data any
}

// TranscriptState holds the current transcript epoch, frozen status, and finalized items.
type TranscriptState struct {
	Epoch  int
	Frozen bool
	Items  *[]TranscriptItem
}

var transcript = kites.Create(TranscriptState{})

// CurrentHeaderData returns a snapshot of HeaderData combining AppState metadata
// with the active session's ModelLabel and Thinking effort.
func CurrentHeaderData() HeaderData {
	appState := GetApp()
	sess := GetSession()
	var warnings []string
	if appState.Warnings != nil {
		warnings = make([]string, len(*appState.Warnings))
		copy(warnings, *appState.Warnings)
	}
	return HeaderData{
		Version:   appState.Version,
		Workspace: appState.Workspace,
		Model:     sess.ModelLabel,
		Thinking:  sess.Thinking,
		Warnings:  warnings,
	}
}

type headerAppMeta struct {
	version   string
	workspace string
	warnings  *[]string
}

// UseHeaderData returns reactive HeaderData subscribed to changes in AppState metadata,
// ModelLabel, and Thinking effort.
func UseHeaderData() HeaderData {
	appMeta := UseApp(func(s AppState) headerAppMeta {
		return headerAppMeta{
			version:   s.Version,
			workspace: s.Workspace,
			warnings:  s.Warnings,
		}
	})
	sessionInfo := UseSession(func(s SessionState) [2]string {
		return [2]string{s.ModelLabel, s.Thinking}
	})

	var warnings []string
	if appMeta.warnings != nil {
		warnings = make([]string, len(*appMeta.warnings))
		copy(warnings, *appMeta.warnings)
	}

	return HeaderData{
		Version:   appMeta.version,
		Workspace: appMeta.workspace,
		Model:     sessionInfo[0],
		Thinking:  sessionInfo[1],
		Warnings:  warnings,
	}
}

// UseTranscript subscribes a component to a comparable slice of TranscriptState.
func UseTranscript[U comparable](selector func(TranscriptState) U) U {
	return kites.Use(transcript, selector)
}

// GetTranscript returns the current TranscriptState snapshot.
func GetTranscript() TranscriptState {
	return transcript.Get()
}

// AppendTranscript appends items to the transcript. It allocates a new
// slice copy so Items gets a new pointer for kites.Use. If an incoming item has
// an ID != "" that already exists in Items, it is skipped to avoid duplicates within
// the current epoch. In frozen mode, if the transcript is currently empty and the
// first item being appended is not a Header, a snapshot HeaderItem is prepended.
// In draft mode (!Frozen), items are simply appended without a Header item.
func AppendTranscript(items ...TranscriptItem) {
	if len(items) == 0 {
		return
	}
	transcript.Set(func(s TranscriptState) TranscriptState {
		var current []TranscriptItem
		if s.Items != nil {
			current = make([]TranscriptItem, len(*s.Items), len(*s.Items)+len(items)+1)
			copy(current, *s.Items)
		} else {
			current = make([]TranscriptItem, 0, len(items)+1)
		}

		if s.Frozen && len(current) == 0 && len(items) > 0 && items[0].Kind != TranscriptKindHeader {
			current = append(current, TranscriptItem{
				ID:   "header",
				Kind: TranscriptKindHeader,
				Data: CurrentHeaderData(),
			})
		}

		for _, item := range items {
			if item.ID != "" {
				duplicate := false
				for _, existing := range current {
					if existing.ID == item.ID {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
			}
			current = append(current, item)
		}
		var itemsPtr *[]TranscriptItem
		if len(current) > 0 {
			itemsPtr = &current
		}
		return TranscriptState{
			Epoch:  s.Epoch,
			Frozen: s.Frozen,
			Items:  itemsPtr,
		}
	})
}

// FreezeTranscript transitions the transcript from live draft mode to frozen static scrollback.
// If items does not already start with a Header item, one is prepended with CurrentHeaderData().
func FreezeTranscript() {
	transcript.Set(func(s TranscriptState) TranscriptState {
		if s.Frozen {
			return s
		}
		var current []TranscriptItem
		if s.Items != nil {
			current = make([]TranscriptItem, len(*s.Items))
			copy(current, *s.Items)
		}
		hasHeader := len(current) > 0 && current[0].Kind == TranscriptKindHeader
		if !hasHeader {
			hdr := TranscriptItem{
				ID:   "header",
				Kind: TranscriptKindHeader,
				Data: CurrentHeaderData(),
			}
			current = append([]TranscriptItem{hdr}, current...)
		}
		var itemsPtr *[]TranscriptItem
		if len(current) > 0 {
			itemsPtr = &current
		}
		return TranscriptState{
			Epoch:  s.Epoch,
			Frozen: true,
			Items:  itemsPtr,
		}
	})
}

// ResetToDraft resets the transcript back to draft mode.
// If the transcript was frozen, Epoch is incremented to clear terminal scrollback.
// If it was not frozen (draft), Epoch is preserved and items are simply cleared.
func ResetToDraft() {
	transcript.Set(func(s TranscriptState) TranscriptState {
		epoch := s.Epoch
		if s.Frozen {
			epoch++
		}
		return TranscriptState{
			Epoch:  epoch,
			Frozen: false,
			Items:  nil,
		}
	})
}

// ResetForSessionSwitch prepares the transcript for a loaded or resumed session.
// It snapshots the Header and enters Frozen mode. If previously frozen, it increments Epoch.
func ResetForSessionSwitch() {
	hdr := TranscriptItem{
		ID:   "header",
		Kind: TranscriptKindHeader,
		Data: CurrentHeaderData(),
	}
	items := []TranscriptItem{hdr}
	transcript.Set(func(s TranscriptState) TranscriptState {
		epoch := s.Epoch
		if s.Frozen {
			epoch++
		}
		return TranscriptState{
			Epoch:  epoch,
			Frozen: true,
			Items:  &items,
		}
	})
}

// AppendHeader appends a header item to the transcript scrollback.
func AppendHeader(d HeaderData) {
	AppendTranscript(TranscriptItem{
		ID:   "header",
		Kind: TranscriptKindHeader,
		Data: d,
	})
}

// AppendTimelineEntry appends an API timeline entry to the transcript scrollback.
func AppendTimelineEntry(e api.TimelineEntry) {
	AppendTranscript(TranscriptItem{
		ID:   e.ID,
		Kind: TranscriptKindTimeline,
		Data: e,
	})
}

// AppendMessage appends a finalized message to the transcript scrollback.
func AppendMessage(msg Message) {
	AppendTranscript(TranscriptItem{
		ID:   msg.ID,
		Kind: TranscriptKindMessage,
		Data: msg,
	})
}

// AppendNotice appends a local informational notice to the transcript scrollback.
func AppendNotice(text string) {
	AppendTranscript(TranscriptItem{
		Kind: TranscriptKindNotice,
		Data: NoticeData{Text: text},
	})
}

// AppendError appends a local or turn error message to the transcript scrollback.
func AppendError(title, msg string) {
	AppendTranscript(TranscriptItem{
		Kind: TranscriptKindError,
		Data: ErrorData{Title: title, Message: msg},
	})
}

// AppendCommandOutput appends the output of a local slash command to the transcript scrollback.
func AppendCommandOutput(cmd, out string) {
	AppendTranscript(TranscriptItem{
		Kind: TranscriptKindCommand,
		Data: CommandData{Command: cmd, Output: out},
	})
}

// ResetTranscript increments Epoch by 1 and sets Items to a copy of initial,
// or nil if len(initial) == 0.
func ResetTranscript(initial ...TranscriptItem) {
	transcript.Set(func(s TranscriptState) TranscriptState {
		var itemsPtr *[]TranscriptItem
		if len(initial) > 0 {
			cp := make([]TranscriptItem, len(initial))
			copy(cp, initial)
			itemsPtr = &cp
		}
		return TranscriptState{
			Epoch:  s.Epoch + 1,
			Frozen: s.Frozen,
			Items:  itemsPtr,
		}
	})
}

// ClearScreen clears the transcript screen.
// In draft mode (!Frozen), it simply clears draft items without incrementing Epoch.
// In frozen mode, it clears terminal scrollback and repaints only the header banner.
func ClearScreen() {
	transcript.Set(func(s TranscriptState) TranscriptState {
		if !s.Frozen {
			return TranscriptState{
				Epoch:  s.Epoch,
				Frozen: false,
				Items:  nil,
			}
		}

		if s.Items != nil && len(*s.Items) == 1 && (*s.Items)[0].Kind == TranscriptKindHeader {
			return s
		}

		hdr := TranscriptItem{
			ID:   "header",
			Kind: TranscriptKindHeader,
			Data: CurrentHeaderData(),
		}
		items := []TranscriptItem{hdr}
		return TranscriptState{
			Epoch:  s.Epoch + 1,
			Frozen: true,
			Items:  &items,
		}
	})
}

// ClearTranscript resets TranscriptState to Epoch 0, Frozen false, and nil Items.
func ClearTranscript() {
	transcript.Set(func(TranscriptState) TranscriptState {
		return TranscriptState{
			Epoch:  0,
			Frozen: false,
			Items:  nil,
		}
	})
}
