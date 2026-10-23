package daemon

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestCreateSessionWithCallerSuppliedID(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)
	id := uuid.Must(uuid.NewV7()).String()

	created, err := cli.CreateSession(ctx, api.CreateSessionRequest{ID: id, Title: "Draft"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.ID != id || created.Title != "Draft" {
		t.Fatalf("created = %+v", created)
	}

	// Creating it again is idempotent, including when optional fields are omitted.
	for _, req := range []api.CreateSessionRequest{{ID: id, Title: "Draft"}, {ID: id}} {
		again, err := cli.CreateSession(ctx, req)
		if err != nil {
			t.Fatalf("repeat CreateSession(%+v): %v", req, err)
		}
		if again.ID != id || again.Title != "Draft" || !again.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("repeat = %+v, want the existing session", again)
		}
	}
	list, err := cli.ListSessions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSessions = %d, %v; want 1", len(list), err)
	}
}

func TestCreateSessionIDErrors(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := cli.CreateSession(ctx, api.CreateSessionRequest{ID: id, Title: "Original"}); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		req  api.CreateSessionRequest
		code int
	}{
		"conflicting title": {api.CreateSessionRequest{ID: id, Title: "Different"}, http.StatusConflict},
		"invalid id":        {api.CreateSessionRequest{ID: "not-a-uuid"}, http.StatusBadRequest},
		"uuid v4":           {api.CreateSessionRequest{ID: uuid.NewString()}, http.StatusBadRequest},
	} {
		_, err := cli.CreateSession(ctx, tc.req)
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.code {
			t.Errorf("%s: err = %v, want API error with status %d", name, err, tc.code)
		}
	}
}
