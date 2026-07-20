package graph

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/warp"
)

const toolsTemplateBlock = `{{if .Agent.Tools}}
<tools_usage>
- **Strictly prefer specialized tools over bash.** Never use bash commands for tasks that can be done with dedicated tools (e.g., use file viewing/editing tools instead of "cat"/"sed", search tools instead of "grep"). Reserve bash for execution (builds, tests, running scripts).
- Search before assuming
- Read files before editing
- Always use absolute paths for file operations (editing, reading, writing).
- When making multiple independent bash calls, send them in a single message with multiple tool calls for parallel execution
- Summarize tool output for user (they don't see it)
- Never use "curl" through the bash tool; it is not allowed—use the fetch tool instead.
- Only use the tools you know exist based on your provided schema.
- When to use tools: use search tools to gather context, view tools to inspect file contents, write/replace tools to make modifications, and execution tools (like bash) to run tests or build commands.

### "batch" tool

**IMPORTANT**: The "description" field is REQUIRED for all "batch" tool calls. Always provide it.

When running non-trivial bash commands (especially those that modify the system):
- Always provide a clear description of what the command does and why you are running it, this ensures that the user understands the intent and can trust your actions.
- Simple read-only commands does not require a detailed description.
- Avoid interactive commmands - use non-interactive flags (e.g., "-y" for npm init) to prevent blocking.
</tools_usage>
{{else}}
<tools_usage>
NONE. You have absolutely no external tools, system access, or file-writing capabilities. You must interact with the user purely via text. Do not attempt to generate XML tool tags, JSON tool calls, or invoke commands.
</tools_usage>
{{end}}`

const skillsTemplateBlock = `{{if .Agent.Skills}}
<skills_and_specialized_knowledge>
You have access to specialized knowledge modules called "Skills". Each skill provides essential domain-specific instructions, conventions, and guidelines.

<available_skills>
{{range .Agent.Skills}}
<skill>
  <name>{{.Name}}</name>
  <description>{{.Description}}</description>
  {{if .UseWhen}}<use_when>{{.UseWhen}}</use_when>{{end}}
  {{if .Keywords}}<keywords>{{range .Keywords}}{{.}} {{end}}</keywords>{{end}}
</skill>
{{end}}
</available_skills>

<skill_usage_rules>
- **Triggers**: Treat the "<description>", "<use_when>", and "<keywords>" of each "<skill>" as *triggers*. If your current task or sub-task involves the concepts mentioned in these fields, you MUST activate that skill to access the required knowledge.
- **First Action**: If a task matches a skill trigger, activating that skill must be the **absolute first action** you take, before searching the codebase, reading files, or formulating plans.
- **Activation Mechanism**: You do not have the skill's knowledge by default. You must call the "skill_activate" tool with the skill's "<name>" to load its instructions.
- **Context Retention**: Activate a relevant skill only once per task. Do not repeatedly activate it once its knowledge is loaded into your context.
- **Deep Exploration**: If a skill provides additional assets (like "scripts/", "examples/", or "references/"), use your tools to read and utilize them as guided by the skill's main instructions.
- **Subagent Delegation**: If delegating a task to a subagent, instruct the subagent to activate the relevant skill itself rather than trying to summarize the skill for them.
</skill_usage_rules>
</skills_and_specialized_knowledge>
{{else}}
<skills_and_specialized_knowledge>
NONE. You do not have access to specialized skills. Do not attempt to activate skills.
</skills_and_specialized_knowledge>
{{end}}`

