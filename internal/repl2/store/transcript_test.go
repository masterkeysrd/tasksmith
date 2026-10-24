package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestTranscriptBasic(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	state := GetTranscript()
	if state.Epoch != 0 || state.Items != nil {
		t.Fatalf("expected initial state Epoch=0, Items=nil, got Epoch=%d, Items=%v", state.Epoch, state.Items)
	}

	headerData := HeaderData{
		Version:   "v0.1.0",
		Model:     "gpt-4",
		Thinking:  "medium",
		Workspace: "/test/ws",
	}
	AppendHeader(headerData)

	state = GetTranscript()
	if state.Items == nil || len(*state.Items) != 1 {
		t.Fatalf("expected 1 item, got %v", state.Items)
	}
	item := (*state.Items)[0]
	if item.ID != "header" || item.Kind != TranscriptKindHeader {
		t.Fatalf("unexpected header item: %+v", item)
	}
	if !reflect.DeepEqual(item.Data, headerData) {
		t.Fatalf("expected header data %+v, got %+v", headerData, item.Data)
	}

	// Appending header again should be deduplicated
	AppendHeader(headerData)
	state = GetTranscript()
	if len(*state.Items) != 1 {
		t.Fatalf("expected duplicate header to be skipped, got %d items", len(*state.Items))
	}
}

func TestTranscriptAppendTimelineEntry(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	entry1 := api.TimelineEntry{
		ID:        "tl-1",
		SessionID: "s-1",
	}
	entry2 := api.TimelineEntry{
		ID:        "tl-2",
		SessionID: "s-1",
	}

	AppendTimelineEntry(entry1)
	AppendTimelineEntry(entry2)
	// Duplicate of entry1
	AppendTimelineEntry(entry1)

	state := GetTranscript()
	if state.Items == nil || len(*state.Items) != 2 {
		t.Fatalf("expected 2 items after deduplication, got %d", len(*state.Items))
	}
	if (*state.Items)[0].ID != "tl-1" || (*state.Items)[1].ID != "tl-2" {
		t.Fatalf("unexpected items: %+v", *state.Items)
	}
}

func TestTranscriptNoticesErrorsCommands(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	AppendNotice("notice 1")
	AppendNotice("notice 2")
	AppendError("err title", "err msg")
	AppendCommandOutput("/cmd", "output line")

	state := GetTranscript()
	if state.Items == nil || len(*state.Items) != 4 {
		t.Fatalf("expected 4 items, got %v", state.Items)
	}

	items := *state.Items
	if items[0].Kind != TranscriptKindNotice || items[0].Data.(NoticeData).Text != "notice 1" {
		t.Fatalf("unexpected notice 1: %+v", items[0])
	}
	if items[1].Kind != TranscriptKindNotice || items[1].Data.(NoticeData).Text != "notice 2" {
		t.Fatalf("unexpected notice 2: %+v", items[1])
	}
	if items[2].Kind != TranscriptKindError || items[2].Data.(ErrorData).Title != "err title" {
		t.Fatalf("unexpected error item: %+v", items[2])
	}
	if items[3].Kind != TranscriptKindCommand || items[3].Data.(CommandData).Command != "/cmd" {
		t.Fatalf("unexpected command item: %+v", items[3])
	}
}

func TestTranscriptPointerImmutability(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	AppendNotice("item 1")
	ptr1 := GetTranscript().Items

	AppendNotice("item 2")
	ptr2 := GetTranscript().Items

	if ptr1 == ptr2 {
		t.Fatal("expected Items pointer to change on append for kites.Use comparability")
	}
	if len(*ptr1) != 1 || len(*ptr2) != 2 {
		t.Fatalf("slice lengths mismatch: ptr1=%d, ptr2=%d", len(*ptr1), len(*ptr2))
	}
}

func TestTranscriptResetAndClear(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	AppendNotice("item 1")
	initialState := GetTranscript()
	if initialState.Epoch != 0 || len(*initialState.Items) != 1 {
		t.Fatalf("unexpected initial state: %+v", initialState)
	}

	initItem := TranscriptItem{
		ID:   "init-1",
		Kind: TranscriptKindNotice,
		Data: NoticeData{Text: "init text"},
	}
	ResetTranscript(initItem)

	resetState := GetTranscript()
	if resetState.Epoch != 1 {
		t.Fatalf("expected Epoch=1, got %d", resetState.Epoch)
	}
	if resetState.Items == nil || len(*resetState.Items) != 1 {
		t.Fatalf("expected 1 item after reset, got %v", resetState.Items)
	}
	if (*resetState.Items)[0].ID != "init-1" {
		t.Fatalf("unexpected item after reset: %+v", (*resetState.Items)[0])
	}

	// Reset with empty items
	ResetTranscript()
	emptyResetState := GetTranscript()
	if emptyResetState.Epoch != 2 {
		t.Fatalf("expected Epoch=2, got %d", emptyResetState.Epoch)
	}
	if emptyResetState.Items != nil {
		t.Fatalf("expected nil Items after empty reset, got %v", emptyResetState.Items)
	}

	// ClearTranscript resets Epoch to 0 and Items to nil
	ClearTranscript()
	clearedState := GetTranscript()
	if clearedState.Epoch != 0 || clearedState.Items != nil {
		t.Fatalf("expected cleared state Epoch=0, Items=nil, got Epoch=%d, Items=%v", clearedState.Epoch, clearedState.Items)
	}
}

func TestUseTranscript(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	var renderedEpoch int
	Comp := trail.SimpleFC("Comp", func() trail.Node {
		renderedEpoch = UseTranscript(func(s TranscriptState) int {
			return s.Epoch
		})
		return trail.Text("ok")
	})

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Comp(), trail.WithBackend(b))
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.LastFrame().Surface != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if renderedEpoch != 0 {
		t.Fatalf("expected renderedEpoch 0, got %d", renderedEpoch)
	}

	ResetTranscript()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if renderedEpoch == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if renderedEpoch != 1 {
		t.Fatalf("expected renderedEpoch to update to 1, got %d", renderedEpoch)
	}
}
