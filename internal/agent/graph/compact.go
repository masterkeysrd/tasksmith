package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/stream"
	"github.com/masterkeysrd/tasksmith/internal/agent/prompt"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

const maskedPlaceholderTokenOverhead = 20

// CompactionConfig controls the message compaction pipeline.
type CompactionConfig struct {
	// ContextWindow is the model's maximum context size in tokens.
	// When 0, compaction is skipped (unlimited mode).
	ContextWindow int

	// OutputReserve is the number of tokens reserved for the model's response.
	// Defaults to 20% of ContextWindow, minimum 4096.
	OutputReserve int

	// Phase 1 Configuration
	Phase1Watermark       float64 // Trigger masking (e.g., 0.70)
	Phase1Target          float64 // Target token footprint post-masking (e.g., 0.35)
	ToolTruncateThreshold int     // Minimum tokens a tool must use to be eligible for masking (e.g., 100)

	// Phase 2 Configuration
	Phase2Watermark float64 // Trigger LLM summarization (e.g., 0.85)
	Phase2Target    float64 // Target token footprint post-summarization (e.g., 0.40)

	// MinProtectedTokens is the absolute minimum number of tokens that must be retained
	// after a Phase 2 LLM summarization. Acts as a safe-break for small context models.
	MinProtectedTokens int // Default: 32000

	// ProtectedTurns is the number of recent assistant turns immune to any compaction.
	ProtectedTurns int // Default: 5
}

// DefaultCompactionConfig returns sensible defaults.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		ContextWindow:         0, // Disabled by default, set by graph Options
		OutputReserve:         0, // Will be computed dynamically if 0
		Phase1Watermark:       0.70,
		Phase1Target:          0.35,
		ToolTruncateThreshold: 100,
		Phase2Watermark:       0.85,
		Phase2Target:          0.40,
		MinProtectedTokens:    32000,
		ProtectedTurns:        5,
	}
}

// ExtractTokensFromLastResponse extracts exact token counts from the last Assistant message metrics.
// If metrics are missing or incomplete, it falls back to a full token count estimate of messages.
func ExtractTokensFromLastResponse(ctx context.Context, messages []message.Message) int {
	var lastAsst *message.Assistant
	var lastAsstIdx = -1
	for i := len(messages) - 1; i >= 0; i-- {
		if asst, ok := messages[i].(*message.Assistant); ok {
			lastAsst = asst
			lastAsstIdx = i
			break
		}
	}
	if lastAsst == nil || lastAsst.Metrics == nil || lastAsst.Metrics.TotalTokens == 0 {
		return 0
	}

	tokens := lastAsst.Metrics.TotalTokens
	// Add subsequent messages (such as tool responses or user messages) since that assistant turn
	for i := lastAsstIdx + 1; i < len(messages); i++ {
		t, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, []message.Message{messages[i]})
		tokens += t
	}
	return tokens
}

// Compactor coordinates the hybrid message compaction pipeline.
type Compactor struct {
	Config        CompactionConfig
	Storage       tools.FileStorage
	Model         LLM
	MetricsStore  *metrics.Store
	SessionID     string
	WorkspacePath string
	ProjectName   string
	AgentName     string
	ProviderName  string
	ModelName     string
	Workspace     *workspace.Workspace
}

// Compact applies the full compaction pipeline to a message list.
func (c *Compactor) Compact(
	ctx context.Context,
	messages []message.Message,
	currentTokens int,
	forceCompaction bool,
) ([]message.Message, error) {
	if c.Config.ContextWindow <= 0 {
		return messages, nil
	}

	if forceCompaction || currentTokens > int(float64(c.Config.ContextWindow)*c.Config.Phase2Watermark) {
		// Run Phase 2 (Smart Compression)
		compactedMsgs, err := c.SummarizeHistory(ctx, messages, currentTokens, forceCompaction)
		if err != nil {
			return nil, err
		}
		// Run Phase 2.5 (Context-Aware State Bridge)
		compactedMsgs = c.InjectStateBridge(ctx, messages, compactedMsgs)
		// Run Phase 3 (Failsafe Token Budget Trimming)
		return c.TrimToBudget(ctx, compactedMsgs)
	} else if currentTokens > int(float64(c.Config.ContextWindow)*c.Config.Phase1Watermark) {
		// Run Phase 1 (Observation Masking)
		compactedMsgs, err := c.MaskObservations(ctx, messages, currentTokens)
		if err != nil {
			return nil, err
		}
		// Run Phase 2.5 (Context-Aware State Bridge)
		return c.InjectStateBridge(ctx, messages, compactedMsgs), nil
	}

	return messages, nil
}

