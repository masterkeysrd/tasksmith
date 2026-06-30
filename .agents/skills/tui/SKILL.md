---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: tui
  description: "Terminal User Interface (TUI) development guidelines, conventions, and best practices for building and maintaining the TaskSmith kite-based TUI components, views, and interactive elements."
spec:
  useWhen: "building or editing TUI components, designing layouts and styling, managing reactive state in the UI, handling user input and keybindings, registering TUI commands, or working with themes and dynamic colors"
  keywords: [tui, kite, components, styling, keybindings, theme]
---
# TUI Development Skill

This skill outlines the guidelines, conventions, and best practices for developing the TaskSmith Terminal User Interface (TUI) using the `kite` framework and `kitex` component system.

## 1. Framework Overview

- **Framework**: Use the `kite` framework (`github.com/masterkeysrd/kite`) for all TUI development.
- **Components**: Use `kitex.FC` for standard components and `kitex.FCC` for components that accept children.
- **Reactive State**: Use `kite`'s reactive primitives (e.g., `kite.Reactive`, `kite.Use`) for managing reactive state outside the VDOM.

## 2. Component Conventions

### Props & Structure
- **Props Struct:** Define a `Props` struct for every component (e.g., `PaperProps`, `ButtonProps`).
- **Naming:** Use `Style` suffix for style variables (e.g., `NormalStyle`, `HoverStyle`).
- **Package-Level Variables:** Declare `style.Style` variables at the package level instead of hardcoding them within render functions to improve readability.
- **Style Merging:** Components should accept a `style.Style` prop for layout and visual overrides. Merge style overrides at the end of the render function.
- **Pure Overrides:** Favor pure style overrides over specific layout props (like padding/margin) to maintain API simplicity.

### Rendering
- Use `kitex.FC` and `kitex.FCC` function signatures for component definitions.
- Keep render logic focused on layout and presentation; move business logic to hooks or separate functions.

## 3. Styling & Theming

- **Theme Provider:** Always use `theme.Provider` at the root to propagate themes through the component tree.
- **Theme Consumption:** Components should consume colors reactively using `theme.UseTheme()` to access the active color scheme (`t.Color.*` and `t.Palette`).
- **Hover States:** For custom default and hover styling, components should use semantic theme-aware styles or the component's `HoverStyle` prop to handle states dynamically rather than embedding static local wrappers.
- **Color Access:** Reference colors via `t.Color.*` for semantic colors and `t.Palette` for palette-based colors.

## 4. Commands & Actions

- **Registration:** Register global actions using `command.Register(id, fn)`.
- **Execution:** Execute reactively in components using `command.UseCommand(id)` or dynamically via `command.Execute(ctx, id, args...)`.
- **Prefix Stripping:** Registered TUI commands do not include the leading `:` prefix. Strip it (e.g., using `strings.TrimPrefix`) before dynamic execution.

## 5. Input Modes

- **Reading Mode:** Use `mode.Use()` to react to the current input state.
- **Changing Mode:** Use `mode.Set()` to transition between input modes (Normal, Insert, Command).
- **Reactive Integration:** Mode state is managed using `kite`'s reactive primitives—ensure components react appropriately to mode changes.

## 6. Data Fetching & API Integration

- **Reactive Queries:** Use the `wind` package for reactive data hooks.
- **Client Access:** Ensure `UseClient` is used to access the API service from within TUI components.
- **Reactivity:** Leverage `wind`'s reactive primitives to automatically update views when underlying data changes.

## 7. Directory Structure Reference

Key TUI packages under `internal/tui/`:

| Package | Purpose |
|---------|---------|
| `tui/api` | TUI-specific API client context |
| `tui/command` | Global command registry and execution |
| `tui/components` | Reusable UI components (button, card, modal, etc.) |
| `tui/icon` | Icon definitions |
| `tui/keymap` | Mode-aware keybinding system |
| `tui/mode` | Reactive input mode store |
| `tui/plugin` | Plugin system for extending TUI |
| `tui/queries` | Reactive data hooks using `wind` |
| `tui/shell` | Shell-level TUI components (titlebar, sidebar, statusline, commandbar) |
| `tui/theme` | Dynamic theme styling and resolution |
| `tui/views` | Top-level views (chat, setup, welcome) |

