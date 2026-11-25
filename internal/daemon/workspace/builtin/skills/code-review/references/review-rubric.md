# Multi-Lens Code Review Rubric

Never review a diff hunk in isolation. For every non-trivial change, read the enclosing function, type definitions, constructors, callers, and nearby tests so you understand both the **old contract** and the **new contract**.

---

## 1. Correctness & Edge Cases

Trace real inputs from entry points through the modified lines to observable outputs:

- **Boundary & empty cases**: empty strings/slices/maps, zero values, nil pointers/interfaces, single-element inputs, max/overflow limits, off-by-one slices or loops.
- **Control flow & boolean logic**: inverted conditions, missing `break`/`return` after error handling, fallthrough bugs, shadowed variables hiding outer state, wrong operator precedence.
- **Error handling**: swallowed errors, overwritten `err` before checking, partial state mutations left behind when a later step fails, panics on malformed external input, silent fallback masking an unexpected failure mode.
- **Data fidelity**: truncation, encoding/escaping mismatches, whitespace/path normalization differences, timezone/clock-skew assumptions, cache keys missing a discriminating field.

## 2. State, Concurrency, & Resource Lifecycles

Trace ownership across startup, steady state, retries, cancellation, and shutdown:

- **Data races**: shared mutable maps/slices/fields accessed across goroutines/threads without synchronization, or copies that alias mutable backing slices/maps.
- **Locking & deadlocks**: lock-order inversion between two mutexes, re-entrant locking on the same non-reentrant mutex, holding locks across blocking I/O, callbacks, or channel sends.
- **Goroutine / task / resource leaks**: spawned background workers, timers, tickers, file handles, sockets, or subscriptions not cancelled or awaited on error/shutdown/reset; stale background completions mutating state after a session/request was replaced.
- **TOCTOU & ordering**: check-then-act races across unlocked windows, publishing a handle before initialization completes, subscribing after a fetch without a cursor/watermark, non-idempotent retries or duplicate side effects.

## 3. Contracts, Callers, & Blast Radius

Use `grep` and `view` to inspect consumers outside the diff:

- **Caller assumptions**: Does any existing caller still rely on an old return value, error sentinel, nil-vs-empty distinction, ordering guarantee, or side effect?
- **Wire, CLI, & persistence compatibility**: Do JSON/API payloads, CLI flags, exit codes, config schemas, or SQLite migrations handle existing persisted rows, older records with empty/default columns, and round-trip serialization cleanly?
- **Cross-surface parity**: When a shared protocol, config field, or resource schema changes, are all frontends/consumers (e.g., daemon, TUI, headless CLI, tests, docs) updated consistently?

## 4. Security, Trust Boundaries, & Permissions

Trace untrusted or external input (user input, repository files, PR/commit text, network responses, child-process output) to sensitive sinks:

- **Filesystem confinement**: path traversal (`..`), symlink escapes, absolute-path injection, accidental deletion/overwrite outside intended directories (e.g., user workdirs vs. managed temp/worktree dirs).
- **Command / query / template injection**: unsanitized interpolation into shell commands, SQL queries, URLs, or prompt/template engines; dangerous tool/git config execution (`fsmonitor`, `ext-diff`, hooks) on untrusted repos.
- **Auth & secret hygiene**: credentials/tokens leaked in logs, error messages, child environments, tests, or tracked files; missing auth/permission checks or stale-credential recovery loops.
- **Trust & policy ceilings**: untrusted/restricted workspaces bypassing deny rules, sandbox confinement loosening, or child/subagent sessions inheriting broader privileges than their parent.

## 5. Project Rules & Architectural Invariants

Check the diff against the repository's explicit written standards (`AGENTS.md`, `WORKSPACE.md`, and matching `.agents/rules/` loaded via `load_rule`):

- Cite the **exact rule** being violated and explain the concrete consequence.
- Verify structural boundaries (e.g., daemon-vs-client separation, UI component/theme rules, docs/embedded-skill sync when user-facing behavior or schemas change).
- Do **not** invent unwritten personal preferences and present them as repository rules.

## 6. High-Signal Test & Verification Gaps

Look for **specific, reachable regressions** that existing or newly added tests fail to catch:

- Does a new test actually exercise the production path (or does it mock out the exact logic that changed)?
- Are error paths, cold-restart/restore paths, boundary inputs, or concurrent/race paths uncovered when the change introduces non-trivial logic there?
- Name the exact regression scenario and assertion needed—never leave a generic *"add more unit tests"* remark.

---

## Severity Calibration

Severity reflects **real-world impact**, not uncertainty. Only report high-confidence findings (see `references/false-positives.md`).

| Severity | Meaning | Concrete Examples |
| --- | --- | --- |
| **Critical** | Severe, immediate harm or total breakage | Security/auth/sandbox bypass, data loss or destructive deletion of user files, guaranteed crash/deadlock on a primary path, credential leak |
| **High** | Substantial, reachable bug or broken invariant | Wrong behavior on common valid inputs, real data race or goroutine leak, broken caller/wire contract, regression breaking an existing workflow, clear violation of a critical repo invariant |
| **Medium** | Narrower defect with a concrete, reproducible consequence | Edge-case failure on valid uncommon input, leaked resource on an error path, missing migration/backward-compat handling for older data, stale UI/state sync on a specific transition |
