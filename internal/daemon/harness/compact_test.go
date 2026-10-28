package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func TestStaticContext(t *testing.T) {
	sc := StaticContext{
		SystemPrompt: 100,
		ContextFiles: 200,
		SystemTools:  300,
		CustomTools:  50,
		MCPTools:     25,
		Skills:       15,
		Subagents:    10,
	}

	if got := sc.Total(); got != 700 {
		t.Errorf("sc.Total() = %d, want 700", got)
	}

	bd := sc.Breakdown()
	if bd.SystemPrompt != 100 || bd.ContextFiles != 200 || bd.SystemTools != 300 ||
		bd.CustomTools != 50 || bd.MCPTools != 25 || bd.Skills != 15 || bd.Subagents != 10 {
		t.Errorf("sc.Breakdown() = %+v, unexpected fields", bd)
	}

	roundtrip := StaticContextFromBreakdown(bd)
	if roundtrip != sc {
		t.Errorf("StaticContextFromBreakdown roundtrip = %+v, want %+v", roundtrip, sc)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"12345678", 2},
		{"123456789", 3},
	}
	for _, tc := range tests {
		if got := estimateTokens(tc.input); got != tc.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}

	// estimateBlockTokens
	if got := estimateBlockTokens(nil); got != 0 {
		t.Errorf("estimateBlockTokens(nil) = %d, want 0", got)
	}
	tb := &message.TextBlock{Text: "12345678"}
	if got := estimateBlockTokens(tb); got != 2 {
		t.Errorf("estimateBlockTokens(TextBlock) = %d, want 2", got)
	}
	thb := &message.ThinkingBlock{Thinking: "1234"}
	if got := estimateBlockTokens(thb); got != 1 {
		t.Errorf("estimateBlockTokens(ThinkingBlock) = %d, want 1", got)
	}
	tcb := &message.ToolCall{Name: "bash", Args: map[string]any{"cmd": "ls"}}
	if got := estimateBlockTokens(tcb); got <= 0 {
		t.Errorf("estimateBlockTokens(ToolCall) = %d, want > 0", got)
	}

	// estimateMessageTokens
	if got := estimateMessageTokens(nil); got != 0 {
		t.Errorf("estimateMessageTokens(nil) = %d, want 0", got)
	}
	u := message.NewUserText("Hello world!")
	if got := estimateMessageTokens(u); got <= 0 {
		t.Errorf("estimateMessageTokens(User) = %d, want > 0", got)
	}
	toolMsg := &message.Tool{
		Base:              message.Base{ID: "tool-1"},
		ToolCallID:        "call-1",
		Name:              "read_file",
		Content:           message.Content{&message.TextBlock{Text: "file contents"}},
		StructuredContent: map[string]any{"lines": 10},
	}
	if got := estimateMessageTokens(toolMsg); got <= 0 {
		t.Errorf("estimateMessageTokens(Tool) = %d, want > 0", got)
	}
}

func TestCompactionTriggerTokens(t *testing.T) {
	tests := []struct {
		name        string
		limits      workspace.ModelLimits
		wantReserve int
		wantTrigger int
	}{
		{
			name:        "128k context with 16k output",
			limits:      workspace.ModelLimits{Context: 128000, Output: 16384},
			wantReserve: 16384 + 8000, // min(16384, 32000) + max(8000, min(32000, 128000*0.03=3840->8000)) = 24384
			wantTrigger: 128000 - 24384,
		},
		{
			name:        "200k context with 16k output",
			limits:      workspace.ModelLimits{Context: 200000, Output: 16384},
			wantReserve: 16384 + 8000, // 200k * 0.03 = 6000 -> clamped to 8000
			wantTrigger: 200000 - 24384,
		},
		{
			name:        "400k context with 64k output",
			limits:      workspace.ModelLimits{Context: 400000, Output: 64000},
			wantReserve: 32000 + 12000, // min(64000, 32000)=32000 + (400000*0.03=12000) = 44000
			wantTrigger: 400000 - 44000,
		},
		{
			name:        "1M context with 64k output",
			limits:      workspace.ModelLimits{Context: 1000000, Output: 64000},
			wantReserve: 32000 + 30000, // 32000 + (1000000*0.03=30000) = 62000
			wantTrigger: 1000000 - 62000,
		},
		{
			name:        "2M context with 64k output (safety margin clamped to 32k)",
			limits:      workspace.ModelLimits{Context: 2000000, Output: 64000},
			wantReserve: 32000 + 32000, // 32000 + clamp(60000, 8000, 32000) = 64000
			wantTrigger: 2000000 - 64000,
		},
		{
			name:        "zero values default to 128k context and 16k output",
			limits:      workspace.ModelLimits{Context: 0, Output: 0},
			wantReserve: 16384 + 8000,
			wantTrigger: 128000 - 24384,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reserve, trigger := CompactionTriggerTokens(tc.limits)
			if reserve != tc.wantReserve {
				t.Errorf("reserve = %d, want %d", reserve, tc.wantReserve)
			}
			if trigger != tc.wantTrigger {
				t.Errorf("trigger = %d, want %d", trigger, tc.wantTrigger)
			}
		})
	}
}

