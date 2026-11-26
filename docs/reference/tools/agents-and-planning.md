# Agents and Planning Tools

See [agents and subagents](../../guides/agents-and-subagents.md) and [modes and planning](../../guides/modes-and-planning.md) for delegation and planning workflows. [Tool catalog and permission modes](../tools.md).

## `invoke_agent`

Spawns a specialized subagent asynchronously in an isolated child session.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Delegate to subagent`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `agent` | string | Yes | Name of the subagent to invoke (must be listed in `<available_subagents>`). |
| `title` | string | Yes | Concise 3-6 word summary of the delegated task for UI display. |
| `prompt` | string | Yes | Detailed task instructions for the subagent. |
| `workspace` | string | No | Workspace mode for the subagent: `'inherit'` (default, shares current working directory) or `'worktree'` (creates an isolated git worktree). |
| `model` | string | No | Optional model override for the subagent: a tier alias (`@fast`, `@balanced`, `@smart`, `@flagship`, `@default`), `@inherit` to reuse this session's model, or a `provider/model` identifier. Must be permitted by the subagent's `spec.models` allowlist when it declares one. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `session_id` | string | Yes | Session ID of the spawned subagent. |
| `agent` | string | Yes | Name of the subagent executing the task. |
| `title` | string | Yes | Summary title of the delegated task. |
| `work_dir` | string | Yes | Working directory where the subagent is executing. |
| `model` | string | No | Optional. Resolved `provider/model` identifier of the child session, not the requested alias. |
| `model_label` | string | No | Optional. Display label of the resolved child model. |
| `status` | string | Yes | Execution status of the subagent (`'running'`). |
| `message` | string | Yes | Status message explaining that the subagent is running asynchronously. |

```json
{
  "agent": "explore",
  "title": "Search auth implementation",
  "prompt": "Find all files defining auth middleware and verify token validation.",
  "workspace": "inherit",
  "model": "@fast"
}
```

## `manage_agents`

Lists subagents, checks a subagent's status, sends a message to a subagent, terminates a running subagent, or discovers models subagents can run on.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Manage subagents`. **Default permission:** List/status/models allowed; send/kill prompt.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `action` | string | Yes | The action to perform: `'list'`, `'status'`, `'send'`, `'kill'`, or `'models'`. |
| `session_id` | string | Conditional | The session ID of the subagent (required for `'status'`, `'send'`, and `'kill'`). |
| `message` | string | Conditional | Message to send to the subagent (required for `'send'`). |
| `query` | string | No | Fuzzy search term for `'models'` (matches model ID, display name, description, and `@aliases`). |
| `provider` | string | No | Provider name filter for `'models'`. |
| `agent` | string | No | Subagent name for `'models'` (must be in the session's subagent roster); intersects candidates against that subagent's `spec.models` allowlist. |
| `limit` | integer | No | Maximum models to return for `'models'` (default 20, max 50). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `models` | array of objects | No | Model summaries (returned for 'models'). |
| `models[].id` | string | Yes | Qualified model identifier (`<provider>/<model>`) to pass as invoke_agent's model parameter. |
| `models[].provider` | string | Yes | Provider name owning the model. |
| `models[].name` | string | No | Display name of the model. |
| `models[].description` | string | No | Short human-readable summary of what the model is good at. |
| `models[].aliases` | array of strings | No | Alternate references this model answers to ('@fast', 'provider/@balanced', ...). |
| `models[].context_limit` | integer | No | Maximum context window in tokens. |
| `models[].tools` | boolean | No | Whether the model supports tool calling. |
| `models[].attachment` | boolean | No | Whether the model accepts image or file attachments. |
| `models[].structured_output` | boolean | No | Whether the model supports structured (JSON schema) output. |
| `agents` | array of objects | No | List of child subagents (returned for 'list'). |
| `agents[].session_id` | string | Yes | Unique session identifier of the subagent. |
| `agents[].agent` | string | Yes | Agent persona name. |
| `agents[].title` | string | Yes | Task summary title. |
| `agents[].status` | string | Yes | Current session status ('idle', 'thinking', 'generating', 'executing', 'waiting_permission', 'waiting_question', 'compacting'). |
| `agents[].work_dir` | string | Yes | Working directory of the subagent. |
| `agents[].error` | string | No | Error message if the subagent turn failed. |
| `session_id` | string | No | Session ID of the target subagent. |
| `agent` | string | No | Agent name of the target subagent. |
| `title` | string | No | Title of the target subagent. |
| `status` | string | No | Current status of the target subagent. |
| `work_dir` | string | No | Working directory of the target subagent. |
| `output` | string | No | Latest assistant response text from the subagent (returned for `'status'`). |
| `message` | string | No | Human-readable summary of the action result. |

```json
{
  "action": "models",
  "query": "qwen",
  "agent": "general"
}
```

## `todos`

Maintains the agent's task checklist and milestone tracking throughout a multi-step session.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Update task checklist`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `todos` | array of objects | Yes | Complete checklist of tasks and milestones. |
| `todos[].content` | string | Yes | Short description of the task or milestone. |
| `todos[].active_text` | string | Yes | Present-tense action phrase describing work underway (e.g., 'Writing unit tests'). |
| `todos[].status` | string | Yes | Status of the task ('pending', 'in_progress', 'completed'). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `todos` | array of objects | Yes | Complete updated checklist of tasks and milestones. |
| `todos[].content` | string | Yes | Short description of the task or milestone. |
| `todos[].active_text` | string | Yes | Present-tense action phrase describing work underway (e.g., 'Writing unit tests'). |
| `todos[].status` | string | Yes | Status of the task ('pending', 'in_progress', 'completed'). |
| `total` | integer | Yes | Total number of todos in checklist. |
| `completed` | integer | Yes | Number of completed todos. |
| `in_progress` | integer | Yes | Number of in-progress todos. |
| `pending` | integer | Yes | Number of pending todos. |
| `message` | string | Yes | Summary message describing checklist progress. |

```json
{
  "todos": [
    {
      "content": "Implement auth middleware",
      "active_text": "Implementing auth middleware",
      "status": "completed"
    },
    {
      "content": "Add unit tests for token parsing",
      "active_text": "Writing token parsing unit tests",
      "status": "in_progress"
    }
  ]
}
```

## `ask_question`

Prompts the user with structured interactive questions to resolve ambiguities or confirm architectural choices. Permission rules and workspace policies apply; an `ask` policy ceiling requires approval before the question is shown unless an allow rule matches.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Ask user: %s`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `questions` | array of objects | Yes | List of 1 to 5 structured questions to present to the user. |
| `questions[].type` | string | Yes | Question kind ('single', 'multi', 'text', or 'confirm'). |
| `questions[].question` | string | Yes | Short, direct one-line question. |
| `questions[].description` | string | Yes | Required context below the question explaining details, tradeoffs, or examples (max 300 chars). |
| `questions[].label` | string | No | Short tab header (max 3 words) used when asking multiple questions. |
| `questions[].options` | array of objects | Conditional | Choices for 'single' and 'multi' questions (2 to 5 options). |
| `questions[].options[].id` | string | Yes | Concise identifier returned when this option is selected. |
| `questions[].options[].label` | string | Yes | Display label for the option. Prefix with '(Recommended)' if recommended. |
| `questions[].options[].description` | string | No | Optional short explanation of the option (max 100 chars). |
| `title` | string | No | Header / confirmation title when asking multiple questions. |
| `description` | string | No | Context summary explaining what will happen based on answers. |

Choice questions require 2–5 options with unique IDs; omit options for text and confirmation questions. Choice questions include a write-in field. For multiple questions, provide a title, description, and short tab labels.

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `answers` | array of objects | Yes | User answers corresponding to each question. |
| `answers[].question` | string | Yes | The question that was answered. |
| `answers[].selected` | array of strings | No | Selected option IDs for 'single' or 'multi' questions. |
| `answers[].text` | string | No | Free-form text entered for 'text' questions or custom write-in answers. |
| `answers[].confirmed` | boolean | No | Boolean response for 'confirm' questions. |
| `canceled` | boolean | No | `true` if the user dismissed or canceled the prompt without submitting. |

```json
{
  "title": "Select Database Backend",
  "description": "Choose the persistent storage engine for the service.",
  "questions": [
    {
      "type": "single",
      "question": "Which database engine should be used?",
      "description": "PostgreSQL provides full relational features; SQLite enables single-binary embedded operation.",
      "options": [
        {
          "id": "sqlite",
          "label": "(Recommended) SQLite",
          "description": "Zero external dependencies, single file on disk."
        },
        {
          "id": "postgres",
          "label": "PostgreSQL",
          "description": "Requires external Postgres instance."
        }
      ]
    }
  ]
}
```

## `enter_plan_mode`

Transitions a top-level session into `plan` mode (idempotent if already in `plan` mode) and returns the session's safe `<session-dir>/plans/` directory alongside the planning behavioral contract. Omitted from delegated subagents.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Enter plan mode`. **Default permission:** Safe read; allowed.

### Input parameters

No parameters.

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `mode` | string | Yes | Active session permission mode (`"plan"`). |
| `already_active` | boolean | No | `true` if the session was already in `plan` mode. |
| `plans_dir` | string | Yes | Session-scoped safe directory (`<session-dir>/plans`) where plan writes and edits auto-allow. |
| `instructions` | string | Yes | Behavioral contract and guidance for `plan` mode. |

## `exit_plan_mode`

Submits a finished plan document for user review and suspends the turn until the user approves (switching to `accept_edits`, `default`, or `auto`) or asks to keep planning with optional feedback. Omitted from delegated subagents.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Submit plan: %s`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Path to the finished plan file inside the workspace or `<session-dir>/plans/`. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `approved` | boolean | Yes | `true` if the user approved the plan and exited `plan` mode. |
| `mode` | string | Yes | Resulting session permission mode after review. |
| `plan_path` | string | Yes | Normalized path of the reviewed plan document. |
| `feedback` | string | No | Optional revision feedback when the user chose to keep planning. |
