---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: todos
  labels:
    category: system
spec:
  annotations:
    isReadOnly: true
  parameters:
    type: object
    properties:
      todos:
        type: array
        description: "The complete, authoritative list of tasks. It replaces the current task list in its entirety on every update."
        items:
          type: object
          properties:
            description:
              type: string
              description: "A brief description of the task."
            status:
              type: string
              description: "The status of the task. Must be one of: 'pending', 'in_progress', 'completed'."
              enum: ["pending", "in_progress", "completed"]
            active_text:
              type: string
              description: "Optional text describing the current activity for in_progress tasks, e.g. 'writing code' or 'waiting for response from database'."
          required: ["description", "status"]
    required: ["todos"]
  outputSchema:
    type: object
    properties:
      todos:
        type: array
        items:
          type: object
          properties:
            description:
              type: string
            status:
              type: string
            active_text:
              type: string
---

Manages a structured task list for multi-step work. Call this tool to create, update, or complete tasks. Every call **replaces the entire task list** — always include all existing tasks when making an update.

## How to Use

Call the tool by providing an array of the **complete, updated list of tasks**. This list will overwrite the current task state entirely. Omitting a task from the array will permanently remove it. Only one task should be `in_progress` at a time to keep work focused — update immediately after state changes for accurate tracking.

## When to Use

Use this tool whenever:

**Initial triggers**
- After receiving new instructions to capture requirements
- When the user provides multiple tasks (numbered, comma-separated list)
- When the user requests todos list management

**Creating and updating**
- A new task is identified or created
- An existing task changes status (e.g., starts, completes, gets blocked)
- A task's description or `active_text` needs to be updated

**Restructuring**
- A task is broken down into smaller subtasks
- Non-trivial tasks that require careful planning and multiple operations

**Completion**
- After completing a task (mark it `completed` and add any follow-up tasks)

## When NOT to Use

- Trivial tasks with no organizational benefit
- Simple yes/no answers or single-line responses that don't require tracking

## Agent Self-Tracking

You should use this tool to track your own workflow, not just tasks assigned by the user.
When you begin any multi-step work — including investigation, debugging, code review,
planning, or implementation — call `todos` with a plan before doing any
file reads or code changes.

**Recommended workflow:**
1. Receive the user's request
2. Call `todos` to establish the full plan (all tasks as `pending`)
3. Mark the first task `in_progress` and execute it
4. Update statuses as you complete each step
5. Only mark a task `completed` when you have verified it

This is good practice for non-trivial work. Even investigative steps (reading files, searching
codebases, understanding designs) count as tasks that should be tracked. Use
this tool to stay organized — structured tracking is part of the work, not overhead.

## Task Status Values

| Status       | Meaning                                              |
|--------------|------------------------------------------------------|
| `pending`    | The task has not been started yet.                   |
| `in_progress`| The task is actively being worked on.                |
| `completed`  | The task is finished and all requirements are met.   |

## State Management

The agent is the **single source of truth** for the task list. Every invocation of this tool replaces the entire list in application state. You must always pass every task — both unchanged ones and any newly added or modified ones.

## Completion Requirement

A task may only be marked `completed` when **all** of its subtasks and associated requirements have been thoroughly verified and resolved. Do not mark a task complete prematurely.

Never mark a task as `completed` if:
- Tests or verification steps are failed or incomplete
- Implementation is partial or missing critical components
- Encountered unresolved errors or blockers that prevent full completion
- Cannot find files or dependencies required to complete the task


## Handling Blockers or Failures

If a task cannot proceed or fails, do not use a blocked state. Instead, create new todos with the tasks required to unblock the work. If you cannot complete the task, leave the todos in their current state and output to the user explaining the situation.

## Breaking Down Tasks

Complex or large tasks must be decomposed into smaller, actionable subtasks. When a task is broken down, add the new subtasks to the array alongside the parent task (which may be left `in_progress` or removed, depending on context).

**IMPORTANT**: Never print or list the todos in your response text. The user already has the full list of tasks in the application state.
