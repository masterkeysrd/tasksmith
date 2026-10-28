package api

import "fmt"

// TextContent returns the rendered instructions for the loaded skill to provide to the LLM.
func (o LoadSkillOutput) TextContent() string {
	if o.Path != "" {
		return fmt.Sprintf("Base directory: %s\n\n%s", o.Path, o.Instructions)
	}
	return o.Instructions
}
