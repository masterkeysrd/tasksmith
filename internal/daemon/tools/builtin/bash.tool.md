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
        description: Clear, concise description of what this command does.
      dir:
        type: string
        description: Optional working directory relative to the sandbox root.
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
    required:
      - output
      - exit_code
      - duration_ms
---

Execute shell commands in the workspace sandbox.

<execution_steps>
1. Directory Verification: If creating directories or files via shell, use the `ls` tool first to verify the parent directory exists.
2. Security & Permissions Check: Safe read-only commands inside the workspace execute without prompts; commands that modify state or access paths outside the workspace require user approval.
3. Command Execution: Execute via `sh -c` with proper quoting and capture combined `stdout` and `stderr`.
4. Output Processing: Output exceeding 500 lines or 32 KB is automatically truncated, keeping the first 100 lines and last 100 lines. The truncation notice includes the saved file path holding the complete output — inspect it with `view` (using `offset` and `limit`) or `grep` when the omitted middle matters instead of re-running the command.
</execution_steps>

<usage_notes>
- `command` is required; `description` (concise summary of what the command does) and `dir` (working directory relative to workspace root, defaults to current directory) are optional.
- IMPORTANT: Use `grep` and `glob` tools instead of `find` or `grep`. Use `view` and `ls` tools instead of `cat`, `head`, `tail`, or `ls`. Use `write`, `edit`, and `remove` tools instead of `sed`, `awk`, `echo >`, or `rm`.
- Chain dependent commands with `&&` or `;`; avoid newlines except inside quoted strings.
- Each command runs in an independent subshell (no `cd` or environment variable persistence between calls).
- Prefer setting `dir` or passing direct paths over `cd` (use `cd` only if the user explicitly requests it).
- Do not run interactive commands or flags (e.g., `git rebase -i`, `git add -i`, TUI editors, or pagers); pass non-interactive flags (`--no-pager`, `-y`) when applicable.
</usage_notes>

<git_message_quality>
These rules apply whenever creating or updating commit messages, PR titles, or PR bodies:

- Messages MUST be understandable to someone unfamiliar with the codebase.
- Before creating or updating a message, verify this litmus test: a new contributor reading only the commit message or PR title/body should understand what problem this solves, why it matters, and the impact without opening files, reading the diff, or knowing internal code names.
- Avoid code identifiers, filenames, function names, and implementation details unless they are necessary for understanding the user-facing impact.
- Bad: "Add NameFromHex with sync.Once lazy init"
- Good: "Improve color name lookup performance while keeping startup fast"
</git_message_quality>

<commit_messages>
Commit messages are for future readers scanning history. Before committing:

- Follow `<git_message_quality>`.
- Draft a concise 1-2 sentence message focusing on why the change exists and what outcome it enables, not a list of files or implementation details.
- Use clear, accurate verbs ("add"=new capability, "update"=enhancement, "fix"=bug fix) and avoid generic messages.
- The first line MUST be under 72 characters.
- Add a body only when it is needed to explain the reasoning, tradeoffs, or important context; wrap body lines at 72 characters.
- If the change is internal-only, still describe the benefit or maintenance outcome rather than naming private code.
- Bad: "fix: nil pointer in session.go"
- Good: "fix: prevent session loading from crashing on missing metadata"
- Bad: "refactor: move PromptBuilder into internal/agent"
- Good: "refactor: make prompt assembly easier to maintain"
</commit_messages>

<git_commits>
When the user asks to create a git commit:

1. Single message with three parallel `bash` tool calls (IMPORTANT for speed):
   - `git status` (untracked files)
   - `git diff` (staged/unstaged changes)
   - `git log -n 5` (recent commit message style)

2. Stage relevant untracked/modified files. Do not commit files that were already modified before the session unless relevant to the request.

3. Analyze staged changes:
   - Summarize nature of changes (feature, enhancement, bug fix, refactoring, test, docs), purpose/motivation, and project impact, and check for sensitive information.

4. Draft a commit message following `<commit_messages>` and verify against `<git_message_quality>`.

5. Create commit using HEREDOC:
   ```sh
   git commit -m "$(cat <<'EOF'
   Commit message here.
   EOF
   )"
   ```

6. If a pre-commit hook fails, fix the issue and retry ONCE.

7. Run `git status` to verify.

Notes: Do not stage unrelated files, NEVER update git config, do not push unless asked, never use `-i` flags, no empty commits, and when rebasing always use `-m`.
</git_commits>

<pull_requests>
Use the `gh` command for GitHub pull request tasks when available.

When the user asks to create or update a PR:

1. Single message with parallel `bash` tool calls:
   - `git status`
   - `git diff`
   - Check if branch tracks remote and is up to date
   - `git log` and `git diff main...HEAD` (full commit history from main divergence)

2. Create a new branch, commit changes, and push to remote with `-u` if needed.

3. Draft a concise PR title and summary (1-2 bullet points focusing on "why") reflecting ALL changes since diverging from `main`, following `<git_message_quality>`.

4. Create PR with `gh pr create` using HEREDOC:
   ```sh
   gh pr create --title "title" --body "$(cat <<'EOF'
   Summary here.
   EOF
   )"
   ```
</pull_requests>

<examples>
Good: pytest /foo/bar/tests (or set `dir: "foo/bar"` and `command: "pytest tests"`)
Bad: cd /foo/bar && pytest tests
</examples>
