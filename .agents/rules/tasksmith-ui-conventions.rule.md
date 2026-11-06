---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: tasksmith-ui-conventions
  displayName: TaskSmith UI Conventions
  description: Non-negotiable conventions for terminal UI code on the trail/kitex stack
  labels:
    category: quality
spec:
  priority: 50
  triggers:
    globs:
      - "internal/ui/**"
      - "internal/tui/**"
      - "internal/cli/components/**"
      - "internal/cli/command/**"
      - "internal/cli/store/**"
    useWhen: Writing or reviewing terminal UI code, components, or interactive command views
---

# TaskSmith UI Conventions

These hold for every UI change. For layering, hooks, props, and tests, load the `tasksmith-tui`
skill — this rule only states what must never be violated.

1. **Components are `trail.FC`/`trail.FCC`/`trail.SimpleFC` returning `trail.Node`**, with props in
   an exported `<Name>Props` struct. State hooks come from `kitex`, input/lifecycle hooks from
   `trail`. Never import `kite/internal/**`.
2. **Never call `kitex.Render` outside `_test.go`.** The app mounts once via `trail.Run`;
   interactive views enqueue a slot (`store.EnqueueSlot`), they never start their own runtime.
3. **Colors come from `theme.UseTheme()`** (`scheme.Color.<Group>.<Role>`). No literal colors,
   hex values, or invented fields — grep the scheme first.
4. **Reuse before authoring.** Extend an existing component's props with an optional field instead
   of forking it. Use `internal/ui/format` for numbers/durations/tokens and
   `internal/core/strutils` for truncation; never hand-roll either.
5. **Effect hooks take `[]any{}` for "once on mount", never `nil`** — `nil` deps re-runs the effect
   after every render. Never pass `nil` to `UseCallback`.
6. **Every new or changed component ships a test that renders it**, using
   `internal/tui/testutils` (`Render`/`NewEnv`, or `NewApp` when the component needs the live
   runtime), asserting on rendered text or key-driven effects. Do not redefine a local helper.
7. **Keys must be discoverable**: every entry in a view's `Keys` map needs a matching
   `KeyBinding`, and `docs/reference/cli.md` must reflect the command, keys, and flags in the same
   change.
