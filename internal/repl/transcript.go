package repl

import (
	"fmt"
	"slices"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// TranscriptControl reads and extends the transcript: the append-only log of
// everything printed into terminal scrollback.
type TranscriptControl struct {
	Entries []components.TranscriptEntry
	// Append adds a local entry such as an error, notice or command output.
	Append func(components.TranscriptEntry)
	// Clear starts a new transcript with a fresh header. Messages already on
	// screen are not printed again.
	Clear func()
}

var transcriptContext = kitex.CreateContext[*TranscriptControl](nil)

// UseTranscript returns the transcript of the active session.
func UseTranscript() TranscriptControl {
	control := kitex.UseContext(transcriptContext)
	if control == nil {
		panic("repl: UseTranscript must be used within a TranscriptProvider")
	}
	return *control
}

// TranscriptProviderProps configures [TranscriptProvider].
type TranscriptProviderProps struct {
	Version   string
	Workspace string

	Children []trail.Node // injected by trail.FCC
}

type transcriptRestart struct {
	Epoch   int
	N       int
	Through string
}

// TranscriptProvider keeps the transcript of the active session. Its state
// lives in a child that is remounted, and so reset, whenever the session
// changes or the transcript is cleared. It requires the session and
// conversation providers above it.
var TranscriptProvider = trail.FCC("TranscriptProvider", func(props TranscriptProviderProps) trail.Node {
	epoch := uihooks.UseSessionEpoch()
	restart, setRestart := kitex.UseState(transcriptRestart{})

	current := restart()
	if current.Epoch != epoch {
		current = transcriptRestart{Epoch: epoch}
	}
	return trail.Box(trail.BoxProps{
		Key: fmt.Sprintf("%d.%d", epoch, current.N),
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, transcriptScope(transcriptScopeProps{
		Version:   props.Version,
		Workspace: props.Workspace,
		Through:   current.Through,
		Restart: func(through string) {
			setRestart(transcriptRestart{Epoch: epoch, N: current.N + 1, Through: through})
		},
	}, props.Children...))
})

type transcriptScopeProps struct {
	Version   string
	Workspace string
	// Through is the ID of the last message that must not be printed again.
	Through string
	Restart func(through string)

	Children []trail.Node // injected by trail.FCC
}

// transcriptScope owns the entries of one transcript. It prints the header
// once the model is known, then every finalized conversation message. Message
// IDs are time-sortable (UUIDv7) and arrive in order, so a high-water mark of
// the last printed ID is enough to avoid printing a message twice.
var transcriptScope = trail.FCC("TranscriptScope", func(props transcriptScopeProps) trail.Node {
	info := uihooks.UseModelInfo()
	session := uihooks.UseSession()
	conversation := uihooks.UseCurrentConversation()

	entries, setEntries := kitex.UseState([]components.TranscriptEntry(nil))
	headerPrinted := kitex.UseRef(false)
	printed := kitex.UseRef(props.Through)
	newest := kitex.UseRef("")
	if n := len(conversation.History); n > 0 {
		newest.Current = conversation.History[n-1].SourceID
	} else {
		newest.Current = ""
	}

	kitex.UseLayoutEffect(func() {
		var added []components.TranscriptEntry
		if !headerPrinted.Current {
			if !info.Ready {
				return
			}
			headerPrinted.Current = true
			added = append(added, components.HeaderEntry(components.HeaderProps{
				Version:  props.Version,
				Model:    info.Label,
				Thinking: info.Thinking,
				Path:     props.Workspace,
			}))
		}
		for _, message := range conversation.History {
			if message.SourceID > printed.Current {
				added = append(added, components.MessageEntry(message))
				printed.Current = message.SourceID
			}
		}
		if len(added) > 0 {
			setEntries(append(slices.Clone(entries()), added...))
		}
	}, []any{info.Ready, session.ID, len(conversation.History)})

	control := &TranscriptControl{
		Entries: entries(),
		Append: func(entry components.TranscriptEntry) {
			setEntries(append(slices.Clone(entries()), entry))
		},
		Clear: func() { props.Restart(newest.Current) },
	}
	return transcriptContext.Provider(control, props.Children...)
})

// TranscriptOutput prints the transcript into terminal scrollback.
var TranscriptOutput = trail.SimpleFC("TranscriptOutput", func() trail.Node {
	return components.Transcript(components.TranscriptProps{Entries: UseTranscript().Entries})
})
