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
        description: "Optional model override ('@fast', '@balanced', '@smart', '@flagship', '@default', '@inherit', or 'provider/model'); omit to use the subagent's default."
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
      model:
        type: string
        description: Resolved provider/model identifier of the child session, when available.
      model_label:
        type: string
        description: Display label of the resolved child model, when available.
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
- **Roster Only**: Only invoke subagents listed in `<available_subagents>`; never invent subagent names.
- **Self-Contained Prompts**: Child sessions do not inherit your conversation history — provide a concise `title` (`3–6` words) and a self-contained `prompt` with relevant file paths, constraints, and expected output. If a skill applies, instruct the subagent to call `load_skill` directly rather than summarizing it.
- **Model Routing**: Omit `model` to run on the subagent's default model — prefer omitting `model` over passing `<default_model>` explicitly, since a tier alias may be unconfigured in this workspace (omitting falls back cleanly, whereas an unconfigured explicit alias errors). Pass `model` only to deviate: a tier alias (`@fast`, `@balanced`, `@smart`, `@flagship`, `@default`), `@inherit`, or a `provider/model` ID verified via `manage_agents` `action: "models"` (subject to `<allowed_models>` when declared).
- **Workspace Isolation**: Use `workspace: "inherit"` (default) to share the current working directory, or `workspace: "worktree"` to isolate risky refactors or parallel conflicting edits in a git worktree.
- **Asynchronous Execution**: Subagents run in parallel in the background and automatically deliver `<subagent_completed>` to wake you up when done — do **not** poll `manage_agents` `status` in a loop.
</guidelines>