## 8. Common Pitfalls

- **Hardcoded Styles:** Never hardcode colors or styles in render functions. Always consume from the theme.
- **Missing Props:** Every component must declare a `Props` struct. Omitting it breaks the component API.
- **Non-Reactive State:** Use `kite` reactive primitives for state that should trigger UI updates. Do not use plain variables for UI-bound state.
- **Command Prefix:** Remember to strip the leading `:` when dynamically executing commands via `command.Execute`.
- **Style Merging:** Always merge incoming style overrides at the end of the render function so consumers can customize components.

## 9. Component Development Checklist

When creating a new component:

1. Define a `Props` struct with all configurable options.
2. Use `kitex.FC` or `kitex.FCC` for the component signature.
3. Declare any `style.Style` variables at the package level.
4. Consume theme colors via `theme.UseTheme()`.
5. Handle `style.Style` prop merging at the end of the render.
6. Wire up any commands using `command.UseCommand()` or `command.Execute()`.
7. Ensure reactive state uses `kite` primitives.
8. Write a corresponding test or example usage.

---

# Concrete Examples

This section provides real, copy-paste-ready examples of common TUI patterns used in TaskSmith.

## 9.1 Simple FC Component (Button-like)

```go
package components

import (
    "image/color"

    "github.com/masterkeysrd/kite/event"
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/style"
    "github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// MyButtonProps defines the properties for the MyButton component.
type MyButtonProps struct {
    // Key is an optional unique identifier for the component.
    Key string
    // Disabled indicates if the button is interactive.
    Disabled bool
    // OnClick is the callback triggered when the button is clicked.
    OnClick func()
    // Style allows passing additional style overrides.
    Style style.Style
    // HoverStyle allows passing additional style overrides for the hovered state.
    HoverStyle style.Style
    // Children is the content of the button.
    Children []kitex.Node
}

// MyButton is an interactive component that triggers an action.
var MyButton = kitex.FC("MyButton", func(props MyButtonProps) kitex.Node {
    isHovered, setIsHovered := kitex.UseState(false)
    t := theme.UseTheme()

    // Fallback if theme is not available
    if t == nil {
        return kitex.Button(kitex.ButtonProps{
            Key:      props.Key,
            Disabled: props.Disabled,
            OnClick: func(e event.Event) {
                if props.OnClick != nil && !props.Disabled {
                    props.OnClick()
                }
            },
        }, props.Children...)
    }

    // Resolve colors from theme
    bgColor := t.Color.Surface.Primary
    fgColor := t.Color.Text.InversePrimary
    hoverBg := t.Color.Surface.PrimaryHover

    var s style.Style
    if isHovered() {
        s = style.S().Background(hoverBg).Foreground(fgColor)
    } else {
        s = style.S().Background(bgColor).Foreground(fgColor)
    }

    // Merge with explicit style overrides (always at the end)
    s = s.Merge(props.Style)
    if isHovered() {
        s = s.Merge(props.HoverStyle)
    }

    return kitex.Button(kitex.ButtonProps{
        Key:      props.Key,
        Style:    s,
        Disabled: props.Disabled,
        OnClick: func(e event.Event) {
            if props.OnClick != nil && !props.Disabled {
                props.OnClick()
            }
        },
        OnMouseEnter: func(e event.Event) {
            setIsHovered(true)
        },
        OnMouseLeave: func(e event.Event) {
            setIsHovered(false)
        },
    }, props.Children...)
})
```

## 9.2 FCC Component (Container with Children)

