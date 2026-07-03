---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: download
  labels:
    category: network
spec:
  inputSchema:
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
Download a file from a URL to the local filesystem. If the download takes longer than `wait_ms`, it transitions to a background task and returns a `taskId`.

<guidelines>
- `destination` defaults to the filename extracted from the URL, saved in the workspace root.
- For large files, set a low `wait_ms` to transition to background quickly and avoid blocking.
- You will be automatically notified and woken up when the download task finishes. You can continue with other work, or stop calling tools to wait. Do not poll `tasks status` repeatedly.
- Check `size_bytes` in the result to confirm the full file was received.
</guidelines>
