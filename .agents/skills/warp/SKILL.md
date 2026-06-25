---
name: warp
description: Guidelines for reading, validating, and interacting with Warp YAML manifests (workspace configuration, agent definitions, MCPs, and LLM providers).
---

# Warp Manifests

TaskSmith uses **Warp** (`github.com/masterkeysrd/warp`), a provider-agnostic, declarative format for defining AI agents and their supporting resources. Warp files are written in Markdown with a YAML frontmatter block.

## Key Warp Features
- **Resource Kinds**: Warp defines specific resource types: `Agent`, `Skill`, `Command`, `Workspace`, `MCP`, and `ModelProvider`.
- **Templating**: Resources support highly dynamic, context-aware prompt rendering using Go's `text/template` engine and a shell-like shorthand syntax (e.g. `$Project.Name`).
- **Standardized Loading**: Resources are typically loaded from the `.agents/` directory in a workspace.

## 1. TaskSmith Implementation (`internal/workspace`)

The `internal/workspace` package acts as the bridge between raw YAML files and the typed Go structs needed by the Loom orchestrator (`AgentGraph`):

### Tool Management
Tools are explicitly whitelisted per workspace in the `WORKSPACE.md` file (which is a `kind: Workspace` Warp resource). The `internal/workspace` package parses this to ensure agents cannot execute arbitrary or dangerous tools unless authorized.
```yaml
# WORKSPACE.md snippet
spec:
  policies:
    tools:
      include:
        - bash
        - fetch
        - mcp__docker__* # Wildcards supported
```

### LLM Provider Management
LLM backends are defined using `kind: ModelProvider`.
- **Global Built-ins**: Located in `internal/workspace/preset/`.
- **Workspace Overrides**: Custom providers can be dropped into `.agents/providers/`.
- **Default Selection**: The active provider is set via `spec.defaultProvider` in `WORKSPACE.md`.

### MCP Server Management
Model Context Protocol (MCP) servers are registered using `kind: MCP` manifests. 
- **Tool Namespacing**: MCP tools are automatically prefixed with `mcp__[server_name]__` to prevent collisions.
- **Authorization**: You must explicitly authorize an MCP tool in `WORKSPACE.md`.

## Specialized Resources and Guides

Detailed guides and specifications on Warp are available in the local directory:
- [Warp Specification](./references/SPECIFICATION.md): The exhaustive format specification for all Warp resources (Agents, Skills, Commands, Workspaces, etc).
- [Templating Guide](./references/templating.md): Instructions on how to use `text/template` and `$Variables` in Warp prompts.
- [Workspace Examples](./resources/): Check this directory for concrete examples of `WORKSPACE.md` and provider configurations.

Whenever you are working with Warp manifests, read these references as needed.
