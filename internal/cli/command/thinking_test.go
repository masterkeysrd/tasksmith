package command

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

type thinkingTransport struct{}

func (thinkingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := `{}`
	if strings.HasPrefix(r.URL.Path, "/v1/models/") {
		body = `{"id":"test","name":"test","thinking":[{"type":"effort","values":[{"value":"minimal","description":"Provider hint"},{"value":"custom"},{"value":"high","description":"More reasoning"}]},{"type":"budget_tokens","min":100,"max":1000},{"type":"toggle"}]}`
		if strings.Contains(r.URL.Path, "unsupported") {
			body = `{"id":"unsupported","name":"unsupported"}`
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestThinkingCommands(t *testing.T) {
	store.ResetSession()
	defer store.ResetSession()
	c, err := client.New("test.sock", client.WithCWD("."), client.WithHTTPClient(&http.Client{Transport: thinkingTransport{}}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	store.InitSession(ctx, c, store.InitSessionOptions{Model: "test"})
	r := NewRegistry()
	store.ClearSlot()
	defer store.ClearSlot()
	if err := r.Execute("/effort"); err != nil {
		t.Fatal(err)
	}
	slot := store.GetSlot().Active
	if slot == nil {
		t.Fatal("expected effort picker")
	}
	picker, ok := slot.Data.(components.PickerProps)
	if !ok || len(picker.Options) != 4 || picker.Options[0].Description != "Provider hint" || picker.Options[1].Value != "custom" || picker.Options[1].Description != "" {
		t.Fatalf("unexpected effort picker: %+v", slot.Data)
	}
	store.ClearSlot()
	candidates, err := completeEffort(ctx, autocomplete.Request{})
	if err != nil || len(candidates) != 4 || candidates[0].Description != "Provider hint" || candidates[1].Value != "custom" || candidates[1].Description != "" {
		t.Fatalf("unexpected effort completion: %+v, %v", candidates, err)
	}
	if err := r.Execute("/effort custom"); err != nil {
		t.Fatalf("unknown model-provided effort rejected: %v", err)
	}
	if err := r.Execute("/effort minimal"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetSession().Config.Thinking; got == nil || got.Effort != "minimal" {
		t.Fatalf("effort: %+v", got)
	}
	if err := r.Execute("/effort low"); err == nil {
		t.Fatal("expected unsupported effort error")
	}
	if err := r.Execute("/budget 1001"); err == nil {
		t.Fatal("expected budget range error")
	}
	if err := r.Execute("/budget 500"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetSession().Config.Thinking; got == nil || got.Budget != 500 {
		t.Fatalf("budget: %+v", got)
	}
	if err := r.Execute("/thinking off"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetSession().Config.Thinking; got == nil || got.Type != "toggle" || got.Enabled {
		t.Fatalf("toggle: %+v", got)
	}
	if err := r.Execute("/thinking default"); err != nil || store.GetSession().Config.Thinking != nil {
		t.Fatalf("reset: %v", err)
	}
	store.SetModel("unsupported")
	for _, name := range []string{"effort", "budget", "thinking"} {
		if _, ok := r.Lookup(name); !ok {
			t.Fatalf("command %s is hidden", name)
		}
		if err := r.Execute("/" + name); err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Fatalf("command %s: %v", name, err)
		}
	}
}
