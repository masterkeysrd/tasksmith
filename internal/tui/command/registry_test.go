package command

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// resetRegistry resets the global registry to a clean state for testing.
func resetRegistry() {
	globalRegistry = newRegistry()
}

// TestRegister_Execute_Success verifies that a registered command receives
// the correct arguments and executes without error.
func TestRegister_Execute_Success(t *testing.T) {
	resetRegistry()

	var received CommandContext
	var fn = func(ctx CommandContext) error {
		received = ctx
		return nil
	}

	Register("chat.send", fn)

	err := Execute(context.Background(), "chat.send", "hello")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if len(received.Args) != 1 || received.Args[0] != "hello" {
		t.Errorf("expected args [hello], got %v", received.Args)
	}
}

// TestRegister_Execute_MultipleArgs verifies that multiple arguments are
// passed through correctly.
func TestRegister_Execute_MultipleArgs(t *testing.T) {
	resetRegistry()

	var received CommandContext
	var fn = func(ctx CommandContext) error {
		received = ctx
		return nil
	}

	Register("test.multi", fn)

	args := []string{"arg1", "arg2", "arg3"}
	err := Execute(context.Background(), "test.multi", args...)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if len(received.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(received.Args))
	}
	for i, expected := range args {
		if received.Args[i] != expected {
			t.Errorf("arg[%d]: expected %q, got %q", i, expected, received.Args[i])
		}
	}
}

// TestRegister_Execute_NoArgs verifies that executing with no arguments works.
func TestRegister_Execute_NoArgs(t *testing.T) {
	resetRegistry()

	var received CommandContext
	var fn = func(ctx CommandContext) error {
		received = ctx
		return nil
	}

	Register("test.noargs", fn)

	err := Execute(context.Background(), "test.noargs")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if received.Args == nil {
		t.Error("expected Args to be non-nil (empty slice), got nil")
	}
	if len(received.Args) != 0 {
		t.Errorf("expected 0 args, got %d", len(received.Args))
	}
}

// TestExecute_NotFound verifies that executing an unregistered command
// returns an error matching "command not found: <id>".
func TestExecute_NotFound(t *testing.T) {
	resetRegistry()

	err := Execute(context.Background(), "xyz")
	if err == nil {
		t.Fatal("expected error for unregistered command, got nil")
	}

	expected := "command not found: xyz"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

// TestRegister_Duplicate_Panics verifies that registering the same command
// ID twice causes a panic.
func TestRegister_Duplicate_Panics(t *testing.T) {
	resetRegistry()

	Register("dup.test", func(ctx CommandContext) error { return nil })

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate registration, got nil")
		}
	}()

	Register("dup.test", func(ctx CommandContext) error { return nil })
}

// TestRegister_NilFunc_Panics verifies that registering a nil command function
// causes a panic.
func TestRegister_NilFunc_Panics(t *testing.T) {
	resetRegistry()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil function, got nil")
		}
	}()

	Register("nil.test", nil)
}

// TestExecute_Concurrent verifies that executing commands from multiple
// goroutines simultaneously does not cause panics or race conditions.
func TestExecute_Concurrent(t *testing.T) {
	resetRegistry()

	const goroutines = 10
	const callsPerGoroutine = 100

	var callCount atomic.Int64
	var errCount atomic.Int64

	var fn = func(ctx CommandContext) error {
		callCount.Add(1)
		return nil
	}

	Register("concurrent.test", fn)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			for range callsPerGoroutine {
				if err := Execute(context.Background(), "concurrent.test", "hello"); err != nil {
					errCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	totalCalls := callCount.Load()
	expectedCalls := int64(goroutines * callsPerGoroutine)
	if totalCalls != expectedCalls {
		t.Errorf("expected %d total calls, got %d", expectedCalls, totalCalls)
	}

	if errs := errCount.Load(); errs != 0 {
		t.Errorf("expected 0 errors, got %d", errs)
	}
}

// TestExecute_Concurrent_Mixed verifies that concurrent execution with
// mixed success/failure commands is safe.
func TestExecute_Concurrent_Mixed(t *testing.T) {
	resetRegistry()

	const goroutines = 10
	const callsPerGoroutine = 50

	var successCount atomic.Int64
	var failCount atomic.Int64

	successFn := func(ctx CommandContext) error {
		successCount.Add(1)
		return nil
	}

	failFn := func(ctx CommandContext) error {
		failCount.Add(1)
		return fmt.Errorf("simulated error")
	}

	Register("ok.cmd", successFn)
	Register("fail.cmd", failFn)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			for range callsPerGoroutine {
				if successCount.Load()%2 == 0 {
					Execute(context.Background(), "ok.cmd")
				} else {
					Execute(context.Background(), "fail.cmd")
				}
			}
		}()
	}

	wg.Wait()

	totalCalls := successCount.Load() + failCount.Load()
	expectedCalls := int64(goroutines * callsPerGoroutine)
	if totalCalls != expectedCalls {
		t.Errorf("expected %d total calls, got %d", expectedCalls, totalCalls)
	}
}

// TestRegister_UniqueIDs verifies that different command IDs can be
// registered and executed independently.
func TestRegister_UniqueIDs(t *testing.T) {
	resetRegistry()

	var cmd1Called, cmd2Called bool

	Register("cmd.one", func(ctx CommandContext) error {
		cmd1Called = true
		return nil
	})

	Register("cmd.two", func(ctx CommandContext) error {
		cmd2Called = true
		return nil
	})

	Execute(context.Background(), "cmd.one")
	Execute(context.Background(), "cmd.two")

	if !cmd1Called {
		t.Error("cmd.one was not executed")
	}
	if !cmd2Called {
		t.Error("cmd.two was not executed")
	}
}
