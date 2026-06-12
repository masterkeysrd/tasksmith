---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: download
  labels:
    category: network
spec:
  command: ["curl", "-O"]
  description: Download a file from a URL.
  parameters:
    type: object
    properties:
      url:
        type: string
        description: URL to download from.
    required: ["url"]
---
