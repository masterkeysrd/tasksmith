---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-code-conventions
  displayName: Go Code Conventions & Readability
  description: Enforces invariant hygiene (no redundant nil/zero guards), readable Go vertical formatting, and cohesive file/test layout
  labels:
    category: quality
spec:
  priority: 50
  triggers:
    globs:
      - "**/*.go"
    useWhen: Writing, refactoring, or reviewing Go code
---

# Go Code Conventions & Readability

Validate invariants once at construction boundaries (`New`, `Open`, `Validate`, `applyDefaults`) and trust them everywhere downstream, and write Go for human readers rather than token compression.

1. **No dead `nil` or zero-value guards on guaranteed fields**:
   - When a constructor or configuration validator guarantees a field, dependency, or helper return value is non-nil (or positive), use it directly without defensive `if s.dep == nil` or `if c.timeout > 0` branches.
   - Redundant guards obscure the real contract, mislead readers into thinking the dependency is optional, and create unreachable error branches.
2. **Never disguise missing internal state as a caller error**:
   - Do not return caller-facing validation errors (such as `ErrInvalidArgument`) for an uninitialized struct field. If a test constructs a bare struct literal bypassing `New`, initialize the required field in the test fixture rather than polluting production methods with fallback error branches.
3. **Guard only genuinely optional state — and match comments to reality**:
   - Check for `nil` or zero only when absence is a valid, reachable runtime state (e.g., an optional callback hook, `Option` variadic slice entries, or nullable wire fields).
   - Never document a field as `"zero disables X"` or `"nil means Y"` unless callers can actually configure that state through the public API.
4. **Readable vertical formatting (beyond `gofmt`)**:
   - Always separate top-level declarations (`func`, methods, `type`, `var`/`const` blocks) with a blank line, and separate distinct logical phases inside functions with blank lines.
   - Break nested or multi-field struct/slice/map literals, long function calls, table-driven test cases, and long boolean conditions across multiple lines (aim for ~100–120 characters max per line; never pack nested structs or 150+ character expressions onto one line).
   - Prefer clear multi-line blocks over dense one-liners, and keep `gofmt` clean.
5. **Cohesive file layout and paired tests**:
   - Keep related types and helpers together in cohesive, domain-owning `.go` files rather than splitting every small helper or feature into its own tiny detached file.
   - Pair test files with the code files they cover (e.g., `foo.go` ↔ `foo_test.go`, or the existing package `<pkg>_test.go`). Add new regression tests to the existing paired `*_test.go` file instead of creating standalone one-off `*_test.go` files.
