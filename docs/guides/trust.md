# Choose which repositories to trust

Open an unfamiliar checkout for inspection without starting its tools or MCP servers.
Grant trust only when you are ready to use the repository's executable configuration.

## Understand how workspace trust works

Trust answers **“May this directory supply executable configuration?”** It does not
answer **“May this particular command run?”** A trusted tool can still be disabled,
require approval, or encounter a kernel sandbox block.

Choose the narrowest scope that covers your work:

| Choice at startup | What it covers | When to choose it |
| --- | --- | --- |
| **Trust this folder** | The specific directory, not every directory beneath it | A checkout or subproject you have reviewed |
| **Trust all folders in …** | Descendants of the path recorded with `parent` scope; not the recorded path itself | A dedicated directory whose contents you control |
| **Restricted mode** | This visit, without granting trust | Inspect an unfamiliar project or investigate a changed definition |
| **Exit** | No session | Even inspection should wait |

The parent choice grants trust to sibling checkouts under the displayed directory.
Prefer folder trust when you have only reviewed the current checkout.

## Complete first-run onboarding

1. From the intended directory, start the interactive CLI:

   ```sh
   tasksmith
   ```

2. At **Do you trust the files in this folder?**, check the displayed path.
   Read the inventory of custom tools, MCP servers, decisions, and local `.env`
   presence. It is an inventory, not an assessment of whether they are safe.
3. If unsure, choose **Restricted mode** with the arrow keys and `Enter`.
   If you recognize the definitions, choose **Trust this folder**. `Esc` exits.
4. In a trusted, configured workspace, supply any missing provider credentials.
   If the workspace has no configuration, follow setup: reuse a personal provider
   or choose a provider and where to save it. Review the planned writes before
   accepting the permission mode and tool choices.

You can revisit provider setup from the composer:

```text
/setup
```

Setup is separate from trust. Choosing a personal provider avoids adding its
configuration to the repository; keep credentials out of version control.

## Compare trusted and restricted mode

| Task | Trusted | Restricted |
| --- | --- | --- |
| Read source with built-in tools | Subject to rules and path checks | Read-only inspection remains available under the same checks |
| Load workspace or project Tool, MCP, and Decision definitions | Eligible for use | Removed from the executable catalog; system and user definitions are separate |
| Use workspace instructions and policies | Normal workspace discovery | Not a blank workspace: filtering executable definitions does not disable every manifest or instruction |
| Load repository `.env` layers | Worktree hub, root, and project layers can apply | These layers are skipped for both daemon and child environments |
| Use personal provider credentials | Available to the daemon | Personal configuration and the host environment remain distinct from repository `.env` |
| Change repository files | Subject to permission and sandbox policy | Non-read-only actions are denied, not merely prompted |
| Write a session plan | Safe plan-directory mutations have a dedicated exception | Same exception for the session's own plans, not a repository `plans/` directory |
| Run commands without the native wrapper | Only if host execution is enabled and permitted | Host exceptions are disabled, including trusted command exclusions |

If you resume a restricted session, or a reload or daemon restart puts the workspace
in restricted mode, the CLI offers the trust choices inline without restarting setup.
Choose **Restricted mode** to keep inspecting without being prompted again during
that restriction. The header and status line identify the read-only workspace;
use `/setup` to review trust later. Trust is not a substitute for tool permissions.

A restricted workspace starts with the default permission posture. Changing
permission mode is not a way to enable repository writes. With the native
backend the workspace is also read-only to child processes; check the active
backend before assuming you have kernel isolation.

## Check what executable fingerprinting protects

On a trust check, TaskSmith compares the recorded fingerprint with the current
one. It hashes relative paths and contents across the enclosing workspace and
its project roots, including executable WARP manifests and files under these directories:

| Watched directory | Example change worth investigating |
| --- | --- |
| `.agents/tools/` | A formatting tool now invokes a different script |
| `.agents/mcps/` | A server changes its executable, arguments, or remote endpoint |
| `.agents/decisions/` | A permission reviewer changes its approval threshold or rubric |

The digest also includes repository `.env` layers. Adding the first executable file,
removing the last one, or changing an inherited manifest invalidates the baseline.
An unreadable file cannot establish trust. A legacy entry with no fingerprint needs
fresh approval. Trust changes made through the daemon reload affected resident
workspaces; a workspace reload also checks the current fingerprint.

Compare the checkout against the revision you actually reviewed.
For example, after a pull for which `ORIG_HEAD` is that revision:

```sh
git diff ORIG_HEAD HEAD -- .agents/tools .agents/mcps .agents/decisions
git status --short --untracked-files=all -- .agents
```

Read referenced scripts too. Scripts outside the watched directories, ordinary
source files, and `WORKSPACE.md` itself are not part of this digest. A missing
prompt is not evidence that everything in the checkout stayed the same.

## Manage trust with trust.json and the CLI

Your account's store is `$XDG_DATA_HOME/tasksmith/trust.json`, normally
`~/.local/share/tasksmith/trust.json`. It is not committed or shared with teammates.
To inspect the stored scopes from your shell, use `jq` if installed:

```sh
store="${XDG_DATA_HOME:-$HOME/.local/share}/tasksmith/trust.json"
jq '.folders[]? | {path, scope, trustedAt, fingerprint}' "$store"
```

No file yet means no remembered grants. A new, empty store has this shape:

```json
{
  "schema": 1,
  "folders": []
}
```

**Keep subprojects independent.** Trust the exact service directory when starting
there. An exact entry for the repository root is not a grant for a separate
launch inside a service. Conversely, separate launches don't isolate a service
from workspace-level resources once you choose to trust it. Check for inherited
parent grants before relying on folder-by-folder prompts.

**Revoke a grant.** Close sessions using the directory first. From your own
editor, remove the matching object from `folders`, preserving other entries and
valid JSON. Clearing `fingerprint` requires fresh approval but does not remove the entry. If a
parent entry still covers the directory, remove that grant too and trust the
other folders individually. There is no per-child negative trust entry.

Start `tasksmith` again in the directory and verify that it offers the trust
choice. Revoking a stored entry is not a way to terminate already running tools.
For a client-managed workflow, the daemon also exposes `GET` and `DELETE` on
`/v1/workspace/trust` using the `X-TaskSmith-CWD` header; target the directory
whose entry you want to remove. The interactive startup picker grants trust;
there is no separate `tasksmith trust revoke` subcommand.

## Next steps

- [Set permission rules](./permissions.md) for the tools you chose to load.
- [Configure the sandbox](./sandbox.md) to require kernel confinement.
- [Consult the daemon API](../reference/daemon-api.md) for trust status and revocation.
