package harness

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// StaticContext represents the token cost of the static prefix (system prompt,
// workspace context files, tools, skills, and subagents) measured at harness init.
type StaticContext struct {
	SystemPrompt int
	ContextFiles int
	SystemTools  int
	CustomTools  int
	MCPTools     int
	Skills       int
	Subagents    int
}

// Total returns the sum of all static context token buckets.
func (s StaticContext) Total() int {
	return s.SystemPrompt + s.ContextFiles + s.SystemTools + s.CustomTools + s.MCPTools + s.Skills + s.Subagents
}

// Breakdown converts StaticContext into an api.ContextBreakdown.
func (s StaticContext) Breakdown() api.ContextBreakdown {
	return api.ContextBreakdown{
		SystemPrompt: s.SystemPrompt,
		ContextFiles: s.ContextFiles,
		SystemTools:  s.SystemTools,
		CustomTools:  s.CustomTools,
		MCPTools:     s.MCPTools,
		Skills:       s.Skills,
		Subagents:    s.Subagents,
	}
}

// StaticContextFromBreakdown creates a StaticContext from an api.ContextBreakdown.
func StaticContextFromBreakdown(b api.ContextBreakdown) StaticContext {
	return StaticContext{
		SystemPrompt: b.SystemPrompt,
		ContextFiles: b.ContextFiles,
		SystemTools:  b.SystemTools,
		CustomTools:  b.CustomTools,
		MCPTools:     b.MCPTools,
		Skills:       b.Skills,
		Subagents:    b.Subagents,
	}
}

// estimateTokens returns an approximate token count using ⌈bytes / 4⌉.
func estimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

// estimateBlockTokens estimates token usage for an individual message block.
func estimateBlockTokens(b message.Block) int {
	if b == nil {
		return 0
	}
	switch blk := b.(type) {
	case *message.TextBlock:
		return estimateTokens(blk.Text)
	case *message.ThinkingBlock:
		return estimateTokens(blk.Thinking)
	case *message.ToolCall:
		data, _ := json.Marshal(blk.Args)
		return estimateTokens(blk.Name) + estimateTokens(string(data))
	default:
		return estimateTokens(fmt.Sprint(blk))
	}
}

// estimateMessageTokens estimates total tokens for a conversation message.
func estimateMessageTokens(msg message.Message) int {
	if msg == nil {
		return 0
	}
	total := 0
	for _, b := range msg.GetContent() {
		total += estimateBlockTokens(b)
	}
	if toolMsg, ok := msg.(*message.Tool); ok {
		total += estimateTokens(toolMsg.Name)
		if toolMsg.StructuredContent != nil {
			if data, err := toolMsg.CanonicalStructuredContent(); err == nil {
				total += estimateTokens(string(data))
			}
		}
	}
	return total
}

// CompactionTriggerTokens calculates the safety reserve and auto-compaction trigger
// threshold per compaction.md Section 2.4.
func CompactionTriggerTokens(limits models.Limits) (reserveTokens, triggerTokens int) {
	contextLimit := limits.Context
	if contextLimit <= 0 {
		contextLimit = 128000
	}
	outputLimit := limits.Output
	if outputLimit <= 0 {
		outputLimit = 16384
	}

	outputReserve := min(outputLimit, 32000)
	safetyMargin := int(float64(contextLimit) * 0.03)
	if safetyMargin < 8000 {
		safetyMargin = 8000
	} else if safetyMargin > 32000 {
		safetyMargin = 32000
	}

	reserveTokens = outputReserve + safetyMargin
	triggerTokens = max(contextLimit-reserveTokens, 0)
	return reserveTokens, triggerTokens
}

