package tools

import (
	"context"
)

// AgentDefine defines a subagent.
func (h *ToolHandlers) AgentDefine(ctx context.Context, in AgentDefineArgs) (AgentDefineOutput, error) {
	return AgentDefineOutput{Success: false, Error: "not implemented"}, nil
}

// AgentInvoke invokes subagents.
func (h *ToolHandlers) AgentInvoke(ctx context.Context, in AgentInvokeArgs) (AgentInvokeOutput, error) {
	return AgentInvokeOutput{Success: false, Error: "not implemented"}, nil
}

// AgentManage manages subagents.
func (h *ToolHandlers) AgentManage(ctx context.Context, in AgentManageArgs) (AgentManageOutput, error) {
	return AgentManageOutput{Success: false, Error: "not implemented"}, nil
}

// AgentSendMessage sends a message to a subagent.
func (h *ToolHandlers) AgentSendMessage(ctx context.Context, in AgentSendMessageArgs) (AgentSendMessageOutput, error) {
	return AgentSendMessageOutput{Success: false, Error: "not implemented"}, nil
}

// AskQuestion asks the user a question.
func (h *ToolHandlers) AskQuestion(ctx context.Context, in AskQuestionArgs) (AskQuestionOutput, error) {
	return AskQuestionOutput{Success: false}, nil
}

// Schedule schedules a timer or cron task.
func (h *ToolHandlers) Schedule(ctx context.Context, in ScheduleArgs) (ScheduleOutput, error) {
	return ScheduleOutput{Success: false, Error: "not implemented"}, nil
}
