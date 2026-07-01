package keymap

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

func waitForTimeoutTarget(t *testing.T, targets <-chan any) any {
	t.Helper()

	select {
	case target := <-targets:
		return target
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for timeout callback")
		return nil
	}
}

func assertNoTimeoutTarget(t *testing.T, targets <-chan any, wait time.Duration) {
	t.Helper()

	select {
	case target := <-targets:
		t.Fatalf("unexpected timeout callback: %v", target)
	case <-time.After(wait):
	}
}

// testKeymap returns a new Keymap with common test bindings registered.
func testKeymap(t *testing.T) *Keymap {
	t.Helper()
	km := New()

	// Normal mode bindings
	km.Set([]mode.Mode{mode.Normal}, "?", func(context.Context) {}, Description("Show help"))
	km.Set([]mode.Mode{mode.Normal}, "gg", func(context.Context) {}, Description("Scroll to top"))
	km.Set([]mode.Mode{mode.Normal}, "G", func(context.Context) {}, Description("Scroll to bottom"))
	km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) {}, Description("Navigate down"))
	km.Set([]mode.Mode{mode.Normal}, "k", func(context.Context) {}, Description("Navigate up"))
	km.Set([]mode.Mode{mode.Normal}, "<C-d>", func(context.Context) {}, Description("Scroll down"))
	km.Set([]mode.Mode{mode.Normal}, "<C-u>", func(context.Context) {}, Description("Scroll up"))
	km.Set([]mode.Mode{mode.Normal}, "<C-b>", func(context.Context) {}, Description("Toggle sidebar"))
	km.Set([]mode.Mode{mode.Normal}, "<Enter>", func(context.Context) {}, Description("Select"))
	km.Set([]mode.Mode{mode.Normal}, "<Esc>", func(context.Context) {}, Description("Close"))
	km.Set([]mode.Mode{mode.Normal}, ":", func(context.Context) {}, Description("Enter command mode"))
	km.Set([]mode.Mode{mode.Normal}, "i", func(context.Context) {}, Description("Enter insert mode"))
	km.Set([]mode.Mode{mode.Normal}, "a", func(context.Context) {}, Description("Enter insert mode"))

	// Insert mode bindings
	km.Set([]mode.Mode{mode.Insert}, "<Esc>", func(context.Context) {}, Description("Exit insert mode"))
	km.Set([]mode.Mode{mode.Insert}, "<C-c>", func(context.Context) {}, Description("Exit insert mode"))
	km.Set([]mode.Mode{mode.Insert}, "<C-Enter>", func(context.Context) {}, Description("Submit composer"))

	// Command mode bindings
	km.Set([]mode.Mode{mode.Command}, "<Esc>", func(context.Context) {}, Description("Exit command mode"))
	km.Set([]mode.Mode{mode.Command}, "<Enter>", func(context.Context) {}, Description("Select"))

	return km
}

