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

Search the workspace's deferred tools via BM25 keyword matching (`query` searches tool names, descriptions, and parameters) and activate matching schemas into your toolset for subsequent turns.

- Use when you need a capability not present in your active toolset or are unsure of an exact deferred/MCP tool name; invoke active tools directly without calling `tool_search`.
