---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: commit
  displayName: Commit Changes
  description: Commit the intended changes using the workspace commit standard
spec:
  context: clean
  rules:
    - commit-hygiene
  models: ["@fast"]
  tools:
    - bash
    - view
  argumentHint: "Usage: /commit [intent hint]"
  arguments:
    - name: hint
      description: Optional intent hint that steers the commit message framing
      required: false
---

Create a git commit for the current work, following the commit-hygiene rule embedded above.

{{if .hint}}Intent hint from the requester: {{.hint}}{{else}}No intent hint was supplied. Derive
the purpose and impact of the change entirely from the staged diff.{{end}}

Work only from repository state, since you have no conversation history:

1. Run `git status`, the staged diff, and `git log -n 5` to understand the change and match the
   existing message style.
2. If nothing is staged, stage only the paths that clearly belong to this change. Never add
   untracked scratch files or anything unrelated, and say so rather than guessing if the intended
   scope is unclear.
3. Confirm the change is coherent as a single commit. If it clearly spans unrelated concerns, stop
   and report that it should be split instead of committing everything.
4. Verify documentation is in sync per the rule, and that no secrets or machine-local paths are
   staged.
5. Write the message per the rule, commit it with a heredoc, then run `git status` to verify.

Do not push, amend published commits, or open a pull request. Report the resulting short hash and
subject when done.
