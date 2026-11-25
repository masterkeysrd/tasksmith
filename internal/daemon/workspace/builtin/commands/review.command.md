---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: review
  displayName: Code Review
  description: Run a clean, read-only code review for verified defects with line citations
spec:
  agent: code-reviewer
  context: clean
  skills: [code-review]
  argumentHint: "[pr, branch, commit, or diff scope]"
---

Perform a thorough, read-only code review following the embedded `code-review` skill and its `references/review-rubric.md` and `references/false-positives.md` playbooks.

{{with index . "RawArguments"}}Review target and focus supplied by the user:
{{.}}
{{else}}No target was supplied. Inspect uncommitted local changes first (staged, unstaged, and untracked files); if the working tree is clean, compare the current branch against its base ref (`origin/HEAD...HEAD` or the default branch).
{{end}}

Do not edit files, switch branches, run mutating commands, or post external PR comments. Independently verify every candidate finding against actual callers, invariants, and repository rules before reporting, and return only high-confidence findings with exact `path/to/file:line` citations.