// MaskObservations executes targeted observation masking on old heavy tool results.
func (c *Compactor) MaskObservations(
	ctx context.Context,
	messages []message.Message,
	currentTokens int,
) ([]message.Message, error) {
	target := int(float64(c.Config.ContextWindow) * c.Config.Phase1Target)
	tokensToReclaim := currentTokens - target
	if tokensToReclaim <= 0 {
		return messages, nil
	}

	// Find Eligible Boundary (skip ProtectedTurns assistant turns)
	eligibleBoundary := 0
	asstCount := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if _, ok := messages[i].(*message.Assistant); ok {
			asstCount++
			if asstCount == c.Config.ProtectedTurns {
				eligibleBoundary = i
				break
			}
		}
	}
	if eligibleBoundary <= 0 {
		return messages, nil
	}

	type candidate struct {
		index  int
		tokens int
		tMsg   *message.Tool
	}
	var candidates []candidate
	totalSavings := 0

	for i := 0; i < eligibleBoundary; i++ {
		tMsg, ok := messages[i].(*message.Tool)
		if !ok || tMsg.IsError {
			continue
		}
		meta := tMsg.GetMetadata()
		if meta != nil && meta["compacted"] == true {
			continue
		}
		tCount, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, []message.Message{tMsg})
		if tCount < c.Config.ToolTruncateThreshold {
			continue
		}
		// Net savings is the original token count minus this overhead.
		savings := max(tCount-maskedPlaceholderTokenOverhead, 0)
		candidates = append(candidates, candidate{
			index:  i,
			tokens: tCount,
			tMsg:   tMsg,
		})
		totalSavings += savings
	}

	if totalSavings < tokensToReclaim {
		return messages, nil // Abort Phase 1: not enough savings
	}

	cloned := make([]message.Message, len(messages))
	for i, m := range messages {
		cloned[i] = message.CloneMessage(m)
	}

	reclaimed := 0
	maskedCount := 0
	for _, cand := range candidates {
		if reclaimed >= tokensToReclaim {
			break
		}

		tMsg := cloned[cand.index].(*message.Tool)
		originalText := tMsg.Content.Text()

		var targetAsst *message.Assistant
		var targetToolCall *message.ToolCall
		for j := cand.index - 1; j >= 0; j-- {
			if asst, ok := cloned[j].(*message.Assistant); ok {
				for _, block := range asst.GetContent() {
					if tc, ok := block.(*message.ToolCall); ok && tc.ID == tMsg.ToolCallID {
						targetAsst = asst
						targetToolCall = tc
						break
					}
				}
			}
			if targetAsst != nil {
				break
			}
		}

		meta := tMsg.GetMetadata()
		if meta == nil {
			meta = make(map[string]any)
		}

		var compactedData CompactedData
		if provider, exists := CompactProviders[tMsg.Name]; exists {
			var args map[string]any
			if targetToolCall != nil {
				args = targetToolCall.Args
			}
			compactedData = provider.CompactContent(args)
		}

		if targetToolCall != nil {
			if compactedData.CompactedArgs != nil {
				targetToolCall.Args = compactedData.CompactedArgs
			} else {
				for k, v := range targetToolCall.Args {
					if sVal, ok := v.(string); ok && len(sVal) > 200 {
						targetToolCall.Args[k] = "[Truncated]"
					}
				}
			}
		}

		offloadedPath := ""
		if pathVal, ok := meta["compacted_path"].(string); ok && pathVal != "" {
			offloadedPath = pathVal
		} else if originalText != "" && c.Storage != nil {
			fileName := fmt.Sprintf("compacted/turn_%s.txt", tMsg.ToolCallID)
			destPath, err := c.Storage.Save(ctx, fileName, strings.NewReader(originalText))
			if err == nil {
				offloadedPath = destPath
				meta["compacted_path"] = offloadedPath
			}
		}

		summary := compactedData.Summary
		if summary == "" {
			summary = fmt.Sprintf("executed %s", tMsg.Name)
		}

		pathPtr := ""
		if offloadedPath != "" {
			pathPtr = fmt.Sprintf(" Offloaded to: %s", offloadedPath)
		}
		formattedText := fmt.Sprintf("[Compacted: %s -> %s. Original output was %d chars.%s]", tMsg.Name, summary, len(originalText), pathPtr)
		tMsg.Content = message.Content{&message.TextBlock{Text: formattedText}}
		meta["compacted"] = true
		tMsg.SetMetadata(meta)

		savings := max(cand.tokens-20, 0)
		reclaimed += savings
		maskedCount++
	}

	if c.MetricsStore != nil && maskedCount > 0 {
		event := coredb.MetricsEvent{
			SessionID:     c.SessionID,
			WorkspacePath: c.WorkspacePath,
			ProjectName:   c.ProjectName,
			AgentName:     c.AgentName,
			CreatedAt:     time.Now(),
		}
		payload := coredb.CompactionPayload{
			Phase:           1,
			TokensReclaimed: reclaimed,
			ToolsMasked:     maskedCount,
		}
		_ = c.MetricsStore.LogCompaction(event, payload)
	}

	if reclaimed > 0 {
		sw, hasWriter := stream.WriterFromContext(ctx)
		if hasWriter {
			msgText := fmt.Sprintf("Compaction completed. Reclaimed %d tokens using observation_masking strategy.", reclaimed)
			msg := message.NewUserText(msgText)
			msg.SetMetadata(map[string]any{
				"type":                   "system_notification",
				"is_system_notification": true,
			})
			_ = sw.Write(ctx, stream.Event{
				Name: "agent_message",
				Data: msg,
			})
		}
	}

	return cloned, nil
}

