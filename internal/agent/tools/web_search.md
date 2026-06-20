---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_search
  labels:
    category: network
spec:
  parameters:
    type: object
    properties:
      query:
        type: string
        description: Search query.
    required: ["query"]
  outputSchema:
    type: object
    properties:
      results:
        type: array
        items:
          type: object
        description: Search results.
---
Search the web.
