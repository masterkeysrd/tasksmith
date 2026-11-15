package ptr

import "testing"

func TestClone(t *testing.T) {
	// Nil pointer returns nil.
	var nilBool *bool
	if got := Clone(nilBool); got != nil {
		t.Fatalf("Clone(nil) = %v, want nil", got)
	}

	// Non-nil pointer returns independent copy.
	orig := 42
	cloned := Clone(&orig)
	if cloned == nil {
		t.Fatal("Clone(&42) returned nil")
	}
	if *cloned != 42 {
		t.Fatalf("Clone(&42) = %d, want 42", *cloned)
	}
	if cloned == &orig {
		t.Fatal("Clone returned same pointer address, want independent copy")
	}

	*cloned = 99
	if orig != 42 {
		t.Fatalf("modifying cloned pointer mutated original: orig = %d", orig)
	}
}