func TestComputeContextState(t *testing.T) {
	static := StaticContext{
		SystemPrompt: 500,
		ContextFiles: 500,
		SystemTools:  1000,
	}
	limits := workspace.ModelLimits{Context: 100000, Output: 10000}
	// reserve = 10000 + 8000 = 18000. trigger = 82000.

	// Case 1: Without Assistant token metrics (fallback to byte estimation)
	userMsg := message.NewUserText("User question 12345678")         // 22 chars -> 6 tokens
	asstMsg := message.NewAssistantText("Assistant answer 12345678") // 26 chars -> 7 tokens

	state := State{
		Messages: message.MessageList{userMsg, asstMsg},
	}

	ctxState := computeContextState(static, state, limits)
	if ctxState.MaxTokens != 100000 {
		t.Errorf("MaxTokens = %d, want 100000", ctxState.MaxTokens)
	}
	if ctxState.TriggerTokens != 82000 {
		t.Errorf("TriggerTokens = %d, want 82000", ctxState.TriggerTokens)
	}
	if ctxState.UsedTokens != static.Total()+ctxState.Breakdown.UserMessages+ctxState.Breakdown.AgentResponses {
		t.Errorf("UsedTokens = %d, unexpected calculation", ctxState.UsedTokens)
	}
	if ctxState.FreeTokens != 82000-ctxState.UsedTokens {
		t.Errorf("FreeTokens = %d, want %d", ctxState.FreeTokens, 82000-ctxState.UsedTokens)
	}

	// Case 2: With Assistant token metrics from provider
	asstWithMetrics := message.NewAssistantText("Calibrated answer")
	asstWithMetrics.Metrics = &message.TokenMetrics{
		TotalTokens: 5000,
		Tokens: message.TokenDetails{
			Input:     4500,
			Output:    500,
			CacheRead: 3000,
		},
	}
	trailingUser := message.NewUserText("Follow-up") // 9 chars -> 3 tokens
	stateWithMetrics := State{
		Messages: message.MessageList{userMsg, asstWithMetrics, trailingUser},
	}

	ctxState2 := computeContextState(static, stateWithMetrics, limits)
	expectedUsed := 5000 + estimateMessageTokens(trailingUser)
	if ctxState2.UsedTokens != expectedUsed {
		t.Errorf("ctxState2.UsedTokens = %d, want %d", ctxState2.UsedTokens, expectedUsed)
	}
	if ctxState2.InputTokens != 4500 || ctxState2.OutputTokens != 500 || ctxState2.CachedTokens != 3000 {
		t.Errorf("ctxState2 metrics mismatch: %+v", ctxState2)
	}
}

