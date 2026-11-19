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

Search the web and return a list of results with titles, URLs, and snippets.

<when-to-use>
- Looking up external libraries, APIs, or tools you are unfamiliar with.
- Finding documentation, changelogs, or release notes for a dependency.
- Researching error messages or issues that are not explained by the codebase.
- Verifying facts or current best practices that may have changed over time.
</when-to-use>

<guidelines>
- Results are summaries only — use `web_fetch` on a result URL to retrieve the full page content.
- Prefer codebase tools (`grep`, `glob`, `view`) over web search for anything that can be answered from the project itself.
- Narrow noisy queries with `allowed_domains` (for example official documentation hosts) or drop known junk sources with `blocked_domains`.
</guidelines>