// TestNormalModeImmediateResolution verifies that Input(Normal, "j")
// returns a func(context.Context) target that can be type-asserted and executed.
func TestNormalModeImmediateResolution(t *testing.T) {
	km := testKeymap(t)
	target, ok := km.Input(mode.Normal, "j")
	if !ok {
		t.Fatal("expected binding to resolve, got no match")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
	fn(context.Background()) // should not panic
}

// TestNormalModeSequenceResolution verifies that Input(Normal, "g") returns
// (nil, false), and a subsequent Input(Normal, "g") returns
// a func(context.Context) target.
func TestNormalModeSequenceResolution(t *testing.T) {
	km := testKeymap(t)

	// First key: buffered as prefix.
	target, ok := km.Input(mode.Normal, "g")
	if ok {
		t.Fatal("expected no immediate resolution for prefix key")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	// Second key: completes the sequence.
	target, ok = km.Input(mode.Normal, "g")
	if !ok {
		t.Fatal("expected binding to resolve for second g")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
	fn(t.Context()) // should not panic
}

// TestSequenceTimeoutFlush verifies that Input(Normal, "g") followed by no
// input for >500ms invokes the onTimeout callback with nil.
func TestSequenceTimeoutFlush(t *testing.T) {
	km := testKeymap(t)
	km.SetTimeout(100 * time.Millisecond) // faster for testing
	targets := make(chan any, 1)
	km.SetOnTimeout(func(target any) {
		targets <- target
	})

	// Buffer a key that has no standalone binding.
	km.Input(mode.Normal, "g")

	if target := waitForTimeoutTarget(t, targets); target != nil {
		t.Errorf("callback target = %v, want nil", target)
	}
}

// TestSequenceTimeoutSingleKeyBinding verifies that exact+prefix bindings buffer
// and timeout to their standalone resolution. A test-only <Esc>q binding makes
// <Esc> both an exact match and a sequence prefix.
func TestSequenceTimeoutSingleKeyBinding(t *testing.T) {
	km := testKeymap(t)
	km.SetTimeout(100 * time.Millisecond)
	km.Set([]mode.Mode{mode.Normal}, "<Esc>q", func(_ context.Context) {})
	targets := make(chan any, 1)
	km.SetOnTimeout(func(target any) {
		targets <- target
	})

	target, ok := km.Input(mode.Normal, "<Esc>")
	if ok {
		t.Fatal("expected <Esc> to buffer while it is also a prefix")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	if target := waitForTimeoutTarget(t, targets); target == nil {
		t.Error("callback target should be a func(context.Context) for <Esc>")
	}
}

// TestModeSpecificBindings verifies that Input(Insert, "j") returns
// (nil, false) while Input(Normal, "j") returns a func(context.Context) target.
func TestModeSpecificBindings(t *testing.T) {
	km := testKeymap(t)

	// Insert mode: no binding for "j".
	target, ok := km.Input(mode.Insert, "j")
	if ok {
		t.Fatal("expected no binding for j in Insert mode")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	// Normal mode: "j" binds to a func(context.Context).
	target, ok = km.Input(mode.Normal, "j")
	if !ok {
		t.Fatal("expected binding for j in Normal mode")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
	fn(t.Context()) // should not panic
}

// TestInsertModeEsc verifies that Input(Insert, "<Esc>") returns
// a func(context.Context) target.
func TestInsertModeEsc(t *testing.T) {
	km := testKeymap(t)
	target, ok := km.Input(mode.Insert, "<Esc>")
	if !ok {
		t.Fatal("expected <Esc> to resolve in Insert mode")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
	fn(t.Context()) // should not panic
}

// TestCommandModeEnter verifies that Input(Command, "<Enter>") returns
// a func(context.Context) target.
func TestCommandModeEnter(t *testing.T) {
	km := testKeymap(t)
	target, ok := km.Input(mode.Command, "<Enter>")
	if !ok {
		t.Fatal("expected <Enter> to resolve in Command mode")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
	fn(t.Context()) // should not panic
}

// TestCommandModeUnboundKey verifies that Input(Command, "x") returns
// (nil, false).
func TestCommandModeUnboundKey(t *testing.T) {
	km := New()
	target, ok := km.Input(mode.Command, "x")
	if ok {
		t.Fatal("expected no binding for unbound key in Command mode")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}
}

// TestSetUnset verifies that dynamic binding addition and removal works,
// and existing bindings are unaffected.
func TestSetUnset(t *testing.T) {
	km := testKeymap(t)

	// Verify existing binding.
	target, ok := km.Input(mode.Normal, "j")
	if !ok {
		t.Fatal("existing binding should work")
	}
	if _, ok := target.(func(context.Context)); !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}

	// Unbind "j".
	km.Unbind(mode.Normal, "j")

	if _, ok := km.Resolve(mode.Normal, "j"); ok {
		t.Fatal("expected j to be unbound")
	}

	// Set a new func(context.Context) binding to "j".
	called := false
	km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) {
		called = true
	})

	// Verify new binding.
	target, ok = km.Resolve(mode.Normal, "j")
	if !ok {
		t.Fatal("new binding should work")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
	fn(t.Context())
	if !called {
		t.Error("func(context.Context) target was not executed")
	}

	// Verify other bindings still work.
	target, ok = km.Resolve(mode.Normal, "gg")
	if !ok {
		t.Fatal("other bindings should be unaffected")
	}
	if _, ok := target.(func(context.Context)); !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}
}

// TestSetTimeout verifies that the timeout is honored and the default is 500ms.
func TestSetTimeout(t *testing.T) {
	km := New()

	// Default timeout.
	if got := km.Timeout(); got != 500*time.Millisecond {
		t.Errorf("default timeout = %v, want %v", got, 500*time.Millisecond)
	}

	// Set a custom timeout.
	km.SetTimeout(1 * time.Second)
	if got := km.Timeout(); got != 1*time.Second {
		t.Errorf("timeout = %v, want %v", got, 1*time.Second)
	}
}

// TestModeChangeClearsBuffer verifies that Input(Normal, "g") followed by
// Input(Insert, "g") does not resolve "gg" because the buffer is cleared
// on mode change.
func TestModeChangeClearsBuffer(t *testing.T) {
	km := New()

	// Buffer "g" in Normal mode.
	km.Input(mode.Normal, "g")

	// Switch to Insert mode and press "g".
	// Since the mode changed, the buffer should be flushed and "g" in Insert
	// mode has no binding, so it should return (nil, false).
	target, ok := km.Input(mode.Insert, "g")
	if ok {
		t.Fatal("expected no binding for g in Insert mode after mode change")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}
}

// TestConcurrentInput verifies that 100 goroutines can call Input concurrently
// without data races.
func TestConcurrentInput(t *testing.T) {
	km := New()
	modes := []mode.Mode{mode.Normal, mode.Insert, mode.Command}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m := modes[n%len(modes)]
			keys := []string{"j", "k", "g", "i", "<Esc>", "<Enter>", "x", "?"}
			for _, key := range keys {
				km.Input(m, key)
			}
		}(i)
	}
	wg.Wait()
}

// TestNew_EmptyKeymap verifies that New() returns a non-nil Keymap
// with an empty Modes map.
func TestNew_EmptyKeymap(t *testing.T) {
	km := New()
	if km == nil {
		t.Fatal("New() returned nil")
	}
	if km.Modes == nil {
		t.Fatal("New() returned Keymap with nil Modes")
	}
	if len(km.Modes) != 0 {
		t.Errorf("Modes has %d entries, want 0", len(km.Modes))
	}
	if km.Timeout() != 500*time.Millisecond {
		t.Errorf("default timeout = %v, want %v", km.Timeout(), 500*time.Millisecond)
	}
}

// TestResolve_NoBinding verifies that Resolve() returns (nil, false)
// for unbound keys.
func TestResolve_NoBinding(t *testing.T) {
	km := New()
	target, ok := km.Resolve(mode.Normal, "zzz")
	if ok {
		t.Fatal("expected no binding for unbound key")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}
}

// TestResolveSequence_NoBinding verifies that ResolveSequence() returns
// (nil, false) for unbound sequences.
func TestResolveSequence_NoBinding(t *testing.T) {
	km := New()
	target, ok := km.ResolveSequence(mode.Normal, []string{"z", "z", "z"})
	if ok {
		t.Fatal("expected no binding for unbound sequence")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}
}

// TestIsPrefix_True verifies that isPrefix() returns true for a prefix of a binding.
func TestIsPrefix_True(t *testing.T) {
	km := testKeymap(t)
	if !km.isPrefix(mode.Normal, []string{"g"}) {
		t.Fatal("expected 'g' to be a prefix of 'gg'")
	}
}

// TestIsPrefix_False verifies that isPrefix() returns false for a non-prefix.
func TestIsPrefix_False(t *testing.T) {
	km := New()
	if km.isPrefix(mode.Normal, []string{"z"}) {
		t.Fatal("expected 'z' to not be a prefix of any binding")
	}
}

// TestTimeout_Getter verifies that Timeout() returns the current timeout value.
func TestTimeout_Getter(t *testing.T) {
	km := New()
	if got := km.Timeout(); got != 500*time.Millisecond {
		t.Errorf("timeout = %v, want %v", got, 500*time.Millisecond)
	}
	km.SetTimeout(2 * time.Second)
	if got := km.Timeout(); got != 2*time.Second {
		t.Errorf("timeout = %v, want %v", got, 2*time.Second)
	}
}

// TestSetOnTimeout_Callback verifies that SetOnTimeout() registers and invokes
// the callback.
func TestSetOnTimeout_Callback(t *testing.T) {
	km := testKeymap(t)
	km.SetTimeout(100 * time.Millisecond)
	km.Set([]mode.Mode{mode.Normal}, "<Esc>q", func(_ context.Context) {})
	targets := make(chan any, 1)

	km.SetOnTimeout(func(target any) {
		targets <- target
	})

	target, ok := km.Input(mode.Normal, "<Esc>")
	if ok {
		t.Fatal("expected <Esc> to buffer while it is also a prefix")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	if got := waitForTimeoutTarget(t, targets); got == nil {
		t.Error("callback target should be a func(context.Context) for <Esc>")
	}
}

// TestFlushOnNoMatch verifies that a non-matching sequence flushes each key
// through Resolve.
func TestFlushOnNoMatch(t *testing.T) {
	km := New()

	// "zx" is not a binding and "z" is not a prefix of any binding.
	target, ok := km.Input(mode.Normal, "z")
	if ok {
		t.Fatal("expected no binding for 'z'")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	// "z" followed by "x" — "zx" is not a binding, and "z" alone is not a prefix.
	target, ok = km.Input(mode.Normal, "x")
	if ok {
		t.Fatal("expected no binding for 'x' after 'z' flush")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}
}

// TestNoMatchFlushPublishesBufferedTarget verifies that a buffered exact+prefix
// target is published when the next key breaks the longer sequence.
func TestNoMatchFlushPublishesBufferedTarget(t *testing.T) {
	km := testKeymap(t)
	km.Set([]mode.Mode{mode.Normal}, "<Esc>q", func(_ context.Context) {})
	targets := make(chan any, 1)
	km.SetOnTimeout(func(target any) {
		targets <- target
	})

	target, ok := km.Input(mode.Normal, "<Esc>")
	if ok {
		t.Fatal("expected <Esc> to buffer while it is also a prefix")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	target, ok = km.Input(mode.Normal, "x")
	if ok {
		t.Fatal("expected x to remain unbound after flushing <Esc>")
	}
	if target != nil {
		t.Errorf("target = %v, want nil", target)
	}

	if got := waitForTimeoutTarget(t, targets); got == nil {
		t.Error("callback target should be a func(context.Context) for <Esc>")
	}
}

// TestResolvedSequenceCancelsTimeout verifies that a fully resolved sequence
// does not leak a stale timeout callback after the buffer is cleared.
func TestResolvedSequenceCancelsTimeout(t *testing.T) {
	km := testKeymap(t)
	km.SetTimeout(100 * time.Millisecond)
	targets := make(chan any, 1)
	km.SetOnTimeout(func(target any) {
		targets <- target
	})

	target, ok := km.Input(mode.Normal, "g")
	if ok || target != nil {
		t.Fatalf("first g = (%v, %v), want (nil, false)", target, ok)
	}

	target, ok = km.Input(mode.Normal, "g")
	if !ok {
		t.Fatalf("second g = (%v, %v), want (func(context.Context), true)", target, ok)
	}
	if _, ok := target.(func(context.Context)); !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}

	assertNoTimeoutTarget(t, targets, 150*time.Millisecond)
}

// TestZeroValueSetSafe verifies that a zero-value Keymap can be mutated safely.
func TestZeroValueSetSafe(t *testing.T) {
	var km Keymap

	km.Set([]mode.Mode{mode.Normal}, "j", func(_ context.Context) {})

	target, ok := km.Resolve(mode.Normal, "j")
	if !ok {
		t.Fatal("expected zero-value keymap binding to resolve")
	}
	if _, ok := target.(func(context.Context)); !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}

	if got := km.Timeout(); got != defaultTimeout {
		t.Errorf("timeout = %v, want %v", got, defaultTimeout)
	}
}

// TestStringTarget verifies that a string macro alias can be mapped,
// resolved, type-asserted to string, and verified for equality.
func TestStringTarget(t *testing.T) {
	km := New()

	// Map a string macro.
	km.Set([]mode.Mode{mode.Normal}, "q", "quit_macro")

	// Resolve and type-assert to string.
	target, ok := km.Resolve(mode.Normal, "q")
	if !ok {
		t.Fatal("expected binding to resolve")
	}
	str, ok := target.(string)
	if !ok {
		t.Fatalf("target = %T, want string", target)
	}
	if str != "quit_macro" {
		t.Errorf("string = %q, want %q", str, "quit_macro")
	}
}

// TestFuncTarget verifies that a func(context.Context) can be mapped, resolved,
// type-asserted to func(context.Context), executed, and its side effects verified.
func TestFuncTarget(t *testing.T) {
	km := New()

	called := 0
	km.Set([]mode.Mode{mode.Normal}, "Q", func(_ context.Context) {
		called++
	})

	target, ok := km.Resolve(mode.Normal, "Q")
	if !ok {
		t.Fatal("expected binding to resolve")
	}
	fn, ok := target.(func(context.Context))
	if !ok {
		t.Fatalf("target = %T, want func(context.Context)", target)
	}

	// Execute the function.
	fn(t.Context())
	if called != 1 {
		t.Errorf("called = %d, want 1", called)
	}

	fn(t.Context())
	fn(t.Context())
	if called != 3 {
		t.Errorf("called = %d, want 3", called)
	}
}

// TestDescriptionOption verifies that the Description option is processed
// and stored correctly.
func TestDescriptionOption(t *testing.T) {
	km := New()

	// This test accesses internal state directly.
	km.Set([]mode.Mode{mode.Normal}, "d", func(_ context.Context) {}, Description("debug command"))

	bindings, ok := km.Modes[mode.Normal]["d"]
	if !ok || len(bindings) == 0 {
		t.Fatal("expected binding to be set")
	}
	if bindings[0].Description() != "debug command" {
		t.Errorf("description = %q, want %q", bindings[0].Description(), "debug command")
	}

	// Also test with string target.
	km.Set([]mode.Mode{mode.Normal}, "m", "my_macro", Description("my macro"))

	bindings2, ok := km.Modes[mode.Normal]["m"]
	if !ok || len(bindings2) == 0 {
		t.Fatal("expected string binding to be set")
	}
	if bindings2[0].Description() != "my macro" {
		t.Errorf("description = %q, want %q", bindings2[0].Description(), "my macro")
	}
}

// TestSet_InvalidType verifies that Set() panics when given an invalid target type.
func TestSet_InvalidType(t *testing.T) {
	km := New()

	defer func(context.Context) {
		if r := recover(); r == nil {
			t.Error("expected Set() to panic for invalid target type")
		}
	}(t.Context())

	km.Set([]mode.Mode{mode.Normal}, "x", 42) // int is not a valid target type
}

// TestSet_MultipleModes verifies that Set() applies a binding to multiple modes.
func TestSet_MultipleModes(t *testing.T) {
	km := New()

	km.Set([]mode.Mode{mode.Normal, mode.Insert, mode.Command}, "x", func(_ context.Context) {})

	for _, m := range []mode.Mode{mode.Normal, mode.Insert, mode.Command} {
		target, ok := km.Resolve(m, "x")
		if !ok {
			t.Errorf("expected binding in mode %d", m)
		}
		if _, ok := target.(func(context.Context)); !ok {
			t.Errorf("mode %d: target = %T, want func(context.Context)", m, target)
		}
	}
}

// TestAllNormalModeBindings verifies every registered Normal mode binding resolves
// to a func(context.Context) target.
func TestAllNormalModeBindings(t *testing.T) {
	km := testKeymap(t)
	keys := []string{"?", "gg", "G", "j", "k", "<C-d>", "<C-u>", "<C-b>",
		"<Enter>", "<Esc>", ":", "i", "a"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			// For sequence keys, buffer the first key then resolve with the second.
			if key == "gg" {
				km.Input(mode.Normal, "g")
				target, ok := km.Input(mode.Normal, "g")
				if !ok {
					t.Errorf("gg: expected binding, got none")
					return
				}
				if _, ok := target.(func(context.Context)); !ok {
					t.Errorf("gg: target = %T, want func(context.Context)", target)
				}
				return
			}
			target, ok := km.Resolve(mode.Normal, key)
			if !ok {
				t.Errorf("key %q: expected binding, got none", key)
				return
			}
			if _, ok := target.(func(context.Context)); !ok {
				t.Errorf("key %q: target = %T, want func(context.Context)", key, target)
			}
		})
	}
}

// TestAllInsertModeBindings verifies every registered Insert mode binding resolves
// to a func(context.Context) target.
func TestAllInsertModeBindings(t *testing.T) {
	km := testKeymap(t)
	keys := []string{"<Esc>", "<C-c>", "<C-Enter>"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			target, ok := km.Resolve(mode.Insert, key)
			if !ok {
				t.Errorf("key %q: expected binding, got none", key)
				return
			}
			if _, ok := target.(func(context.Context)); !ok {
				t.Errorf("key %q: target = %T, want func(context.Context)", key, target)
			}
		})
	}
}