```go
package components

import (
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/style"
    "github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// CardCtx holds shared state passed down to child components.
type CardCtx struct {
    expanded func() bool
}

var cardCtx = kitex.CreateContext[*CardCtx](nil)

// CardProps defines the properties for the Card container component.
type CardProps struct {
    // DefaultExpanded indicates if the card content is expanded by default.
    DefaultExpanded bool
    // OnToggle is triggered when the card is expanded/collapsed.
    OnToggle func(bool)
    // Style allows passing additional style overrides.
    Style style.Style
    // Children are the card's content nodes.
    Children []kitex.Node
}

// Card is a container component that can be expanded or collapsed.
var Card = kitex.FCC("Card", func(props CardProps) kitex.Node {
    expanded, setExpanded := kitex.UseState(props.DefaultExpanded)

    ctx := &CardCtx{
        expanded: func() bool { return expanded },
    }

    // Toggle handler
    toggle := func(val bool) {
        setExpanded(val)
        if props.OnToggle != nil {
            props.OnToggle(val)
        }
    }

    // Pass context to children
    return cardCtx.Provider(ctx,
        Paper(PaperProps{
            Color:   PaperSurface,
            Variant: PaperOutlined,
            Style:   style.S().Padding(1).Merge(props.Style),
        },
            // Render children
            kitex.Fragment(props.Children...),
        ),
    )
})
```

## 9.3 Using Theme Colors in a Component

```go
import (
    "image/color"
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/style"
    "github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

var BadgeBaseStyle = style.S().
    Display(style.DisplayFlex).
    AlignItems(style.AlignCenter).
    PaddingHorizontal(1).
    BorderRadius(2)

func Badge(props BadgeProps) kitex.Node {
    t := theme.UseTheme()

    if t == nil {
        return kitex.Box(kitex.BoxProps{}, kitex.Text("No Theme"))
    }

    // Resolve semantic color from theme
    var bgColor, fgColor color.Color
    switch props.Color {
    case BadgeSuccess:
        bgColor = t.Color.Surface.Success
        fgColor = t.Color.Text.InversePrimary
    case BadgeError:
        bgColor = t.Color.Surface.Error
        fgColor = t.Color.Text.InversePrimary
    default:
        bgColor = t.Color.Surface.Primary
        fgColor = t.Color.Text.InversePrimary
    }

    s := BadgeBaseStyle.Background(bgColor).Foreground(fgColor)
    s = s.Merge(props.Style)

    return kitex.Box(kitex.BoxProps{Style: s}, props.Children...)
}
```

## 9.4 Reactive State with kitex.UseState

```go
import "github.com/masterkeysrd/kite/extras/kitex"

var Counter = kitex.FC("Counter", func(props CounterProps) kitex.Node {
    count, setCount := kitex.UseState(0)
    name, setName := kitex.UseState("TaskSmith")

    return kitex.Box(kitex.BoxProps{},
        kitex.Text("Count: " + strconv.Itoa(count())),
        kitex.Button(kitex.ButtonProps{
            OnClick: func(e event.Event) {
                setCount(count() + 1)
            },
        }, kitex.Text("Increment")),
        kitex.Input(kitex.InputProps{
            Value:    name(),
            OnChange: func(e event.Event) {
                if ie, ok := e.(*event.InputEvent); ok {
                    setName(ie.Value)
                }
            },
        }),
    )
})
```

## 9.5 Side Effects with kitex.UseEffect

```go
import "github.com/masterkeysrd/kite/extras/kitex"

var AutoRefresh = kitex.FC("AutoRefresh", func(props AutoRefreshProps) kitex.Node {
    data, setData := kitex.UseState([]string{})

    // Run effect when `interval` or `enabled` changes
    kitex.UseEffect(func() {
        if !props.Enabled {
            return
        }
        // Fetch data on mount and when dependencies change
        fetchData(func(result []string) {
            setData(result)
        })
    }, []any{props.Interval, props.Enabled})

    return kitex.Box(kitex.BoxProps{},
        kitex.Text("Items: " + strconv.Itoa(len(data()))),
    )
})
```