const subagentsTemplateBlock = `{{if and (call .HasTool "invoke_agent") .Agent.Subagents}}
<subagents_and_delegation>
You have access to specialized subagents in your roster. You can delegate tasks to them using the "invoke_agent" tool.

<available_subagents>
{{range .Agent.Subagents}}
<subagent>
  <name>{{.Name}}</name>
  <description>{{.Description}}</description>
</subagent>
{{end}}
</available_subagents>

<delegation_rules>
- **No Hallucinations**: You must only delegate to subagents that are explicitly listed in the "<available_subagents>" list above. Never make up or hallucinate non-existent subagents.
- **Task Description**: Provide a clear, detailed instruction task description to the subagent so it knows exactly what to do.
- **Asynchronous Work**: If a subagent execution takes longer than "wait_ms", it will run in the background. The system will notify you with the final response once it finishes.
</delegation_rules>
</subagents_and_delegation>
{{else}}
<subagents_and_delegation>
NONE. You cannot delegate tasks or invoke other agents. Do not attempt to use the invoke_agent tool or call subagents.
</subagents_and_delegation>
{{end}}`

const environmentTemplateBlock = `<execution_environment>
You are operating under the following environment:
- Current Date: {{.Date}}
- Operating System: {{.OS}}
- Architecture: {{.Arch}}
- User: {{.User}}
- Host: {{.Host}}
- Shell: {{.Shell}}
- Home Directory: {{.Home}}
- Workspace Directory: {{.CWD}}
- Terminal: {{.Terminal}}
{{if .GitBranch}}- Git Branch: {{.GitBranch}}{{end}}
</execution_environment>`

const contextTemplateBlock = `{{if .Context}}
{{if .Context.Instructions}}
<project_context path="{{.Context.Path}}">
{{.Context.Instructions}}
</project_context>
{{end}}
{{end}}`

// AgentManifestPreWriteHook processes written agent manifests to append standard templates.
func AgentManifestPreWriteHook(filePath string, content string) (string, error) {
	// 1. Parse using official WARP parser
	res, err := warp.Parse(filePath, content)
	if err != nil {
		// Not a valid WARP resource, return original content
		return content, nil
	}

	// 2. Check if the resource is an Agent
	agent, ok := res.Resource.(*warp.Agent)
	if !ok {
		return content, nil
	}

	modified := false
	inst := agent.Spec.Instructions

	// Append/Prepend blocks unconditionally if missing. The runtime templates will dynamically
	// evaluate and render the usage blocks (if enabled) or the negation blocks (if disabled).

	// 3. Prepend environment template block if missing
	if !strings.Contains(inst, "<execution_environment>") && !strings.Contains(inst, "{{.OS}}") {
		inst = environmentTemplateBlock + "\n\n" + strings.TrimSpace(inst)
		modified = true
	}

	// 4. Append tools template block if missing
	if !strings.Contains(inst, "<tools_usage>") && !strings.Contains(inst, "{{if .Agent.Tools}}") {
		inst = strings.TrimSpace(inst) + "\n\n" + toolsTemplateBlock + "\n"
		modified = true
	}

	// 5. Append skills template block if missing
	if !strings.Contains(inst, "<skills_and_specialized_knowledge>") && !strings.Contains(inst, "{{if .Agent.Skills}}") {
		inst = strings.TrimSpace(inst) + "\n\n" + skillsTemplateBlock + "\n"
		modified = true
	}

	// 6. Append subagents template block if missing
	if !strings.Contains(inst, "<subagents_and_delegation>") && !strings.Contains(inst, "{{if .Agent.Subagents}}") && !strings.Contains(inst, "invoke_agent") {
		inst = strings.TrimSpace(inst) + "\n\n" + subagentsTemplateBlock + "\n"
		modified = true
	}

	// 7. Append project context template block if missing
	if !strings.Contains(inst, "<project_context>") && !strings.Contains(inst, "{{.Context}}") {
		inst = strings.TrimSpace(inst) + "\n\n" + contextTemplateBlock + "\n"
		modified = true
	}

	// 7. If modified, format the resource back to standard WARP representation
	if modified {
		agent.Spec.Instructions = inst
		formattedBytes, err := warp.Format(agent)
		if err != nil {
			return "", fmt.Errorf("failed to format enriched agent: %w", err)
		}
		return string(formattedBytes), nil
	}

	return content, nil
}