// TestAllCommandModeBindings verifies every registered Command mode binding resolves
// to a func(context.Context) target.
func TestAllCommandModeBindings(t *testing.T) {
	km := testKeymap(t)
	keys := []string{"<Esc>", "<Enter>"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			target, ok := km.Resolve(mode.Command, key)
			if !ok {
				t.Errorf("key %q: expected binding, got none", key)
				return
			}
			if _, ok := target.(func(context.Context)); !ok {
				t.Errorf("key %q: target = %T, want func(context.Context)", key, target)
			}
		})
	}
}

// TestUnboundKeyReturnsNil verifies that unbound keys return (nil, false).
func TestUnboundKeyReturnsNil(t *testing.T) {
	km := New()
	unboundKeys := []string{"z", "x", "l", "y", "h"}
	for _, key := range unboundKeys {
		t.Run(key, func(t *testing.T) {
			target, ok := km.Resolve(mode.Normal, key)
			if ok {
				t.Errorf("key %q: expected no binding", key)
			}
			if target != nil {
				t.Errorf("key %q: target = %v, want nil", key, target)
			}
		})
	}
}

// BenchmarkResolve benchmarks single-key resolution.
func BenchmarkResolve(b *testing.B) {
	km := New()
	b.ReportAllocs()
	for b.Loop() {
		km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) {})
		km.Resolve(mode.Normal, "j")
	}
}

