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

<when-to-use>
- Ambiguous requirements, styling choices, user preferences, or when choosing between multiple implementation designs.
- Gathering multiple options, allowing the user to select one or multiple choices.
- Resolving complex design options interactively rather than making assumptions.
</when-to-use>

<guidelines>
- Format options as the user's direct response (e.g., "Implement approach A", not "Would you like approach A?").
- Do NOT include generic 'other' or 'none' options; the UI automatically provides a write-in text field for custom entries.
- If you recommend a specific option, list it first and prefix the option text with "(Recommended)".
- Use standard Markdown links `[filename](file:///path/to/file)` when referencing specific files in your question.
- Do NOT enumerate the options (e.g., don't write "1. Option A", the UI handles numbering).
- Avoid using this tool for trivial yes/no questions; output regular text to ask simple yes/no questions.
- If multi-select is enabled, clearly specify it in the question text.
</guidelines>
