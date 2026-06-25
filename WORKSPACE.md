---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: tasksmith
spec:
  projects: [.]
  defaultProvider: ollama
  defaultAgent: ""
  plugins: []
  policies:
    tools:
      include:
        - bash
        - download
        - edit
        - multi_edit
        - fetch
        - glob
        - grep
        - ls
        - lsp_diagnostics
        - lsp_restart
        - lsp_search
        - mcp_read_resources
        - remove
        - view
        - web_fetch
        - web_search
        - write
        - tasks
        - todos
        - activate_skill
---
