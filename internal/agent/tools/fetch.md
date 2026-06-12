---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: fetch
  labels:
    category: network
spec:
  command: ["curl", "-L"]
  description: Fetch a URL.
  parameters:
    type: object
    properties:
      url:
        type: string
        description: URL to fetch.
    required: ["url"]
---
