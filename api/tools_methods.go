package api

// TextContent returns the rendered instructions for the loaded skill to provide to the LLM.
func (o LoadSkillOutput) TextContent() string {
	return o.Instructions
}
