---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: ask_question
  displayName: Ask User Question
  description: Ask the user structured questions for clarification, confirmation, or design choices.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Ask user: %s"
  inputSchema:
    type: object
    properties:
      questions:
        type: array
        description: List of 1 to 5 structured questions to present to the user.
        items:
          type: object
          title: Question
          properties:
            type:
              type: string
              description: Question kind ('single', 'multi', 'text', or 'confirm').
              enum:
                - single
                - multi
                - text
                - confirm
            question:
              type: string
              description: Short, direct one-line question.
            description:
              type: string
              description: Required context below the question explaining details, tradeoffs, or examples (max 300 chars).
            label:
              type: string
              description: Short tab header (max 3 words) used when asking multiple questions.
            options:
              type: array
              description: Choices for 'single' and 'multi' questions (2 to 5 options).
              items:
                type: object
                title: QuestionOption
                properties:
                  id:
                    type: string
                    description: Concise identifier returned when this option is selected.
                  label:
                    type: string
                    description: Display label for the option. Prefix with '(Recommended)' if recommended.
                  description:
                    type: string
                    description: Optional short explanation of the option (max 100 chars).
                required:
                  - id
                  - label
          required:
            - type
            - question
            - description
      title:
        type: string
        description: Optional header / confirmation title when asking multiple questions (e.g. 'Ready to configure?').
      description:
        type: string
        description: Optional summary of what will happen based on the answers when asking multiple questions.
    required:
      - questions
  outputSchema:
    type: object
    properties:
      answers:
        type: array
        description: User answers corresponding to each question.
        items:
          type: object
          title: QuestionAnswer
          properties:
            question:
              type: string
              description: The question that was answered.
            selected:
              type: array
              items:
                type: string
              description: Selected option IDs for 'single' or 'multi' questions.
            text:
              type: string
              description: Free-form text entered for 'text' questions or custom write-in answers.
            confirmed:
              type: boolean
              x-go-pointer: true
              description: Boolean response for 'confirm' questions.
          required:
            - question
      canceled:
        type: boolean
        description: True if the user dismissed or canceled the prompt without submitting.
    required:
      - answers
---

Ask the user one or more structured questions and wait for their response.

<validation_and_types>
Pre-flight validation rejects calls that violate any rule below:
- **1 to 5 questions** per call; every question MUST include `type`, a direct one-line `question`, and `description` (`<= 300` chars explaining context, tradeoffs, or impact).
- **2 to 5 `options`** with unique `id`s (and optional `description` `<= 100` chars) are required for `'single'` and `'multi'`, and MUST be omitted for `'text'` and `'confirm'`.
- **Question types**:
  - `'confirm'`: Yes/No proposition the user affirms or rejects (e.g., *"Drop legacy v1 migration tables?"*). **Never** use `'confirm'` for A-vs-B choices where both are valid — use `'single'` with 2 options instead.
  - `'single'`: Pick one named option (e.g., *"SQLite or PostgreSQL?"*). Put your recommended option first and append `(Recommended)` to its `label`.
  - `'multi'`: Pick one or more options.
  - `'text'`: Open-ended free-text input for identifiers or narrative answers that cannot be enumerated.
- **Built-in write-in**: `'single'` and `'multi'` automatically include a custom write-in input in the UI — **never** add manual `"Other"` or `"Custom"` options.
- **Multiple questions (`len > 1`)**: Render as a tabbed wizard with a confirmation summary screen — include a `1–3` word `label` on each question plus top-level `title` and `description`.
</validation_and_types>

<when_to_use>
- Ambiguous requirements with multiple valid product or architectural interpretations, or concrete implementation tradeoffs.
- Confirming high-impact or irreversible operations not covered by standard tool permissions.
- **Never use** for facts you can verify yourself by reading code, configs, or docs (`view`, `grep`, `glob`), or for routine tool execution approvals.
</when_to_use>

<example>
```json
{
  "title": "Ready to scaffold authentication?",
  "description": "Generates auth middleware and session storage based on your selections.",
  "questions": [
    {
      "label": "Session Store",
      "type": "single",
      "question": "Where should user sessions be stored?",
      "description": "Controls session persistence and horizontal scaling behavior.",
      "options": [
        {"id": "sqlite", "label": "SQLite (Recommended)", "description": "Zero-config local database"},
        {"id": "redis", "label": "Redis", "description": "Shared store for multi-instance deployments"}
      ]
    },
    {
      "label": "Drop Legacy",
      "type": "confirm",
      "question": "Drop legacy v1 session tables?",
      "description": "Existing v2 data is unaffected, but v1 rollback will no longer be possible."
    }
  ]
}
```
</example>
