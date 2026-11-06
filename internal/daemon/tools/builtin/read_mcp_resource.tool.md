---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: read_mcp_resource
  displayName: Read MCP Resource
  description: Reads one resource URI from a configured MCP server and returns its text content.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Read MCP resource: %s"
  inputSchema:
    type: object
    properties:
      server:
        type: string
        description: Name of the configured MCP server to read from.
      uri:
        type: string
        description: Resource URI exactly as the server advertises it.
    required:
      - server
      - uri
  outputSchema:
    type: object
    properties:
      server:
        type: string
        description: Name of the MCP server the resource was read from.
      uri:
        type: string
        description: Resource URI that was read.
      mime_type:
        type: string
        description: MIME type the server declared for the resource, when known.
      text:
        type: string
        description: Text content of the resource, truncated with a path to the full output when it exceeded output limits.
      truncated:
        type: boolean
        description: Whether the resource text exceeded output limits and was spilled to disk.
    required:
      - server
      - uri
      - text
---
Read a single resource exposed by an MCP server.

Use the `server` name shown in MCP listings and the `uri` exactly as that server advertises it. A resource the server manifest prohibits under `spec.policies.resources` is rejected; only a user acting interactively may authorize a single prohibited read, so ask the user rather than retrying.

Resources are static or template-bound content the server chooses to publish, which is distinct from workspace files: use `view` and `grep` for the repository itself.
