package command

import (
	"context"
	"fmt"
	"sync"
)

// CommandContext holds arguments passed during command execution.
// (Additional fields like Deps can be added here in the future if strictly necessary,
// but we keep it minimal for now).
type CommandContext struct {
	Ctx context.Context

	// Payload holds an optional arbitrary payload passed to the command.
	Payload any

	// Args holds the arguments passed to the command.
	Args []string
}

// CommandFn is the signature for executable commands.
type CommandFn func(ctx CommandContext) error

// registry holds the command map, protected by a RWMutex.
type registry struct {
	mu       sync.RWMutex
	commands map[string]CommandFn
}

// newRegistry creates a new initialized registry.
func newRegistry() *registry {
	return &registry{
		commands: make(map[string]CommandFn),
	}
}

// globalRegistry is the singleton command registry used by Register and Execute.
var globalRegistry = newRegistry()

// Register adds a new command to the global registry.
// It panics if the ID is already registered (to catch duplicates on startup)
// or if fn is nil.
func Register(id string, fn CommandFn) {
	if fn == nil {
		panic("commands: Register: command function cannot be nil")
	}
	globalRegistry.register(id, fn)
}

// register is the internal method on registry that adds a command.
func (r *registry) register(id string, fn CommandFn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.commands[id]; exists {
		panic(fmt.Sprintf("commands: register duplicate command %q", id))
	}
	r.commands[id] = fn
}

// Execute looks up the command by ID and runs it with the provided arguments.
// It returns an error if the ID is not found.
func Execute(ctx context.Context, id string, args ...string) error {
	return globalRegistry.execute(ctx, id, nil, args)
}

// ExecuteWithPayload looks up the command by ID and runs it with the provided
// payload and arguments. It returns an error if the ID is not found.
func ExecuteWithPayload(ctx context.Context, id string, payload any, args ...string) error {
	return globalRegistry.execute(ctx, id, payload, args)
}

// execute is the internal method on registry that looks up and runs a command.
func (r *registry) execute(ctx context.Context, id string, payload any, args []string) error {
	r.mu.RLock()
	fn, ok := r.commands[id]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("command not found: %s", id)
	}

	if args == nil {
		args = []string{}
	}

	return fn(CommandContext{
		Ctx:     ctx,
		Payload: payload,
		Args:    args,
	})
}

// ResetForTest resets the global registry to a clean state.
// This is intended for use in tests only.
func ResetForTest() {
	globalRegistry = newRegistry()
}
