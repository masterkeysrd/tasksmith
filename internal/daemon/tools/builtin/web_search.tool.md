---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: web_search
  displayName: Search The Web
  description: Run a web search query and return structured result titles, URLs, and snippets.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: false
    isDangerous: false
    isOpenWorld: true
    userHint: "Search the web: %s"
  inputSchema:
    type: object
    properties:
      query:
        type: string
        description: Search query.
      allowed_domains:
        type: array
        description: Optional host filter; only results whose host matches one of these domains are returned.
        items:
          type: string
      blocked_domains:
        type: array
        description: Optional host filter; results whose host matches one of these domains are dropped. Takes precedence over allowed_domains.
        items:
          type: string
      limit:
        type: integer
        description: Maximum number of results to return (default 10, max 20).
    required:
      - query
  outputSchema:
    type: object
    properties:
      query:
        type: string
        description: Normalized search query that was executed.
      results:
        type: array
        description: Search results in ranking order.
        items:
          type: object
          title: WebSearchResult
          properties:
            title:
              type: string
              description: Title of the search result.
            url:
              type: string
              description: Destination URL of the search result.
            snippet:
              type: string
              description: Short description or snippet of the search result.
          required:
            - title
            - url
      truncated:
        type: boolean
        description: True when the results page carried more results than the limit.
    required:
      - query
      - results
---

Search the web and return result titles, URLs, and snippets.

<guidelines>
- **When to use**: External libraries/APIs, dependency changelogs/docs, unfamiliar error messages, or facts not in the codebase — prefer `grep`, `glob`, and `view` for anything answerable from the repository itself.
- **Follow-up & filtering**: Results are snippets only; call `web_fetch` on promising URLs to read full pages, and use `allowed_domains` or `blocked_domains` to narrow noisy queries.
</guidelines>
