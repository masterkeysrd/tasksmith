---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  displayName: Execute Shell Command
  description: Execute a shell command inside the workspace sandbox.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: true
    isOpenWorld: true
    userHint: "Run command: %s"
  inputSchema:
    type: object
    properties:
      command:
        type: string
        description: Shell command string passed to sh -c.
      description:
        type: string
        description: Clear, concise description of what this command does (recommend 3-6 words for UI display).
      dir:
        type: string
        description: Optional working directory relative to the sandbox root.
      run_in_background:
        type: boolean
        description: Set to true to run long-running commands, servers, or watchers in the background and return a task_id immediately.
      wait_ms:
        type: integer
        description: Milliseconds to wait before transitioning to a background task (default 60000, or 500 when run_in_background is true).
      bypass_sandbox:
        type: boolean
        description: Run directly on the host outside the OS sandbox (requires user approval; only after a real kernel-sandbox failure).
    required:
      - command
  outputSchema:
    type: object
    properties:
      output:
        type: string
        description: Combined standard output and standard error from the command.
      exit_code:
        type: integer
        description: Process exit status code.
      duration_ms:
        type: integer
        format: int64
        description: Execution duration in milliseconds.
      task_id:
        type: string
        description: Background task ID if execution transitioned to the background.
      status:
        type: string
        description: Execution status ('completed', 'running', 'failed', 'canceled').
    required:
      - output
      - exit_code
      - duration_ms
---

Execute shell commands via `sh -c` inside the workspace sandbox (capturing combined `stdout` and `stderr`).

<usage_and_execution>
- **Prefer dedicated tools**: Use `grep` and `glob` instead of `grep` or `find`; `view` and `ls` instead of `cat`, `head`, `tail`, or `ls`; and `write`, `edit`, and `remove` instead of `sed`, `awk`, `echo >`, or `rm`.
- **Subshell & flags**: Each call runs in an independent subshell (no `cd` or env persistence between calls). Prefer setting `dir` or passing direct paths over `cd` (use `cd` only if requested), chain dependent commands with `&&`, and pass non-interactive flags (`--no-pager`, `-y`) — never run interactive TUI editors or pagers (`git rebase -i`, `git add -i`).
- **Output truncation**: Output exceeding 500 lines or 32 KB is truncated (keeping first 100 and last 100 lines) with the saved log file path in the notice — inspect omitted output with `view` (`offset`/`limit`) or `grep` instead of re-running.
</usage_and_execution>

<background_execution>
- Commands running longer than `wait_ms` (default 60000ms) or launched with `run_in_background: true` transition to the background and return `task_id` and `status: "running"`.
- **Never** append `&` manually — always use `run_in_background: true` so the task manager tracks the process.
- Do **not** poll `tasks` `status` in a loop: the system automatically notifies and wakes you up when a background task finishes. Use `tasks` (`list`, `status`, `send_input`, `kill`) only on demand.
- **Use `run_in_background: true` for**: long-running dev servers (`npm start`, `go run ./cmd/server`), watchers (`npm run watch`, `tail -f`), or indefinite processes.
- **Do NOT set `run_in_background: true` for**: standard builds/tests (`go build`, `go test`, `npm test`, `pytest`), git operations, or short-lived scripts.
</background_execution>

<unsandboxed_execution>
Commands normally run inside the OS sandbox. Some commands genuinely need host access the sandbox withholds, such as GPG commit signing, SSH push, or an integration test requiring a host socket.
- **Always** attempt the command inside the sandbox first.
- If a confirmed kernel sandbox restriction blocks a command needed for the task, submit one `bash` retry with `bypass_sandbox: true` rather than merely suggesting it. Run only what is needed to complete the blocked work, and explain what the sandbox blocked in `description`.
- The retry requires user approval unless covered by a `bash_unsandboxed(...)` allow rule. If denied or unavailable, report the blocker; do not report the command or check as passed.
- Do not request bypass for a tool permission denial or an ordinary failure such as a compile error or a failing assertion. Do not disable signing or hooks, skip tests, or weaken assertions to avoid the restriction.
- `excludedCommands` in `sandbox.json` may pre-exempt trusted command patterns while keeping normal permission rules.
</unsandboxed_execution>

<git_and_pr_guidelines>
- **Commit & PR message quality**: Write for someone unfamiliar with the codebase — a reader seeing only the commit message or PR title/body should understand what problem it solves, why it matters, and its impact without reading the diff or knowing internal identifiers/filenames.
  - First line `< 72` chars using accurate verbs (`add`, `update`, `fix`, `refactor`); wrap an optional context/tradeoff body at 72 chars.
  - Bad: `"Add NameFromHex with sync.Once lazy init"` → Good: `"Improve color name lookup performance while keeping startup fast"`
  - Bad: `"fix: nil pointer in session.go"` → Good: `"fix: prevent session loading from crashing on missing metadata"`
- **Creating commits** (only when explicitly asked):
  1. Run `git status`, `git diff`, and `git log -n 5` in parallel in a single message.
  2. Stage only relevant files (do not stage unrelated pre-existing changes or secrets; never update `git config`, never use `-i` or empty commits, use `-m` on rebase, and do not push unless asked).
  3. Commit via HEREDOC, fix + retry once if a pre-commit hook fails, and verify with `git status`:
     ```sh
     git commit -m "$(cat <<'EOF'
     Commit message here.
     EOF
     )"
     ```
- **Pull requests** (`gh`, when asked):
  1. Run `git status`, `git diff`, remote tracking check, and `git log` + `git diff main...HEAD` in parallel.
  2. Create a branch, commit, and push with `-u` if needed; summarize **all** changes since diverging from `main` (1–2 "why"-focused bullets):
     ```sh
     gh pr create --title "title" --body "$(cat <<'EOF'
     Summary here.
     EOF
     )"
     ```
</git_and_pr_guidelines>