// TurnBlock represents a chunk of messages belonging to a turn.
type TurnBlock struct {
	startIndex       int
	endIndex         int
	isAnchor         bool
	userMessageCount int
}

// SummarizeHistory compresses a block of history using either Deterministic Timeline or LLM Summarization.
func (c *Compactor) SummarizeHistory(
	ctx context.Context,
	messages []message.Message,
	currentTokens int,
	forceCompaction bool,
) ([]message.Message, error) {
	target := max(int(float64(c.Config.ContextWindow)*c.Config.Phase2Target), c.Config.MinProtectedTokens)
	tokensToReclaim := currentTokens - target
	if tokensToReclaim <= 0 && !forceCompaction {
		return messages, nil
	}

	var blocks []TurnBlock
	currentBlockStart := 0
	userCount := 0
	isAnchor := false

	for i := range messages {
		msg := messages[i]
		meta := msg.GetMetadata()
		anchor := meta != nil && meta["compaction_anchor"] == true

		if i > 0 && ((msg.Role() == message.RoleAssistant) || anchor) {
			blocks = append(blocks, TurnBlock{
				startIndex:       currentBlockStart,
				endIndex:         i - 1,
				isAnchor:         isAnchor,
				userMessageCount: userCount,
			})
			currentBlockStart = i
			userCount = 0
			isAnchor = anchor
		}

		if msg.Role() == message.RoleUser {
			userCount++
		}
	}
	if len(messages) > 0 {
		blocks = append(blocks, TurnBlock{
			startIndex:       currentBlockStart,
			endIndex:         len(messages) - 1,
			isAnchor:         isAnchor,
			userMessageCount: userCount,
		})
	}

	maxCompressibleBlocks := len(blocks) - c.Config.ProtectedTurns
	if maxCompressibleBlocks <= 0 {
		return messages, nil
	}

	var selectedBlocks []TurnBlock
	selectedTokens := 0

	for i := range maxCompressibleBlocks {
		blockMsgs := messages[blocks[i].startIndex : blocks[i].endIndex+1]
		tCount, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, blockMsgs)
		selectedBlocks = append(selectedBlocks, blocks[i])
		selectedTokens += tCount
		if !forceCompaction && selectedTokens >= tokensToReclaim {
			break
		}
	}

	if len(selectedBlocks) == 0 {
		return messages, nil
	}

	firstBlock := selectedBlocks[0]
	lastBlock := selectedBlocks[len(selectedBlocks)-1]
	messagesToCondense := messages[firstBlock.startIndex : lastBlock.endIndex+1]

	totalUserCount := 0
	for _, block := range selectedBlocks {
		totalUserCount += block.userMessageCount
	}

	var compactedText string
	strategy := "timeline"

	if totalUserCount <= 1 {
		compactedText = GenerateTimeline(ctx, messages, selectedBlocks)
	} else {
		strategy = "llm_summary"
		existingSummary := ""
		if firstBlock.isAnchor {
			existingSummary = messages[firstBlock.startIndex].GetContent().Text()
		}
		var err error
		compactedText, err = c.LLMSummarize(ctx, messagesToCondense, existingSummary)
		if err != nil {
			// Fallback to timeline
			compactedText = GenerateTimeline(ctx, messages, selectedBlocks)
			strategy = "timeline"
		}
	}

	anchorMsg := message.NewSystemText(compactedText)
	meta := anchorMsg.GetMetadata()
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["compaction_anchor"] = true
	anchorMsg.SetMetadata(meta)

	sw, hasWriter := stream.WriterFromContext(ctx)
	if hasWriter && selectedTokens > 0 {
		msgText := fmt.Sprintf("Compaction completed. Reclaimed %d tokens using %s strategy.", selectedTokens, strategy)
		msg := message.NewUserText(msgText)
		msg.SetMetadata(map[string]any{
			"type":                   "system_notification",
			"is_system_notification": true,
		})
		_ = sw.Write(ctx, stream.Event{
			Name: "agent_message",
			Data: msg,
		})
	}

	var newMessages []message.Message
	newMessages = append(newMessages, anchorMsg)
	for i := lastBlock.endIndex + 1; i < len(messages); i++ {
		newMessages = append(newMessages, message.CloneMessage(messages[i]))
	}

	if c.MetricsStore != nil {
		event := coredb.MetricsEvent{
			SessionID:     c.SessionID,
			WorkspacePath: c.WorkspacePath,
			ProjectName:   c.ProjectName,
			AgentName:     c.AgentName,
			CreatedAt:     time.Now(),
		}
		payload := coredb.CompactionPayload{
			Phase:           2,
			Strategy:        strategy,
			TokensReclaimed: selectedTokens,
		}
		_ = c.MetricsStore.LogCompaction(event, payload)
	}

	return newMessages, nil
}

