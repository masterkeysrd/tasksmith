---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: manage_agents
  displayName: Manage Subagents
  description: List subagents, check a subagent's status, send a message to a subagent, kill a running subagent, or discover models subagents can run on.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Manage subagents"
  inputSchema:
    type: object
    properties:
      action:
        type: string
        description: "The action to perform: 'list', 'status', 'send', 'kill', or 'models'."
      session_id:
        type: string
        description: "The session ID of the subagent (required for 'status', 'send', and 'kill')."
      message:
        type: string
        description: "Message to send to the subagent (required for 'send')."
      query:
        type: string
        description: "Optional fuzzy search term for the 'models' action (matches model ID, display name, description, and @aliases)."
      provider:
        type: string
        description: "Optional provider name filter for the 'models' action."
      agent:
        type: string
        description: "Optional subagent name for the 'models' action (must be in the session's subagent roster); intersects candidates against that subagent's spec.models allowlist."
      limit:
        type: integer
        description: "Optional maximum number of models to return for the 'models' action (default 20, max 50)."
    required:
      - action
  outputSchema:
    type: object
    properties:
      models:
        type: array
        description: "Model summaries (returned for 'models')."
        items:
          type: object
          title: ManageAgentsModel
          properties:
            id:
              type: string
              description: Qualified model identifier ("<provider>/<model>") to pass as invoke_agent's model parameter.
            provider:
              type: string
              description: Provider name owning the model.
            name:
              type: string
              description: Display name of the model.
            description:
              type: string
              description: Short human-readable summary of what the model is good at.
            aliases:
              type: array
              description: "Alternate references this model answers to ('@fast', 'provider/@balanced', ...)."
              items:
                type: string
            context_limit:
              type: integer
              description: Maximum context window in tokens.
            tools:
              type: boolean
              description: Whether the model supports tool calling.
            attachment:
              type: boolean
              description: Whether the model accepts image or file attachments.
            structured_output:
              type: boolean
              description: Whether the model supports structured (JSON schema) output.
          required:
            - id
            - provider
      agents:
        type: array
        description: "List of child subagents (returned for 'list')."
        items:
          type: object
          title: ManageAgentsAgent
          properties:
            session_id:
              type: string
              description: Unique session identifier of the subagent.
            agent:
              type: string
              description: Agent persona name.
            title:
              type: string
              description: Task summary title.
            status:
              type: string
              description: "Current session status ('idle', 'thinking', 'generating', 'executing', 'waiting_permission', 'waiting_question', 'compacting')."
            work_dir:
              type: string
              description: Working directory of the subagent.
            error:
              type: string
              description: Error message if the subagent turn failed.
          required:
            - session_id
            - agent
            - title
            - status
            - work_dir
      session_id:
        type: string
        description: Session ID of the target subagent.
      agent:
        type: string
        description: Agent name of the target subagent.
      title:
        type: string
        description: Title of the target subagent.
      status:
        type: string
        description: Current status of the target subagent.
      work_dir:
        type: string
        description: Working directory of the target subagent.
      output:
        type: string
        description: Latest assistant response text from the subagent (returned for 'status').
      message:
        type: string
        description: Human-readable summary of the action result.
---

Manage and monitor child subagent sessions, or discover valid subagent model overrides.

<actions>
- `list`: List all child subagent sessions spawned by this session along with their current `status`, `work_dir`, and any `error`.
- `status`: Retrieve the current execution `status` and latest assistant `output` of a child subagent (`session_id` required).
- `send`: Deliver a follow-up `message` to steer or continue a child subagent (`session_id` and `message` required).
- `kill`: Cancel and terminate a running child subagent (`session_id` required).
- `models`: List or search completion-capable models subagents can run on (optional `query` fuzzy search term, `provider` filter, `agent` to intersect against that subagent's `spec.models` allowlist, and bounded `limit`).
</actions>

<guidelines>
- The system automatically delivers a `<subagent_completed>` notification and wakes you up when a subagent turn finishes. Do NOT poll `status` in a loop waiting for completion; use `status` only to inspect intermediate progress on demand.
- Use `action: "models"` to find valid `provider/model` IDs or `@aliases` before passing a non-tier `model` override to `invoke_agent` instead of guessing identifiers. Prefer omitting `agent` unless checking which models a specific subagent's allowlist permits.
</guidelines>
