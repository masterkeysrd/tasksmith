# Session Resume, Forking, & Multimodal Input

## Quick session resume (`-c` / `--continue`)

- `tasksmith -c` / `--continue` (supported on the REPL, `tasksmith exec`, and `tasksmith -p`) resumes the workspace's most recently updated top-level session without needing its session ID.
- `--session <id>` takes precedence over `-c`.
- When the workspace has no prior sessions, `-c` quietly starts a fresh session rather than failing — never promise that `-c` errors on an empty workspace.

## Session forking (`/fork`, `--fork`, & `/sessions`)

- **REPL `/fork [title]`**: branches the active session into a new top-level session seeded from the current conversation and switches to it immediately. An empty title defaults to `"<source title> (fork)"`.
- **`/sessions` browser (`F3`)**: forks the highlighted session and switches to the new branch.
- **Startup `--fork`** (REPL, `exec`, and `-p`): branches the session resolved by `--session` or `-c` before attaching so the source session stays untouched (`--title` sets the fork title). Passing `--fork` with no resolvable source session is a usage error (exit `2`).
- **Headless `/fork [title]`**: supported mid-session in `tasksmith exec`; under `--output-format jsonl`, the NDJSON event stream rebinds to the forked session from that point onward.
- **What a fork copies vs. resets**:
  - **Copied**: conversation checkpoint history and session-scoped permission grants.
  - **Not copied**: pending permission/question/plan-review prompts, child subagents, and file-journal rollback history (`/revert` stays with the originating session).
  - **Timeline markers**: the fork records `⑂ Forked from "<source title>"` and the source records `⑂ Forked into "<fork title>"` at the branch point.
  - **Idle requirement**: forking requires the source session to be idle (not running or waiting on an approval/question prompt); suggest cancelling or completing the turn first if a conflict is returned.

## Inspecting subagents (`/subagents`)

- Run `/subagents` to see running child sessions, press `a` to include idle sessions, or press `Enter` to inspect the selected child. The list, inspector header, live activity rows, and `invoke_agent` result show the resolved model label or short ID when available, not the alias that requested it.

## Multimodal attachments & model-switch checks

- **`@<path>` media mentions** (REPL and `tasksmith exec`): attach supported images (`.png`, `.jpg`, `.jpeg`, `.webp`, `.gif`), PDFs (`.pdf`), and audio (`.wav`, `.mp3`, `.m4a`, `.ogg`, `.flac`), snapshotting payloads into content-addressed storage under `<session-dir>/media/<sha256>`. `.svg` files remain on the UTF-8 text path.
- **Clipboard image paste (`Ctrl+V` / `Alt+V`)**: in the REPL composer, pastes clipboard images as inline `[Image #N]` pills uploaded on submit; deleting the `[Image #N]` token detaches the image.
- **Capability-mismatch guard on model switch**:
  - When active post-compaction messages contain media modalities unsupported by the target model (`capability_mismatch`), the REPL opens a confirmation prompt (`Switch anyway` retries with `force: true`, omitting unsupported media on subsequent turns, or `Cancel`).
  - In `tasksmith exec`, switching fails safely unless `/model --force <id>` (or `UpdateSessionRequest.Force: true`) is passed.
  - Running `/compact` summarizes older turns into text and clears the active-modality gate.
