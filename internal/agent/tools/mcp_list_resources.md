---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mcp_list_resources
  labels:
    category: mcp
spec:
  annotations:
    isReadOnly: true
  parameters:
    type: object
    properties:
      server_name:
        type: string
        description: Optional MCP server name to list resources from. If omitted, lists resources from all configured/running servers.
  outputSchema:
    type: object
    properties:
      resources:
        type: array
        items:
          type: object
          properties:
            server:
              type: string
              description: The MCP server name.
            name:
              type: string
              description: The resource name.
            uri:
              type: string
              description: The resource URI.
            description:
              type: string
              description: The resource description.
            mime_type:
              type: string
              description: The resource MIME type.
      success:
        type: boolean
        description: Whether listing the resources succeeded.
      error:
        type: string
        description: Error message if the operation failed.
---
List resources from MCP servers.
