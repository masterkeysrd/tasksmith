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
Make an HTTP GET request to a URL and return the response content. Prefer `web_fetch` for reading web pages — use this tool when you need raw control over the output format or are fetching non-HTML resources (e.g. JSON APIs, XML feeds, plain text files).

<guidelines>
- `format` controls how the response is returned: `text` (default), `markdown` (HTML converted), or `html` (raw markup).
- If `truncated` is true, the full content has been saved to `cached_path` — read it from there rather than relying on the inline response.
- Check `status` for HTTP errors (4xx/5xx) before using the content.
</guidelines>
