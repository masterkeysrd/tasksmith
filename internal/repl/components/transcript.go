package components

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// TranscriptEntry is one finalized item of the transcript. Exactly one field
// is set; use the constructors below.
type TranscriptEntry struct {
	Header   *ui.HeaderProps
	Message  *hooks.Message
	Notice   *ui.NoticeProps
	Error    *ui.ErrorMessageProps
	Command  *ui.CommandOutputProps
	Decision *ui.DecisionSummaryProps
	Changes  *ui.ChangesListProps
	Diff     *ui.DiffProps
}

func HeaderEntry(p ui.HeaderProps) TranscriptEntry {
	return TranscriptEntry{Header: &p}
}

func MessageEntry(m hooks.Message) TranscriptEntry {
	return TranscriptEntry{Message: &m}
}

func NoticeEntry(p ui.NoticeProps) TranscriptEntry {
	return TranscriptEntry{Notice: &p}
}

func ErrorEntry(p ui.ErrorMessageProps) TranscriptEntry {
	return TranscriptEntry{Error: &p}
}

func CommandEntry(p ui.CommandOutputProps) TranscriptEntry {
	return TranscriptEntry{Command: &p}
}

func DecisionEntry(p ui.DecisionSummaryProps) TranscriptEntry {
	return TranscriptEntry{Decision: &p}
}

func ChangesEntry(p ui.ChangesListProps) TranscriptEntry {
	return TranscriptEntry{Changes: &p}
}

func DiffEntry(p ui.DiffProps) TranscriptEntry {
	return TranscriptEntry{Diff: &p}
}

// TranscriptProps defines the properties for the Transcript component.
type TranscriptProps struct {
	// Entries are finalized items in print order. The list may only grow;
	// each entry is written to terminal scrollback exactly once. Remount the
	// component (change its key) to start a new transcript.
	Entries []TranscriptEntry
}

// Transcript is the single place that permanently prints finalized items into
// terminal scrollback: the header, conversation messages, notices, errors,
// command output and decision summaries.
var Transcript = trail.FC("Transcript", func(props TranscriptProps) trail.Node {
	return trail.Static(props.Entries, func(e TranscriptEntry, _ int) trail.Node {
		switch {
		case e.Header != nil:
			return ui.Header(*e.Header)
		case e.Message != nil:
			return Message(MessageProps{Message: *e.Message})
		case e.Notice != nil:
			return ui.Notice(*e.Notice)
		case e.Error != nil:
			return ui.ErrorMessage(*e.Error)
		case e.Command != nil:
			return ui.CommandOutput(*e.Command)
		case e.Decision != nil:
			return ui.DecisionSummary(*e.Decision)
		case e.Changes != nil:
			return ui.ChangesList(*e.Changes)
		case e.Diff != nil:
			return ui.Diff(*e.Diff)
		}
		return nil
	})
})
