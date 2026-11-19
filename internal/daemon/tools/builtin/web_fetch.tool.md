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

Use the `web_fetch` tool to retrieve content from an `http` or `https` `url`.
You may optionally specify `headers` and `format` (`"markdown"` by default to convert HTML to Markdown and pretty-print JSON/XML, `"text"` for plain text extraction from HTML and pretty-printed JSON/XML, or `"html"` for the raw unmodified response body).

Responses exceeding 500 lines or 32 KB are automatically truncated, keeping the head and tail lines. The truncation notice includes the saved session output file path holding the complete response — inspect it with `view` (using `offset` and `limit`) or `grep` when the omitted content matters.
