---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
    name: agent-tooling
    description: "The exact process and scaffolding required to create a new builtin AI agent tool for TaskSmith."
spec:
    useWhen: "defining a new agent tool, writing tool manifests or parameter schemas, implementing tool execution handlers, handling tool permissions or user approval flows, managing tool output formatting or caching, or regenerating tool registration code"
    keywords: [tools, agent-tools, tasksmith, codegen]
---

# Creating Agent Tools

All built-in agent tools live in `internal/agent/tools/`. TaskSmith uses a code generation pipeline to automatically register tools based on their Warp manifests.

## The Real Flow for Adding a Tool

### 1. Create the Tool Manifest (`[tool].md`)
Create a markdown file defining the tool using the Warp manifest format. This file is parsed by the generator and sent to the LLM.
```yaml
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mytool
spec:
  annotations:
    isReadOnly: true    # Optional flags: isReadOnly, isDangerous, isIdempotent, isOpenWorld, userHint
  parameters: ... (JSON Schema for inputs)
  outputSchema: ... (JSON Schema for outputs)
---
Description of the tool goes here.
```
**Annotations**: You can add `spec.annotations` to flag behaviors (e.g., `isDangerous` to prompt for user approval, `isReadOnly` for safety).

### 2. Implement the Go Handler (`[tool].go`)
Create the Go implementation. 
- Define the input and output structs matching your schemas.
- Implement the execution logic as a method on `ToolHandlers`: `func (h *ToolHandlers) MyTool(ctx context.Context, in MyToolArgs) (MyToolOutput, error)`.

#### Context Truncation & Providers
To prevent context window bloat, you **must** handle how the output is rendered by implementing one of the following interfaces from `loom/tool`:
- **`tool.TextContentProvider`**: Implement `TextContent() string`. Use this for tools like `glob` to return a clean string. You can append notes like `[Truncated: showing 100 of 1000]`.
- **`tool.ContentProvider`**: Implement `ToolContent() message.Content`. Use this for tools that return complex block types, such as `message.ImageBlock` or `message.DocumentBlock`.

#### Binary Data & Rehydration (File Cache)
If your tool handles large binary files (e.g., downloading an image or rendering a PDF):
**DO NOT** store the raw bytes in the state struct or return them directly. This will bloat the Loom checkpoint database. Instead:
1. Save the file to disk (you can utilize the `FileStorage` interface).
2. Implement the `FileCacheProvider` interface on your output struct, returning a slice of `FileCacheMetadata` with the `CachedPath` and `IsBinary: true`.
3. In your `ContentProvider`, return a `message.ImageBlock` or `message.DocumentBlock` with the `Data` field explicitly set to `nil`.
Before sending the prompt to the LLM, TaskSmith's `RehydrateMessagesForLLM()` will automatically read the file from disk and populate the `Data` field just-in-time.

### 3. Permissions Management (For Dangerous Tools)
If your tool interacts with the filesystem, executes commands, or does anything requiring explicit user consent, you must integrate it with the permissions system (`internal/agent/permissions`):
1. **Annotate**: Set `isDangerous: true` in your `[tool].md` manifest.
2. **Implement Handler**: Create a struct that implements `permissions.PermissionHandler`. This handler defines how to evaluate the request and what approval options (e.g. exact path, directory, domain, wildcard) to offer the user.
3. **Register**: In an `init()` block (usually in `internal/agent/tools/permissions.go`), register your handler:
   ```go
   permissions.RegisterHandler("mytool", &MyToolPermissionHandler{})
   ```

### 4. Generate Registration Code
**Do not manually edit `handlers.go`.** It is a generated file.
Run the code generator to update `handlers.go`:
```bash
go run tools/warp-gen # or go generate ./...
```

### 5. Testing (`[tool]_test.go`)
Write unit tests for your tool's execution to ensure reliability.

## Guidelines
- **Context**: Always use `context.Context` for operations that might block.
- **Payload Size**: Always use providers to return concise outputs. Truncate large lists defensively.
