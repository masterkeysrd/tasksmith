---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tool_search
  displayName: Tool Search
  description: Search available (deferred) tools by name, description, or parameters and load the matches into context.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Search available tools"
  inputSchema:
    type: object
    properties:
      query:
        type: string
        description: Search query terms to find relevant tools by name, description, or parameters.
    required:
      - query
  outputSchema:
    type: object
    properties:
      tools:
        type: array
        description: Names of matching tools loaded into context.
        items:
          type: string
    required:
      - tools
---

Search the workspace's deferred (not eagerly loaded) tools using keyword (BM25) matching and load the best matches into the active toolset. Automatically activated whenever the session has at least one deferred tool.

<how_it_works>
- Provide a `query` of one or more terms describing the capability you need (e.g. "grep", "http download", "list background tasks").
- Matching is performed over each deferred tool's name, description, and parameter names/descriptions.
- The returned `tools` list contains the names of the matching tools, which are then available for direct invocation on subsequent turns.
</how_it_works>

<when_to_use>
- You need a capability that is not among your eagerly loaded tools.
- You are unsure of the exact name of a tool that might exist in the workspace.
</when_to_use>

<when_not_to_use>
- The tool you need is already in your active toolset — invoke it directly.
- You have no deferred tools available (this tool is only registered when deferred tools exist).
</when_not_to_use>
