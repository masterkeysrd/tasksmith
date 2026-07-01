package command

import (
	"context"
	"fmt"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/tui/focus"
)

// CommandContext holds arguments passed during command execution.
type CommandContext struct {
	Ctx context.Context

	// Payload holds an optional arbitrary payload passed to the command.
	Payload any

	// Args holds the arguments passed to the command.
	Args []string

	// FocusContext holds the resolved focus context name when the command was run.
	FocusContext string
}

// CommandFn is the signature for executable commands.
type CommandFn func(ctx CommandContext) error

// Options holds metadata for a registered command.
type Options struct {
	Context string
}

// Option is a functional option for configuring a command.
type Option func(*Options)

// Context returns an Option that restricts the command to a specific focus context/pane.
func Context(ctx string) Option {
	return func(o *Options) {
		o.Context = ctx
	}
}

// registry holds the command maps, protected by a RWMutex.
type registry struct {
	mu              sync.RWMutex
	commands        map[string]CommandFn
	contextCommands map[string]map[string]CommandFn
}

// newRegistry creates a new initialized registry.
func newRegistry() *registry {
	return &registry{
		commands:        make(map[string]CommandFn),
		contextCommands: make(map[string]map[string]CommandFn),
	}
}

// globalRegistry is the singleton command registry used by Register and Execute.
var globalRegistry = newRegistry()

// Register adds a new command to the global or context-scoped registry.
// It panics if the ID is already registered (to catch duplicates on startup)
// or if fn is nil.
func Register(id string, fn CommandFn, opts ...Option) {
	if fn == nil {
		panic("commands: Register: command function cannot be nil")
	}
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	globalRegistry.register(id, fn, o.Context)
}

// register is the internal method on registry that adds a command.
func (r *registry) register(id string, fn CommandFn, ctxName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ctxName != "" {
		if r.contextCommands[ctxName] == nil {
			r.contextCommands[ctxName] = make(map[string]CommandFn)
		}
		if _, exists := r.contextCommands[ctxName][id]; exists {
			panic(fmt.Sprintf("commands: register duplicate context command %q in context %q", id, ctxName))
		}
		r.contextCommands[ctxName][id] = fn
	} else {
		if _, exists := r.commands[id]; exists {
			panic(fmt.Sprintf("commands: register duplicate command %q", id))
		}
		r.commands[id] = fn
	}
}

// ExecFunc returns a keymap-ready callback that runs the specified command.
func ExecFunc(id string, args ...string) func(context.Context) {
	return func(ctx context.Context) {
		_ = Execute(ctx, id, args...)
	}
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
	var activeContext string
	r.mu.RLock()
	var fn CommandFn
	var ok bool

	focus.WalkContextChain(func(ctxName string) bool {
		if ctxCmds, exists := r.contextCommands[ctxName]; exists {
			if fn, ok = ctxCmds[id]; ok {
				activeContext = ctxName
				return false // Found, stop walking
			}
		}
		return true // Keep looking
	})

	if !ok {
		fn, ok = r.commands[id]
	}
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("command not found: %s", id)
	}

	if args == nil {
		args = []string{}
	}

	return fn(CommandContext{
		Ctx:          ctx,
		Payload:      payload,
		Args:         args,
		FocusContext: activeContext,
	})
}

// ResetForTest resets the global registry to a clean state.
// This is intended for use in tests only.
func ResetForTest() {
	globalRegistry = newRegistry()
}

// List returns a list of all registered command names.
func List() []string {
	return globalRegistry.list()
}

func (r *registry) list() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.commands))
	for k := range r.commands {
		keys = append(keys, k)
	}
	return keys
}
