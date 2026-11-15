package session

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
	coreuuid "github.com/masterkeysrd/tasksmith/internal/core/uuid"
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

	sess, created, err := mgr.Create(ctx, api.CreateSessionRequest{ID: id, Title: "Draft"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created || sess.ID != id || sess.Title != "Draft" || sess.CreatedAt.IsZero() {
		t.Fatalf("Create = %+v created=%v", sess, created)
	}
	got, err := mgr.Get(ctx, id)
	if err != nil || got.ID != id {
		t.Fatalf("Get after Create = %+v, %v", got, err)
	}
}

func TestEnsureIsIdempotentForMatchingParams(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	ctx := context.Background()
	id := newV7(t)

	first, created, err := mgr.Create(ctx, api.CreateSessionRequest{ID: id, Title: "Same"})
	if err != nil || !created {
		t.Fatalf("first Create = %+v created=%v err=%v", first, created, err)
	}

	for name, params := range map[string]api.CreateSessionRequest{
		"identical":      {ID: id, Title: "Same"},
		"omitted fields": {ID: id},
	} {
		again, created, err := mgr.Create(ctx, params)
		if err != nil {
			t.Fatalf("%s: Create: %v", name, err)
		}
		if created || again.ID != id || !again.CreatedAt.Equal(first.CreatedAt) {
			t.Errorf("%s: Create = %+v created=%v, want the existing session", name, again, created)
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
	if _, _, err := mgr.Create(ctx, api.CreateSessionRequest{ID: id, Title: "Original"}); err != nil {
		t.Fatal(err)
	}

	for name, params := range map[string]api.CreateSessionRequest{
		"title": {ID: id, Title: "Other"},
		"agent": {ID: id, Config: api.SessionConfig{Agent: "some-other-agent"}},
	} {
		if _, _, err := mgr.Create(ctx, params); !errors.Is(err, ErrSessionConflict) {
			t.Errorf("%s: err = %v, want ErrSessionConflict", name, err)
		}
	}
	got, err := mgr.Get(ctx, id)
	if err != nil || got.Title != "Original" {
		t.Errorf("conflicting Create modified the session: %+v, %v", got, err)
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
		if _, _, err := mgr.Create(ctx, api.CreateSessionRequest{ID: id}); !errors.Is(err, ErrInvalidSessionID) {
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
		wg.Go(func() {
			_, created, err := mgr.Create(ctx, api.CreateSessionRequest{ID: id, Title: "Race"})
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			results <- created
		})
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
		t.Errorf("%d concurrent Create calls created the session, want exactly 1", createdCount)
	}
}

func TestCreateWithoutIDGeneratesV7(t *testing.T) {
	_, _, _, _, mgr := setupTestEnv(t, nil)
	sess, created, err := mgr.Create(context.Background(), api.CreateSessionRequest{Title: "Generated"})
	if err != nil || !created {
		t.Fatalf("Create: %v, created=%v", err, created)
	}
	if !coreuuid.Validate(sess.ID) {
		t.Errorf("generated ID %q is not a canonical UUIDv7", sess.ID)
	}
}
