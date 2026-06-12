---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_fetch
  labels:
    category: network
spec:
  command: ["curl", "-L"]
  description: Fetch a web page content.
  parameters:
    type: object
    properties:
      url:
        type: string
        description: Web page URL.
    required: ["url"]
---
