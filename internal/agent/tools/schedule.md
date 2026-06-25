---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: schedule
  labels:
    category: workflow
spec:
  parameters:
    type: object
    properties:
      prompt:
        type: string
        description: The notification prompt text that will be sent back when triggered.
      duration_seconds:
        type: string
        description: Number of seconds to wait (one-shot timer). Mutually exclusive with cron_expression.
      cron_expression:
        type: string
        description: A standard 5-field cron pattern (e.g. '*/5 * * * *') for recurring notifications.
      timer_condition:
        type: string
        description: Controls timer early termination. 'never' (default), 'any', or a specific subagent/task ID.
      max_iterations:
        type: string
        description: Optional limit on triggers for recurring schedules.
    required: ["prompt"]
  outputSchema:
    type: object
    properties:
      task_id:
        type: string
        description: The scheduled task ID.
      success:
        type: boolean
        description: Whether scheduling succeeded.
      error:
        type: string
        description: Error message if scheduling failed.
---
Schedule a one-shot notification timer or recurring cron job.
