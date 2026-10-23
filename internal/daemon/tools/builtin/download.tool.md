---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: download
  displayName: Download File
  description: Download a remote HTTP or HTTPS resource to a file in the workspace.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: true
    isDangerous: false
    isOpenWorld: true
    userHint: "Download file: %s"
  inputSchema:
    type: object
    properties:
      url:
        type: string
        description: Source HTTP or HTTPS URL to download.
      path:
        type: string
        description: Destination file path within the workspace.
      headers:
        type: object
        description: Optional HTTP request headers.
        additionalProperties:
          type: string
      force:
        type: boolean
        description: Bypass read-before-overwrite and Compare-And-Swap checks when overwriting.
      timeout_ms:
        type: integer
        description: Optional download timeout in milliseconds.
      max_bytes:
        type: integer
        format: int64
        description: Maximum allowed download size in bytes.
    required:
      - url
      - path
  outputSchema:
    type: object
    properties:
      url:
        type: string
        description: Source URL downloaded.
      path:
        type: string
        description: Normalized destination file path in the workspace.
      status_code:
        type: integer
        description: HTTP status code returned by the server.
      content_type:
        type: string
        description: Response Content-Type header.
      bytes_written:
        type: integer
        description: Number of bytes written to disk.
      created:
        type: boolean
        description: True if a new file was created rather than overwritten.
      before_hash:
        type: string
        description: Previous SHA-256 hex digest when overwriting an existing file.
      after_hash:
        type: string
        description: New SHA-256 hex digest of the downloaded file.
      is_binary:
        type: boolean
        description: True if the downloaded content was detected as binary.
    required:
      - url
      - path
      - status_code
      - bytes_written
      - created
      - after_hash
---

Use the `download` tool to fetch a remote `http` or `https` `url` and save it directly to `path` within the workspace sandbox.
If the destination file already exists, read it first or set `force: true`.
