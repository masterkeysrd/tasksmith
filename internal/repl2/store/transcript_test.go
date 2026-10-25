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

	// 1. In draft mode (!Frozen), timeline entries are appended without auto-prepended header
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
		t.Fatalf("expected 2 items in draft after deduplication, got %d", len(*state.Items))
	}
	if (*state.Items)[0].ID != "tl-1" || (*state.Items)[1].ID != "tl-2" {
		t.Fatalf("unexpected items: %+v", *state.Items)
	}

	// 2. In frozen mode, appending to an empty transcript auto-prepends header
	ClearTranscript()
	FreezeTranscript()
	AppendTimelineEntry(entry1)
	AppendTimelineEntry(entry2)

	state = GetTranscript()
	if state.Items == nil || len(*state.Items) != 3 {
		t.Fatalf("expected 3 items in frozen mode (header + 2 timeline), got %d", len(*state.Items))
	}
	if (*state.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected item 0 to be header, got %+v", (*state.Items)[0])
	}
	if (*state.Items)[1].ID != "tl-1" || (*state.Items)[2].ID != "tl-2" {
		t.Fatalf("unexpected items in frozen mode: %+v", *state.Items)
	}
}

func TestTranscriptNoticesErrorsCommands(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	// In draft mode (!Frozen), items are appended directly without prepending a header
	AppendNotice("notice 1")
	AppendNotice("notice 2")
	AppendError("err title", "err msg")
	AppendCommandOutput("/cmd", "output line")

	state := GetTranscript()
	if state.Items == nil || len(*state.Items) != 4 {
		t.Fatalf("expected 4 items in draft mode, got %v", state.Items)
	}
	if state.Frozen {
		t.Fatal("expected transcript to remain unfrozen (draft mode)")
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

	// FreezeTranscript transitions to Frozen and prepends snapshot Header
	FreezeTranscript()
	state = GetTranscript()
	if !state.Frozen {
		t.Fatal("expected transcript to be Frozen after FreezeTranscript")
	}
	if state.Items == nil || len(*state.Items) != 5 {
		t.Fatalf("expected 5 items after FreezeTranscript (header + 4 items), got %d", len(*state.Items))
	}
	if (*state.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected item 0 to be header after FreezeTranscript, got %+v", (*state.Items)[0])
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
	if clearedState.Epoch != 0 || clearedState.Frozen || clearedState.Items != nil {
		t.Fatalf("expected cleared state Epoch=0, Frozen=false, Items=nil, got %+v", clearedState)
	}
}

func TestResetToDraft(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	// 1. ResetToDraft from draft preserves Epoch and clears Items
	AppendNotice("draft item")
	if GetTranscript().Epoch != 0 || len(*GetTranscript().Items) != 1 {
		t.Fatalf("unexpected state before ResetToDraft")
	}
	ResetToDraft()
	st := GetTranscript()
	if st.Epoch != 0 || st.Frozen || st.Items != nil {
		t.Fatalf("expected Epoch=0, Frozen=false, Items=nil from draft ResetToDraft, got %+v", st)
	}

	// 2. ResetToDraft from frozen increments Epoch, sets Frozen=false and Items=nil
	FreezeTranscript()
	AppendNotice("frozen item")
	if !GetTranscript().Frozen {
		t.Fatalf("expected Frozen=true")
	}
	ResetToDraft()
	st = GetTranscript()
	if st.Epoch != 1 || st.Frozen || st.Items != nil {
		t.Fatalf("expected Epoch=1, Frozen=false, Items=nil from frozen ResetToDraft, got %+v", st)
	}
}

func TestResetForSessionSwitch(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	SetHeaderMeta("1.0.0", "/ws/switch")
	SetModelInfo("switch-model", "high")

	// 1. From draft mode: sets Frozen=true, Items=&[Header], preserves Epoch
	ResetForSessionSwitch()
	st := GetTranscript()
	if st.Epoch != 0 || !st.Frozen || st.Items == nil || len(*st.Items) != 1 {
		t.Fatalf("expected Epoch=0, Frozen=true, 1 Header item, got %+v", st)
	}
	if (*st.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected Header item at index 0")
	}

	// 2. From frozen mode: increments Epoch, sets Frozen=true, Items=&[Header]
	ResetForSessionSwitch()
	st = GetTranscript()
	if st.Epoch != 1 || !st.Frozen || st.Items == nil || len(*st.Items) != 1 {
		t.Fatalf("expected Epoch=1, Frozen=true, 1 Header item, got %+v", st)
	}
}

func TestHeaderMetaAndCurrentHeaderData(t *testing.T) {
	SetHeaderMeta("v1.2.3", "/workspace/test")
	SetModelInfo("test-model", "high")

	hd := CurrentHeaderData()
	if hd.Version != "v1.2.3" || hd.Workspace != "/workspace/test" || hd.Model != "test-model" || hd.Thinking != "high" {
		t.Fatalf("unexpected CurrentHeaderData: %+v", hd)
	}
}

func TestAppendTranscriptAutoPrependsHeader(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	SetHeaderMeta("v1.0.0", "/workspace/auto-header")
	SetModelInfo("auto-model", "medium")

	// In draft mode (!Frozen), appending a notice does NOT auto-prepend a header
	AppendNotice("hello world")
	s := GetTranscript()
	if s.Items == nil || len(*s.Items) != 1 {
		t.Fatalf("expected 1 item in draft mode, got %v", s.Items)
	}
	if (*s.Items)[0].Kind != TranscriptKindNotice {
		t.Fatalf("expected notice item, got %v", (*s.Items)[0].Kind)
	}

	// In frozen mode, appending to an empty transcript auto-prepends the snapshot header
	ClearTranscript()
	FreezeTranscript()
	AppendNotice("hello world")

	s = GetTranscript()
	if s.Items == nil || len(*s.Items) != 2 {
		t.Fatalf("expected 2 items in frozen mode (header + notice), got %v", s.Items)
	}

	items := *s.Items
	if items[0].Kind != TranscriptKindHeader || items[0].ID != "header" {
		t.Fatalf("expected items[0] to be header, got %+v", items[0])
	}
	hd, ok := items[0].Data.(HeaderData)
	if !ok {
		t.Fatalf("expected HeaderData in items[0], got %T", items[0].Data)
	}
	if hd.Version != "v1.0.0" || hd.Workspace != "/workspace/auto-header" || hd.Model != "auto-model" || hd.Thinking != "medium" {
		t.Fatalf("unexpected HeaderData in prepended item: %+v", hd)
	}

	if items[1].Kind != TranscriptKindNotice || items[1].Data.(NoticeData).Text != "hello world" {
		t.Fatalf("unexpected items[1]: %+v", items[1])
	}

	// Appending another item should NOT prepend another header
	AppendNotice("second line")
	s = GetTranscript()
	if len(*s.Items) != 3 {
		t.Fatalf("expected 3 items after second append, got %d", len(*s.Items))
	}
}

func TestClearScreen(t *testing.T) {
	ClearTranscript()
	defer ClearTranscript()

	SetHeaderMeta("1.0", "/ws")

	// 1. Empty transcript: ClearScreen should be a no-op
	ClearScreen()
	s := GetTranscript()
	if s.Epoch != 0 || s.Items != nil {
		t.Fatalf("expected Epoch=0 and nil items on empty ClearScreen, got Epoch=%d, items=%v", s.Epoch, s.Items)
	}

	// 2. Draft transcript (!Frozen): ClearScreen resets Items to nil without incrementing Epoch
	AppendNotice("draft notice 1")
	AppendNotice("draft notice 2")
	if len(*GetTranscript().Items) != 2 {
		t.Fatalf("expected 2 draft notices before clear")
	}
	ClearScreen()
	s = GetTranscript()
	if s.Epoch != 0 {
		t.Fatalf("expected Epoch=0 after ClearScreen on draft transcript, got %d", s.Epoch)
	}
	if s.Items != nil {
		t.Fatalf("expected nil Items after ClearScreen in draft, got %v", s.Items)
	}

	// 3. Frozen transcript: ClearScreen increments Epoch and resets Items to &[Header]
	FreezeTranscript()
	AppendNotice("frozen notice")
	if len(*GetTranscript().Items) != 2 {
		t.Fatalf("expected 2 items (header + notice) before clear in frozen mode")
	}
	ClearScreen()
	s = GetTranscript()
	if s.Epoch != 1 {
		t.Fatalf("expected Epoch=1 after ClearScreen on dirty frozen transcript, got %d", s.Epoch)
	}
	if s.Items == nil || len(*s.Items) != 1 || (*s.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected &[Header] after ClearScreen in frozen mode, got %v", s.Items)
	}

	// 4. Clean frozen transcript (already &[Header]): ClearScreen is a no-op
	ClearScreen()
	s = GetTranscript()
	if s.Epoch != 1 {
		t.Fatalf("expected Epoch to remain 1 after ClearScreen on clean frozen transcript, got %d", s.Epoch)
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
