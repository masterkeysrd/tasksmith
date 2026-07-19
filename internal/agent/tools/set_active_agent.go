package tools

import (
	"context"
)

// SetActiveAgent handles switching the session's active agent.
func (h *ToolHandlers) SetActiveAgent(ctx context.Context, in SetActiveAgentArgs) (SetActiveAgentOutput, error) {
	if h.OnSetActiveAgent != nil {
		if err := h.OnSetActiveAgent(ctx, in.AgentName); err != nil {
			return SetActiveAgentOutput{}, err
		}
	}

	return SetActiveAgentOutput{}, nil
}

// TextContent returns a clean summary of the tool output.
func (o SetActiveAgentOutput) TextContent() string {
	return "Success: Active agent updated."
}
