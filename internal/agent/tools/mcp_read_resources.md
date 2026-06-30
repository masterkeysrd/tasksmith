---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mcp_read_resources
  labels:
    category: mcp
spec:
  annotations:
    isReadOnly: true
  inputSchema:
    type: object
    properties:
      uri:
        type: string
        description: MCP resource URI.
    required: ["uri"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Content of the MCP resource.
      success:
        type: boolean
        description: Whether reading the resource succeeded.
      cached_path:
        type: string
        description: Cached path in workspace session storage.
      truncated:
        type: boolean
        description: Whether the content was truncated due to context limits.
---
Read the content of a specific MCP (Model Context Protocol) resource.

<guidelines>
- Use this to fetch the data exposed by an MCP resource.
- You must provide the exact `uri` discovered via the `mcp_list_resources` tool.
</guidelines>
