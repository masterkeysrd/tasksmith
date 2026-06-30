---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: ask_question
  labels:
    category: workflow
spec:
  annotations:
    isReadOnly: true
  inputSchema:
    type: object
    properties:
      question:
        type: string
        description: The question to present to the user.
      options:
        type: array
        items:
          type: string
        description: The list of multiple-choice options.
      is_multi_select:
        type: boolean
        description: If true, the user can select multiple choices.
    required: ["question", "options"]
  outputSchema:
    type: object
    properties:
      selected:
        type: array
        items:
          type: string
        description: The selected option(s).
      write_in:
        type: string
        description: Custom text written by the user.
      success:
        type: boolean
        description: Whether the interaction succeeded.
---
Ask the user one or more multiple-choice questions for clarification or design decisions.
