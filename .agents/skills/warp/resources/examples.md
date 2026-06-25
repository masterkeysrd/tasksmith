# Warp Manifest Examples

Below are concrete examples of how TaskSmith orchestrates tools, providers, and MCP servers via Warp YAML manifests.

## 1. The Workspace Manifest (`WORKSPACE.md`)

This file sits at the root of the user's project and dictates the rules of engagement for the agent.

```yaml
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: tasksmith
spec:
  projects: [.]
  defaultProvider: ollama    # Uses the provider defined in .agents/providers/ollama.yaml
  defaultAgent: ""
  policies:
    tools:
      include:
        - bash               # Built-in tool
        - view               # Built-in tool
        - mcp__docker__*     # Authorizes ALL tools from the "docker" MCP server
        - mcp__github__read  # Authorizes ONLY the "read" tool from the "github" MCP server
---
# Free-form markdown can go here for human readability
```

## 2. Model Provider Manifest (`.agents/providers/ollama.yaml`)

Defines how Loom connects to a specific LLM backend.

```yaml
---
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: ollama
spec:
  provider: ollama
  defaultModel: qwen2.5-coder:14b
  baseURL: http://localhost:11434/api
```

## 3. MCP Server Manifest (e.g., `.agents/mcp/docker.yaml`)

Registers an external MCP server. TaskSmith will spawn or connect to this server and dynamically extract its tools.

```yaml
---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: docker
spec:
  command: npx
  args:
    - "-y"
    - "@docker/mcp-server"
```
*Note: Because its name is `docker`, its tools will be prefixed with `mcp__docker__` in the tool registry.*
