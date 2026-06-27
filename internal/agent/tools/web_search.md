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
      max_results:
        type: integer
        description: Maximum number of search results to return (default 10, max 20).
    required: ["query"]
  outputSchema:
    type: object
    properties:
      results:
        type: array
        items:
          type: object
          properties:
            title:
              type: string
              description: "Title of the search result."
            url:
              type: string
              description: "URL of the search result."
            snippet:
              type: string
              description: "Description or snippet of the search result."
        description: Search results.
---
Search the web and return a list of results with titles, URLs, and snippets. Results are summaries only — use `web_fetch` on a result URL to retrieve the full page content.