## 9.6 Intervals with kitex.UseInterval

```go
import (
    "time"
    "github.com/masterkeysrd/kite/extras/kitex"
)

var LiveClock = kitex.FC("LiveClock", func(props LiveClockProps) kitex.Node {
    now, setNow := kitex.UseState(time.Now())

    // Tick every second
    kitex.UseInterval(func() {
        setNow(time.Now())
    }, 1*time.Second, nil)

    return kitex.Box(kitex.BoxProps{},
        kitex.Text(now().Format(time.Stamp)),
    )
})
```

## 9.7 Refs with kitex.UseRef and kitex.CreateRef

```go
import (
    "github.com/masterkeysrd/kite/dom"
    "github.com/masterkeysrd/kite/extras/kitex"
)

var FocusableInput = kitex.FC("FocusableInput", func(props FocusableInputProps) kitex.Node {
    inputRef := kitex.CreateRef[dom.Element]()
    focused, setFocused := kitex.UseState(false)

    // Focus the input when a button is clicked
    focusInput := func() {
        kitex.PostMacro(func() {
            if inputRef.Current != nil {
                doc := inputRef.Current.OwnerDocument()
                if doc != nil {
                    doc.Focus(inputRef.Current)
                }
            }
        })
    }

    return kitex.Box(kitex.BoxProps{},
        kitex.Input(kitex.InputProps{
            Ref:    inputRef,
            OnFocus: func(e event.Event) { setFocused(true) },
            OnBlur:  func(e event.Event) { setFocused(false) },
        }),
        kitex.Button(kitex.ButtonProps{
            OnClick: func(e event.Event) {
                focusInput()
            },
        }, kitex.Text("Focus Input")),
    )
})
```

## 9.8 Wind Reactive Queries

```go
package queries

import (
    "github.com/masterkeysrd/kite/extras/wind"
    "github.com/masterkeysrd/kite/promise"
    "github.com/masterkeysrd/tasksmith/internal/api"
    tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseListSessions retrieves a reactive list of all user sessions.
func UseListSessions() wind.Result[*api.ListSessionsResponse] {
    client := tuiapi.UseClient()
    return wind.Use(api.ListSessionsRequest{}, promise.WrapWithProps(client.ListSessions))
}

// UseGetSessionMessages retrieves a reactive message log for the given session.
func UseGetSessionMessages(sessionID string) wind.Result[*api.GetSessionMessagesResponse] {
    client := tuiapi.UseClient()
    return wind.Use(api.GetSessionMessagesRequest{SessionID: sessionID},
        promise.WrapWithProps(client.GetSessionMessages))
}
```

## 9.9 Using Wind Queries in a View

```go
package chat

import (
    "github.com/masterkeysrd/kite/extras/wind"
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/tasksmith/internal/api"
    "github.com/masterkeysrd/tasksmith/internal/tui/queries"
    tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

var View = kitex.FC("ChatView", func(props ViewProps) kitex.Node {
    client := tuiapi.UseClient()
    windClient := wind.UseClient()

    // Fetch sessions reactively
    sessionsQuery := queries.UseListSessions()

    // Fetch messages reactively for the given session
    msgsQuery := queries.UseGetSessionMessages(props.SessionID)

    // Access query data
    var sessions []api.Session
    if sessionsQuery.Data != nil {
        sessions = sessionsQuery.Data.Sessions
    }

    return kitex.Box(kitex.BoxProps{},
        kitex.Text("Sessions: " + strconv.Itoa(len(sessions))),
        kitex.Box(kitex.BoxProps{},
            kitex.Fragment(func() []kitex.Node {
                var nodes []kitex.Node
                for _, s := range sessions {
                    nodes = append(nodes, kitex.Text(s.Title))
                }
                return nodes
            }()...),
        ),
    )
})
```

## 9.10 Invalidating Wind Queries

