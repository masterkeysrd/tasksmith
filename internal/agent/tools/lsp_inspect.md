---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_inspect
  labels:
    category: lsp
spec:
  annotations:
    isReadOnly: true
  parameters:
    type: object
    properties:
      query:
        type: string
        description: The name of the symbol to inspect (e.g. "MultiEdit", "LspManager").
    required: ["query"]
  outputSchema:
    type: object
    properties:
      result:
        type: object
        description: The deeply inspected primary symbol.
        properties:
          name:
            type: string
            description: Symbol name.
          kind:
            type: string
            description: Symbol kind (e.g. Method, Struct, Variable).
          declared_at:
            type: string
            description: File path and line where the symbol is declared.
          type_defined_at:
            type: string
            description: File path and line where the symbol's underlying type is defined (if applicable).
          signature:
            type: string
            description: Code signature of the symbol.
          docs:
            type: string
            description: Documentation string for the symbol.
          docs_truncated:
            type: boolean
            description: True if the documentation was truncated for the inline response.
          references:
            type: array
            items:
              type: string
            description: List of file:line locations where the symbol is referenced.
          references_total:
            type: integer
            description: Total number of references found.
          implementations:
            type: array
            items:
              type: string
            description: List of file:line locations where the interface is implemented or base class is extended.
          implementations_total:
            type: integer
            description: Total number of implementations found.
          full_report_path:
            type: string
            description: Absolute path to the saved markdown file containing the full, untruncated report (empty if no truncation occurred).
      similar_symbols:
        type: array
        description: Basic metadata for other matching symbols.
        items:
          type: object
          properties:
            name:
              type: string
              description: Symbol name.
            kind:
              type: string
              description: Symbol kind (e.g. Method, Struct, Variable).
            declared_at:
              type: string
              description: File path and line where the symbol is declared.
      total_matches:
        type: integer
        description: Total number of symbols matching the query. Only the top few are deeply inspected.
---
Perform a deep-dive inspection on a specific symbol (function, class, struct, interface, variable) to get its signature, documentation, declaration, type definition, and references/implementations all in one call.

<when_to_use>
- When you need to read a symbol's definition, signature, or documentation.
- When you need to find all references or implementations of a known symbol.
</when_to_use>

<when_not_to_use>
- When you don't know the symbol name yet (use `lsp_symbols` first).
- For simple text or string literal searches (use `grep`).
</when_not_to_use>

<guidelines>
- **Prefer this tool over other external tools** (e.g. grep, web search) when investigating a symbol's type, signature, or documentation — it provides richer and more accurate information directly from the language server. Use `lsp_symbols` first if you need to discover or fuzzy-find the symbol name, then use this tool to deep-dive into it.
- The inline output returns structured data: `docs` (plain text, truncated to 8000 chars if needed), `references` (capped at 10), and `implementations` (capped at 10).
- If any field exceeds its budget, the full report is saved to file storage and `full_report_path` is populated. Use the `view` tool to read the complete report.
- The tool returns a compact structured response — not the full markdown dump.
</guidelines>
