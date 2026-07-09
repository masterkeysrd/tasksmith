package graph_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/stream"
	agentgraph "github.com/masterkeysrd/tasksmith/internal/agent/graph"
)

type mockStreamWriter struct {
	events []stream.Event
}

func (m *mockStreamWriter) Write(ctx context.Context, ev any) error {
	if event, ok := ev.(stream.Event); ok {
		m.events = append(m.events, event)
	}
	return nil
}

type mockFileStorage struct {
	saved map[string]string
	tmp   string
}

func (m *mockFileStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	data, _ := io.ReadAll(r)
	path := filepath.Join(m.tmp, relativePath)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
	m.saved[relativePath] = string(data)
	return path, nil
}

func (m *mockFileStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(m.tmp, relativePath))
}

func TestExtractTokensFromLastResponse(t *testing.T) {
	// Case 1: No Assistant message
	msgs := []message.Message{
		message.NewUserText("Hello"),
	}
	if tokens := agentgraph.ExtractTokensFromLastResponse(context.Background(), msgs); tokens != 0 {
		t.Errorf("expected 0 tokens, got %d", tokens)
	}

	// Case 2: Assistant message with metrics
	asst := &message.Assistant{
		Metrics: &message.TokenMetrics{
			TotalTokens: 100,
		},
	}
	msgs = []message.Message{
		message.NewUserText("Hello"),
		asst,
	}
	if tokens := agentgraph.ExtractTokensFromLastResponse(context.Background(), msgs); tokens != 100 {
		t.Errorf("expected 100 tokens, got %d", tokens)
	}

	// Case 3: Subsequent messages after Assistant
	msgs = []message.Message{
		message.NewUserText("Hello"),
		asst,
		&message.Tool{
			Name:    "bash",
			Content: message.Content{&message.TextBlock{Text: "output"}},
		},
	}
	tokens := agentgraph.ExtractTokensFromLastResponse(context.Background(), msgs)
	if tokens <= 100 {
		t.Errorf("expected >100 tokens due to tool response, got %d", tokens)
	}
}

