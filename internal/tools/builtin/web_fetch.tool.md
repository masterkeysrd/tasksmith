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
      method:
        type: string
        description: HTTP method verb (defaults to GET).
      headers:
        type: object
        description: Optional HTTP request headers.
        additionalProperties:
          type: string
      body:
        type: string
        description: Optional HTTP request body payload.
      timeout_ms:
        type: integer
        description: Optional request timeout in milliseconds.
      max_bytes:
        type: integer
        format: int64
        description: Maximum number of response body bytes to read.
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
      status:
        type: string
        description: HTTP response status string.
      headers:
        type: object
        description: Response HTTP headers.
        additionalProperties:
          type: string
      content_type:
        type: string
        description: Response Content-Type header.
      text:
        type: string
        description: Decoded text response body.
      truncated:
        type: boolean
        description: Whether the response body was truncated at max_bytes.
    required:
      - url
      - status_code
      - status
---

Use the `web_fetch` tool to retrieve content from an `http` or `https` `url`.
You may optionally specify `method` (defaults to `GET`), `headers`, `body`, `timeout_ms`, and `max_bytes`.
