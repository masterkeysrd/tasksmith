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
  argumentHint: "[intent hint]"
---

Create a git commit for the current work, following the commit-hygiene rule embedded above.

{{with index . "RawArguments"}}Intent hint from the requester:
{{.}}{{else}}No intent hint was supplied. Derive the purpose and impact of the change entirely from the staged diff.{{end}}

Work only from repository state, since you have no conversation history:

1. Run `git status`, `git diff --cached --stat`, `git diff --cached`, and `git log -n 5`. Read and
   understand the full scope of what is being committed—if the diff is large, inspect the remaining
   changed areas rather than summarizing from a partial diff.
2. If files are already staged (`git diff --cached` is non-empty), treat the staged index as the
   intended commit scope and leave unstaged or untracked working-tree files untouched unless the
   requester's intent hint explicitly asks to stage additional paths. If nothing is staged, stage
   only the paths that clearly belong to this change; never add untracked scratch files or anything
   unrelated, and say so rather than guessing if the intended scope is unclear.
3. Check whether the intended commit scope is coherent as a single commit. If it clearly spans
   multiple distinct concerns (for example: security or bug fixes, lint/refactor cleanup, and
   CI/rules/docs) and the intent hint does not ask for a single commit (such as `--single`),
   automatically split the intended scope into an ordered sequence of atomic commits at whole-file
   boundaries rather than stopping:
   - Never stage or modify files outside the original intended scope; keep unstaged and untracked
     files untouched.
   - Partition into 2–4 logical groups in dependency order (behavioral/security fixes first, then
     refactors/lint cleanup, then CI/rules/docs last).
   - Never use interactive staging (`git add -p`). If a single file contains two concerns, assign
     it to the commit that owns its primary behavioral change.
   - Reset the index (`git reset`) and, for each group, stage only that group's files, verify
     `git diff --cached --check`, and commit it with its own focused message before moving to the
     next group.
4. Verify documentation is in sync per the rule, and that no secrets or machine-local paths are
   staged.
5. For each commit, write a complete commit message (subject plus explanatory body per the rule),
   commit it with a heredoc, and after the final commit run `git status` to verify the intended
   scope is cleanly committed and nothing outside it was touched. If `git commit` fails on GPG or
   SSH commit signing because the OS sandbox blocks `~/.gnupg` or the signing agent
   (`Operation not permitted` / `No secret key` under `~/.gnupg`), retry that single `git commit`
   command once with `bypass_sandbox: true` rather than changing git config or aborting.

Do not push, amend published commits, or open a pull request. Report each resulting short hash and
commit subject when done.