// computeContextState computes the context window token usage and calibrated breakdown
// according to compaction.md Section 2.3.
func computeContextState(static StaticContext, state State, limits models.Limits) api.ContextState {
	contextLimit := limits.Context
	if contextLimit <= 0 {
		contextLimit = 128000
	}
	outputLimit := limits.Output
	if outputLimit <= 0 {
		outputLimit = 16384
	}
	reserveTokens, triggerTokens := CompactionTriggerTokens(limits)

	rawSummary := 0
	rawUser := 0
	rawAgent := 0
	rawTools := 0

	for _, msg := range state.Messages {
		if msg == nil {
			continue
		}
		switch m := msg.(type) {
		case *message.User:
			if SystemKindOf(m) == SystemKindCompaction {
				rawSummary += estimateMessageTokens(m)
			} else {
				rawUser += estimateMessageTokens(m)
			}
		case *message.Assistant:
			for _, b := range m.GetContent() {
				if _, ok := b.(*message.ToolCall); ok {
					rawTools += estimateBlockTokens(b)
				} else {
					rawAgent += estimateBlockTokens(b)
				}
			}
		case *message.Tool:
			rawTools += estimateMessageTokens(m)
		default:
			rawUser += estimateMessageTokens(m)
		}
	}

	staticTotal := static.Total()
	rawConversationEstimate := rawSummary + rawUser + rawAgent + rawTools

	// Locate the latest Assistant message with valid TokenMetrics
	lastAsstIdx := -1
	var lastAsst *message.Assistant
	for i, v := range slices.Backward(state.Messages) {
		if asst, ok := v.(*message.Assistant); ok && asst != nil {
			if asst.Metrics != nil && (asst.Metrics.TotalTokens > 0 || (asst.Metrics.Tokens.Input+asst.Metrics.Tokens.Output) > 0) {
				lastAsstIdx = i
				lastAsst = asst
				break
			}
		}
	}

	var usedTokens int
	var inputTokens, outputTokens, cachedTokens int

	if lastAsstIdx >= 0 {
		asstTokens := lastAsst.Metrics.TotalTokens
		if asstTokens == 0 {
			asstTokens = lastAsst.Metrics.Tokens.Input + lastAsst.Metrics.Tokens.Output
		}
		trailingEstimate := 0
		for j := lastAsstIdx + 1; j < len(state.Messages); j++ {
			trailingEstimate += estimateMessageTokens(state.Messages[j])
		}
		usedTokens = asstTokens + trailingEstimate
		inputTokens = lastAsst.Metrics.Tokens.Input
		outputTokens = lastAsst.Metrics.Tokens.Output
		cachedTokens = lastAsst.Metrics.Tokens.CacheRead
	} else {
		usedTokens = staticTotal + rawConversationEstimate
		cachedTokens = state.Context.CachedTokens
	}

	var summaryTokens, userTokens, agentTokens, toolTokens int
	if rawConversationEstimate > 0 {
		convUsed := max(0, usedTokens-staticTotal)
		scale := float64(convUsed) / float64(rawConversationEstimate)
		summaryTokens = int(math.Round(float64(rawSummary) * scale))
		userTokens = int(math.Round(float64(rawUser) * scale))
		agentTokens = int(math.Round(float64(rawAgent) * scale))
		toolTokens = int(math.Round(float64(rawTools) * scale))
	}

	freeTokens := max(0, triggerTokens-usedTokens)

	breakdown := api.ContextBreakdown{
		SystemPrompt:   static.SystemPrompt,
		ContextFiles:   static.ContextFiles,
		SystemTools:    static.SystemTools,
		CustomTools:    static.CustomTools,
		MCPTools:       static.MCPTools,
		Skills:         static.Skills,
		Subagents:      static.Subagents,
		Summary:        summaryTokens,
		UserMessages:   userTokens,
		AgentResponses: agentTokens,
		ToolCalls:      toolTokens,
	}

	return api.ContextState{
		UsedTokens:    usedTokens,
		MaxTokens:     contextLimit,
		OutputLimit:   outputLimit,
		ReserveTokens: reserveTokens,
		TriggerTokens: triggerTokens,
		FreeTokens:    freeTokens,
		Breakdown:     breakdown,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		CachedTokens:  cachedTokens,
		Compactions:   state.Context.Compactions,
		LastCompacted: state.Context.LastCompacted,
	}
}

