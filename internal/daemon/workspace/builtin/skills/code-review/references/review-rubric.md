# Multi-Lens Code Review Rubric

Never review a diff hunk in isolation. For every non-trivial change, read the enclosing function, type definitions, constructors, callers, and nearby tests so you understand both the **old contract** and the **new contract**.

---

## 1. Correctness & Edge Cases

Trace real inputs from entry points through the modified lines to observable outputs:

- **Boundary & empty cases**: empty strings/slices/maps, zero values, nil pointers/interfaces, single-element inputs, max/overflow limits, off-by-one slices or loops.
- **Control flow & boolean logic**: inverted conditions, missing `break`/`return` after error handling, fallthrough bugs, shadowed variables hiding outer state, wrong operator precedence.
- **Error handling**: swallowed errors, overwritten `err` before checking, partial state mutations left behind when a later step fails, panics on malformed external input, silent fallbacks or weak `any`/cast boundaries that paper over an unclear invariant.
- **Data fidelity**: truncation, encoding/escaping mismatches, whitespace/path normalization differences, timezone/clock-skew assumptions, cache keys missing a discriminating field.

## 2. State, Concurrency, Performance, & Resource Lifecycles

Trace ownership across startup, steady state, retries, cancellation, and shutdown:

- **Data races**: shared mutable maps/slices/fields accessed across goroutines/threads without synchronization, or copies that alias mutable backing slices/maps.
- **Locking & deadlocks**: lock-order inversion between two mutexes, re-entrant locking on the same non-reentrant mutex, holding locks across blocking I/O, callbacks, or channel sends.
- **Goroutine / task / resource leaks**: spawned background workers, timers, tickers, file handles, sockets, or subscriptions not cancelled or awaited on error/shutdown/reset; stale background completions mutating state after a session/request was replaced.
- **TOCTOU & ordering**: check-then-act races across unlocked windows, publishing a handle before initialization completes, subscribing after a fetch without a cursor/watermark, non-idempotent retries or duplicate side effects.
- **Real performance hazards** *(skip micro-optimizations)*: N+1 queries or repeated SQL/disk/network calls inside loops, unbounded loops or buffer/memory growth, blocking synchronous I/O on UI/event-loop/locked paths, and unstable UI effect keys or pointer churn triggering redundant re-renders/fetches.

## 3. Contracts, Callers, & Dependencies

Use `grep` and `view` to inspect consumers outside the diff:

- **Caller assumptions**: Does any existing caller still rely on an old return value, error sentinel, nil-vs-empty distinction, ordering guarantee, or side effect?
- **Wire, CLI, & persistence compatibility**: Do JSON/API payloads, CLI flags, exit codes, config schemas, or SQLite migrations handle existing persisted rows, older records with empty/default columns, and round-trip serialization cleanly?
- **Cross-surface parity**: When a shared protocol, config field, or resource schema changes, are all frontends/consumers (e.g., daemon, TUI, headless CLI, tests, docs) updated consistently?
- **Dependency & lockfile hygiene** *(when `go.mod`, `package.json`, or lockfiles change)*: Check whether stdlib or existing dependencies already solve the need, inspect the lockfile diff for unexpected transitive bloat, and verify tests exercise the upgraded dependency's behavior.

## 4. Security, Trust Boundaries, & Permissions

Trace untrusted or external input (user input, repository files, PR/commit text, network responses, child-process output) to sensitive sinks:

- **Filesystem confinement**: path traversal (`..`), symlink escapes, absolute-path injection, accidental deletion/overwrite outside intended directories (e.g., user workdirs vs. managed temp/worktree dirs).
- **Command / query / template injection**: unsanitized interpolation into shell commands, SQL queries, URLs, or prompt/template engines; dangerous tool/git config execution (`fsmonitor`, `ext-diff`, hooks) on untrusted repos.
- **Auth & secret hygiene**: credentials/tokens leaked in logs, error messages, child environments, tests, or tracked files; missing auth/permission checks or stale-credential recovery loops.
- **Trust & policy ceilings**: untrusted/restricted workspaces bypassing deny rules, sandbox confinement loosening, or child/subagent sessions inheriting broader privileges than their parent.

## 5. Project Rules, Domain Skills, & Local Conventions

Check the diff against the repository's written standards (`AGENTS.md`, `WORKSPACE.md`, `CONTRIBUTING.md`, `CODING_STANDARDS.md`, matching `.agents/rules/` via `load_rule`, and matching domain skills from `<available_skills>` via `load_skill`) as well as established conventions in sibling files:

- **Written rules & domain playbooks**: Cite the **exact rule or skill** being violated and explain the concrete consequence. Verify structural boundaries (e.g., daemon-vs-client separation, UI component/theme/layout rules, docs/embedded-skill sync when user-facing behavior or schemas change).
- **Reuse & sibling consistency**: Compare new code against neighboring files in the same package/subsystem. Flag material drift such as hand-rolling a duplicate formatter/truncator/test-helper/lock/error pattern when the codebase already provides a canonical one, or breaking a local invariant that sibling implementations uphold (e.g., viewport-width fallbacks, cleanup ordering, shared fixtures).
- **No invented style rules**: Only flag local convention drift when there is a concrete maintainability, reuse, or behavioral consequence—do **not** dress up personal naming or formatting preferences as repository conventions.

## 6. Spec & Scope Fidelity (When a Spec Exists)

When an originating spec is available (user prompt/arguments, PR body, commit issue reference, or plan/spec file), compare the diff against what was actually requested—quoting the relevant spec line for each finding:

- **Missing or partial requirements**: Behavior explicitly asked for in the spec that is omitted or only half-wired.
- **Misimplemented requirements**: Code that appears to address a requirement on the surface but diverges from the specified semantics or edge cases.
- **Unrequested scope creep**: Extra behavior, production plumbing, or configuration surfaces added beyond what the spec asked for (especially bespoke engine changes added for a task that didn't need them).

## 7. Structural Smells & Dead Code (Judgement Calls Only)

After checking defects and written standards, watch for structural problems that frequently creep into diffs. Three strict rules govern this lens: **(1) documented repository conventions always override**, **(2) every smell is a labeled judgement call, never a hard defect** (skip anything minor, speculative, or enforced by linters), and **(3) always name the concrete restructuring**—preferring a move that *deletes* moving pieces over one that merely relocates complexity:

- **Speculative Generality**: Unused abstractions, config knobs, interfaces, or extension hooks added for hypothetical future needs → inline or delete until a real use case exists.
- **Duplicated Code**: The same non-trivial logic shape repeated across hunks or files instead of sharing one helper or reusing an existing canonical utility → extract or reuse the canonical helper.
- **Feature Logic in Shared Modules / Divergent Change**: Feature-specific branches bolted into a general-purpose or shared module, or unrelated refactors bundled into a focused change → move logic into its owning layer or split unrelated concerns.
- **Complexity Relocation / Middle Man**: Refactors or thin pass-through wrappers that move code around without reducing the concepts or branches a reader must hold → delete the wrapper or collapse redundant layers.
- **Dead / Orphaned Code**: Functions, types, constants, or shims left unreachable after the change → list them explicitly for removal.

## 8. High-Signal Test & Verification Gaps

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
