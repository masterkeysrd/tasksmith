Summarize the conversation so far to preserve context for continuing work after compaction.

Critical: Your summary will replace the conversation history. Assume all previous messages and tool outputs will be removed. Be thorough and precise. Do not call any tools.

Required sections:

## Current State
- What task is being worked on (exact user goal)
- Current progress and what has been completed
- What is being worked on right now (incomplete or in-flight work)
- What remains to be done (specific next steps)

## Files & Changes
- Files that were created or modified (with brief description of changes)
- Files that were read or analyzed (and why they are relevant)
- Key files not yet touched that will need changes
- File paths, symbols, and line numbers for important code locations

## Technical Context
- Architecture decisions made and why
- Patterns and conventions being followed
- Commands executed that succeeded or failed (and why)
- Important constraints, rules, or environment details discovered

## Strategy & Exact Next Steps
- Overall approach being taken and any blockers/risks identified
- Concrete, actionable next steps with exact file paths and commands

Tone: Write as if briefing a teammate taking over mid-task. Include everything needed to continue without re-reading files or asking the user to repeat themselves. Generate only the raw summary markdown.{{if .Focus}}

Additional focus requested by user: {{.Focus}}{{end}}