// extractUserRequests deterministically extracts chronological user prompts across turns,
// preserving any earlier # User Requests from preceding compaction messages.
func extractUserRequests(messages message.MessageList) []string {
	var userRequests []string

	for _, msg := range messages {
		if SystemKindOf(msg) == SystemKindCompaction {
			switch requests := msg.GetMetadata()[MetaCompactionRequests].(type) {
			case []string:
				userRequests = append(userRequests, requests...)
			case []any:
				for _, value := range requests {
					if request, ok := value.(string); ok {
						userRequests = append(userRequests, request)
					}
				}
			}
			break
		}
	}

	// Append chronological real user requests
	for _, msg := range messages {
		u, ok := msg.(*message.User)
		if !ok || u == nil {
			continue
		}
		if SystemKindOf(u) != "" {
			continue
		}
		text := strings.TrimSpace(u.GetContent().Text())
		if text != "" {
			userRequests = append(userRequests, text)
		}
	}

	// Keep up to last N = 10 requests, always keeping the latest
	if len(userRequests) > 10 {
		userRequests = userRequests[len(userRequests)-10:]
	}

	return userRequests
}

// splitEntryCompactionMessages splits messages into the history prefix up to the last
// Assistant message and any trailing non-tool messages (such as a newly arrived user prompt)
// when triggered by entry auto-compaction. For mid-turn or manual compaction, returns all
// messages without splitting.
func splitEntryCompactionMessages(msgs message.MessageList, trigger string) (history, trailing message.MessageList) {
	if trigger != "auto" {
		return msgs, nil
	}
	lastAsstIdx := -1
	for i, m := range slices.Backward(msgs) {
		if _, ok := m.(*message.Assistant); ok {
			lastAsstIdx = i
			break
		}
	}
	if lastAsstIdx <= 0 || lastAsstIdx == len(msgs)-1 {
		return msgs, nil
	}
	for _, m := range msgs[lastAsstIdx+1:] {
		if _, isTool := m.(*message.Tool); isTool {
			// Mid-turn after tools: tool results cannot be separated from their Assistant tool_call.
			return msgs, nil
		}
	}
	return msgs[:lastAsstIdx+1], msgs[lastAsstIdx+1:]
}

// formatTodos formats the session todo checklist for compaction summary injection.
func formatTodos(todos []tools.TodoItem) string {
	if len(todos) == 0 {
		return "- (none)"
	}
	var sb strings.Builder
	for i, t := range todos {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "- [%s] %s", t.Status, t.Content)
	}
	return sb.String()
}

// buildContextSummaryMessage creates the synthetic <context_summary> message replacing state.Messages.
func buildContextSummaryMessage(userRequests []string, todos []tools.TodoItem, summary string, trigger string, focus string, hasTrailing bool) message.Message {
	var sb strings.Builder
	sb.WriteString("<context_summary>\n")
	sb.WriteString("The conversation history prior to this point was compacted to free context window space.\n\n")

	sb.WriteString("# User Requests (Chronological)\n")
	if len(userRequests) == 0 {
		sb.WriteString("1. (none)\n")
	} else {
		for i, req := range userRequests {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, req)
		}
	}
	sb.WriteString("\n")

	sb.WriteString("# Active Todos\n")
	sb.WriteString(formatTodos(todos))
	sb.WriteString("\n\n")

	sb.WriteString("# Conversation Summary\n")
	sb.WriteString(strings.TrimSpace(summary))
	sb.WriteString("\n</context_summary>")

	if trigger == "auto" && !hasTrailing {
		sb.WriteString("\n\nContinue working on the latest user request above from the current state without asking for confirmation or repeating completed steps.")
	}

	meta := map[string]any{
		MetaSystemKind:         SystemKindCompaction,
		MetaCompactionTrigger:  trigger,
		MetaCompactionRequests: slices.Clone(userRequests),
	}
	if strings.TrimSpace(focus) != "" {
		meta[MetaCompactionFocus] = strings.TrimSpace(focus)
	}

	return message.NewUserTextMeta(sb.String(), meta)
}

