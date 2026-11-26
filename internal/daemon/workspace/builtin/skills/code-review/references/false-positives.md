# Adversarial False-Positive Validation

Every candidate finding starts as a **hypothesis**, not a confirmed bug. Before including any finding in your final report, actively try to **disprove** it using the protocol below. A short report with 2 rock-solid defects is far more valuable than 10 speculative comments.

---

## Mandatory Disproof Protocol

Run all five checks on every candidate finding:

1. **Prove reachability from real code**:
   - Inspect actual callers, entry points, constructors, and factories (`grep` + `view`).
   - Confirm that the exact input, nil value, empty state, or concurrent interleaving required to trigger the bug can actually happen in this codebase—not merely in an abstract vacuum.
2. **Check upstream validation and downstream recovery**:
   - Verify whether an earlier parser, schema validator, type system invariant, middleware, permission check, or caller lock already prevents the bad state.
   - Verify whether a downstream handler, `defer`, retry loop, or cleanup path already recovers safely.
3. **Compare against the base revision**:
   - Inspect the pre-change code (`git show <base>:<path>` or diff context).
   - Drop unrelated pre-existing bugs or tech debt **unless** this change introduces them, makes them newly reachable, or materially worsens their impact.
4. **Search for counterevidence**:
   - Check nearby tests, comments, documentation, and `git log -- <path>` for intentional contract changes, platform constraints, or documented invariants.
   - Read what the tests actually assert—do not claim a case is untested without checking existing test files first.
5. **Re-verify exact citations and root-cause grouping**:
   - Re-read the exact lines you plan to cite in the reviewed revision so `path/to/file:line`, symbol names, and your minimal fix match the real code verbatim.
   - Merge multiple symptoms that share one root cause into a single clear finding.

---

## Automatic Drop List (Suppress These)

Discard any candidate that falls into these categories unless an explicit repository rule mandates otherwise:

- **Style & personal taste**: variable naming preferences, comment phrasing, import grouping, formatting, or minor structural preferences where both idioms are valid.
- **Linter/compiler trivia**: issues a compiler or formatter catches immediately, unless the diff genuinely fails to compile or breaks CI right now.
- **Hypothetical defensive guards**: *"what if a future caller passes nil / constructs a bare struct / calls this concurrently"* when current constructors and invariants guarantee initialization (especially in repos that explicitly discourage dead nil/zero-value guards).
- **Unsubstantiated micro-optimizations**: *"this allocates a small slice"* or *"use a map instead of a 4-element slice"* on non-hot paths without measurable impact.
- **Generic test nudges**: *"consider adding more tests"* without pointing to a specific, reachable regression that escapes current assertions.
- **Unchanged code nitpicks**: refactoring suggestions for untouched functions or files outside the diff's blast radius.
- **Speculative external assumptions**: guessing how an external library or API behaves without verifying its actual code, types, or documentation.

---

## Confidence Bar & Handling Uncertainty

- **Report** a finding only when you can point to the concrete trigger, trace the exact code path, and explain the observable failure with high confidence (~80%+ certainty).
- **Do not downgrade a guess into a Medium finding**: Severity measures *impact*, not *confidence*. A low-confidence guess about a Critical topic is still noise—either verify it with code evidence or drop it.
- **Keep judgement calls out of hard defects**: Never report structural smells (`references/review-rubric.md` §7) as **Critical**, **High**, or **Medium** defects; place only material, concrete structural concerns under **Design Notes (Judgement Calls)**, and suppress them completely whenever a repository convention endorses the pattern.
- **Surface real blind spots honestly**: If a suspicious interaction depends on code, schemas, or PR metadata you cannot access (e.g., closed external service, missing base ref, unreadable PR), omit it from Findings and note the exact gap under **Scope / Coverage Limits**.