```go
import (
    "github.com/masterkeysrd/kite/extras/wind"
    "github.com/masterkeysrd/tasksmith/internal/api"
)

var InvalidateExample = kitex.FC("InvalidateExample", func(props InvalidateExampleProps) kitex.Node {
    windClient := wind.UseClient()

    return kitex.Box(kitex.BoxProps{},
        kitex.Button(kitex.ButtonProps{
            OnClick: func(e event.Event) {
                // Invalidate specific queries to trigger refetch
                windClient.InvalidateQueries(api.ListSessionsRequest{})
                windClient.InvalidateQueries(api.GetSessionMessagesRequest{
                    SessionID: props.SessionID,
                })
            },
        }, kitex.Text("Refresh")),
    )
})
```

## 9.11 API Client Context (Dependency Injection)

```go
package api

import (
    "context"
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/tasksmith/internal/api"
)

// Client interface defines all API methods available to the TUI.
type Client interface {
    ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error)
    CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error)
    SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error)
    // ... more methods
}

// clientCtx is the kitex context for dependency injection.
var clientCtx = kitex.CreateContext[Client](nil)

// Provider injects the API client into the component tree.
func Provider(props Props, children ...kitex.Node) kitex.Node {
    return clientCtx.Provider(props.Client, children...)
}

// UseClient retrieves the injected API client. Must be called within a component.
func UseClient() Client {
    return kitex.UseContext(clientCtx)
}
```

## 9.12 Reactive Store (Outside VDOM)

```go
package mode

import (
    "github.com/masterkeysrd/kite/extras/kites"
)

type state struct {
    current Mode
}

// store is the global reactive store for the TUI mode.
var store = kites.Create(state{
    current: Normal,
})

// Set updates the current TUI mode.
func Set(m Mode) {
    store.Set(func(s state) state {
        s.current = m
        return s
    })
}

// Use returns the current mode reactively. Must be called within a component render.
func Use() Mode {
    return kites.Use(store, func(s state) Mode {
        return s.current
    })
}
```

## 9.13 Command Registration (in app/commands.go)

```go
package app

import (
    "github.com/masterkeysrd/tasksmith/internal/tui/command"
    "github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

func (app *Application) InitializeCommands() {
    // Register a quit command
    command.Register("quit", func(ctx command.CommandContext) error {
        app.Quit()
        return nil
    })

    // Register a mode-switching command
    command.Register("startinsert", func(ctx command.CommandContext) error {
        mode.Set(mode.Insert)
        return nil
    })

    command.Register("stopinsert", func(ctx command.CommandContext) error {
        mode.Set(mode.Normal)
        return nil
    })
}
```

## 9.14 Using UseCommand in a Component

```go
package sidebar

import (
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/tasksmith/internal/tui/command"
)

var CommandButton = kitex.FC("CommandButton", func(props CommandButtonProps) kitex.Node {
    // UseCommand returns (state, execute) — state is reactive
    cmdState, execute := command.UseCommand("quit")

    return kitex.Box(kitex.BoxProps{},
        kitex.Button(kitex.ButtonProps{
            OnClick: func(e event.Event) {
                execute() // Runs the "quit" command
            },
            Disabled: cmdState().IsPending,
        }, kitex.Text(
            func() string {
                if cmdState().IsPending {
                    return "Quitting..."
                }
                return "Quit"
            }(),
        )),
        kitex.If(cmdState().Error != nil,
            func() kitex.Node {
                return kitex.Text("Error: " + cmdState().Error.Error())
            },
        ),
    )
})
```

## 9.15 Executing Commands Dynamically (with prefix stripping)

```go
import (
    "strings"
    "context"
    "github.com/masterkeysrd/tasksmith/internal/tui/command"
)

// In a keymap handler or component:
func handleCommandInput(cmdStr string) {
    // Strip leading ":" prefix if present
    cmdStr = strings.TrimPrefix(cmdStr, ":")
    ctx := context.Background()
    if err := command.Execute(ctx, cmdStr); err != nil {
        // Handle unknown command
    }
}
```

## 9.16 Keymap Registration (in app/keymap.go)

