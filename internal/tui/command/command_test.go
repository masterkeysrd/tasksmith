package command

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/masterkeysrd/kite/promise"
)

// syncScheduler runs all tasks synchronously for deterministic testing.
type syncScheduler struct {
	mu        sync.Mutex
	pending   []func()
	executing bool
}

func (s *syncScheduler) RunBackground(task func(context.Context)) {
	task(context.Background())
}

func (s *syncScheduler) QueueMicrotask(task func()) {
	s.mu.Lock()
	s.pending = append(s.pending, task)
	s.mu.Unlock()
	s.flush()
}

func (s *syncScheduler) QueueMacrotask(task func()) {
	s.mu.Lock()
	s.pending = append(s.pending, task)
	s.mu.Unlock()
	s.flush()
}

func (s *syncScheduler) flush() {
	s.mu.Lock()
	if s.executing {
		s.mu.Unlock()
		return
	}
	s.executing = true
	tasks := s.pending
	s.pending = nil
	s.mu.Unlock()

	for _, task := range tasks {
		task()
	}

	s.mu.Lock()
	s.executing = false
	s.mu.Unlock()
}

// resetScheduler installs a synchronous scheduler so promise callbacks
// fire immediately and deterministically in tests.
func resetScheduler() {
	promise.SetScheduler(&syncScheduler{})
}

// mockStateSetter simulates kitex.UseState for testing.
type mockStateSetter struct {
	mu      sync.RWMutex
	state   CommandState
	updated chan struct{}
}

func newMockStateSetter(initial CommandState) *mockStateSetter {
	return &mockStateSetter{
		state:   initial,
		updated: make(chan struct{}, 1),
	}
}

func (m *mockStateSetter) set(next CommandState) {
	m.mu.Lock()
	m.state = next
	m.mu.Unlock()
	select {
	case m.updated <- struct{}{}:
	default:
	}
}

func (m *mockStateSetter) get() CommandState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// executeFn is the same logic as UseCommand's execute closure, but without
// kitex.UseState so it can be tested in isolation.
func executeFn(_ context.Context, id string, args []string, setState func(CommandState)) {
	setState(CommandState{IsPending: true})

	promise.New(func(runCtx context.Context) (any, error) {
		return nil, Execute(runCtx, id, args...)
	}).Then(
		func(any) {
			setState(CommandState{IsPending: false, Error: nil})
		},
		func(err error) {
			setState(CommandState{IsPending: false, Error: err})
		},
	)
}

// TestUseCommand_StateTransitions verifies that the execute closure transitions
// state from idle -> pending -> idle with no error.
func TestUseCommand_StateTransitions(t *testing.T) {
	resetRegistry()
	resetScheduler()

	Register("test.transition", func(ctx CommandContext) error {
		return nil
	})

	setter := newMockStateSetter(CommandState{})

	// Execute the command using the same pattern as UseCommand.
	executeFn(context.Background(), "test.transition", nil, setter.set)

	// After promise settles, state should be idle.
	s := setter.get()
	if s.IsPending {
		t.Fatal("expected IsPending false after completion")
	}
	if s.Error != nil {
		t.Errorf("expected no error after completion, got %v", s.Error)
	}
}

// TestUseCommand_ErrorCapture verifies that errors from Execute are surfaced
// in state.Error.
func TestUseCommand_ErrorCapture(t *testing.T) {
	resetRegistry()
	resetScheduler()

	setter := newMockStateSetter(CommandState{})

	executeFn(context.Background(), "test.missing", nil, setter.set)

	s := setter.get()
	if s.IsPending {
		t.Fatal("expected IsPending false after completion")
	}
	if s.Error == nil {
		t.Fatal("expected error to be captured, got nil")
	}
	if !strings.Contains(s.Error.Error(), "command not found") {
		t.Errorf("expected error containing 'command not found', got %v", s.Error)
	}
}

// TestUseCommand_CommandNotFound verifies that executing an unregistered
// command sets state.Error to the appropriate error.
func TestUseCommand_CommandNotFound(t *testing.T) {
	resetRegistry()
	resetScheduler()

	setter := newMockStateSetter(CommandState{})

	executeFn(context.Background(), "nonexistent.command", nil, setter.set)

	s := setter.get()
	if s.Error == nil {
		t.Fatal("expected error for unregistered command, got nil")
	}
	expected := "command not found: nonexistent.command"
	if s.Error.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, s.Error.Error())
	}
}

// TestUseCommand_MultipleExecutions verifies that rapid successive calls to
// execute correctly update state each time.
func TestUseCommand_MultipleExecutions(t *testing.T) {
	resetRegistry()
	resetScheduler()

	var callCount int
	Register("test.multi", func(ctx CommandContext) error {
		callCount++
		return nil
	})

	setter := newMockStateSetter(CommandState{})

	// Execute three times in quick succession.
	for range 3 {
		executeFn(context.Background(), "test.multi", nil, setter.set)
	}

	s := setter.get()
	if s.IsPending {
		t.Fatal("expected IsPending false after all executions")
	}
	if callCount != 3 {
		t.Errorf("expected 3 command executions, got %d", callCount)
	}
}

// TestUseCommand_WithArgs verifies that arguments are passed through correctly.
func TestUseCommand_WithArgs(t *testing.T) {
	resetRegistry()
	resetScheduler()

	var receivedArgs []string
	Register("test.args", func(ctx CommandContext) error {
		receivedArgs = ctx.Args
		return nil
	})

	setter := newMockStateSetter(CommandState{})

	executeFn(context.Background(), "test.args", []string{"foo", "bar", "baz"}, setter.set)

	s := setter.get()
	if s.IsPending {
		t.Fatal("expected IsPending false after completion")
	}
	if s.Error != nil {
		t.Errorf("expected no error, got %v", s.Error)
	}
	if len(receivedArgs) != 3 {
		t.Errorf("expected 3 args, got %d", len(receivedArgs))
	}
	for i, expected := range []string{"foo", "bar", "baz"} {
		if receivedArgs[i] != expected {
			t.Errorf("arg[%d]: expected %q, got %q", i, expected, receivedArgs[i])
		}
	}
}