func TestMaskObservations(t *testing.T) {
	tmpDir := t.TempDir()

	// Register a compact content provider
	agentgraph.CompactProviders["mock_tool"] = mockCompactProvider{}

	config := agentgraph.DefaultCompactionConfig()
	config.ContextWindow = 1000
	config.Phase1Watermark = 0.50
	config.Phase1Target = 0.40 // Target is 400 tokens, so we need to reclaim 900 - 400 = 500 tokens (Turn 1 only saves ~350, forcing Turn 2 to also mask)
	config.ProtectedTurns = 1
	config.ToolTruncateThreshold = 5

	// Create messages: 1 Assistant turn (protected) + 2 older turns with tool calls
	msgs := []message.Message{
		// Turn 1 (Eligible for masking)
		&message.Assistant{
			Content: message.Content{
				&message.ToolCall{ID: "call_1", Name: "mock_tool", Args: map[string]any{"path": "file.txt", "heavy": strings.Repeat("A", 300)}},
			},
		},
		&message.Tool{
			ToolCallID: "call_1",
			Name:       "mock_tool",
			Content:    message.Content{&message.TextBlock{Text: strings.Repeat("very heavy tool result payload", 50)}},
		},

		// Turn 2 (Eligible for masking)
		&message.Assistant{
			Content: message.Content{
				&message.ToolCall{ID: "call_2", Name: "other_tool", Args: map[string]any{"heavy_arg": strings.Repeat("B", 300)}},
			},
		},
		&message.Tool{
			ToolCallID: "call_2",
			Name:       "other_tool",
			Content:    message.Content{&message.TextBlock{Text: strings.Repeat("some heavy results here", 50)}},
		},

		// Turn 3 (Protected from masking because config.ProtectedTurns = 1)
		&message.Assistant{
			Content: message.Content{
				&message.TextBlock{Text: "recent thought"},
			},
		},
	}

	mockStorage := &mockFileStorage{saved: make(map[string]string), tmp: tmpDir}
	compactor := &agentgraph.Compactor{
		Config:        config,
		Storage:       mockStorage,
		SessionID:     "session_123",
		WorkspacePath: tmpDir,
		ProjectName:   "project_abc",
		AgentName:     "main",
		ProviderName:  "openai",
		ModelName:     "gpt-4",
	}

	sw := &mockStreamWriter{}
	ctx := stream.WithWriter(context.Background(), sw)

	compacted, err := compactor.MaskObservations(ctx, msgs, 900)
	if err != nil {
		t.Fatalf("MaskObservations failed: %v", err)
	}

	// Verify masking results
	if len(compacted) != len(msgs) {
		t.Fatalf("expected same number of messages, got %d", len(compacted))
	}

	// Verify Turn 1 tool call args compacted via provider
	tc1 := compacted[0].(*message.Assistant).GetContent()[0].(*message.ToolCall)
	if tc1.Args["heavy"] != "lightweight" {
		t.Errorf("expected tool call args to be compacted by provider, got %v", tc1.Args)
	}

	// Verify Turn 1 tool result masked
	tr1 := compacted[1].(*message.Tool)
	if !strings.Contains(tr1.Content.Text(), "[Compacted: mock_tool -> success. Original output was") {
		t.Errorf("expected tool 1 compacted output, got %q", tr1.Content.Text())
	}

	// Verify Turn 2 fallback masking (heavy arg replaced by [Truncated])
	tc2 := compacted[2].(*message.Assistant).GetContent()[0].(*message.ToolCall)
	if tc2.Args["heavy_arg"] != "[Truncated]" {
		t.Errorf("expected fallback argument truncation, got %v", tc2.Args)
	}

	// Verify Turn 3 protected assistant turn untouched
	if !strings.Contains(compacted[4].GetContent().Text(), "recent thought") {
		t.Errorf("expected turn 3 untouched, got %v", compacted[4])
	}

	// Verify Universal File Offload saved output via FileStorage
	offloadedContent, exists := mockStorage.saved["compacted/turn_call_2.txt"]
	if !exists {
		t.Fatalf("expected offloaded file in storage compacted/turn_call_2.txt")
	}
	if !strings.Contains(offloadedContent, "some heavy results here") {
		t.Errorf("unexpected offloaded content: %q", offloadedContent)
	}

	// Verify compaction notification written to stream writer
	if len(sw.events) != 1 {
		t.Fatalf("expected 1 stream event, got %d", len(sw.events))
	}
	if sw.events[0].Name != "agent_message" {
		t.Errorf("expected agent_message event, got %s", sw.events[0].Name)
	}
	msg, ok := sw.events[0].Data.(message.Message)
	if !ok {
		t.Fatalf("expected message.Message event data")
	}
	if !strings.Contains(msg.GetContent().Text(), "Compaction completed. Reclaimed") {
		t.Errorf("unexpected notification text: %q", msg.GetContent().Text())
	}
}

