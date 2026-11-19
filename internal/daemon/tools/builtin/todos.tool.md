---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: todos
  displayName: Manage Task Checklist
  description: Create, update, or track conversational task checklist items and milestones.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Update task checklist"
  inputSchema:
    type: object
    properties:
      todos:
        type: array
        description: Complete checklist of tasks and milestones.
        items:
          title: TodoItem
          type: object
          properties:
            content:
              type: string
              description: Short description of the task or milestone.
            active_text:
              type: string
              description: Present-tense action phrase describing work underway (e.g., 'Writing unit tests').
            status:
              type: string
              description: Status of the task ('pending', 'in_progress', 'completed').
              enum:
                - pending
                - in_progress
                - completed
          required:
            - content
            - active_text
            - status
    required:
      - todos
  outputSchema:
    type: object
    properties:
      todos:
        type: array
        description: Complete updated checklist of tasks and milestones.
        items:
          title: TodoItem
          type: object
          properties:
            content:
              type: string
              description: Short description of the task or milestone.
            active_text:
              type: string
              description: Present-tense action phrase describing work underway (e.g., 'Writing unit tests').
            status:
              type: string
              description: Status of the task ('pending', 'in_progress', 'completed').
              enum:
                - pending
                - in_progress
                - completed
          required:
            - content
            - active_text
            - status
      total:
        type: integer
        description: Total number of todos in the checklist.
      completed:
        type: integer
        description: Number of completed todos.
      in_progress:
        type: integer
        description: Number of in-progress todos.
      pending:
        type: integer
        description: Number of pending todos.
      message:
        type: string
        description: Summary message describing the updated checklist state.
    required:
      - todos
      - total
      - completed
      - in_progress
      - pending
      - message
---

Maintain the session's ordered task checklist for multi-step operations.
Pass the complete list of `todos` on each call, keep at most one item `in_progress` at a time (with a present-tense `active_text` for the live spinner), and mark items `completed` immediately as they finish.
