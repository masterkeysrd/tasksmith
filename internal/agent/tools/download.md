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
---
Download a file from a URL.
