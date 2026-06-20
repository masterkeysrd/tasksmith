---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: fetch
  labels:
    category: network
spec:
  parameters:
    type: object
    properties:
      url:
        type: string
        description: URL to fetch.
    required: ["url"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Content of the response.
      status:
        type: integer
        description: HTTP status code.
---
Fetch a URL.
