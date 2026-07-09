package graph

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

// compact is a dedicated Loom node that manages context window token budgets.
func (a *AgentGraph) compact(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	if a.compaction.ContextWindow == 0 {
		return nil, nil // Disabled
	}

	// Fast path: Extract exact token count from the last LLM response usage metrics
	currentTokens := ExtractTokensFromLastResponse(ctx, s.Messages)
	if currentTokens == 0 {
		// Fallback: Manually recount if metrics are missing
		sysTokens, _ := llm.ApproximateTokenCounter{}.CountTokens(
			ctx, message.MessageList{message.NewSystemText(a.systemPrompt)},
		)
		msgTokens, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, s.Messages)
		currentTokens = sysTokens + msgTokens
	}

	compactor := &Compactor{
		Config:        a.compaction,
		Storage:       a.storage,
		Model:         a.model,
		MetricsStore:  a.metricsStore,
		SessionID:     a.sessionID,
		WorkspacePath: a.wsPath,
		ProjectName:   a.projectName,
		AgentName:     a.agentName,
		ProviderName:  a.providerName,
		ModelName:     a.modelName,
		Workspace:     a.workspace,
	}
	compactedMessages, err := compactor.Compact(
		ctx,
		s.Messages,
		currentTokens,
		s.ForceCompaction,
	)
	if err != nil {
		log.Warn(fmt.Sprintf("[AgentGraph] compaction failed: %v", err))
		return nil, nil
	}

	// Mutate the actual AgentState so checkpoints are physically reduced in size,
	// and reset the ForceCompaction flag to false.
	return graph.Update[AgentState](func(state AgentState) AgentState {
		state.Messages = compactedMessages
		state.ForceCompaction = false
		return state
	}), nil
}
