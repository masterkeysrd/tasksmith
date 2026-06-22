---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: download
  labels:
    category: network
spec:
  parameters:
    type: object
    properties:
      url:
        type: string
        description: URL to download from.
      destination:
        type: string
        description: Local file path to save the downloaded file (optional, defaults to the filename in the URL in workspace root).
      wait_ms:
        type: integer
        description: Time (in milliseconds) to wait for synchronous completion before shifting to background execution. Defaults to 5000.
    required: ["url"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the downloaded file.
      success:
        type: boolean
        description: Whether the download succeeded.
      taskId:
        type: string
        description: The ID of the background task if execution transitioned to background.
      size_bytes:
        type: integer
        description: Size of the downloaded file.
      message:
        type: string
        description: A human-readable description of the execution status.
---
Download a file from a URL. If it takes longer than wait_ms, it automatically transitions to a background task.