// GenerateTimeline formats a highly token-efficient chronological log of turn events.
func GenerateTimeline(ctx context.Context, messages []message.Message, selectedBlocks []TurnBlock) string {
	var timelineParts []string
	startIndex := 0

	if len(selectedBlocks) > 0 && selectedBlocks[0].isAnchor {
		anchorMsg := messages[selectedBlocks[0].startIndex]
		existingText := anchorMsg.GetContent().Text()
		timelineParts = append(timelineParts, existingText)
		startIndex = 1
	} else {
		timelineParts = append(timelineParts, "### Summary of Autonomous Execution")
	}

	for i := startIndex; i < len(selectedBlocks); i++ {
		block := selectedBlocks[i]
		var asstMsg *message.Assistant
		var toolResults []*message.Tool

		for idx := block.startIndex; idx <= block.endIndex; idx++ {
			msg := messages[idx]
			if am, ok := msg.(*message.Assistant); ok {
				asstMsg = am
			} else if tm, ok := msg.(*message.Tool); ok {
				toolResults = append(toolResults, tm)
			}
		}

		if asstMsg == nil {
			continue
		}

		thought := asstMsg.Content.Text()
		thought = strings.TrimSpace(strings.ReplaceAll(thought, "\n", " "))
		if len(thought) > 150 {
			thought = thought[:147] + "..."
		}
		if thought == "" {
			thought = "None"
		}

		timelineParts = append(timelineParts, "\n**Turn**")
		timelineParts = append(timelineParts, fmt.Sprintf("- **Thought**: %s", thought))

		for _, block := range asstMsg.GetContent() {
			tc, ok := block.(*message.ToolCall)
			if !ok {
				continue
			}

			var matchedResult *message.Tool
			for _, tr := range toolResults {
				if tr.ToolCallID == tc.ID {
					matchedResult = tr
					break
				}
			}

			argsStr := fmt.Sprintf("%v", tc.Args)
			actionStr := fmt.Sprintf("Executed `%s(%s)`", tc.Name, argsStr)
			timelineParts = append(timelineParts, fmt.Sprintf("- **Action**: %s", actionStr))

			resultStr := "Success."
			if matchedResult != nil {
				if matchedResult.IsError {
					resultStr = fmt.Sprintf("Failed: %s", matchedResult.Content.Text())
				} else {
					if provider, exists := TimelineProviders[tc.Name]; exists {
						timelineData := provider.TimelineContent(tc.Args)
						resultStr = timelineData.Summary
					} else {
						previewText := matchedResult.Content.Text()
						if len(previewText) > 200 {
							previewText = previewText[:197] + "..."
						}
						resultStr = previewText
					}
				}
			}
			timelineParts = append(timelineParts, fmt.Sprintf("- **Result**: %s", resultStr))
		}
	}

	return strings.Join(timelineParts, "\n")
}

