package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

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
func ExtractTokensFromLastResponse(messages []message.Message) int {
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
		t, _ := llm.ApproximateTokenCounter{}.CountTokens(context.TODO(), []message.Message{messages[i]})
		tokens += t
	}
	return tokens
}

// GetTextFromContent extracts raw text from the content blocks.
func GetTextFromContent(content message.Content) string {
	var sb strings.Builder
	for _, b := range content {
		if tb, ok := b.(*message.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

// CloneMessage performs a deep copy of a message.
func CloneMessage(msg message.Message) message.Message {
	if msg == nil {
		return nil
	}
	switch m := msg.(type) {
	case *message.Assistant:
		clonedContent := make(message.Content, len(m.Content))
		for i, b := range m.Content {
			switch block := b.(type) {
			case *message.ToolCall:
				tcCloned := &message.ToolCall{
					ID:   block.ID,
					Name: block.Name,
					Args: make(map[string]any),
				}
				for k, v := range block.Args {
					tcCloned.Args[k] = v
				}
				clonedContent[i] = tcCloned
			default:
				clonedContent[i] = b
			}
		}
		asst := &message.Assistant{
			Base:    m.Base,
			Content: clonedContent,
			Metrics: m.Metrics,
		}
		if m.GetMetadata() != nil {
			meta := make(map[string]any)
			for k, v := range m.GetMetadata() {
				meta[k] = v
			}
			asst.SetMetadata(meta)
		}
		return asst
	case *message.Tool:
		clonedContent := make(message.Content, len(m.Content))
		copy(clonedContent, m.Content)
		tMsg := &message.Tool{
			Base:              m.Base,
			ToolCallID:        m.ToolCallID,
			Name:              m.Name,
			Content:           clonedContent,
			IsError:           m.IsError,
			StructuredContent: m.StructuredContent,
		}
		if m.GetMetadata() != nil {
			meta := make(map[string]any)
			for k, v := range m.GetMetadata() {
				meta[k] = v
			}
			tMsg.SetMetadata(meta)
		}
		return tMsg
	case *message.User:
		clonedContent := make(message.Content, len(m.Content))
		copy(clonedContent, m.Content)
		user := &message.User{
			Base:    m.Base,
			Content: clonedContent,
		}
		if m.GetMetadata() != nil {
			meta := make(map[string]any)
			for k, v := range m.GetMetadata() {
				meta[k] = v
			}
			user.SetMetadata(meta)
		}
		return user
	case *message.System:
		clonedContent := make(message.Content, len(m.Content))
		copy(clonedContent, m.Content)
		sys := &message.System{
			Base:    m.Base,
			Content: clonedContent,
		}
		if m.GetMetadata() != nil {
			meta := make(map[string]any)
			for k, v := range m.GetMetadata() {
				meta[k] = v
			}
			sys.SetMetadata(meta)
		}
		return sys
	default:
		return msg
	}
}

// resolveModelProviderNames looks up model settings from the workspace.
func resolveModelProviderNames(ctx context.Context, ws *workspace.Workspace, agentName string) (string, string) {
	if ws == nil {
		return "unknown", "unknown"
	}
	var providerName, modelName string
	providers := ws.Providers()
	if agentName != "" {
		if agent, err := ws.ResolveAgent(ctx, agentName); err == nil && agent != nil && agent.Agent != nil && len(agent.Agent.Spec.Models) > 0 {
			for _, modelID := range agent.Agent.Spec.Models {
				for _, p := range providers {
					for _, mInfo := range p.Spec.Models {
						if mInfo.ID == modelID {
							providerName = p.GetName()
							modelName = modelID
							break
						}
					}
					if modelName != "" {
						break
					}
				}
				if modelName != "" {
					break
				}
			}
		}
	}
	if providerName == "" || modelName == "" {
		if len(providers) > 0 {
			providerName = providers[0].GetName()
			if len(providers[0].Spec.Models) > 0 {
				modelName = providers[0].Spec.Models[0].ID
			}
		}
	}
	if providerName == "" {
		providerName = "unknown"
	}
	if modelName == "" {
		modelName = "unknown"
	}
	return providerName, modelName
}

// CompactMessages applies the full compaction pipeline to a message list.
func CompactMessages(
	ctx context.Context,
	messages []message.Message,
	currentTokens int,
	config CompactionConfig,
	forceCompaction bool,
	cwd string,
	model LLM,
	metricsStore *metrics.Store,
	sessionID string,
	wsPath string,
	projectName string,
	agentName string,
	ws *workspace.Workspace,
) ([]message.Message, error) {
	if config.ContextWindow <= 0 {
		return messages, nil
	}

	if forceCompaction || currentTokens > int(float64(config.ContextWindow)*config.Phase2Watermark) {
		// Run Phase 2 (Smart Compression)
		compactedMsgs, err := RunPhase2(ctx, messages, currentTokens, config, forceCompaction, model, metricsStore, sessionID, wsPath, projectName, agentName, ws)
		if err != nil {
			return nil, err
		}
		// Run Phase 2.5 (Context-Aware State Bridge)
		compactedMsgs = RunPhase2_5(ctx, messages, compactedMsgs)
		// Run Phase 3 (Failsafe Token Budget Trimming)
		return RunPhase3(ctx, compactedMsgs, config)
	} else if currentTokens > int(float64(config.ContextWindow)*config.Phase1Watermark) {
		// Run Phase 1 (Observation Masking)
		compactedMsgs, err := RunPhase1(ctx, messages, currentTokens, config, cwd, metricsStore, sessionID, wsPath, projectName, agentName)
		if err != nil {
			return nil, err
		}
		// Run Phase 2.5 (Context-Aware State Bridge)
		return RunPhase2_5(ctx, messages, compactedMsgs), nil
	}

	return messages, nil
}

// RunPhase1 executes targeted observation masking on old heavy tool results.
func RunPhase1(
	ctx context.Context,
	messages []message.Message,
	currentTokens int,
	config CompactionConfig,
	cwd string,
	metricsStore *metrics.Store,
	sessionID string,
	wsPath string,
	projectName string,
	agentName string,
) ([]message.Message, error) {
	target := int(float64(config.ContextWindow) * config.Phase1Target)
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
			if asstCount == config.ProtectedTurns {
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
		if tCount < config.ToolTruncateThreshold {
			continue
		}
		savings := tCount - 20
		if savings < 0 {
			savings = 0
		}
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
		cloned[i] = CloneMessage(m)
	}

	reclaimed := 0
	maskedCount := 0
	for _, cand := range candidates {
		if reclaimed >= tokensToReclaim {
			break
		}

		tMsg := cloned[cand.index].(*message.Tool)
		originalText := GetTextFromContent(tMsg.Content)

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
		if pathVal, ok := meta["OffloadedPath"].(string); ok && pathVal != "" {
			offloadedPath = pathVal
		} else if originalText != "" && cwd != "" {
			dirPath := filepath.Join(cwd, ".tasksmith", "compacted")
			_ = os.MkdirAll(dirPath, 0755)
			fileName := fmt.Sprintf("turn_%s.txt", tMsg.ToolCallID)
			filePath := filepath.Join(dirPath, fileName)
			_ = os.WriteFile(filePath, []byte(originalText), 0644)
			offloadedPath = filepath.Join(".tasksmith", "compacted", fileName)
			meta["CompactedPath"] = offloadedPath
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

		savings := cand.tokens - 20
		if savings < 0 {
			savings = 0
		}
		reclaimed += savings
		maskedCount++
	}

	if metricsStore != nil && maskedCount > 0 {
		event := coredb.MetricsEvent{
			SessionID:     sessionID,
			WorkspacePath: wsPath,
			ProjectName:   projectName,
			AgentName:     agentName,
			CreatedAt:     time.Now(),
		}
		payload := coredb.CompactionPayload{
			Phase:           1,
			TokensReclaimed: reclaimed,
			ToolsMasked:     maskedCount,
		}
		_ = metricsStore.LogCompaction(event, payload)
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

// RunPhase2 compresses a block of history using either Deterministic Timeline or LLM Summarization.
func RunPhase2(
	ctx context.Context,
	messages []message.Message,
	currentTokens int,
	config CompactionConfig,
	forceCompaction bool,
	model LLM,
	metricsStore *metrics.Store,
	sessionID string,
	wsPath string,
	projectName string,
	agentName string,
	ws *workspace.Workspace,
) ([]message.Message, error) {
	target := int(float64(config.ContextWindow) * config.Phase2Target)
	if target < config.MinProtectedTokens {
		target = config.MinProtectedTokens
	}
	tokensToReclaim := currentTokens - target
	if tokensToReclaim <= 0 && !forceCompaction {
		return messages, nil
	}

	var blocks []TurnBlock
	currentBlockStart := 0
	userCount := 0
	isAnchor := false

	for i := 0; i < len(messages); i++ {
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

	maxCompressibleBlocks := len(blocks) - config.ProtectedTurns
	if maxCompressibleBlocks <= 0 {
		return messages, nil
	}

	var selectedBlocks []TurnBlock
	selectedTokens := 0

	for i := 0; i < maxCompressibleBlocks; i++ {
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
			existingSummary = GetTextFromContent(messages[firstBlock.startIndex].GetContent())
		}
		var err error
		compactedText, err = LLMSummarize(ctx, model, messagesToCondense, existingSummary, metricsStore, sessionID, wsPath, projectName, agentName, ws)
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

	var newMessages []message.Message
	newMessages = append(newMessages, anchorMsg)
	for i := lastBlock.endIndex + 1; i < len(messages); i++ {
		newMessages = append(newMessages, CloneMessage(messages[i]))
	}

	if metricsStore != nil {
		event := coredb.MetricsEvent{
			SessionID:     sessionID,
			WorkspacePath: wsPath,
			ProjectName:   projectName,
			AgentName:     agentName,
			CreatedAt:     time.Now(),
		}
		payload := coredb.CompactionPayload{
			Phase:           2,
			Strategy:        strategy,
			TokensReclaimed: selectedTokens,
		}
		_ = metricsStore.LogCompaction(event, payload)
	}

	return newMessages, nil
}

// GenerateTimeline formats a highly token-efficient chronological log of turn events.
func GenerateTimeline(ctx context.Context, messages []message.Message, selectedBlocks []TurnBlock) string {
	var timelineParts []string
	startIndex := 0

	if len(selectedBlocks) > 0 && selectedBlocks[0].isAnchor {
		anchorMsg := messages[selectedBlocks[0].startIndex]
		existingText := GetTextFromContent(anchorMsg.GetContent())
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

		thought := GetTextFromContent(asstMsg.Content)
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
					resultStr = fmt.Sprintf("Failed: %s", GetTextFromContent(matchedResult.Content))
				} else {
					if provider, exists := TimelineProviders[tc.Name]; exists {
						timelineData := provider.TimelineContent(tc.Args)
						resultStr = timelineData.Summary
					} else {
						previewText := GetTextFromContent(matchedResult.Content)
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
func LLMSummarize(
	ctx context.Context,
	model LLM,
	messagesToCondense []message.Message,
	existingSummary string,
	metricsStore *metrics.Store,
	sessionID string,
	wsPath string,
	projectName string,
	agentName string,
	ws *workspace.Workspace,
) (string, error) {
	var sb strings.Builder
	sb.WriteString("You are the compaction_summarizer sub-agent. Your task is to summarize the following conversation history.\n")
	if existingSummary != "" {
		sb.WriteString("Here is the existing summary of the previous part of the conversation. You must merge the new history into this existing summary:\n")
		sb.WriteString("<existing_summary>\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n</existing_summary>\n\n")
	}
	sb.WriteString("Here is the history to summarize:\n")
	sb.WriteString("<history>\n")
	for _, msg := range messagesToCondense {
		role := string(msg.Role())
		text := GetTextFromContent(msg.GetContent())
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", role, text))
	}
	sb.WriteString("</history>\n\n")
	sb.WriteString("Write a cohesive, dense, token-efficient summary. Avoid placeholders.")

	summarizerPrompt := sb.String()

	sysMsg := message.NewSystemText("You are a helpful assistant specialized in conversation summarization.")
	userMsg := message.NewUserText(summarizerPrompt)

	resp, err := model.Invoke(ctx, []message.Message{sysMsg, userMsg})
	if err != nil {
		return "", err
	}

	summary := GetTextFromContent(resp.Content)

	if metricsStore != nil && resp.Metrics != nil {
		providerName, modelName := resolveModelProviderNames(ctx, ws, agentName)
		sysTokens, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, []message.Message{sysMsg})
		event := coredb.MetricsEvent{
			SessionID:     sessionID,
			WorkspacePath: wsPath,
			ProjectName:   projectName,
			AgentName:     "compaction_summarizer",
			NodeName:      func(s string) *string { return &s }("think"),
			CreatedAt:     time.Now(),
		}
		payload := coredb.LLMCallPayload{
			Provider:            providerName,
			Model:               modelName,
			SystemTokens:        sysTokens,
			PromptTokens:        resp.Metrics.Tokens.Input,
			CompletionTokens:    resp.Metrics.Tokens.Output,
			TotalTokens:         resp.Metrics.TotalTokens,
			CacheCreationTokens: resp.Metrics.Tokens.CacheWrite,
			CacheReadTokens:     resp.Metrics.Tokens.CacheRead,
			EstimatedCostUSD:    resp.Metrics.TotalCost.AsUSD(),
		}
		_ = metricsStore.LogLLMCall(event, payload)
	}

	return summary, nil
}

// RunPhase2_5 inserts a synthetic state recovery message at the boundary.
func RunPhase2_5(ctx context.Context, originalMessages []message.Message, compactedMsgs []message.Message) []message.Message {
	if len(compactedMsgs) <= 1 {
		return compactedMsgs
	}

	// Check if the uncompacted zone contains recent todos
	hasRecentTodos := false
	for _, msg := range compactedMsgs[1:] {
		if tMsg, ok := msg.(*message.Tool); ok && (tMsg.Name == "todos" || tMsg.Name == "update_todos" || tMsg.Name == "get_todos") {
			hasRecentTodos = true
			break
		}
	}

	var lastTodoPayload string
	if !hasRecentTodos {
		// Reverse-scan original messages for the latest non-error todos payload
		for i := len(originalMessages) - 1; i >= 0; i-- {
			if tMsg, ok := originalMessages[i].(*message.Tool); ok && (tMsg.Name == "todos" || tMsg.Name == "update_todos" || tMsg.Name == "get_todos") && !tMsg.IsError {
				lastTodoPayload = GetTextFromContent(tMsg.Content)
				break
			}
		}
	}

	// Identify activated skills that were swallowed by compaction
	compactedLen := len(originalMessages) - len(compactedMsgs) + 1
	skillSet := make(map[string]bool)
	if compactedLen > 0 && compactedLen <= len(originalMessages) {
		for i := 0; i < compactedLen; i++ {
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

// RunPhase3 enforces the hard context budget via loom's TrimMessages.
func RunPhase3(ctx context.Context, messages []message.Message, config CompactionConfig) ([]message.Message, error) {
	outputReserve := config.OutputReserve
	if outputReserve <= 0 {
		outputReserve = int(float64(config.ContextWindow) * 0.20)
		if outputReserve < 4096 {
			outputReserve = 4096
		}
	}
	maxTokens := config.ContextWindow - outputReserve
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