func TestSummarizeHistory_Timeline(t *testing.T) {
	// Register timeline provider
	agentgraph.TimelineProviders["mock_tool"] = mockTimelineProvider{}

	config := agentgraph.DefaultCompactionConfig()
	config.ContextWindow = 1000
	config.Phase2Watermark = 0.80
	config.Phase2Target = 0.40
	config.ProtectedTurns = 1
	config.MinProtectedTokens = 10 // Bypass large default limit

	// Create messages representing pure autonomous execution (0 user messages)
	msgs := []message.Message{
		&message.Assistant{
			Content: message.Content{
				&message.TextBlock{Text: "Thinking about step 1"},
				&message.ToolCall{ID: "call_1", Name: "mock_tool", Args: map[string]any{"path": "file.txt"}},
			},
		},
		&message.Tool{
			ToolCallID: "call_1",
			Name:       "mock_tool",
			Content:    message.Content{&message.TextBlock{Text: "result 1"}},
		},
		&message.Assistant{
			Content: message.Content{
				&message.TextBlock{Text: "recent thinking"},
			},
		},
	}

	compactor := &agentgraph.Compactor{
		Config:       config,
		SessionID:    "sess_1",
		ProviderName: "openai",
		ModelName:    "gpt-4",
	}

	sw := &mockStreamWriter{}
	ctx := stream.WithWriter(context.Background(), sw)

	compacted, err := compactor.SummarizeHistory(ctx, msgs, 900, true)
	if err != nil {
		t.Fatalf("SummarizeHistory failed: %v", err)
	}

	// Verify compaction notification written to stream writer
	if len(sw.events) != 1 {
		t.Fatalf("expected 1 stream event, got %d", len(sw.events))
	}
	if sw.events[0].Name != "agent_message" {
		t.Errorf("expected agent_message event, got %s", sw.events[0].Name)
	}
	msg, ok := sw.events[0].Data.(message.Message)
	if !ok {
		t.Fatalf("expected message.Message event data")
	}
	if !strings.Contains(msg.GetContent().Text(), "Compaction completed. Reclaimed") || !strings.Contains(msg.GetContent().Text(), "timeline") {
		t.Errorf("unexpected notification text: %q", msg.GetContent().Text())
	}

	// Compacted output should have 2 messages: 1 Anchor + 1 Protected Turn
	if len(compacted) != 2 {
		t.Fatalf("expected 2 messages post phase 2, got %d", len(compacted))
	}

	anchorMsg := compacted[0]
	if meta := anchorMsg.GetMetadata(); meta == nil || meta["compaction_anchor"] != true {
		t.Errorf("expected compaction anchor metadata, got %v", meta)
	}

	timelineText := anchorMsg.GetContent().Text()
	if !strings.Contains(timelineText, "### Summary of Autonomous Execution") {
		t.Errorf("expected timeline header, got %q", timelineText)
	}
	if !strings.Contains(timelineText, "Executed `mock_tool(map[path:file.txt])`") {
		t.Errorf("expected tool call logged, got %q", timelineText)
	}
	if !strings.Contains(timelineText, "- **Result**: mock_tool summary") {
		t.Errorf("expected custom timeline summary, got %q", timelineText)
	}
}

func TestSummarizeHistory_LLMSummary(t *testing.T) {
	config := agentgraph.DefaultCompactionConfig()
	config.ContextWindow = 1000
	config.Phase2Watermark = 0.80
	config.Phase2Target = 0.40
	config.ProtectedTurns = 1
	config.MinProtectedTokens = 10

	// Create messages representing a conversation (multiple User messages)
	msgs := []message.Message{
		message.NewUserText("User query 1"),
		&message.Assistant{
			Content: message.Content{&message.TextBlock{Text: "Response 1"}},
		},
		message.NewUserText("User query 2"),
		&message.Assistant{
			Content: message.Content{&message.TextBlock{Text: "Response 2"}},
		},
		&message.Assistant{
			Content: message.Content{&message.TextBlock{Text: "Protected turn"}},
		},
	}

	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			return &message.Assistant{
				Content: message.Content{&message.TextBlock{Text: "This is the generated conversation summary."}},
			}, nil
		},
	}

	compactor := &agentgraph.Compactor{
		Config:       config,
		Model:        mockModel,
		SessionID:    "sess_1",
		ProviderName: "openai",
		ModelName:    "gpt-4",
	}

	sw := &mockStreamWriter{}
	ctx := stream.WithWriter(context.Background(), sw)

	compacted, err := compactor.SummarizeHistory(ctx, msgs, 900, true)
	if err != nil {
		t.Fatalf("SummarizeHistory failed: %v", err)
	}

	// Verify compaction notification written to stream writer
	if len(sw.events) != 1 {
		t.Fatalf("expected 1 stream event, got %d", len(sw.events))
	}
	if sw.events[0].Name != "agent_message" {
		t.Errorf("expected agent_message event, got %s", sw.events[0].Name)
	}
	msg, ok := sw.events[0].Data.(message.Message)
	if !ok {
		t.Fatalf("expected message.Message event data")
	}
	if !strings.Contains(msg.GetContent().Text(), "Compaction completed. Reclaimed") || !strings.Contains(msg.GetContent().Text(), "llm_summary") {
		t.Errorf("unexpected notification text: %q", msg.GetContent().Text())
	}

	if len(compacted) != 2 {
		t.Fatalf("expected 2 messages post phase 2, got %d", len(compacted))
	}

	anchorMsg := compacted[0]
	summaryText := anchorMsg.GetContent().Text()
	if summaryText != "This is the generated conversation summary." {
		t.Errorf("expected generated summary, got %q", summaryText)
	}
}

