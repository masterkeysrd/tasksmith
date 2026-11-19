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

Transition the current session into `plan` mode so you can investigate the codebase and produce a structured implementation plan before modifying project code.

<when_to_use>
- Non-trivial implementation tasks with architectural choices, multiple affected components, or ambiguous requirements where alignment on a plan saves rework.
- When the user asks to plan or design an approach before making changes.
</when_to_use>

<how_it_works>
- Calling `enter_plan_mode` is idempotent: if the session is already in `plan` mode, it confirms the active mode and returns the session's safe plans directory (`plans_dir`).
- While in `plan` mode, investigate using read-only tools, clarify ambiguities via `ask_question`, ask the user via `ask_question` whether to save the plan document in `plans_dir` or in the current workspace directory, write the plan file, and call `exit_plan_mode` with its `path` for review.
</how_it_works>
