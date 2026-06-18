# TUI Development Guide

TaskSmith features a modal editing console designed with keyboard efficiency. The terminal UI is constructed using the **Kite** DOM framework and **Kitex** functional components.

---

## 🏗️ Functional Components & Reactivity

The UI uses standard functional component definitions (`kitex.FC` and `kitex.SimpleFC`) and reactive hooks similar to React:

- **`kitex.UseState(initial)`**: Declare local state. Returns a getter function and a setter function:
  ```go
  isExited, setIsExited := kitex.UseState(false)
  // Get: isExited()
  // Set: setIsExited(true)
  ```
- **`kitex.UseEffect(fn, deps)`**: Runs side-effects when dependencies in the `deps` slice change.
- **`kitex.Map(slice, fn)`**: Maps slices of data to virtual node trees.

### Evaluating Props vs. Passing Getters

To optimize rendering speeds, components are only updated when their properties change. If you pass a state getter function directly to a child component, the function reference pointer remains identical, and the child component will skip updates:

```go
// ⚠️ Might prevent updates inside ChildComponent if state changes
ChildComponent(ChildProps{
    State: myState, // Passes the getter function pointer
})

//  Correct: Force update on state changes
ChildComponent(ChildProps{
    State: myState(), // Passes the raw evaluated value
})
```

---

## ⚡ Global Actions & TUI Commands

TaskSmith registers and executes operations using the `internal/tui/command` registry:

- **Registration**: Builtin commands are registered during initialization:
  ```go
  command.Register("quit", func(ctx command.CommandContext) error {
      app.Quit()
      return nil
  })
  ```
- **Execution**: Views execute these commands via the `command.UseCommand` hook:
  ```go
  _, quit := command.UseCommand("quit")
  // Run it: quit()
  ```

---

## 🎨 Theme & Color Schemes

TaskSmith includes a centralized styling system that is fully responsive to active schemes:

- **Theme Configuration**: Users can select themes during setup, which is saved in `tasksmith.config.json` inside the XDG directory.
- **Accessing Theme Colors**: Components consume theme values dynamically:
  ```go
  t := theme.UseTheme()
  primaryColor := t.Color.Surface.Primary
  ```
- **Color Schemes**: Builtin themes (such as `default`, `tokyo-night`, `solarized`, and `github-dark`) are defined as JSON structures under `internal/tui/theme/builtin/` and resolved at runtime.
