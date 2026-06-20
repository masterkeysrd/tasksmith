---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mcp_read_resources
  labels:
    category: mcp
spec:
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
Read resources from MCP.
