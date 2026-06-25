# Loom State Management in TaskSmith

In `internal/agent/graph/graph.go`, TaskSmith implements Loom's state machine using the `AgentState` struct.

## State Structure

```go
type AgentState struct {
	Messages              message.MessageList                 `json:"messages"`
	Todos                 []tools.Todo                        `json:"todos"`
	ActivatedSkills       []string                            `json:"activated_skills"`
	PendingAuthorizations []permissions.AuthorizationRequest  `json:"pending_authorizations,omitempty"`
	Decisions             []permissions.AuthorizationDecision `json:"decisions,omitempty"`
}
```

## Deep Copy Requirement

Because Loom checkpoints states asynchronously and can fork graph execution, your state struct **must** implement a deep copy method:

```go
// Copy performs a deep copy of AgentState to satisfy the loom graph.State interface.
func (s AgentState) Copy() AgentState {
	copied := AgentState{}
    // Slices MUST be explicitly copied to prevent race conditions during node execution!
	if s.Messages != nil {
		copied.Messages = make(message.MessageList, len(s.Messages))
		copy(copied.Messages, s.Messages)
	}
	// ... (Copy Todos, Skills, Authorizations, etc.)
	return copied
}
```

## Node Execution Updates

When a node successfully completes, it should return a `graph.Update[AgentState]`. Inside the update closure, modify the state slice and return it. Loom will apply this mutation to the global state:

```go
return graph.Update[AgentState](func(state AgentState) AgentState {
    state.Messages = append(state.Messages, newMsg)
    return state
}), nil
```

## Tool State Hooks (`hooks.go`)

Sometimes a tool execution needs to mutate the graph state (for example, the `todos` tool needs to actually update `AgentState.Todos`). 
This is handled via `ToolStateHook`s in `hooks.go`.

```go
type ToolStateHook func(ctx context.Context, args map[string]any, a *AgentGraph) (func(AgentState) AgentState, error)
```
When `executeTools` sees a tool call match a hook (e.g., `"activate_skill"`), it parses the JSON arguments and generates a state update closure:
```go
return func(s AgentState) AgentState {
    s.ActivatedSkills = append(s.ActivatedSkills, input.Skill)
    return s
}, nil
```
This keeps tool logic pure while safely allowing them to manipulate the orchestrator's state.
