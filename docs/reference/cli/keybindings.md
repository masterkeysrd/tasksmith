# Keyboard Shortcuts

Keys act on the view that has focus. For command arguments and view fields, see [Interactive Slash Commands](./slash-commands.md).

## Application Navigation & Control

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Ctrl+C` | Global | First press: Clears composer input. Second press (within 2s): Exits the application. |
| `Esc` | Global | Cancels a running LLM turn, dismisses active autocomplete menus, or closes open modals/pickers. |

## Composer & Prompt Input

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Composer | Submits prompt to the agent, executes a `/command`, or runs a direct `!bash` command. |
| `Shift+Enter` / `Alt+Enter` | Composer | Inserts a literal newline without submitting the prompt. |
| `Shift+Tab` | Composer | Cycles session permission mode: `default` → `accept_edits` → `plan` → `ask` → `auto`. |
| `!` *(on empty input)* | Composer | Switches composer to direct Bash command mode (`!command`). Shell syntax colors appear in the transcript, Bash tool cards, and permission previews, not while typing. |
| `Backspace` *(on empty Bash prompt)* | Composer | Exits direct Bash command mode and returns to standard prompt. |
| `Ctrl+Y` | Composer | Approves the pending tool authorization request for the first waiting subagent once without leaving the composer. |
| `Ctrl+G` | Composer | Opens the live subagent chat view for the waiting subagent in the slot. |
| `Ctrl+V` / `Alt+V` | Composer | Paste an image from the OS clipboard as an `[Image #N]` attachment; it uploads when the prompt is sent. |

## Autocomplete & History

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Tab` | Autocomplete | Inserts or completes the currently highlighted autocomplete suggestion. |
| `↑` / `↓` | Autocomplete | Navigates candidate suggestions up and down when the autocomplete menu is visible. |
| `Ctrl+P` / `Ctrl+N` | Autocomplete | Navigates candidate suggestions up and down, mirroring `↑` / `↓`. |
| `↑` / `↓` / `Ctrl+P` / `Ctrl+N` | Composer | Cycles backward and forward through prompt history. Autocomplete is suppressed while browsing, so recalling a `/command` or `@` mention keeps navigating instead of opening the menu. |
| `Esc` | Composer (history browsing) | Abandons browsing and restores the draft you were typing before you pressed `↑`. |
| Any edit key | Composer (history browsing) | Exits browsing mode and keeps the edited entry as your draft; autocomplete resumes. |

While browsing history, the composer's bottom border is replaced by a centered rule reading `───── history (<n>/<total>) ─────`, and the status line hint reads `prompt history · enter to send · esc to cancel`.

## Modals & Pickers (General)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `k` / `Ctrl+P` | Modals & Pickers | Move selection cursor up. |
| `↓` / `j` / `Ctrl+N` | Modals & Pickers | Move selection cursor down. |
| `Enter` | Modals & Pickers | Confirm and activate highlighted item. |
| `Esc` | Modals & Pickers | Dismiss and close modal without applying changes. |
| *Character keys* | Modals & Pickers | Dynamically filter list items with live search. |

## Permission Prompt

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `e` | Main session permission prompt | **Yes, and switch to accept_edits**: approve the selected write and switch the session to `accept_edits` without creating a rule. Available for writes not classified as unsafe, except explicit sandbox-bypass retries or when already in `accept_edits` or `auto`. See [Interactive Tool Approvals](../../guides/permissions.md#respond-to-interactive-approval-prompts). |

## Plan Review Prompt (`exit_plan_mode`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `k` / `Ctrl+P` | Plan Review | Move option cursor up. |
| `↓` / `j` / `Ctrl+N` | Plan Review | Move option cursor down. |
| `p` | Plan Review | Open scrollable Markdown plan viewer. |
| `1` / `2` / `3` | Plan Review | Approve plan and switch to `accept_edits`, `default`, or `auto` mode. |
| `4` / `r` | Plan Review | Open inline feedback input for **Keep planning**. |
| `Enter` | Plan Review | Confirm highlighted choice (or submit feedback when editing). |
| `Esc` | Plan Review | Cancel review and stay in `plan` mode (or leave feedback input). |
| `↑` / `↓` or `j` / `k` / `Ctrl+P` / `Ctrl+N` | Plan Viewer | Scroll plan Markdown one line up or down. |
| `PgUp` / `PgDn` / `Space` / `b` / `f` / `Ctrl+U` / `Ctrl+D` / `Ctrl+B` / `Ctrl+F` | Plan Viewer | Scroll plan Markdown one page up or down. |
| `g` / `Home` / `G` / `End` | Plan Viewer | Jump to top (`g` / `Home`) or bottom (`G` / `End`) of plan. |
| `p` / `q` / `Esc` / `Enter` | Plan Viewer | Return to the Plan Review options without canceling. |

## Session Browser (`/sessions`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Session Browser | Switches active workspace session to the selected session. |
| `F2` | Session Browser | Prompts inline to rename the selected session title. |
| `F3` | Session Browser | Forks the selected session into a new top-level session and switches to it. |
| `F4` | Session Browser | Prompts confirmation (`y`/`Enter`) to permanently delete the selected session. |

## Providers Browser (`/providers`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` | Providers Browser | Moves the highlighted provider. |
| `Enter` / `m` | Providers Browser | Opens the [model manager](./slash-commands.md#provider-model-manager) for the highlighted provider. |
| `Space` / `e` | Providers Browser | Toggles `spec.enabled`: patches the owning manifest, or writes a Workspace-tier mute shadow (enable flag only) when disabling an inherited User provider. Re-enabling removes a mute shadow when the inherited manifest is already enabled, clears the flag in place when a hand-authored full workspace override shares the name, and promotes a disabled inherited manifest into a full workspace copy otherwise. |
| `l` | Providers Browser | Opens the sign-in screen for an OAuth provider and refreshes its row status upon completion. |
| `a` / `n` | Providers Browser | Opens the [add-provider flow](./slash-commands.md#adding-a-provider). With no providers configured, the empty screen accepts `a` / `n` the same way. |
| `Esc` | Providers Browser | Closes the browser and leaves `Exited /providers command` in the transcript. |
| `F2` | Model Manager / Model Picker | Opens the shared model alias editor (see [Model Alias Editor](./slash-commands.md#model-alias-editor-f2)). |

## Changes Browser (`/changes`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Changes Browser | Opens the full-screen unified diff viewer for the highlighted file change. |
| `r` | Changes Browser | Prompts confirmation to revert mutations on the highlighted file. |
| `Esc` | Diff Viewer / Browser | Closes the diff viewer or returns to composer. |

## Tasks Browser (`/tasks`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Tasks Browser | Opens the live streaming output viewer for the highlighted background task. |
| `x` | Tasks Browser | Sends SIGTERM/kill signal to terminate the highlighted running task. |
| `a` | Tasks Browser | Toggles between displaying all session tasks and active running tasks only. |
| `Esc` | Output Viewer / Browser | Closes the streaming log viewer or dismisses the task manager. |

## Subagents Browser (`/subagents`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Enter` | Subagents Browser | Opens the live streaming chat view and history for the highlighted subagent. Browser rows and the viewer header show the resolved model when available. |
| `x` | Subagents Browser | Sends cancel signal to terminate the highlighted running subagent. |
| `a` | Subagents Browser | Toggles between displaying all subagents and active running subagents only. |
| `Esc` | Subagents Browser | Closes the subagents browser and returns to composer. |

## Subagent Live Viewer

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Subagent Viewer | Scrolls the live chat view up or down by 1 line. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` or `b` / `f` | Subagent Viewer | Scrolls the live chat view up or down by 8 lines. |
| `Home` / `End` or `g` / `G` | Subagent Viewer | Scrolls directly to the top or bottom of the live chat view. |
| `x` | Subagent Viewer | Sends cancel signal to terminate the subagent if currently active. |
| `Esc` | Subagent Viewer | Returns to the subagents browser or dismisses the viewer. |

## Daemon Browser (`/daemon`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `r` | Daemon Browser | Restarts the daemon (or starts if offline) and refreshes health state. |
| `s` | Daemon Browser | Starts the daemon if currently offline. |
| `x` | Daemon Browser | Gracefully stops the running daemon. |
| `g` | Daemon Browser | Re-queries daemon health and refreshes status in-place. |
| `Esc` / `q` | Daemon Browser | Closes the daemon browser and returns to composer. |

## Rules Browser (`/rules`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | Rules Browser | Moves the highlighted rule. |
| `Enter` / `i` | Rules Browser | Opens the rule markdown inspector for the highlighted rule. |
| `r` | Rules Browser | Reloads workspace resources from disk and refreshes the rule list. |
| `Esc` | Rules Browser | Closes the rules browser and returns to composer. |

## Rule Inspector

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Rule Inspector | Scrolls the rule body by 1 line. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` or `b` / `f` | Rule Inspector | Scrolls the rule body by 8 lines. |
| `Home` / `End` or `g` / `G` | Rule Inspector | Scrolls to the top or bottom of the rule body. |
| `Enter` | Rule Inspector | Closes the browser and submits `@rule:<name> ` to attach the rule to the next prompt. |
| `Esc` | Rule Inspector | Returns to the rules list. |

## Tools Browser (`/tools`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Tools Browser | Moves the highlighted tool. |
| `Enter` / `i` | Tools Browser | Opens the tool inspector for the highlighted tool. |
| `Space` | Tools Browser | Enables or disables the highlighted tool (see below). |
| `r` | Tools Browser | Reloads workspace resources from disk and refreshes the tool list. |
| `Esc` | Tools Browser | Closes the tools browser and returns to composer. |

## Tool Inspector

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` or `Ctrl+P` / `Ctrl+N` | Tool Inspector | Scrolls the tool detail by 1 line. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` or `b` / `f` | Tool Inspector | Scrolls the tool detail by 8 lines. |
| `Home` / `End` or `g` / `G` | Tool Inspector | Scrolls to the top or bottom of the tool detail. |
| `Esc` | Tool Inspector | Returns to the tools list. |

## Onboarding & Setup Wizard (`/setup`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` | Setup Wizard | Navigates between selectable options, list items, or menu entries. |
| `Enter` | Setup Wizard | Selects current option, confirms input, verifies credentials, or advances to the next step. |
| `Tab` | Setup Wizard | Toggles between options (such as trust scope `Folder` vs `Parent`, or provider scope `User` vs `Workspace`). |
| `Space` | Setup Wizard | Toggles selection checkbox for individual tools or MCP servers on the tools screen. |
| `Esc` | Setup Wizard | Navigates back to the previous screen or exits the setup wizard. |

## Sandbox Editor (`/sandbox`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Tab` / `Shift+Tab` / `←` / `→` / `1` `2` `3` | Sandbox Editor | Switch tab (forward / reverse / direct). |
| `s` | Policy / Exclusions | Switch the edited scope between `Project` and `Global`. |
| `↑` / `↓` / `k` / `j` / `Ctrl+P` / `Ctrl+N` | Policy / Exclusions | Move the row cursor. |
| `Space` / `Enter` / `t` | Policy | Cycle the scope's default mode (`native → local → inherit`) or toggle the highlighted posture knob (an unset knob toggles from its effective value). |
| `x` / `Backspace` / `Delete` | Policy | Clear the mode or highlighted posture-knob override so the scope inherits again. |
| `a` | Exclusions | Start adding an excluded-command pattern (inline input; `Enter` submits, `Esc` cancels). |
| `e` | Exclusions | Edit the highlighted pattern in place (inline input seeded with the pattern). |
| `d` / `Backspace` / `Delete` | Exclusions | Remove the highlighted pattern. |
| `↑` / `↓` / `k` / `j` / `PgUp` / `PgDn` / `Space` / `g` / `G` | Effective | Scroll the viewport (line / page / top / bottom). |
| `Esc` | Sandbox Editor | Close the editor and return to the composer. |

## MCP Browser (`/mcp`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | MCP Browser | Moves the highlighted server. |
| `Enter` / `i` | MCP Browser | Opens the overview for the highlighted server. |
| `e` / `Space` | MCP Browser | Enables or disables the highlighted server (see [Enabling and Disabling](./slash-commands.md#mcp-browser-mcp) below). |
| `a` | MCP Browser | Opens the [authorization screen](./slash-commands.md#mcp-authentication) for the highlighted server. |
| `r` | MCP Browser | Restarts (reconnects) the highlighted server. |
| `R` / `Ctrl+R` | MCP Browser | Reloads workspace resources from disk and refreshes the server list. |
| `Esc` | MCP Browser | Closes the browser and leaves `Exited /mcp command` in the transcript. |

## MCP Server Overview

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | MCP Overview | Moves the menu cursor. |
| `Enter` | MCP Overview | Runs the highlighted menu entry. |
| `1` – `9` | MCP Overview | Runs the menu entry printed with that number. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` | MCP Overview | Scrolls the card by 8 lines. |
| `g` / `G` | MCP Overview | Scrolls the card to the top or bottom. |
| `p` | MCP Overview | Opens the prompt picker (same entry as `Run prompt`). |
| `u` | MCP Overview | Opens the resource picker (same entry as `Attach resource`). |
| `a` | MCP Overview | Opens the [authorization screen](./slash-commands.md#mcp-authentication) for this server, whenever the menu offers `Authenticate`. |
| `r` | MCP Overview | Restarts (reconnects) this server. |
| `Esc` | MCP Overview | Returns to the server list. |

## MCP Tool Drill-Down

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | Tool List | Moves the selection. |
| any text | Tool List | Filters tools by name. |
| `Enter` | Tool List | Opens the highlighted tool's detail. |
| `Esc` | Tool List | Returns to the overview. |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N` | Tool Detail | Scrolls. |
| `PgUp` / `PgDn` or `Ctrl+U` / `Ctrl+D` or `Ctrl+B` / `Ctrl+F` | Tool Detail | Scrolls by 8 lines. |
| `g` / `G` | Tool Detail | Scrolls to the top or bottom. |
| `e` | Tool Detail | Permits this tool in its manifest, shown only for a prohibited tool whose manifest can be rewritten. |
| `Esc` | Tool Detail | Returns to the tool list. |

## MCP Authentication

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Ctrl+Y` | MCP Authorization | Copies the authorization URL regardless of what the paste field holds. |
| `Enter` | MCP Authorization | Submits whatever the paste field holds to the waiting authorization. |
| `Esc` | MCP Authorization | Stops reporting on the flow and returns to the server list, or closes the browser when `/mcp auth <name>` opened it. |

## Provider Model Manager

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` | Model Manager | Moves the highlighted model. |
| `Space` | Model Manager | Marks the model for removal (`(will remove)`). |
| `d` | Model Manager | Stages the highlighted model as the manifest's default completion model. |
| `s` | Model Manager | Saves staged removals and the pending default to the manifest. |
| `n` | Model Manager | Adds a custom model id through an inline prompt (for OpenAI-compatible and local endpoints), saved immediately. |
| `c` | Model Manager | Opens the [model catalog](./slash-commands.md#provider-model-catalog) for the provider's type. |
| `F2` | Model Manager | Opens the shared [Model Alias Editor](./slash-commands.md#model-alias-editor-f2) on the highlighted model. |
| `Esc` | Model Manager | Returns to the provider browser. |

## Provider Model Catalog

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` | Catalog | Moves the highlighted entry. |
| type | Catalog | Filters the catalog — all printable keys reach the filter box. |
| `Enter` | Catalog | Toggles selection (`☑ … selected`). |
| `Ctrl+S` | Catalog | Adds all selected entries to the manifest. |
| `Esc` | Catalog | Returns to the model manager. |

## Adding a Provider

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` | Add Provider | Moves the highlighted field. |
| `Enter` | Add Provider | Toggles `Scope` between Workspace (`.agents/providers`) and User (`~/.config/tasksmith/agents/providers`), or writes the manifest on `Create provider`. |
| `e` | Add Provider (regular providers only) | Edits the highlighted text field through an inline input seeded with its current value. |
| `Esc` | Add Provider | Returns to the preset list, then to the browser. |

## Model Alias Editor (`F2`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `↑` / `↓` or `k` / `j` | Alias Editor | Moves the checklist cursor. |
| `Space` | Alias Editor | Toggles the highlighted alias. |
| `n` | Alias Editor | Adds a custom alias (inline input; `Enter` confirms, `Esc` cancels the input). |
| `Enter` | Alias Editor | Saves the staged alias set to the owning `*.provider.yaml`. |
| `Esc` | Alias Editor | Cancels and returns to the calling view. |

## Provider Sign-In (`/login`)

| Keybinding | Scope | Action |
| :--- | :--- | :--- |
| `Ctrl+Y` | Antigravity Sign-In | Copies the authorization URL. |
| `Enter` | Antigravity Sign-In | Submits whatever the paste field holds (full address-bar URL, bare query string, or authorization code alone) to the waiting flow. |
| `Esc` | Provider Sign-In | Dismisses the screen while the daemon's flow continues. |

## Status Line Editor (`/statusline`)

| Key | Action |
| :--- | :--- |
| `↑` / `↓` (or `k` / `j`) | Select a segment. |
| `Space` / `Enter` / `t` | Toggle the selected segment (at least one segment must stay enabled). |
| `←` / `→` (or `h` / `l`) | Reorder the selected enabled segment within its row. |
| `r` | Move the selected enabled segment to the other row. |
| `c` | Toggle compact mode. |
| `s` | Save the composition to `~/.config/tasksmith/statusline.json` and exit. |
| `Esc` | Discard the draft, restore the previous composition, and exit. |

## Warnings View (`/warnings`)

| Key | Action |
| :--- | :--- |
| `↑` / `↓`, `k` / `j`, or `Ctrl+P` / `Ctrl+N` | Select a warning, or scroll its details. |
| `Enter` | Open the selected warning's details. |
| `Esc` | Return to the list from details, or close the list. |

## Usage Browser (`/usage`)

| Key | Action |
| :--- | :--- |
| `←` / `→` / `Tab` / `Shift+Tab` / `1` `2` `3` `4` | Switch tab. |
| `↑` / `↓` / `Ctrl+P` / `Ctrl+N` / `PgUp` / `PgDn` / `Ctrl+B` / `Ctrl+F` | Scroll long views. |
| `g` / `m` `p` `a` `u` `i` `d` | Cycle or directly select the Workspace and Global grouping by model, provider, agent, purpose, pricing tier, or day (the busiest day is marked with `*`). On the Quotas tab, `m` expands or collapses the full model list for each shared pool. |
| `t` | Cycle the range: last 24 hours, 7 days, 30 days, all time (default 30 days). |
| `r` | Refresh. |
| `Esc` / `q` | Close. |

## Stats Browser (`/stats`)

| Key | Action |
| :--- | :--- |
| `←` / `→` / `Tab` / `Shift+Tab` / `1` `2` `3` | Switch tab. |
| `↑` / `↓` / `Ctrl+P` / `Ctrl+N` / `PgUp` / `PgDn` / `Ctrl+B` / `Ctrl+F` | Scroll long views. |
| `t` | Cycle the range: today, last 7 days, last 30 days, all time (default today). |
| `r` | Refresh. |
| `Esc` / `q` | Close. |

## See Also

- [CLI reference index](../cli.md)
- [Cli guide](../../guides/cli.md)
- [Permissions guide](../../guides/permissions.md)
- [Sandbox guide](../../guides/sandbox.md)
- [Appearance guide](../../guides/appearance.md)
