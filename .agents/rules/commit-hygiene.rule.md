---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: commit-hygiene
  displayName: Commit Hygiene Standard
  description: Standard process for staging, message authoring, and verification when creating commits
  labels:
    category: quality
spec:
  priority: 40
  triggers:
    keywords:
      - commit
      - staged
      - git
---

# Commit Hygiene Standard

Follow this process for every commit. It complements the Go verification gate, which already
requires build, format, vet, lint, and tests to pass before work is declared complete.

## 1. Establish ground truth

Run `git status`, the staged diff, and `git log -n 5` before writing anything. Never compose a
commit message from memory of what you changed; read the staged diff.

## 2. Stage deliberately

- Stage only the paths that belong to this change.
- Never sweep untracked files in with a blanket add. Scratch notes, editor droppings, and
  unrelated new files stay unstaged unless they are part of the change.
- If a file was already modified before this session and is unrelated to the request, leave it.
- Never stage files containing secrets, credentials, or machine-local absolute paths.

## 3. Keep the change coherent

One commit, one intent. If the staged set spans unrelated concerns (for example a feature plus a
lint cleanup), split it rather than bundling. Do not silently fold opportunistic refactors into a
feature commit.

## 4. Match documentation to the change

Per the repository documentation rule, a commit that alters user-facing behavior, configuration
schemas, or subsystem architecture must carry the matching `docs/` update in the same commit:
CLI surface in `docs/reference/cli.md`, built-in tools in `docs/reference/tools.md`, manifest
schemas in `docs/reference/manifests.md`, providers in `docs/reference/providers.md`, workflows in
`docs/guides/`, and architecture in `docs/architecture/`. Internal fixes, refactors, and test-only
changes need no documentation update; do not churn docs for them.

## 5. Author the message

- Prefix with a conventional type and area scope: `feat(daemon):`, `fix(cli):`, `refactor(gateway):`.
- Subject is imperative, under 72 characters, and names the outcome, not the mechanism.
- Body explains **why** the change exists and what it enables, in prose a new contributor can
  follow without opening a file or knowing an internal name. Do not list changed filenames.
- Wrap body lines at 72 characters. Add a body only when there is reasoning, a tradeoff, or
  context worth recording.
- Choose verbs accurately: `feat` adds capability, `fix` repairs behavior, `update` enhances,
  `refactor` preserves behavior while improving structure.

Before finalizing, apply the litmus test: could someone unfamiliar with this codebase understand
the problem solved and its impact from the message alone?

## 6. Commit and verify

Commit with a heredoc so multi-line messages survive intact. Afterwards run `git status` and
confirm the intended commit exists and nothing intended was left behind.

## 7. Never do

- Never push, or open or update a pull request, unless explicitly asked.
- Never amend, rebase, or force-push commits that are already published.
- Never create empty commits, use interactive flags, or update git configuration.
- Never disable or bypass hooks to make a commit succeed; fix the cause instead.