//go:embed prompts/summarize.md
var summarizePromptTemplate string

var summarizeTemplate = template.Must(template.New("summarize").Parse(summarizePromptTemplate))

// CompactionNode performs in-chat conversation summarization and state replacement.
type CompactionNode struct {
	model        *llm.Model
	tools        tools.Container
	systemPrompt string
	limits       models.Limits
	static       StaticContext
	logger       log.Interface
}

// NewCompactionNode constructs a new CompactionNode bound to dependencies.
func NewCompactionNode(cfg CompactionNodeConfig) *CompactionNode {
	return &CompactionNode{
		model:        cfg.Model,
		tools:        cfg.Container,
		systemPrompt: strings.TrimSpace(cfg.SystemPrompt),
		limits:       cfg.Limits,
		static:       cfg.Static,
		logger:       cfg.Logger.ForComponent("harness.compact"),
	}
}

// Execute performs in-chat summarization and atomically replaces state.Messages with <context_summary>.
func (n *CompactionNode) Execute(ctx context.Context, state State) (graph.Command[State], error) {
	focus := ""
	trigger := "auto"
	if state.PendingCompaction != nil {
		trigger = "manual"
		focus = state.PendingCompaction.Focus
	}

	compactID := uuid.NewString()
	emit(ctx, CompactionStarted{
		ID:      compactID,
		Trigger: trigger,
		Focus:   focus,
	})

	// For entry auto-compaction, split messages into history prefix and trailing user message.
	startTime := time.Now()
	beforeTokens := state.Context.UsedTokens
	history, trailing := splitEntryCompactionMessages(state.Messages, trigger)

	// Clone history into a temporary slice for the summarization prompt.
	messages := make([]message.Message, 0, len(history)+2)
	if n.systemPrompt != "" {
		messages = append(messages, message.NewSystemText(n.systemPrompt))
	}
	messages = append(messages, history...)

	var buf strings.Builder
	compactCtx := struct{ Focus string }{
		Focus: strings.TrimSpace(focus),
	}
	if err := summarizeTemplate.Execute(&buf, compactCtx); err != nil {
		return nil, fmt.Errorf("harness: execute compaction prompt template: %w", err)
	}
	messages = append(messages, message.NewUserText(buf.String()))

	model := n.model
	if defs := n.tools.Definitions(); len(defs) > 0 {
		model = model.BindToolDefs(defs...)
	}

	resp, err := model.Invoke(scope.With(ctx, scope.Scope{
		TurnID:  state.LastUserMessageID(),
		Purpose: string(metrics.PurposeCompaction),
	}), messages)
	if err != nil {
		return nil, fmt.Errorf("harness: invoke compaction llm: %w", err)
	}

	summaryText := ""
	if resp != nil {
		summaryText = strings.TrimSpace(resp.GetContent().Text())
	}
	if summaryText == "" {
		return nil, fmt.Errorf("harness: compaction returned empty summary")
	}

	userRequests := extractUserRequests(history)
	hasTrailing := len(trailing) > 0
	summaryMsg := buildContextSummaryMessage(userRequests, state.Todos, summaryText, trigger, focus, hasTrailing)
	summaryMsg.SetID(compactID)

	return graph.Update[State](func(s State) State {
		s.Messages = slices.Clone(trailing)
		s = s.PrependMessage(summaryMsg, MessageUpdateOptions{SkipContextRecalculation: true})
		s = s.ApplyCompaction(n.static, n.limits)

		afterTokens := s.Context.UsedTokens
		durationMs := time.Since(startTime).Milliseconds()

		emit(ctx, CompactionCompleted{
			ID:           compactID,
			Trigger:      trigger,
			Focus:        focus,
			Summary:      summaryText,
			BeforeTokens: beforeTokens,
			AfterTokens:  afterTokens,
			DurationMs:   durationMs,
		})
		emit(ctx, ContextUpdatedEvent{Context: s.Context})

		return s
	}), nil
}
