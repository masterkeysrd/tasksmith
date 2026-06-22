---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_fetch
  labels:
    category: network
spec:
  parameters:
    type: object
    properties:
      url:
        type: string
        description: Web page URL.
    required: ["url"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Web page content.
      title:
        type: string
        description: Web page title.
      url:
        type: string
        description: The fetched URL.
      truncated:
        type: boolean
        description: Whether the content was truncated due to context limits.
      cached_path:
        type: string
        description: Cached path in workspace session storage.
      mime_type:
        type: string
        description: Detected MIME type.
      is_binary:
        type: boolean
        description: Whether the fetched file is binary.
---
Fetch a web page content.