// LLMSummarize invokes the model to condense history into a narrative summary.
func (c *Compactor) LLMSummarize(
	ctx context.Context,
	messagesToCondense []message.Message,
	existingSummary string,
) (string, error) {
	var compactorAgent *warp.ResolvedAgent
	if c.Workspace != nil {
		compactorAgent, _ = c.Workspace.ResolveAgent(ctx, "compaction-summarizer")
	}

	var systemPrompt string
	if compactorAgent != nil && c.Workspace != nil {
		rendered, err := prompt.RenderAgent(
			compactorAgent,
			c.Workspace.WorkspaceSpec(),
			c.Workspace.Project(),
			nil,
		)
		if err == nil {
			systemPrompt = rendered
		}
	}

	var sb strings.Builder
	if existingSummary != "" {
		sb.WriteString("Existing Summary:\n")
		sb.WriteString("<existing_summary>\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n</existing_summary>\n\n")
	}
	sb.WriteString("History to summarize:\n")
	sb.WriteString("<history>\n")
	for _, msg := range messagesToCondense {
		role := string(msg.Role())
		text := msg.GetContent().Text()
		fmt.Fprintf(&sb, "[%s]: %s\n", role, text)
	}
	sb.WriteString("</history>")

	summarizerPrompt := sb.String()

	sysMsg := message.NewSystemText(systemPrompt)
	userMsg := message.NewUserText(summarizerPrompt)

	resp, err := c.Model.Invoke(ctx, []message.Message{sysMsg, userMsg})
	if err != nil {
		return "", err
	}

	summary := resp.Content.Text()

	if c.MetricsStore != nil && resp.Metrics != nil {
		sysTokens, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, []message.Message{sysMsg})
		event := coredb.MetricsEvent{
			SessionID:     c.SessionID,
			WorkspacePath: c.WorkspacePath,
			ProjectName:   c.ProjectName,
			AgentName:     "compaction_summarizer",
			NodeName:      func(s string) *string { return &s }("compact"),
			CreatedAt:     time.Now(),
		}
		payload := coredb.LLMCallPayload{
			Provider:            c.ProviderName,
			Model:               c.ModelName,
			SystemTokens:        sysTokens,
			PromptTokens:        resp.Metrics.Tokens.Input,
			CompletionTokens:    resp.Metrics.Tokens.Output,
			TotalTokens:         resp.Metrics.TotalTokens,
			CacheCreationTokens: resp.Metrics.Tokens.CacheWrite,
			CacheReadTokens:     resp.Metrics.Tokens.CacheRead,
			EstimatedCostUSD:    resp.Metrics.TotalCost.AsUSD(),
		}
		_ = c.MetricsStore.LogLLMCall(event, payload)
	}

	return summary, nil
}

