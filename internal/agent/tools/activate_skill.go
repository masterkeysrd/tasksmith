package tools

import (
	"context"
	"fmt"
)

// ActivateSkill activates a specialized skill to load its domain-specific instructions.
func (h *ToolHandlers) ActivateSkill(ctx context.Context, in ActivateSkillArgs) (ActivateSkillOutput, error) {
	if h.SkillResolver == nil {
		return ActivateSkillOutput{
			Success: false,
			Message: "Skill resolver is not configured in this session.",
		}, nil
	}

	instructions, path, err := h.SkillResolver.ResolveSkill(ctx, in.Skill)
	if err != nil {
		return ActivateSkillOutput{
			Success: false,
			Message: fmt.Sprintf("Failed to resolve skill: %v", err),
		}, nil
	}

	return ActivateSkillOutput{
		Success:      true,
		Instructions: instructions,
		Path:         path,
		Message:      fmt.Sprintf("Successfully activated skill %q from path: %s", in.Skill, path),
	}, nil
}

// TextContent returns the rendered text content of the skill activation.
func (o ActivateSkillOutput) TextContent() string {
	if !o.Success {
		return o.Message
	}

	return fmt.Sprintf("Success: Activated skill from path: %s.\n\nInstructions:\n%s", o.Path, o.Instructions)
}
