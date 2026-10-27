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

Ask the user one or more structured questions and wait for their response. Use this when you need clarification, confirmation, or a design choice before proceeding.

<how_it_works>
- Always provide a `questions` array with 1 to 5 items.
- A single question renders directly; multiple questions render as a tabbed multi-step form with a confirmation summary screen at the end.
- Every question MUST include:
  1. `type` — `'single'`, `'multi'`, `'text'`, or `'confirm'`
  2. `question` — a short, direct one-line question
  3. `description` — context shown below the question with details, tradeoffs, or examples (**always required**; omitting it returns a validation error)
</how_it_works>

<hard_limits>
Violations fail pre-flight validation and return an error:
- **1 to 5 questions** per call. If you have more, prioritize or split across turns.
- **2 to 5 options** required for `'single'` and `'multi'` questions; `options` must be omitted for `'text'` and `'confirm'`.
- **Question `description` is required** on every question and must be under 300 characters.
- **Option `description`** (when provided) must be under 100 characters.
- **Option `id`s** must be unique within each question.
</hard_limits>

<question_types>
- `'confirm'` — Yes/No proposition the user affirms or rejects (e.g., "Proceed with schema migration?", "Enable query caching?"). Never use `'confirm'` for A-vs-B choices or preference questions where both alternatives are valid approaches — use `'single'` with 2 options instead.
- `'single'` — Pick one option from `options`. Use for any selection among named alternatives, including binary choices (e.g., "SQLite or PostgreSQL?"). If you recommend an option, put it first and append `(Recommended)` to its `label`.
- `'multi'` — Pick one or more options from `options`.
- `'text'` — Open-ended free-text input for narrative answers, identifiers, or custom values that cannot be enumerated as options.

Note: `'single'` and `'multi'` questions automatically include a custom write-in input in the UI so the user can provide their own answer. Never add an "Other", "Custom", or "Something else" option manually.
</question_types>

<multiple_questions>
- When asking multiple questions (`len(questions) > 1`), include a short `label` (1–3 words, e.g., `"Database"`, `"Caching"`) on each question for the step/tab header. If omitted, the first 3 words of `question` are used.
- A confirmation screen is automatically shown after all questions are answered so the user can review their choices before submitting.
- Provide top-level `title` (e.g., `"Ready to configure?"`) and `description` summarizing what will happen once confirmed.
</multiple_questions>

<when_to_use>
- Ambiguous requirements with multiple valid architectural or product interpretations
- Choosing between concrete implementation approaches or tradeoffs
- Confirming high-impact or irreversible operations not covered by tool permissions
- Gathering a small batch of related configuration decisions at once
</when_to_use>

<when_not_to_use>
- Questions you can answer yourself by reading code, config files, or documentation (`view`, `grep`, `glob`)
- Standard tool execution authorization (handled automatically by the permission system)
</when_not_to_use>

<examples>
Single question:
```json
{
  "questions": [
    {
      "type": "confirm",
      "question": "Drop legacy v1 migration tables?",
      "description": "Removes 4 deprecated tables. Existing v2 data is unaffected, but v1 rollback will no longer be possible."
    }
  ]
}
```

Multiple questions with confirmation:
```json
{
  "title": "Ready to scaffold authentication?",
  "description": "We will generate the auth middleware, session store, and route guards based on your selections.",
  "questions": [
    {
      "label": "Session Store",
      "type": "single",
      "question": "Where should user sessions be stored?",
      "description": "Controls session persistence and horizontal scaling behavior.",
      "options": [
        {"id": "sqlite", "label": "SQLite (Recommended)", "description": "Zero-config local database"},
        {"id": "redis", "label": "Redis", "description": "Shared in-memory store for multi-instance deployments"},
        {"id": "jwt", "label": "Stateless JWT", "description": "Signed cookies without server-side storage"}
      ]
    },
    {
      "label": "OAuth Providers",
      "type": "multi",
      "question": "Which OAuth providers should we wire up?",
      "description": "Select all providers to generate callback handlers for.",
      "options": [
        {"id": "github", "label": "GitHub"},
        {"id": "google", "label": "Google"}
      ]
    }
  ]
}
```
</examples>
