package ui

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
)

// TranscriptKind differentiates items recorded into scrollback.
type TranscriptKind int

const (
	TranscriptKindHeader TranscriptKind = iota
	TranscriptKindPrompt
	TranscriptKindMessage
	TranscriptKindDiff
	TranscriptKindDecision
	TranscriptKindError
	TranscriptKindNotice
)

// TranscriptItem represents an immutable item logged to terminal scrollback.
type TranscriptItem struct {
	Kind     TranscriptKind
	Header   *HeaderProps
	Text     string
	Message  *api.Message
	Diff     string
	Decision *api.PermissionDecision
	PermReq  *api.PermissionRequest
}

// TranscriptItemView is a trail functional component rendering a single scrollback item.
var TranscriptItemView = trail.FC("TranscriptItem", func(item TranscriptItem) trail.Node {
	switch item.Kind {
	case TranscriptKindHeader:
		if item.Header != nil {
			return Header(*item.Header)
		}
		return nil

	case TranscriptKindPrompt:
		return UserPrompt(UserPromptProps{Text: item.Text})

	case TranscriptKindMessage:
		if item.Message != nil {
			return Message(MessageProps{Message: *item.Message})
		}
		return nil

	case TranscriptKindDiff:
		return Diff(DiffProps{Text: item.Diff})

	case TranscriptKindDecision:
		if item.PermReq != nil && item.Decision != nil {
			return DecisionSummary(DecisionSummaryProps{
				Request:  *item.PermReq,
				Decision: *item.Decision,
			})
		}
		return nil

	case TranscriptKindError:
		return ErrorMessage(ErrorMessageProps{Text: item.Text})

	case TranscriptKindNotice:
		return Notice(NoticeProps{Text: item.Text})

	default:
		return Notice(NoticeProps{Text: item.Text})
	}
})

// Transcript renders a slice of items permanently into terminal scrollback using trail.Static.
func Transcript(items []TranscriptItem) trail.Node {
	return trail.Static(items, func(item TranscriptItem, _ int) trail.Node {
		return TranscriptItemView(item)
	})
}

// HeaderItem creates a header transcript item.
func HeaderItem(h HeaderProps) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindHeader, Header: &h}
}

// PromptItem creates a prompt transcript item.
func PromptItem(text string) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindPrompt, Text: text}
}

// MessageItem creates a message transcript item.
func MessageItem(msg api.Message) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindMessage, Message: &msg}
}

// DiffItem creates a diff transcript item.
func DiffItem(diff string) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindDiff, Diff: diff}
}

// DecisionItem creates a decision transcript item.
func DecisionItem(perm api.PermissionRequest, dec api.PermissionDecision) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindDecision, PermReq: &perm, Decision: &dec}
}

// ErrorItem creates an error transcript item.
func ErrorItem(text string) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindError, Text: text}
}

// NoticeItem creates a notice transcript item.
func NoticeItem(text string) TranscriptItem {
	return TranscriptItem{Kind: TranscriptKindNotice, Text: text}
}
