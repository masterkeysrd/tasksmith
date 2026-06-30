---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_fetch
  labels:
    category: network
spec:
  inputSchema:
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
Fetch the full content of a URL, returned as converted text. Use this to read a specific page in full after finding it via `web_search`, or to fetch any known URL directly.

<guidelines>
- If `truncated` is true, the content was cut due to size limits — focus on the most relevant section.
- Binary files (e.g. images, PDFs) are stored at `cached_path` rather than returned inline.
- Does not execute JavaScript; pages that require it may return incomplete content.
</guidelines>
