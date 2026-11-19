---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: enter_plan_mode
  displayName: Enter Plan Mode
  description: Transition the session into plan mode to investigate and draft an implementation plan before making changes.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Enter plan mode"
  inputSchema:
    type: object
    properties: {}
  outputSchema:
    type: object
    properties:
      mode:
        type: string
        description: Active session permission mode after entering plan mode.
      already_active:
        type: boolean
        description: True if the session was already in plan mode.
      plans_dir:
        type: string
        description: Session-scoped safe directory for storing plan documents.
      instructions:
        type: string
        description: Behavioral contract and guidance for plan mode.
    required:
      - mode
      - plans_dir
      - instructions
---

Transition the current session into `plan` mode (idempotent; returns `plans_dir`) to investigate the codebase and produce a structured plan before modifying project code.

<guidelines>
- **When to use**: Non-trivial tasks with architectural tradeoffs, multiple affected components, or ambiguous requirements, or whenever the user asks to plan first.
- **Workflow**: Explore using read-only tools, clarify ambiguities via `ask_question`, ask via `ask_question` whether to save the plan in `plans_dir` or the workspace directory, write the plan file, and call `exit_plan_mode` with its `path` for review.
</guidelines>