```go
package app

import (
    "context"
    "github.com/masterkeysrd/tasksmith/internal/tui/command"
    "github.com/masterkeysrd/tasksmith/internal/tui/keymap"
    "github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

func (app *Application) InitializeKeymap() {
    // Normal Mode bindings
    keymap.Set([]mode.Mode{mode.Normal}, "q", func(ctx context.Context) {
        _ = command.Execute(ctx, "quit")
    }, keymap.Description("Quit application"))

    keymap.Set([]mode.Mode{mode.Normal}, "i", func(ctx context.Context) {
        _ = command.Execute(ctx, "startinsert")
    }, keymap.Description("Enter insert mode"))

    keymap.Set([]mode.Mode{mode.Normal}, ":", func(ctx context.Context) {
        mode.Set(mode.Command)
    }, keymap.Description("Enter command mode"))

    // Insert Mode bindings
    keymap.Set([]mode.Mode{mode.Insert}, "<Esc>", func(ctx context.Context) {
        _ = command.Execute(ctx, "stopinsert")
    }, keymap.Description("Exit insert mode"))

    // Command Mode bindings
    keymap.Set([]mode.Mode{mode.Command}, "<Esc>", func(ctx context.Context) {
        mode.Set(mode.Normal)
    }, keymap.Description("Exit command mode"))
}
```

## 9.17 Promise-Based API Calls in Views

```go
import (
    "context"
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/promise"
    "github.com/masterkeysrd/tasksmith/internal/api"
    tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

var CreateSessionButton = kitex.FC("CreateSessionButton", func(props CreateSessionButtonProps) kitex.Node {
    client := tuiapi.UseClient()
    windClient := wind.UseClient()
    submitting, setSubmitting := kitex.UseState(false)

    return kitex.Button(kitex.ButtonProps{
        Disabled: submitting(),
        OnClick: func(e event.Event) {
            setSubmitting(true)
            promise.New(func(ctx context.Context) (string, error) {
                resp, err := client.CreateSession(ctx, api.CreateSessionRequest{
                    Title: "New Chat",
                })
                if err != nil {
                    return "", err
                }
                return resp.Session.ID, nil
            }).Then(func(id string) {
                setSubmitting(false)
                // Invalidate queries to refresh the session list
                windClient.InvalidateQueries(api.ListSessionsRequest{})
                // Navigate to the new session
                active.SetSessionID(id)
            }, func(err error) {
                setSubmitting(false)
                // Handle error
            })
        },
    }, kitex.Text(
        func() string {
            if submitting() {
                return "Creating..."
            }
            return "New Session"
        }(),
    ))
})
```

## 9.18 Using kitex.UseContext for Component State Sharing

```go
package components

import (
    "github.com/masterkeysrd/kite/extras/kitex"
)

// State shared between parent and child components.
type accordionState struct {
    expanded    func() bool
    setExpanded func(bool)
}

var accordionCtx = kitex.CreateContext[*accordionState](nil)

// Parent component provides state via context.
var Accordion = kitex.FCC("Accordion", func(props AccordionProps) kitex.Node {
    expanded, setExpanded := kitex.UseState(false)

    state := &accordionState{
        expanded:    func() bool { return expanded },
        setExpanded: func(val bool) { setExpanded(val) },
    }

    return accordionCtx.Provider(state,
        Paper(PaperProps{},
            kitex.Box(kitex.BoxProps{},
                kitex.Text("Accordion"),
            ),
        ),
    )
})

// Child component consumes state from context.
var AccordionSummary = kitex.FCC("AccordionSummary", func(props AccordionSummaryProps) kitex.Node {
    state := kitex.UseContext(accordionCtx)
    if state == nil {
        return kitex.Text("AccordionSummary must be inside Accordion")
    }

    return kitex.Button(kitex.ButtonProps{
        OnClick: func(e event.Event) {
            state.setExpanded(!state.expanded())
        },
    }, kitex.Text("Toggle"))
})
```

