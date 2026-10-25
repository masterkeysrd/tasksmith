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

// TranscriptState holds the current transcript epoch and finalized items.
type TranscriptState struct {
	Epoch int
	Items *[]TranscriptItem
}

var transcript = kites.Create(TranscriptState{})

// UseTranscript subscribes a component to a comparable slice of TranscriptState.
func UseTranscript[U comparable](selector func(TranscriptState) U) U {
	return kites.Use(transcript, selector)
}

// GetTranscript returns the current TranscriptState snapshot.
func GetTranscript() TranscriptState {
	return transcript.Get()
}

// AppendTranscript appends items to the transcript scrollback. It allocates a new
// slice copy so Items gets a new pointer for kites.Use. If an incoming item has
// an ID != "" that already exists in Items, it is skipped to avoid duplicates within
// the current epoch.
func AppendTranscript(items ...TranscriptItem) {
	if len(items) == 0 {
		return
	}
	transcript.Set(func(s TranscriptState) TranscriptState {
		var current []TranscriptItem
		if s.Items != nil {
			current = make([]TranscriptItem, len(*s.Items), len(*s.Items)+len(items))
			copy(current, *s.Items)
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
			Epoch: s.Epoch,
			Items: itemsPtr,
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

// SyncHeaderOrClear synchronizes the header banner with model and thinking.
// It only increments Epoch (clearing scrollback) if either:
// 1) The transcript is dirty (has items other than just the header) and forceIfDirty is true, OR
// 2) The model label or thinking differs from the current header banner.
// On initial startup before the header has model/thinking populated, it updates in place without incrementing Epoch.
func SyncHeaderOrClear(model, thinking string, forceIfDirty bool) {
	transcript.Set(func(s TranscriptState) TranscriptState {
		if s.Items == nil || len(*s.Items) == 0 {
			return s
		}
		items := *s.Items
		headerIdx := -1
		var currentHeader HeaderData
		for i, item := range items {
			if item.Kind == TranscriptKindHeader {
				if hd, ok := item.Data.(HeaderData); ok {
					headerIdx = i
					currentHeader = hd
					break
				}
			}
		}
		if headerIdx == -1 {
			return s
		}

		if currentHeader.Model == "" && currentHeader.Thinking == "" {
			currentHeader.Model = model
			currentHeader.Thinking = thinking
			updatedHeaderItem := TranscriptItem{
				ID:   items[headerIdx].ID,
				Kind: TranscriptKindHeader,
				Data: currentHeader,
			}
			cp := make([]TranscriptItem, len(items))
			copy(cp, items)
			cp[headerIdx] = updatedHeaderItem
			return TranscriptState{
				Epoch: s.Epoch,
				Items: &cp,
			}
		}

		isClean := len(items) == 1 && headerIdx == 0
		modelChanged := currentHeader.Model != model || currentHeader.Thinking != thinking
		shouldClear := (forceIfDirty && !isClean) || modelChanged
		if !shouldClear {
			return s
		}

		currentHeader.Model = model
		currentHeader.Thinking = thinking
		updatedHeaderItem := TranscriptItem{
			ID:   items[headerIdx].ID,
			Kind: TranscriptKindHeader,
			Data: currentHeader,
		}
		cp := []TranscriptItem{updatedHeaderItem}
		return TranscriptState{
			Epoch: s.Epoch + 1,
			Items: &cp,
		}
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
			Epoch: s.Epoch + 1,
			Items: itemsPtr,
		}
	})
}

// ClearScreen clears the terminal transcript while preserving the header banner item.
// If the transcript is already clean (only header present or empty), it does not increment Epoch.
func ClearScreen() {
	s := GetTranscript()
	if s.Items == nil || len(*s.Items) == 0 || (len(*s.Items) == 1 && (*s.Items)[0].Kind == TranscriptKindHeader) {
		return
	}
	var headers []TranscriptItem
	for _, item := range *s.Items {
		if item.Kind == TranscriptKindHeader {
			headers = append(headers, item)
		}
	}
	ResetTranscript(headers...)
}

// ClearTranscript resets TranscriptState to Epoch 0 and nil Items.
func ClearTranscript() {
	transcript.Set(func(TranscriptState) TranscriptState {
		return TranscriptState{
			Epoch: 0,
			Items: nil,
		}
	})
}
