---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: invoke_agent
  displayName: Invoke Subagent
  description: Spawn a specialized subagent asynchronously in an isolated child session.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Delegate to subagent"
  inputSchema:
    type: object
    properties:
      agent:
        type: string
        description: Name of the subagent to invoke (must be listed in <available_subagents>).
      title:
        type: string
        description: Concise 3-6 word summary of the delegated task for UI display.
      prompt:
        type: string
        description: Detailed task instructions for the subagent.
      workspace:
        type: string
        description: "Workspace mode for the subagent: 'inherit' (default, shares current working directory) or 'worktree' (creates an isolated git worktree)."
      model:
        type: string
        description: "Optional model override for the subagent: a tier alias ('@fast', '@balanced', '@smart', '@flagship', '@default'), '@inherit' to reuse this session's model, or a provider/model identifier. Must be permitted by the subagent's spec.models allowlist when it declares one."
    required:
      - agent
      - title
      - prompt
  outputSchema:
    type: object
    properties:
      session_id:
        type: string
        description: Session ID of the spawned subagent.
      agent:
        type: string
        description: Name of the subagent executing the task.
      title:
        type: string
        description: Summary title of the delegated task.
      work_dir:
        type: string
        description: Working directory where the subagent is executing.
      status:
        type: string
        description: Execution status of the subagent ('running').
      message:
        type: string
        description: Status message explaining that the subagent is running asynchronously.
    required:
      - session_id
      - agent
      - title
      - work_dir
      - status
      - message
---

Spawn an autonomous specialized subagent asynchronously in an isolated child session.

<guidelines>
- **Roster Only**: Only invoke subagents explicitly available in your roster (such as `<available_subagents>`). Never invent or guess subagent names.
- **Self-Contained Prompts**: Each subagent runs in an isolated child session without your conversation history. Provide `title` (concise 3-6 word UI summary) and a clear, self-contained `prompt` with relevant file paths, constraints, and expected output. If a skill applies, instruct the subagent to load it directly rather than summarizing it.
- **Model Routing**: Omit `model` to run the subagent on its default model (the first resolvable entry in its `spec.models` / `<allowed_models>`) — prefer omitting it over passing the default explicitly, since a tier alias may be unconfigured in this workspace. Pass `model` only to deviate: a tier alias (`@fast`, `@balanced`, `@smart`, `@flagship`, `@default`), `@inherit` to reuse your current session model, or an explicit `provider/model` ID (discover valid IDs with `manage_agents` `action: "models"` instead of guessing). When a subagent declares `<allowed_models>`, only matching references are accepted; a subagent without `<allowed_models>` accepts any model.
- **Workspace Isolation**: Use `workspace: "inherit"` (the default) to share the current working directory, or `workspace: "worktree"` to run risky refactors or parallel conflicting edits in an isolated git worktree.
- **Asynchronous Execution**: Subagents execute in parallel in the background. When a subagent turn finishes, the system automatically delivers a `<subagent_completed>` notification to your execution queue and wakes you up. Do NOT poll `manage_agents` `status` in a loop; continue independent work or stop calling tools to wait.
</guidelines>
