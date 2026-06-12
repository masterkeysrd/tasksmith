---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_search
  labels:
    category: network
spec:
  command: ["curl"]
  description: Search the web.
  parameters:
    type: object
    properties:
      query:
        type: string
        description: Search query.
    required: ["query"]
---
