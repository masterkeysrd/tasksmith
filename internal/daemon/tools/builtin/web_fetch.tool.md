---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_fetch
  displayName: Fetch Web URL
  description: Perform an HTTP or HTTPS request through the sandbox network client.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: true
    userHint: "Fetch URL: %s"
  inputSchema:
    type: object
    properties:
      url:
        type: string
        description: Target HTTP or HTTPS URL to fetch.
      headers:
        type: object
        description: Optional HTTP request headers.
        additionalProperties:
          type: string
      format:
        type: string
        description: 'Output format for fetched content: "markdown" (default, converts HTML to Markdown and pretty-prints JSON/XML), "text" (extracts plain text from HTML and pretty-prints JSON/XML), or "html" (returns the raw unmodified response body).'
        enum:
          - markdown
          - text
          - html
    required:
      - url
  outputSchema:
    type: object
    properties:
      url:
        type: string
        description: Normalized request URL.
      status_code:
        type: integer
        description: HTTP response status code.
      content_type:
        type: string
        description: Response Content-Type header.
      text:
        type: string
        description: Decoded text response body.
    required:
      - url
      - status_code
---

Fetch content from an `http` or `https` `url` (`format` defaults to `"markdown"` for HTML-to-Markdown and pretty-printed JSON/XML; `"text"` extracts plain text; `"html"` returns the raw body).
Responses exceeding 500 lines or 32 KB are truncated (head + tail kept) with the saved spill-file path in the notice — inspect omitted sections with `view` (`offset`/`limit`) or `grep`.
