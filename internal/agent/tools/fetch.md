---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: fetch
  labels:
    category: network
spec:
  parameters:
    type: object
    properties:
      url:
        type: string
        description: URL to fetch.
      format:
        type: string
        enum: ["text", "markdown", "html"]
        description: Output format. Must be one of text, markdown, html. Defaults to text.
      timeout:
        type: integer
        description: Request timeout in seconds (up to 120). Defaults to 30.
    required: ["url"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Content of the response.
      status:
        type: integer
        description: HTTP status code.
      truncated:
        type: boolean
        description: Whether the content was truncated due to context limits.
      cached_path:
        type: string
        description: Cached path in workspace session storage.
---
Fetch a URL.