func TestShouldAutoCompact(t *testing.T) {
	limits := workspace.ModelLimits{Context: 100000, Output: 10000}
	_, trigger := CompactionTriggerTokens(limits)

	u1 := message.NewUserText("User 1")
	a1 := message.NewAssistantText("Assistant 1")

	// 1. Used tokens below trigger -> false
	state := State{
		Messages: message.MessageList{u1, a1},
		Context: api.ContextState{
			UsedTokens:    trigger - 100,
			TriggerTokens: trigger,
		},
	}
	if state.ShouldAutoCompact() {
		t.Error("shouldAutoCompact returned true when UsedTokens < TriggerTokens")
	}

	// 2. Used tokens >= trigger, but pending tool calls -> false
	aWithPendingTool := &message.Assistant{
		Content: message.Content{
			&message.TextBlock{Text: "Tool call"},
			&message.ToolCall{ID: "call-1", Name: "bash"},
		},
	}
	stateTool := State{
		Messages: message.MessageList{u1, aWithPendingTool},
		Context: api.ContextState{
			UsedTokens:    trigger + 10,
			TriggerTokens: trigger,
		},
	}
	if stateTool.ShouldAutoCompact() {
		t.Error("shouldAutoCompact returned true with pending tool calls")
	}

	// 3. Used tokens >= trigger, fewer than 2 messages -> false
	stateFewMsgs := State{
		Messages: message.MessageList{u1},
		Context: api.ContextState{
			UsedTokens:    trigger + 10,
			TriggerTokens: trigger,
		},
	}
	if stateFewMsgs.ShouldAutoCompact() {
		t.Error("shouldAutoCompact returned true with < 2 messages")
	}

	// 4. Used tokens >= trigger, but last message is already a compaction message -> false
	compactMsg := message.NewUserTextMeta("<context_summary>...</context_summary>", map[string]any{
		MetaSystemKind: SystemKindCompaction,
	})
	stateAlreadyCompacted := State{
		Messages: message.MessageList{u1, compactMsg},
		Context: api.ContextState{
			UsedTokens:    trigger + 10,
			TriggerTokens: trigger,
		},
	}
	if stateAlreadyCompacted.ShouldAutoCompact() {
		t.Error("shouldAutoCompact returned true when last message is compaction")
	}

	// 5. Normal qualification -> true
	stateValid := State{
		Messages: message.MessageList{u1, a1},
		Context: api.ContextState{
			UsedTokens:    trigger,
			TriggerTokens: trigger,
		},
	}
	if !stateValid.ShouldAutoCompact() {
		t.Error("shouldAutoCompact returned false on valid candidate")
	}
}

func TestExtractUserRequests(t *testing.T) {
	// History with earlier compaction message and several turns
	earlierCompaction := message.NewUserTextMeta("<context_summary>\n# User Requests (Chronological)\n1. First task\n2. Second task\n\n# Active Todos\n- (none)\n</context_summary>", map[string]any{
		MetaSystemKind: SystemKindCompaction,
	})
	u3 := message.NewUserText("Third task")
	a3 := message.NewAssistantText("Working on it")
	cmdResult := message.NewUserTextMeta("command executed", map[string]any{
		MetaSystemKind: "command",
	})
	u4 := message.NewUserText("Fourth task")

	msgs := message.MessageList{earlierCompaction, u3, a3, cmdResult, u4}
	requests := extractUserRequests(msgs)

	want := []string{"First task", "Second task", "Third task", "Fourth task"}
	if len(requests) != len(want) {
		t.Fatalf("extractUserRequests returned %d items, want %d: %v", len(requests), len(want), requests)
	}
	for i, req := range requests {
		if req != want[i] {
			t.Errorf("requests[%d] = %q, want %q", i, req, want[i])
		}
	}

	// Test truncation to last 10 requests
	var longMsgs message.MessageList
	for i := 1; i <= 15; i++ {
		longMsgs = append(longMsgs, message.NewUserText(strings.Repeat("req", 1)+"-"+string(rune('A'+i-1))))
	}
	truncated := extractUserRequests(longMsgs)
	if len(truncated) != 10 {
		t.Fatalf("expected 10 truncated requests, got %d", len(truncated))
	}
	// Last one should be req-O
	if !strings.HasSuffix(truncated[9], "-O") {
		t.Errorf("truncated[9] = %q, want suffix -O", truncated[9])
	}
}

func TestFormatTodos(t *testing.T) {
	if got := formatTodos(nil); got != "- (none)" {
		t.Errorf("formatTodos(nil) = %q, want %q", got, "- (none)")
	}
	todos := []tools.TodoItem{
		{Content: "Task 1", Status: "in_progress"},
		{Content: "Task 2", Status: "completed"},
	}
	want := "- [in_progress] Task 1\n- [completed] Task 2"
	if got := formatTodos(todos); got != want {
		t.Errorf("formatTodos(todos) = %q, want %q", got, want)
	}
}

