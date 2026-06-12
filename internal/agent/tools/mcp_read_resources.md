---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mcp_read_resources
  labels:
    category: mcp
spec:
  command: ["mcp-client", "read"]
  description: Read resources from MCP.
  parameters:
    type: object
    properties:
      uri:
        type: string
        description: MCP resource URI.
    required: ["uri"]
---
