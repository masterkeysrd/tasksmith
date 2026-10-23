package session

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func newV7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestEnsureCreatesWithCallerSuppliedID(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	ctx := context.Background()
	id := newV7(t)

	sess, created, err := mgr.Ensure(ctx, CreateParams{ID: id, Title: "Draft"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !created || sess.ID != id || sess.Title != "Draft" || sess.CreatedAt.IsZero() {
		t.Fatalf("Ensure = %+v created=%v", sess, created)
	}
	got, err := mgr.Get(ctx, id)
	if err != nil || got.ID != id {
		t.Fatalf("Get after Ensure = %+v, %v", got, err)
	}
}

func TestEnsureIsIdempotentForMatchingParams(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	ctx := context.Background()
	id := newV7(t)

	first, created, err := mgr.Ensure(ctx, CreateParams{ID: id, Title: "Same"})
	if err != nil || !created {
		t.Fatalf("first Ensure = %+v created=%v err=%v", first, created, err)
	}

	for name, params := range map[string]CreateParams{
		"identical":      {ID: id, Title: "Same"},
		"omitted fields": {ID: id},
	} {
		again, created, err := mgr.Ensure(ctx, params)
		if err != nil {
			t.Fatalf("%s: Ensure: %v", name, err)
		}
		if created || again.ID != id || !again.CreatedAt.Equal(first.CreatedAt) {
			t.Errorf("%s: Ensure = %+v created=%v, want the existing session", name, again, created)
		}
	}

	list, err := mgr.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d sessions, err=%v; want exactly 1", len(list), err)
	}
}

func TestEnsureConflictsOnDifferentParams(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	ctx := context.Background()
	id := newV7(t)
	if _, _, err := mgr.Ensure(ctx, CreateParams{ID: id, Title: "Original"}); err != nil {
		t.Fatal(err)
	}

	for name, params := range map[string]CreateParams{
		"title": {ID: id, Title: "Other"},
		"agent": {ID: id, Agent: "some-other-agent"},
	} {
		if _, _, err := mgr.Ensure(ctx, params); !errors.Is(err, ErrSessionConflict) {
			t.Errorf("%s: err = %v, want ErrSessionConflict", name, err)
		}
	}
	got, err := mgr.Get(ctx, id)
	if err != nil || got.Title != "Original" {
		t.Errorf("conflicting Ensure modified the session: %+v, %v", got, err)
	}
}

func TestEnsureRejectsInvalidIDs(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	ctx := context.Background()
	v4 := uuid.NewString()
	upper := []byte(newV7(t))
	for i, b := range upper {
		if b >= 'a' && b <= 'f' {
			upper[i] = b - 32
		}
	}

	for name, id := range map[string]string{
		"not a uuid":     "not-a-uuid",
		"path traversal": "../../etc/passwd",
		"uuid v4":        v4,
		"uppercase v7":   string(upper),
	} {
		if _, _, err := mgr.Ensure(ctx, CreateParams{ID: id}); !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("%s: err = %v, want ErrInvalidSessionID", name, err)
		}
	}
	if list, err := mgr.List(ctx); err != nil || len(list) != 0 {
		t.Errorf("invalid IDs created sessions: %d, %v", len(list), err)
	}
}

func TestEnsureConcurrentCreatesOneSession(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	ctx := context.Background()
	id := newV7(t)

	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := mgr.Ensure(ctx, CreateParams{ID: id, Title: "Race"})
			if err != nil {
				t.Errorf("Ensure: %v", err)
				return
			}
			results <- created
		}()
	}
	wg.Wait()
	close(results)

	createdCount := 0
	for created := range results {
		if created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Errorf("%d concurrent Ensure calls created the session, want exactly 1", createdCount)
	}
}

func TestCreateWithoutIDGeneratesV7(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	sess, err := mgr.Create(context.Background(), CreateParams{Title: "Generated"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSessionID(sess.ID); err != nil {
		t.Errorf("generated ID %q is not a canonical UUIDv7: %v", sess.ID, err)
	}
}