func TestBuildContextSummaryMessage(t *testing.T) {
	userRequests := []string{"Fix issue A", "Add feature B"}
	todos := []tools.TodoItem{{Content: "Refactor engine", Status: "in_progress"}}
	summary := "Refactored the core logic and verified test suite."

	// Auto trigger should include continuation directive
	msgAuto := buildContextSummaryMessage(userRequests, todos, summary, "auto", "", false)
	textAuto := msgAuto.GetContent().Text()

	if !strings.Contains(textAuto, "<context_summary>") || !strings.Contains(textAuto, "</context_summary>") {
		t.Errorf("msgAuto missing <context_summary> tags: %s", textAuto)
	}
	if !strings.Contains(textAuto, "1. Fix issue A\n2. Add feature B") {
		t.Errorf("msgAuto missing user requests: %s", textAuto)
	}
	if !strings.Contains(textAuto, "- [in_progress] Refactor engine") {
		t.Errorf("msgAuto missing active todos: %s", textAuto)
	}
	if !strings.Contains(textAuto, summary) {
		t.Errorf("msgAuto missing summary text: %s", textAuto)
	}
	if !strings.Contains(textAuto, "Continue working on the latest user request above") {
		t.Errorf("msgAuto missing auto continuation directive: %s", textAuto)
	}
	if SystemKindOf(msgAuto) != SystemKindCompaction {
		t.Errorf("msgAuto SystemKindOf = %q, want %q", SystemKindOf(msgAuto), SystemKindCompaction)
	}
	if msgAuto.GetMetadata()[MetaCompactionTrigger] != "auto" {
		t.Errorf("msgAuto MetaCompactionTrigger = %v, want auto", msgAuto.GetMetadata()[MetaCompactionTrigger])
	}

	// Manual trigger with focus
	msgManual := buildContextSummaryMessage(userRequests, todos, summary, "manual", "memory optimization", false)
	textManual := msgManual.GetContent().Text()
	if strings.Contains(textManual, "Continue working on the latest user request above") {
		t.Errorf("msgManual should not have auto continuation directive: %s", textManual)
	}
	if msgManual.GetMetadata()[MetaCompactionFocus] != "memory optimization" {
		t.Errorf("msgManual MetaCompactionFocus = %v, want 'memory optimization'", msgManual.GetMetadata()[MetaCompactionFocus])
	}
}

