package tools

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
)

// ActivateSkill activates a specialized skill to load its domain-specific instructions.
func (h *ToolHandlers) ActivateSkill(ctx context.Context, in ActivateSkillArgs) (ActivateSkillOutput, error) {
	if h.Resolver == nil {
		return ActivateSkillOutput{
			Success: false,
			Message: "Resolver is not configured in this session.",
		}, nil
	}

	path, err := h.Resolver.ResolvePath(ctx, in.Skill, resolver.TypeSkill, h.AgentName)
	if err != nil {
		return ActivateSkillOutput{
			Success: false,
			Message: fmt.Sprintf("Failed to resolve skill path: %v", err),
		}, nil
	}

	res, err := h.Resolver.LoadResource(ctx, path, resolver.TypeSkill, h.AgentName)
	if err != nil {
		return ActivateSkillOutput{
			Success: false,
			Message: fmt.Sprintf("Failed to load skill: %v", err),
		}, nil
	}

	skillRes, ok := res.(*resolver.ResolvedSkill)
	if !ok {
		return ActivateSkillOutput{
			Success: false,
			Message: "Resolved resource is not a skill.",
		}, nil
	}

	return ActivateSkillOutput{
		Success:      true,
		Instructions: skillRes.Instructions,
		Path:         skillRes.SkillPath,
		Message:      fmt.Sprintf("Successfully activated skill %q from path: %s", in.Skill, skillRes.SkillPath),
	}, nil
}

// TextContent returns the rendered text content of the skill activation.
func (o ActivateSkillOutput) TextContent() string {
	if !o.Success {
		return o.Message
	}

	return fmt.Sprintf("Success: Activated skill from path: %s.\n\nInstructions:\n%s", o.Path, o.Instructions)
}
