---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_fetch
  labels:
    category: network
spec:
  command: ["curl", "-L"]
  parameters:
    type: object
    properties:
      url:
        type: string
        description: Web page URL.
    required: ["url"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Web page content.
      title:
        type: string
        description: Web page title.
---
Fetch a web page content.
