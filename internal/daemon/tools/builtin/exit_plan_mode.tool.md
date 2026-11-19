---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: exit_plan_mode
  displayName: Exit Plan Mode
  description: Submit a finished plan document for user review and exit plan mode upon approval.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Submit plan: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the finished plan file inside the workspace or session plans directory.
    required:
      - path
  outputSchema:
    type: object
    properties:
      approved:
        type: boolean
        description: True if the user approved the plan and exited plan mode.
      mode:
        type: string
        description: Resulting session permission mode after plan review.
      plan_path:
        type: string
        description: Normalized path of the reviewed plan document.
      feedback:
        type: string
        description: Optional revision feedback if the user chose to keep planning.
    required:
      - approved
      - mode
      - plan_path
---

Submit a finished plan document for user review and exit `plan` mode once approved.

<guidelines>
- `path` is required and must point to an existing plan file inside the workspace or the session's `plans` directory.
- Execution pauses while the user chooses to approve (switching to `accept_edits`, `default`, or `auto`) or keep planning with feedback (stay in `plan` mode, incorporate feedback into the plan file, and call `exit_plan_mode` again).
</guidelines>