func TestCompactionNodeExecute(t *testing.T) {
	ctx := context.Background()
	_, reg, _ := newTestToolsRegistry(t)

	// Case 1: Manual compaction with focus popped from queue
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Generated compaction summary text"}, "comp-reply-1", nil
		},
	}
	model := newTestModel(t, prov)
	limits := workspace.ModelLimits{Context: 128000, Output: 16384}
	static := StaticContext{SystemPrompt: 100, SystemTools: 500}

	node := NewCompactionNode(CompactionNodeConfig{
		Model:        model,
		Container:    reg,
		SystemPrompt: "You are a test agent",
		Limits:       limits,
		Static:       static,
		Logger:       log.Default(),
	})

	initState := State{
		PendingCompaction: &CompactInput{
			Focus: "database migrations",
		},
		Messages: message.MessageList{
			message.NewUserText("User question 1"),
			message.NewAssistantText("Assistant answer 1"),
		},
		Context: api.ContextState{
			UsedTokens:    50000,
			TriggerTokens: 100000,
		},
	}

	cmd, err := node.Execute(ctx, initState)
	if err != nil {
		t.Fatalf("node.Execute error: %v", err)
	}

	newState := cmd.Apply(initState)
	if len(newState.Messages) != 1 {
		t.Fatalf("expected 1 message after compaction, got %d", len(newState.Messages))
	}
	compMsg := newState.Messages[0]
	if SystemKindOf(compMsg) != SystemKindCompaction {
		t.Errorf("compMsg SystemKindOf = %q, want %q", SystemKindOf(compMsg), SystemKindCompaction)
	}
	if compMsg.GetMetadata()[MetaCompactionTrigger] != "manual" {
		t.Errorf("compMsg trigger = %v, want manual", compMsg.GetMetadata()[MetaCompactionTrigger])
	}
	if compMsg.GetMetadata()[MetaCompactionFocus] != "database migrations" {
		t.Errorf("compMsg focus = %v, want 'database migrations'", compMsg.GetMetadata()[MetaCompactionFocus])
	}
	if newState.Context.Compactions != 1 {
		t.Errorf("Context.Compactions = %d, want 1", newState.Context.Compactions)
	}

	// Verify model was called with summarization prompt containing the focus
	turns := prov.recordedTurns()
	if len(turns) != 1 {
		t.Fatalf("expected 1 model turn, got %d", len(turns))
	}
	lastTurnMsg := turns[0].messages[len(turns[0].messages)-1]
	if !strings.Contains(lastTurnMsg.GetContent().Text(), "Additional focus requested by user: database migrations") {
		t.Errorf("summarization prompt missing focus: %s", lastTurnMsg.GetContent().Text())
	}

	provErr := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return nil, "", errors.New("provider failure")
		},
	}
	nodeErr := NewCompactionNode(CompactionNodeConfig{
		Model:     newTestModel(t, provErr),
		Container: reg,
		Limits:    limits,
		Static:    static,
		Logger:    log.Default(),
	})
	cmdErr, err := nodeErr.Execute(ctx, initState)
	if err != nil {
		t.Fatalf("nodeErr.Execute should return nil error, got %v", err)
	}
	if cmdErr == nil {
		t.Fatal("expected a recovery update on model invocation failure")
	}
	recovered := cmdErr.Apply(initState)
	if recovered.PendingCompaction != nil || len(recovered.Messages) != len(initState.Messages) {
		t.Fatalf("unexpected recovery state: %+v", recovered)
	}
}

func TestStateMethodsAndMutators(t *testing.T) {
	static := StaticContext{SystemPrompt: 100}
	limits := workspace.ModelLimits{Context: 128000, Output: 16384}
	opts := MessageUpdateOptions{Static: static, Limits: limits}

	s := State{}
	if s.HasLeadingCompact() {
		t.Errorf("empty state HasLeadingCompact should be false")
	}
	s.PendingCompaction = &CompactInput{Focus: "test focus"}
	if !s.HasLeadingCompact() {
		t.Errorf("state with PendingCompaction HasLeadingCompact should be true")
	}

	if s.IsAutoCompacted() {
		t.Errorf("state with no messages IsAutoCompacted should be false")
	}

	u1 := message.NewUserText("User prompt 1")
	u1.SetID("user-1")
	s = s.AppendMessage(u1, opts)
	if s.LastUserMessageID() != "user-1" {
		t.Errorf("LastUserMessageID = %q, want user-1", s.LastUserMessageID())
	}
	if !s.ShouldWakeAgent() {
		t.Errorf("ShouldWakeAgent with user message should be true")
	}

	asst1 := message.NewAssistantText("Asst reply")
	asst1.SetID("asst-1")
	s = s.AppendMessage(asst1, opts)
	if s.ShouldWakeAgent() {
		t.Errorf("ShouldWakeAgent with assistant message should be false")
	}

	cmdMsg := message.NewUserTextMeta("cmd output", map[string]any{MetaSystemKind: SystemKindCommand})
	s = s.AppendMessage(cmdMsg, opts)
	if s.ShouldWakeAgent() {
		t.Errorf("ShouldWakeAgent with command message should be false")
	}

	u2 := message.NewUserText("User prompt 2")
	u2.SetID("user-2")
	s = s.AppendMessages(message.MessageList{u2}, opts)
	if s.LastUserMessageID() != "user-2" {
		t.Errorf("LastUserMessageID = %q, want user-2", s.LastUserMessageID())
	}

	// ApplyCompaction
	summary := buildContextSummaryMessage([]string{"User prompt 1", "User prompt 2"}, nil, "Summary text", "auto", "", false)
	s.Messages = nil
	s = s.PrependMessage(summary, MessageUpdateOptions{SkipContextRecalculation: true})
	s = s.ApplyCompaction(static, limits)
	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message after compaction, got %d", len(s.Messages))
	}
	if s.PendingCompaction != nil {
		t.Errorf("ApplyCompaction should clear PendingCompaction")
	}
	if !s.IsAutoCompacted() {
		t.Errorf("IsAutoCompacted should be true for auto summary")
	}
	if s.Context.Compactions != 1 {
		t.Errorf("Compactions = %d, want 1", s.Context.Compactions)
	}
}