func TestInjectStateBridge(t *testing.T) {
	// Original messages contain a todos call and an activate_skill call
	original := []message.Message{
		&message.Assistant{
			Content: message.Content{
				&message.ToolCall{ID: "call_skill", Name: "activate_skill", Args: map[string]any{"skill": "go_expert"}},
			},
		},
		&message.Tool{
			ToolCallID: "call_skill",
			Name:       "activate_skill",
			Content:    message.Content{&message.TextBlock{Text: "Success"}},
		},
		&message.Tool{
			ToolCallID: "call_todos",
			Name:       "todos",
			Content:    message.Content{&message.TextBlock{Text: "- [ ] Finish feature A"}},
		},
		&message.Assistant{
			Content: message.Content{&message.TextBlock{Text: "uncompacted assistant"}},
		},
	}

	// Compacted messages (represented as anchor + last uncompacted message)
	compacted := []message.Message{
		&message.System{
			Base:    message.Base{Metadata: map[string]any{"compaction_anchor": true}},
			Content: message.Content{&message.TextBlock{Text: "Anchor Text"}},
		},
		original[3],
	}

	compactor := &agentgraph.Compactor{}
	bridged := compactor.InjectStateBridge(context.Background(), original, compacted)

	// Bridged should now contain 3 messages: Anchor, State Bridge, Uncompacted Assistant
	if len(bridged) != 3 {
		t.Fatalf("expected 3 messages after state bridge, got %d", len(bridged))
	}

	bridgeMsgText := bridged[1].GetContent().Text()
	if !strings.Contains(bridgeMsgText, "Active Todos:\n- [ ] Finish feature A") {
		t.Errorf("expected todos in state bridge, got %q", bridgeMsgText)
	}
	if !strings.Contains(bridgeMsgText, "Utilized Skills: go_expert") {
		t.Errorf("expected skill in state bridge, got %q", bridgeMsgText)
	}
}

func TestTrimToBudget(t *testing.T) {
	config := agentgraph.DefaultCompactionConfig()
	config.ContextWindow = 200
	config.OutputReserve = 50

	// 5 messages of 50 tokens each (using character counter heuristic: 4 chars = 1 token, approx)
	msgs := []message.Message{
		message.NewSystemText(strings.Repeat("A", 200)), // system (always preserved)
		message.NewUserText(strings.Repeat("B", 200)),
		message.NewUserText(strings.Repeat("C", 200)),
		message.NewUserText(strings.Repeat("D", 200)),
	}

	compactor := &agentgraph.Compactor{Config: config}
	trimmed, err := compactor.TrimToBudget(context.Background(), msgs)
	if err != nil {
		t.Fatalf("TrimToBudget failed: %v", err)
	}

	// Should trim messages from the front while preserving system prompt
	if len(trimmed) >= len(msgs) {
		t.Errorf("expected messages trimmed, got %d", len(trimmed))
	}
	if trimmed[0].Role() != message.RoleSystem {
		t.Errorf("expected first message to remain system, got %s", trimmed[0].Role())
	}
}

// Mocks

type mockCompactProvider struct{}

func (m mockCompactProvider) CompactContent(args map[string]any) agentgraph.CompactedData {
	return agentgraph.CompactedData{
		Summary:       "success",
		CompactedArgs: map[string]any{"heavy": "lightweight"},
	}
}

type mockTimelineProvider struct{}

func (m mockTimelineProvider) TimelineContent(args map[string]any) agentgraph.TimelineData {
	return agentgraph.TimelineData{
		Summary: "mock_tool summary",
	}
}