// InjectStateBridge inserts a synthetic state recovery message at the boundary.
func (c *Compactor) InjectStateBridge(ctx context.Context, originalMessages []message.Message, compactedMsgs []message.Message) []message.Message {
	if len(compactedMsgs) <= 1 {
		return compactedMsgs
	}

	// Check if the uncompacted zone contains recent todos
	hasRecentTodos := false
	for _, msg := range compactedMsgs[1:] {
		if tMsg, ok := msg.(*message.Tool); ok && tMsg.Name == "todos" {
			hasRecentTodos = true
			break
		}
	}

	var lastTodoPayload string
	if !hasRecentTodos {
		// Reverse-scan original messages for the latest non-error todos payload
		for i := len(originalMessages) - 1; i >= 0; i-- {
			if tMsg, ok := originalMessages[i].(*message.Tool); ok && tMsg.Name == "todos" && !tMsg.IsError {
				lastTodoPayload = tMsg.Content.Text()
				break
			}
		}
	}

	// Identify activated skills that were swallowed by compaction
	compactedLen := len(originalMessages) - len(compactedMsgs) + 1
	skillSet := make(map[string]bool)
	if compactedLen > 0 && compactedLen <= len(originalMessages) {
		for i := range compactedLen {
			if asstMsg, ok := originalMessages[i].(*message.Assistant); ok {
				for _, b := range asstMsg.GetContent() {
					if tc, ok := b.(*message.ToolCall); ok && tc.Name == "activate_skill" {
						if skillName, ok := tc.Args["skill"].(string); ok && skillName != "" {
							skillSet[skillName] = true
						}
					}
				}
			}
		}
	}

	var bridgeParts []string
	if lastTodoPayload != "" {
		bridgeParts = append(bridgeParts, fmt.Sprintf("Active Todos:\n%s", lastTodoPayload))
	}
	if len(skillSet) > 0 {
		var skills []string
		for s := range skillSet {
			skills = append(skills, s)
		}
		bridgeParts = append(bridgeParts, fmt.Sprintf("Utilized Skills: %s", strings.Join(skills, ", ")))
	}

	if len(bridgeParts) > 0 {
		bridgeText := fmt.Sprintf("[State Recovery:\n%s]", strings.Join(bridgeParts, "\n\n"))
		bridgeMsg := message.NewSystemText(bridgeText)

		result := make([]message.Message, 0, len(compactedMsgs)+1)
		result = append(result, compactedMsgs[0]) // Anchor
		result = append(result, bridgeMsg)        // State Bridge Message
		result = append(result, compactedMsgs[1:]...)
		return result
	}

	return compactedMsgs
}

// TrimToBudget enforces the hard context budget via loom's TrimMessages.
func (c *Compactor) TrimToBudget(ctx context.Context, messages []message.Message) ([]message.Message, error) {
	outputReserve := c.Config.OutputReserve
	if outputReserve <= 0 {
		outputReserve = max(int(float64(c.Config.ContextWindow)*0.20), 4096)
	}
	maxTokens := c.Config.ContextWindow - outputReserve
	if maxTokens <= 0 {
		return messages, nil
	}

	trimConfig := &message.TrimConfig{
		Strategy:      message.TrimStrategyLast,
		IncludeSystem: true,
		AllowPartial:  true,
		StartOn:       []message.Role{message.RoleUser},
		CountTokens: func(ctx context.Context, list message.MessageList) (int, error) {
			return llm.ApproximateTokenCounter{}.CountTokens(ctx, list)
		},
	}

	trimmed, err := message.TrimMessages(ctx, messages, maxTokens, trimConfig)
	if err != nil {
		return nil, fmt.Errorf("TrimMessages failsafe failed: %w", err)
	}
	return trimmed, nil
}