func TestStreamEvents(t *testing.T) {
	// NodeMessageEvent
	nme := NodeMessageEvent{Message: message.NewUserText("hello")}
	if nme.EventName() != eventNodeMessage {
		t.Errorf("EventName = %q, want %q", nme.EventName(), eventNodeMessage)
	}
	ev := nme.ToEvent()
	if ev.Type != EventMessage || ev.Message == nil {
		t.Errorf("unexpected event: %+v", ev)
	}

	// CommandStarted
	cs := CommandStarted{ID: "c1", Command: "ls"}
	if cs.EventName() != eventCommandStarted {
		t.Errorf("EventName = %q, want %q", cs.EventName(), eventCommandStarted)
	}
	if ev := cs.ToEvent(); ev.Type != EventCommandStarted || ev.CommandStarted.ID != "c1" {
		t.Errorf("unexpected event: %+v", ev)
	}

	// CommandCompleted
	cc := CommandCompleted{ID: "c1", Command: "ls", ExitCode: 0}
	if cc.EventName() != eventCommandCompleted {
		t.Errorf("EventName = %q, want %q", cc.EventName(), eventCommandCompleted)
	}
	if ev := cc.ToEvent(); ev.Type != EventCommandCompleted || ev.CommandCompleted.ID != "c1" {
		t.Errorf("unexpected event: %+v", ev)
	}

	// TodosUpdatedEvent
	tue := TodosUpdatedEvent{Todos: []tools.TodoItem{{Content: "item 1"}}}
	if tue.EventName() != eventTodosUpdated {
		t.Errorf("EventName = %q, want %q", tue.EventName(), eventTodosUpdated)
	}
	if ev := tue.ToEvent(); ev.Type != EventTodosUpdated || len(ev.Todos) != 1 {
		t.Errorf("unexpected event: %+v", ev)
	}

	// CompactionStarted
	cmpS := CompactionStarted{ID: "cmp1", Trigger: "manual", Focus: "focus"}
	if cmpS.EventName() != eventCompactionStarted {
		t.Errorf("EventName = %q, want %q", cmpS.EventName(), eventCompactionStarted)
	}
	if ev := cmpS.ToEvent(); ev.Type != EventCompactionStarted || ev.CompactionStarted.Focus != "focus" {
		t.Errorf("unexpected event: %+v", ev)
	}

	// CompactionCompleted
	cmpC := CompactionCompleted{ID: "cmp1", Trigger: "manual", Summary: "summary"}
	if cmpC.EventName() != eventCompactionCompleted {
		t.Errorf("EventName = %q, want %q", cmpC.EventName(), eventCompactionCompleted)
	}
	if ev := cmpC.ToEvent(); ev.Type != EventCompactionCompleted || ev.CompactionCompleted.Summary != "summary" {
		t.Errorf("unexpected event: %+v", ev)
	}

	// ContextUpdatedEvent
	cue := ContextUpdatedEvent{Context: api.ContextState{UsedTokens: 100}}
	if cue.EventName() != eventContextUpdated {
		t.Errorf("EventName = %q, want %q", cue.EventName(), eventContextUpdated)
	}
	if ev := cue.ToEvent(); ev.Type != EventContextUpdated || ev.Context.UsedTokens != 100 {
		t.Errorf("unexpected event: %+v", ev)
	}

	// CacheBroken
	cb := CacheBroken{PrevCached: 200, CurrCached: 50}
	if cb.EventName() != eventCacheBroken {
		t.Errorf("EventName = %q, want %q", cb.EventName(), eventCacheBroken)
	}
	if ev := cb.ToEvent(); ev.Type != EventCacheBroken || ev.CacheBroken.PrevCached != 200 {
		t.Errorf("unexpected event: %+v", ev)
	}
}