## 9.19 Layout Patterns with style.S()

```go
import (
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/style"
)

// Common layout patterns:

// Full-width, full-height flex column
var ColumnLayout = style.S().
    Width(style.Percent(100)).
    Height(style.Percent(100)).
    Display(style.DisplayFlex).
    FlexDirection(style.FlexColumn)

// Flex row with items spaced evenly
var RowLayout = style.S().
    Display(style.DisplayFlex).
    FlexDirection(style.FlexRow).
    JustifyContent(style.JustifyBetween).
    AlignItems(style.AlignCenter)

// Centered content
var CenteredLayout = style.S().
    Display(style.DisplayFlex).
    JustifyContent(style.JustifyCenter).
    AlignItems(style.AlignCenter)

// Scrollable container
var ScrollableLayout = style.S().
    Flex(1, 1, style.Cells(0)).
    MinHeight(style.Cells(0)).
    Display(style.DisplayFlex).
    Overflow(style.OverflowAuto)
```

## 9.20 Mode-Aware Component

```go
package components

import (
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/tasksmith/internal/tui/mode"
    "github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ModeAwareBadge shows different styling based on the current input mode.
var ModeAwareBadge = kitex.FC("ModeAwareBadge", func(props ModeAwareBadgeProps) kitex.Node {
    t := theme.UseTheme()
    currentMode := mode.Use()

    if t == nil {
        return kitex.Text("No theme")
    }

    // Choose color based on mode
    var bgColor color.Color
    switch currentMode {
    case mode.Insert:
        bgColor = t.Color.Surface.Info
    case mode.Command:
        bgColor = t.Color.Surface.Tertiary
    default: // mode.Normal
        bgColor = t.Color.Surface.Success
    }

    return kitex.Box(kitex.BoxProps{
        Style: style.S().Background(bgColor).Padding(1),
    }, kitex.Text(currentMode.String()))
})
```

## 9.21 Window Titlebar Pattern (from shell/titlebar/view.go)

```go
package titlebar

import (
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/style"
    "github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

var View = kitex.FC("Titlebar", func(props ViewProps) kitex.Node {
    t := theme.UseTheme()

    var s style.Style
    if t != nil {
        s = style.S().
            Width(style.Percent(100)).
            Height(style.Cells(1)).
            Display(style.DisplayFlex).
            AlignItems(style.AlignCenter).
            JustifyContent(style.JustifyCenter).
            Background(t.Color.Surface.BaseFocus).
            Foreground(t.Color.Text.Primary).
            Bold(true)
    }

    return kitex.Box(kitex.BoxProps{Style: s},
        kitex.Text(props.Title),
    )
})
```

## 9.22 Statusline Pattern (from shell/statusline/view.go)

```go
package statusline

import (
    "github.com/masterkeysrd/kite/extras/kitex"
    "github.com/masterkeysrd/kite/style"
    "github.com/masterkeysrd/tasksmith/internal/tui/mode"
    "github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

var View = kitex.FC("Statusline", func(props ViewProps) kitex.Node {
    t := theme.UseTheme()
    m := mode.Use()

    var s style.Style
    if t != nil {
        s = style.S().
            Width(style.Percent(100)).
            Height(style.Cells(1)).
            Display(style.DisplayFlex).
            AlignItems(style.AlignCenter).
            Background(t.Color.Surface.BaseDisabled).
            Foreground(t.Color.Text.Primary)
    }

    return kitex.Box(kitex.BoxProps{Style: s},
        kitex.Box(kitex.BoxProps{
            Style: style.S().
                Background(style.ColorRGB{R: 0, G: 0, B: 0}).
                Foreground(t.Color.Surface.Base).
                PaddingHorizontal(1),
        }, kitex.Text(m.String())),
        kitex.Box(kitex.BoxProps{
            Style: style.S().Flex(1, 1, style.Cells(0)),
        }),
        kitex.Text(" TaskSmith"),
    )
})
```
