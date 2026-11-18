package uuid

import (
	"sort"
	"testing"
	"time"

	gouuid "github.com/google/uuid"
)

func TestMonotonicOrdersBySequence(t *testing.T) {
	base := time.Now().UTC()
	const count = 5000 // spans the 4096-per-millisecond rollover
	ids := make([]string, count)
	for i := range ids {
		ids[i] = Monotonic(base, i)
	}

	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for i := range ids {
		if ids[i] != sorted[i] {
			t.Fatalf("id %d = %q, want %q (sequence order must match lexicographic order)", i, ids[i], sorted[i])
		}
	}
}

func TestMonotonicShape(t *testing.T) {
	id, err := gouuid.Parse(Monotonic(time.Now().UTC(), 0))
	if err != nil {
		t.Fatalf("Monotonic did not produce a valid UUID: %v", err)
	}
	if id.Version() != 7 {
		t.Errorf("version = %d, want 7", id.Version())
	}
	if id.Variant() != gouuid.RFC4122 {
		t.Errorf("variant = %v, want RFC4122", id.Variant())
	}
}