// BenchmarkResolveSequence benchmarks two-key sequence resolution.
func BenchmarkResolveSequence(b *testing.B) {
	km := New()
	b.ReportAllocs()
	km.Set([]mode.Mode{mode.Normal}, "gg", func(context.Context) {})
	for b.Loop() {
		km.ResolveSequence(mode.Normal, []string{"g", "g"})
	}
}

// BenchmarkInput benchmarks end-to-end Input() performance.
func BenchmarkInput(b *testing.B) {
	km := New()
	km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) {})
	km.Set([]mode.Mode{mode.Normal}, "gg", func(context.Context) {})
	km.Set([]mode.Mode{mode.Normal}, "i", func(context.Context) {})
	km.Set([]mode.Mode{mode.Normal}, "<Esc>", func(context.Context) {})
	km.Set([]mode.Mode{mode.Normal}, ":", func(context.Context) {})
	km.Set([]mode.Mode{mode.Normal}, "<Enter>", func(context.Context) {})
	km.Set([]mode.Mode{mode.Insert}, "<Esc>", func(context.Context) {})
	b.ReportAllocs()
	keys := []string{"j", "k", "g", "i", "<Esc>", ":", "<Enter>"}
	b.Run("normal_mode", func(b *testing.B) {
		for range b.N {
			for _, key := range keys {
				km.Input(mode.Normal, key)
			}
		}
	})
	b.Run("insert_mode", func(b *testing.B) {
		for range b.N {
			for _, key := range keys {
				km.Input(mode.Insert, key)
			}
		}
	})
	b.Run("command_mode", func(b *testing.B) {
		for range b.N {
			for _, key := range keys {
				km.Input(mode.Command, key)
			}
		}
	})
}

