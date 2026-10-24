package daemon

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestSendWithClientSuppliedID(t *testing.T) {
	ctx := context.Background()

	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: "asst-chunk-1"},
					Model:     req.Model,
					Content:   []message.Block{&message.TextBlock{Text: "hello back"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Custom ID Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	events := make(chan api.Event, 16)
	subReady := make(chan struct{})

	go func() {
		for ev, err := range cli.EventsWithReady(subCtx, "", "", func(string) { close(subReady) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				return
			}
			events <- ev
			if ev.Type == api.EventCompleted {
				return
			}
		}
	}()

	select {
	case <-subReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscription ready")
	}

	customMsgID := uuid.Must(uuid.NewV7()).String()

	err = cli.Send(ctx, sess.ID, api.SendRequest{
		ID:      customMsgID,
		Content: "Hello with custom ID",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Verify EventEntryAdded contains the exact client-supplied UUIDv7 ID
	var confirmedID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && confirmedID == "" {
		select {
		case ev := <-events:
			if ev.Type == api.EventEntryAdded {
				if p, ok := ev.Payload.(api.EntryAddedEvent); ok && p.Entry.Kind == api.TimelineKindMessage {
					if p.Entry.Message != nil && p.Entry.Message.Role == api.RoleUser {
						confirmedID = p.Entry.ID
						if p.Entry.Message.ID != customMsgID {
							t.Errorf("p.Entry.Message.ID = %q, want %q", p.Entry.Message.ID, customMsgID)
						}
					}
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	if confirmedID != customMsgID {
		t.Fatalf("EventEntryAdded ID = %q, want %q", confirmedID, customMsgID)
	}

	// Verify persisted timeline also has the exact ID
	timeline, err := cli.Timeline(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	var foundUser bool
	for _, entry := range timeline {
		if entry.Kind == api.TimelineKindMessage && entry.Message != nil && entry.Message.Role == api.RoleUser {
			foundUser = true
			if entry.ID != customMsgID {
				t.Errorf("persisted timeline entry.ID = %q, want %q", entry.ID, customMsgID)
			}
			if entry.Message.ID != customMsgID {
				t.Errorf("persisted timeline entry.Message.ID = %q, want %q", entry.Message.ID, customMsgID)
			}
		}
	}
	if !foundUser {
		t.Fatal("expected user message entry in timeline")
	}
}

func TestSendWithInvalidID(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Validation Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	tests := map[string]struct {
		req  api.SendRequest
		code int
	}{
		"not-a-uuid": {
			req:  api.SendRequest{ID: "not-a-uuid", Content: "hello"},
			code: http.StatusBadRequest,
		},
		"uuid-v4": {
			req:  api.SendRequest{ID: uuid.NewString(), Content: "hello"},
			code: http.StatusBadRequest,
		},
		"decision-and-id": {
			req: api.SendRequest{
				ID:       uuid.Must(uuid.NewV7()).String(),
				Decision: &api.PermissionDecision{Kind: api.DecisionAllowOnce},
			},
			code: http.StatusBadRequest,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := cli.Send(ctx, sess.ID, tc.req)
			var apiErr *api.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.code {
				t.Fatalf("Send error = %v, want HTTP status %d", err, tc.code)
			}
		})
	}
}
