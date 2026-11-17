---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-code-conventions
  displayName: Go Invariants & Defensive Check Hygiene
  description: Prevents redundant nil and zero-value guards when constructors and invariants already guarantee initialization
  labels:
    category: quality
spec:
  priority: 50
  triggers:
    globs:
      - "**/*.go"
    useWhen: Writing, refactoring, or reviewing Go code
---

# Go Invariants & Defensive Check Hygiene

Validate invariants once at construction boundaries (`New`, `Open`, `Validate`, `applyDefaults`) and trust them everywhere downstream.

1. **No dead `nil` or zero-value guards on guaranteed fields**:
   - When a constructor or configuration validator guarantees a field, dependency, or helper return value is non-nil (or positive), use it directly without defensive `if s.dep == nil` or `if c.timeout > 0` branches.
   - Redundant guards obscure the real contract, mislead readers into thinking the dependency is optional, and create unreachable error branches.
2. **Never disguise missing internal state as a caller error**:
   - Do not return caller-facing validation errors (such as `ErrInvalidArgument`) for an uninitialized struct field. If a test constructs a bare struct literal bypassing `New`, initialize the required field in the test fixture rather than polluting production methods with fallback error branches.
3. **Guard only genuinely optional state — and match comments to reality**:
   - Check for `nil` or zero only when absence is a valid, reachable runtime state (e.g., an optional callback hook, `Option` variadic slice entries, or nullable wire fields).
   - Never document a field as `"zero disables X"` or `"nil means Y"` unless callers can actually configure that state through the public API.
