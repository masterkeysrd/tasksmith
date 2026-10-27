# Tools, Toolkits & MCP Servers

TaskSmith provides a robust tool execution system that equips agents with capabilities to inspect files, execute commands, manipulate code, and integrate external services. You can extend this foundation with custom project tools, curated toolkits, and Model Context Protocol (MCP) servers.

---

## Built-in Tools

TaskSmith ships with core tools built directly into the daemon binary and executed within an isolated, path-confined sandbox:

| Tool | Action Type | Safety | Description |
| :--- | :--- | :--- | :--- |
| `view` | `read` | `safe` | Read file contents or specific line ranges with line numbering. Supports images, PDFs, and syntax highlighting. |
| `write` | `write` | `outside` / `unsafe` | Create new files or overwrite existing files in the workspace. |
| `edit` | `write` | `outside` / `unsafe` | Perform precise, surgical find-and-replace edits across files. |
| `bash` | `exec` | Evaluated by AST | Execute shell commands in the workspace environment or spawn long-running background tasks. |
| `grep` | `read` | `safe` | Search for regex patterns across files with line numbers and file path filtering. |
| `glob` | `read` | `safe` | Find files matching wildcard patterns (e.g., `**/*.go`, `src/**/*.tsx`). |
| `web_fetch`| `net` | `outside` | Fetch web pages or REST API endpoints, converting HTML into clean Markdown for context injection. |
| `ask_question` | `read` | `safe` | Prompt the user with interactive single-select, multi-select, or free-form questionnaires. |

For complete parameter schemas and tool options, see the [Built-in Tools Reference](../reference/tools.md).

---

## Creating Custom Tools (`*.tool.md`)

Custom tools allow agents to execute project-specific scripts, test runners, linters, or external utilities. Tools are authored as Markdown files with YAML frontmatter specifying their parameters and sandbox behavior.

### Where Tools Live

- **Project Scope**: `.agents/tools/<name>.tool.md` (or `.agents/tools/<name>.tool.yaml`)
- **Global User Scope**: `~/.config/tasksmith/tools/<name>.tool.md`

### Example: Docker Service Inspector Tool

Create `.agents/tools/docker-service-logs.tool.md`:

```markdown
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: docker-service-logs
  displayName: Docker Service Logs
  description: Fetches recent container logs for a running docker-compose service.
spec:
  command:
    - docker
    - compose
    - logs
    - "--tail"
    - "{{.tail}}"
    - "{{.service}}"
  inputSchema:
    type: object
    properties:
      service:
        type: string
        description: Name of the docker-compose service (e.g., "db", "redis", "api").
      tail:
        type: integer
        description: Number of recent log lines to retrieve.
        default: 50
    required:
      - service
  outputSchema:
    type: string
  annotations:
    isReadOnly: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Fetch logs for docker service: %s"
---

# Docker Service Logs

Use this tool when debugging containerized services or database connection issues in local development.
Always inspect logs before attempting to restart failing containers.
```

### Tool Annotations
- **`isReadOnly`**: Marks the tool as safe for read-only exploration modes (such as `plan` mode).
- **`isDangerous`**: Flags potentially destructive tools, ensuring they trigger confirmation even in lenient permission modes.
- **`userHint`**: Defines the prompt displayed to the user when requesting authorization in the CLI.

---

## Grouping Tools with Toolkits (`*.toolkit.yaml`)

Toolkits allow you to bundle related tools into reusable collections that agents can reference by a single identifier.

### Where Toolkits Live

- **Project Scope**: `.agents/toolkits/<name>.toolkit.yaml`
- **Global User Scope**: `~/.config/tasksmith/toolkits/<name>.toolkit.yaml`

### Defining a Toolkit

Create `.agents/toolkits/database-ops.toolkit.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: database-ops
  displayName: Database Operations
  description: Tools for database migration, inspection, and schema verification.
spec:
  tools:
    # Reference existing workspace tools
    - ref: docker-service-logs

    # Define an inline tool directly in the bundle
    - name: psql-query
      description: Run a read-only SQL query against the local development database.
      command:
        - psql
        - "-U"
        - "postgres"
        - "-d"
        - "dev"
        - "-c"
        - "{{.query}}"
      inputSchema:
        type: object
        properties:
          query:
            type: string
            description: Read-only SQL query to execute.
        required:
          - query
      annotations:
        isReadOnly: true
```

### Referencing Toolkits in Agents

Agents can authorize all tools in a toolkit using the `tools` list in their manifest:

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: db-engineer
spec:
  tools:
    - database-ops
    - view
    - edit
---
```

---

## Connecting Model Context Protocol (MCP) Servers

TaskSmith supports the open **Model Context Protocol (MCP)**, allowing agents to access tools, resources, and prompt templates exposed by local or remote MCP servers.

### Where MCP Configurations Live

- **Project Scope**: `.agents/mcps/<name>.mcp.yaml`
- **Global User Scope**: `~/.config/tasksmith/mcps/<name>.mcp.yaml`

### 1. Stdio MCP Server (Local Process)

Stdio MCP servers run as child processes communicating over standard input and output:

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github
  displayName: GitHub MCP
  description: Access GitHub issues, pull requests, and repository metadata.
spec:
  type: stdio
  command:
    - npx
    - "-y"
    - "@modelcontextprotocol/server-github"
  env:
    GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_TOKEN}"
  annotations:
    isReadOnly: false
    isDangerous: false
  policies:
    tools:
      include:
        - "get_issue"
        - "list_issues"
        - "create_issue"
      exclude:
        - "delete_repo"
```

### 2. HTTP / SSE MCP Server (Remote Endpoint)

Remote MCP servers connect over HTTP using Server-Sent Events (SSE):

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sentry
  displayName: Sentry Error Monitoring
  description: Search and inspect production exceptions and error traces.
spec:
  type: sse
  endpoint: https://mcp.sentry.internal.acme.com/sse
  auth:
    type: header
    header: Authorization
    env: SENTRY_AUTH_TOKEN
  annotations:
    isReadOnly: true
  policies:
    tools:
      include:
        - "*"
```

### Scoping MCP Resources

Use `policies.tools` to control which tools from the MCP server are exposed to agents:
- **`include: ["*"]`**: Expose all tools provided by the server.
- **`include: ["get_*", "list_*"]`**: Use wildcards to expose only safe inspection tools.
- **`exclude: [...]`**: Blacklist dangerous operations (e.g., delete actions).

---

## Scaffolding Tools with `tool-creator`

You can create custom tools and MCP server definitions interactively inside the CLI using the built-in `tool-creator` agent:

1. Switch to the creator agent:
   ```text
   /agent tool-creator
   ```
2. The agent prompts you for:
   - Tool name and description.
   - Command line executable and arguments.
   - Input properties (names, types, descriptions, required fields).
   - Safety annotations (`isReadOnly`, `isDangerous`, `userHint`).
3. The agent formats and writes the manifest to `.agents/tools/<name>.tool.md` (or `.agents/mcps/<name>.mcp.yaml`) and restores your active agent.

---

For complete YAML frontmatter specifications and manifest schemas, see the [Manifests Reference](../reference/manifests.md).
