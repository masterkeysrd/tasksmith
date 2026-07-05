---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: golang
  description: "Go coding conventions, best practices, formatting rules, testing guidelines, and circular import avoidance strategies."
spec:
  useWhen: "editing or reviewing Go source code (.go files), writing unit tests (*_test.go), resolving circular import errors, or refactoring package dependencies in go.mod"
  keywords: [go, golang, testing, go-test, imports, circular-dependency, interface]
---
# Go (Golang) Development Skill

This skill outlines coding guidelines, conventions, testing standards, and architectural patterns to keep the Go codebase clean, maintainable, and decoupled.

## 1. Code Style, Performance, & Utility Best Practices
- **Names:** Use `CamelCase` for Go symbols (structs, interfaces, methods, functions, variables). Capitalize the first letter for exported symbols, and keep it lowercase for package-private symbols.
- **Acronyms:** Keep acronyms in consistent casing (e.g. `JSON`, `API`, `UUID`, `SQL`, `DB` rather than `Json`, `Api`, `Uuid`, `Sql`, `Db`).
- **Built-in Utilities:** Use Go's built-in `min` and `max` functions (introduced in Go 1.21) for comparisons instead of writing custom helper functions or using `if x < n { x = n }` patterns.
- **String Building:** Avoid using `sb.WriteString(fmt.Sprintf(...))` or `sb.Write([]byte(fmt.Sprintf(...)))` as it causes unnecessary heap allocations. Use `fmt.Fprintf(&sb, ...)` to format and write to the builder in a single pass.
- **SplitSeq over Split:** When iterating over split strings, prefer `strings.SplitSeq` over `strings.Split` + `for range` to avoid allocating an intermediate slice. Use `for item := range strings.SplitSeq(s, sep) { ... }` instead of `lines := strings.Split(s, sep); for _, line := range lines { ... }`.
- **Tagged Switches:** Replace `if/else if` chains that compare the same variable against multiple values with a tagged `switch`. Use `switch v { case a, b: ... case c: ... }` for multiple values in one case.
- **maps.Copy:** Replace manual map copy loops (`for k, v := range src { dst[k] = v }`) with `maps.Copy(dst, src)` for clarity and conciseness.
- **WaitGroup.Go:** When using `sync.WaitGroup.Go()` (Go 1.26+), **do not** add a `defer wg.Done()` inside the callback — `wg.Go()` already handles it automatically. Adding one manually causes a double `Done()` call, resulting in a negative counter panic.
- **Unused Code:** Remove unused constants, functions, and variables. If a parameter is not used in the function body, rename it to `_` to suppress the "unused parameter" hint.
- **Index-Only Loops:** When looping through arrays, slices, or maps and only the index is needed, prefer `for i := range items` instead of `for i := 0; i < len(items); i++`.
- **Documentation Lookup:** Prefer running `go doc <package> [symbol]` to retrieve package/symbol information and specifications quickly.
- **Formatting:** Always ensure Go source code is formatted using standard `go fmt` rules before compiling or testing.

## 2. Interface Design & Circular Import Avoidance
Go strictly forbids circular package imports. To keep packages decoupled:
- **Interfaces at Consumer Point:** Define interfaces in the package that consumes/uses the behavior, not the package that implements it.
- **Dependency Inversion:** If package `A` imports package `B`, then package `B` cannot import package `A`. If `B` needs to notify or call back into `A`, package `B` should declare a callback type, functional option, or interface, and package `A` should inject its implementation at runtime.
- **Avoid Package Bloat:** Keep packages cohesive and focused on a single domain area (e.g. `session`, `tools`, `workspace`).

## 3. Error Handling & Context
- **Error Wrapping:** Always wrap errors to preserve the call stack and add contextual details (e.g. `fmt.Errorf("failed to load resource: %w", err)`). Do not discard or ignore errors.
- **Context Propagation:** Use `context.Context` as the first argument in long-running operations, network calls, and database queries to support cancellation and timeouts.

## 4. Testing Guidelines
- **Structure:** Save unit tests in `*_test.go` files in the same directory as the code they verify.
- **Table-Driven Tests:** Prefer table-driven testing patterns for validating multiple inputs, edge cases, and expected failures:
   ```go
  tests := []struct {
      name    string
      input   string
      wantErr bool
   }{
       {"valid input", "ok", false},
       {"empty input", "", true},
   }
   for _, tt := range tests {
       t.Run(tt.name, func(t *testing.T) {
            // run test assertions
        })
   }
   ```
- **Clean Assertions:** Use `t.Fatalf` for immediate failures (e.g., failed setup) and `t.Errorf` for non-fatal comparison mismatches so the rest of the test suite can run.
