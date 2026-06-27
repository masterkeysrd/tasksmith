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
  parameters:
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
---
Read the content of a specific MCP (Model Context Protocol) resource.

<guidelines>
- Use this to fetch the data exposed by an MCP resource.
- You must provide the exact `uri` discovered via the `mcp_list_resources` tool.
</guidelines>