// TestFocusContextOption verifies that key resolution, ExecuteTarget, and Input processing
// respect the Context option, resolving based on the active focus context.
func TestFocusContextOption(t *testing.T) {
	origContext := testFocusContext
	defer func() {
		testFocusContext = origContext
	}()

	testFocusContext = ""

	km := New()

	var explorerCalled, chatCalled, fallbackCalled bool

	// Register multiple actions for the same key 'j' under different contexts
	km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) { explorerCalled = true }, Context("explorer"))
	km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) { chatCalled = true }, Context("chat"))
	km.Set([]mode.Mode{mode.Normal}, "j", func(context.Context) { fallbackCalled = true }) // general screen-level fallback

	keJ := &event.KeyEvent{}
	keJ.Text = "j"

	// 1. Focus Explorer
	testFocusContext = "explorer"
	explorerCalled = false
	chatCalled = false
	fallbackCalled = false

	ok, err := km.ExecuteTarget(context.Background(), mode.Normal, keJ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || !explorerCalled || chatCalled || fallbackCalled {
		t.Errorf("expected only explorer action to run, ok=%v, explorer=%v, chat=%v, fallback=%v", ok, explorerCalled, chatCalled, fallbackCalled)
	}

	// 2. Focus Chat
	testFocusContext = "chat"
	explorerCalled = false
	chatCalled = false
	fallbackCalled = false

	ok, err = km.ExecuteTarget(context.Background(), mode.Normal, keJ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || explorerCalled || !chatCalled || fallbackCalled {
		t.Errorf("expected only chat action to run, ok=%v, explorer=%v, chat=%v, fallback=%v", ok, explorerCalled, chatCalled, fallbackCalled)
	}

	// 3. Focus Unknown (should trigger fallback)
	testFocusContext = "unknown"
	explorerCalled = false
	chatCalled = false
	fallbackCalled = false

	ok, err = km.ExecuteTarget(context.Background(), mode.Normal, keJ)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || explorerCalled || chatCalled || !fallbackCalled {
		t.Errorf("expected only fallback action to run, ok=%v, explorer=%v, chat=%v, fallback=%v", ok, explorerCalled, chatCalled, fallbackCalled)
	}
}

// TestModalBarrier verifies that non-modal keybindings are blocked when a modal is active,
// but specific modal or wildcard 'modal' keybindings can execute.
func TestModalBarrier(t *testing.T) {
	origContext := testFocusContext
	defer func() {
		testFocusContext = origContext
	}()

	testFocusContext = "chat"

	km := New()

	var normalCalled, authCalled, wildcardCalled bool

	km.Set([]mode.Mode{mode.Normal}, "x", func(context.Context) { normalCalled = true })
	km.Set([]mode.Mode{mode.Normal}, "x", func(context.Context) { authCalled = true }, Context("modal:auth"))
	km.Set([]mode.Mode{mode.Normal}, "y", func(context.Context) { wildcardCalled = true }, Context("modal"))

	keX := &event.KeyEvent{}
	keX.Text = "x"
	keY := &event.KeyEvent{}
	keY.Text = "y"

	// 1. With NO modal active
	testFocusContext = "chat"
	normalCalled = false
	authCalled = false
	wildcardCalled = false

	// 'x' should resolve to normalCalled
	ok, err := km.ExecuteTarget(context.Background(), mode.Normal, keX)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || !normalCalled || authCalled {
		t.Errorf("expected only normal mapping, ok=%v, normal=%v, auth=%v", ok, normalCalled, authCalled)
	}

	// 'y' should NOT resolve since it expects a modal to be open
	ok, _ = km.ExecuteTarget(context.Background(), mode.Normal, keY)
	if ok {
		t.Error("expected 'y' to be blocked with no active modal")
	}

	// 2. With 'auth' modal active
	testFocusContext = "modal:auth"
	normalCalled = false
	authCalled = false
	wildcardCalled = false

	// 'x' should resolve to authCalled (normal mapping is blocked by barrier)
	ok, err = km.ExecuteTarget(context.Background(), mode.Normal, keX)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || normalCalled || !authCalled {
		t.Errorf("expected only auth mapping, ok=%v, normal=%v, auth=%v", ok, normalCalled, authCalled)
	}

	// 'y' should resolve because wildcard 'modal' matches any active modal
	ok, err = km.ExecuteTarget(context.Background(), mode.Normal, keY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || !wildcardCalled {
		t.Errorf("expected wildcard modal mapping to execute, ok=%v, wildcard=%v", ok, wildcardCalled)
	}
}
