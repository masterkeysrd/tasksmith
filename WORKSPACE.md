---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: tasksmith
spec:
  projects: [.]
  defaultProvider: codex
  defaultModels:
    decision: ollama/nimble-v3
---
