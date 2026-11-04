---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-verification-gate
  displayName: Go Verification Gate Before Completion
  description: Requires tests, vet, go fix, gofmt, and lint to pass before any task is declared complete
  labels:
    category: quality
spec:
  priority: 60
  triggers:
    always: true
---

# Go Verification Gate Before Completion

No task in this workspace is finished until every check below passes. Run them from the
repository root **after your last edit**, then report the results honestly.

## Required checks (in order)

1. **Apply standard fixes** — `go fix ./...`
   Rewrites code in place. If it changes anything, re-run the checks below afterwards.
2. **Format** — `gofmt -l .`
   Must print **no** file paths. If it lists any, run `gofmt -w` on exactly those files and
   re-check. Never leave a listed file unformatted.
3. **Vet** — `go vet ./...`
   Must exit `0` with no diagnostics.
4. **Lint** — `golangci-lint run --new-from-rev=HEAD~ ./...`
   Must report no issues attributable to your change. This repository carries a large
   pre-existing lint baseline, so gate on **new** findings rather than a globally clean run;
   never widen or edit lint config, or add `nolint` comments, to silence a finding you caused.
5. **Tests** — `go test -count=1 ./...`
   Must pass. Scope to affected packages while iterating (for example
   `go test -count=1 ./internal/daemon/harness/...`), but always finish with the full suite.
   `-count=1` is required so results are never served from cache.

## Rules of conduct

- **Do not declare a task complete, summarize it as done, or offer to commit while any check
  above is failing.** Fix the cause and re-run.
- Fix the **root cause** of a failure. Never delete, skip (`t.Skip`), comment out, or weaken an
  assertion, and never edit a test merely to make it agree with new behavior, unless the test
  itself is genuinely wrong and you say so explicitly.
- A check you did not actually run did not pass. Never report green results you have not
  observed in this session; re-run rather than assume, since edits after a green run invalidate it.
- If a failure is **pre-existing and unrelated** to your change, verify it also fails on the
  base commit, then report it clearly as pre-existing instead of silently proceeding or
  bundling unrelated fixes into the change.
- If a check genuinely cannot be run (missing tool, blocked environment), state exactly which
  one, the command, and the blocker. Do not imply the gate passed.
- When `golangci-lint` is not installed, still run `gofmt -l .`, `go vet ./...`, and
  `go test -count=1 ./...`, and report that lint was skipped for that reason.
