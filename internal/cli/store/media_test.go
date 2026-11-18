package store

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSetModelTriggersCapabilityMismatchSlotAndForceRetries(t *testing.T) {
	ResetSession()
	ClearSlot()
	ClearTranscript()
	defer ResetSession()
	defer ClearSlot()
	defer ClearTranscript()

	var calls []api.UpdateSessionRequest
	c, err := client.New("/tmp/nonexistent.sock",
		client.WithCWD("/workspace"),
		client.WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPatch && r.URL.Path == "/v1/sessions/s1" {
					var req api.UpdateSessionRequest
					_ = json.NewDecoder(r.Body).Decode(&req)
					calls = append(calls, req)
					if !req.Force {
						errBody, _ := json.Marshal(api.ErrorResponse{
							Code:              api.ErrCodeCapabilityMismatch,
							Message:           `model "local/text" does not support image, pdf used in this session`,
							Model:             "local/text",
							MissingModalities: []string{"image", "pdf"},
						})
						return &http.Response{
							StatusCode: http.StatusConflict,
							Header:     http.Header{"Content-Type": []string{"application/json"}},
							Body:       io.NopCloser(bytes.NewReader(errBody)),
						}, nil
					}
					sessBody, _ := json.Marshal(api.Session{
						ID:     "s1",
						Config: api.SessionConfig{Model: "local/text"},
					})
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(bytes.NewReader(sessBody)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader("{}")),
				}, nil
			}),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	sessMu.Lock()
	activeClient = c
	activeCtx = context.Background()
	sessMu.Unlock()
	SetSession(&api.Session{ID: "s1", Config: api.SessionConfig{Model: "anthropic/claude"}})

	SetModel("local/text")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if active := GetSlot().Active; active != nil && active.Kind == SlotKindModelSwitch {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	active := GetSlot().Active
	if active == nil || active.Kind != SlotKindModelSwitch {
		t.Fatalf("expected SlotKindModelSwitch in slot, got %+v", active)
	}
	req, ok := active.Data.(ModelSwitchRequest)
	if !ok || req.Model != "local/text" || len(req.MissingModalities) != 2 {
		t.Fatalf("unexpected ModelSwitchRequest: %+v", active.Data)
	}

	ClearSlot()
	ForceSetModel("local/text")

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetSession().Config.Model == "local/text" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if GetSession().Config.Model != "local/text" {
		t.Fatalf("expected forced model update to local/text, got %q", GetSession().Config.Model)
	}
	if len(calls) != 2 || calls[0].Force || !calls[1].Force {
		t.Fatalf("expected [Force:false, Force:true], got %+v", calls)
	}
}

func TestConversationProjectsMediaBlocks(t *testing.T) {
	acc := newConversationAccumulator()
	acc.setSessionID("s1")
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "s1",
			Entry: api.TimelineEntry{
				ID:   "e1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:   "m1",
					Role: api.RoleUser,
					Content: []api.Block{
						{Type: api.BlockText, Text: "see attached"},
						{Type: api.BlockImage, Media: &api.Media{SHA256: strings.Repeat("a", 64), MIMEType: "image/png", ByteSize: 128, DisplayName: "ui.png"}},
					},
				},
			},
		},
	})
	history, streamMsgs := acc.projectLocked()
	all := append(history, streamMsgs...)
	if len(all) != 1 || len(all[0].Parts) != 2 {
		t.Fatalf("expected 1 message with 2 parts, got %+v", all)
	}
	if all[0].Parts[1].Type != PartMedia || all[0].Parts[1].Media == nil || all[0].Parts[1].Media.Media.DisplayName != "ui.png" {
		t.Fatalf("unexpected PartMedia: %+v", all[0].Parts[1])
	}
}
