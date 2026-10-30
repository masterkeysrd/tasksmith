// Package command implements the REPL slash command system for the TaskSmith
// CLI. It defines a command registry, the built-in slash commands, and the
// interactive browser views that render inside the store's slot area, bridging
// the interactive CLI store and the daemon API client.
//
// # Command Registry
//
// A [Registry] maintains the collection of [Command] values, each pairing a
// name (and optional aliases) with a [Handler] that runs when the command is
// invoked. [NewRegistry] returns a registry pre-populated with every built-in
// command. [Registry.Register] adds or replaces a command and indexes its name
// and aliases (normalized to lowercase with any leading "/" stripped), while
// [Registry.Lookup] resolves a command by its primary name or alias.
// [Registry.Execute] parses a raw slash command line, resolves it through
// [Registry.Lookup], and invokes the matching handler with the remaining
// arguments; an unknown command or a command with no handler returns an error.
// [Registry.List] returns all registered commands in registration order.
//
// # Built-in Commands
//
// [NewRegistry] registers the built-in commands, grouped by purpose:
//
//   - System: /help, /mode, /model, /agent, /effort, /budget, /thinking,
//     /theme, and /exit.
//   - Session: /new, /clear, and /sessions.
//   - Changes: /changes and /revert.
//   - Permissions: /permissions.
//   - Tasks: /tasks.
//   - Context: /compact and /context.
//   - Usage: /usage.
//   - Skills: /skills.
//
// Each command either mutates the store directly (for example /mode and /model)
// or enqueues an interactive browser into the store's slot area.
//
// # Interactive Browsers
//
// Several commands render interactive, slot-scoped views built as trail
// functional components: [ChangesBrowser], [PermissionBrowser],
// [SessionBrowser], [SkillsBrowser], [TasksBrowser], [ModelPicker], and
// [AgentPicker]. Each view is enqueued into the store, reads live data from the
// daemon client, and closes itself through the store when the user cancels.
//
// # Workspace Commands and Skills
//
// [Registry.RegisterWorkspace] adapts workspace Command and Skill manifests
// (via [Registry.RegisterCommandResource] and [Registry.RegisterSkillResource])
// into interactive slash commands. Executing one validates its declared
// arguments client-side and then forwards the reconstructed line to the store,
// so workspace commands and skills run through the same pipeline as user input.
// [SkillScope] and [EstimateSkillTokens] provide the canonical scope label and
// token estimate used to present skills.
//
// # Autocomplete
//
// [Registry.Sources] snapshots the registered commands as headless autocomplete
// sources for the REPL, including per-command argument completers.
// [CompletionViews] returns command-specific completion views for the active
// permission mode.
package command
