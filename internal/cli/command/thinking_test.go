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
	return thinkingResponse(thinkingPreviewBody)(r)
}

// thinkingPreviewBody is the model description the stub daemon returns for a
// session preview, which is how the CLI learns what a draft session can do.
const thinkingPreviewBody = `{"model":{"id":"test","name":"test","thinking":[{"type":"effort","values":[{"value":"minimal","description":"Provider hint"},{"value":"custom"},{"value":"high","description":"More reasoning"}]},{"type":"budget_tokens","min":100,"max":1000},{"type":"toggle"}]}}`

func thinkingResponse(body string) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		out := "{}"
		if r.URL.Path == "/v1/sessions/preview" {
			data, _ := io.ReadAll(r.Body)
			out = body
			if strings.Contains(string(data), `"unsupported"`) {
				out = `{"model":{"id":"unsupported","name":"unsupported"}}`
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out))}, nil
	}
}

// thinkingCommandsWithModel starts a draft session whose preview reports the given
// model description, so a test can state exactly which thinking strategies exist.
func thinkingCommandsWithModel(t *testing.T, previewBody string) *Registry {
	t.Helper()
	store.ResetSession()
	store.ClearTranscript()
	t.Cleanup(store.ResetSession)
	t.Cleanup(store.ClearTranscript)
	c, err := client.New("test.sock", client.WithCWD("."),
		client.WithHTTPClient(&http.Client{Transport: roundTripFunc(thinkingResponse(previewBody))}))
	if err != nil {
		t.Fatal(err)
	}
	store.InitSession(t.Context(), c, store.InitSessionOptions{Model: "test"})
	return NewRegistry()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// commandOutputs returns every command result currently in the transcript.
func commandOutputs() []string {
	var outs []string
	state := store.GetTranscript()
	if state.Items == nil {
		return nil
	}
	for _, item := range *state.Items {
		if item.Kind != store.TranscriptKindCommand {
			continue
		}
		if data, ok := item.Data.(store.CommandData); ok {
			outs = append(outs, data.Output)
		}
	}
	return outs
}

// lastCommandOutput is the most recent command result, or "" when there is none.
func lastCommandOutput() string {
	outs := commandOutputs()
	if len(outs) == 0 {
		return ""
	}
	return outs[len(outs)-1]
}

// report runs a command that only reads state and returns what it printed. A
// command refusing to run is a failure of its own, so it fails the test.
func report(t *testing.T, r *Registry, command string) string {
	t.Helper()
	before := len(commandOutputs())
	if err := r.Execute(command); err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	outs := commandOutputs()
	if len(outs) == before {
		t.Fatalf("%s printed nothing", command)
	}
	return outs[len(outs)-1]
}

func TestThinkingAdaptive(t *testing.T) {
	// A model that only adapts: on/off must be refused, adaptive must apply, and
	// the reported state must name the model's own default.
	adaptive := `{"model":{"id":"adaptive-only","name":"adaptive-only","thinking":[{"type":"adaptive"}],"thinking_default":{"type":"adaptive"}}}`
	r := thinkingCommandsWithModel(t, adaptive)

	if err := r.Execute("/thinking on"); err == nil || !strings.Contains(err.Error(), "toggle is not supported") {
		t.Fatalf("toggle on an adaptive-only model: %v", err)
	}
	if err := r.Execute("/thinking adaptive"); err != nil {
		t.Fatalf("adaptive on an adaptive-capable model rejected: %v", err)
	}
	if got := store.GetSession().Config.Thinking; got == nil || got.Type != "adaptive" {
		t.Fatalf("thinking: %+v", got)
	}
	if got := lastCommandOutput(); got != "Thinking set to adaptive" {
		t.Fatalf("confirmation = %q", got)
	}

	if err := r.Execute("/thinking default"); err != nil || store.GetSession().Config.Thinking != nil {
		t.Fatalf("reset: %v", err)
	}
	if got := report(t, r, "/thinking"); got != "Thinking: default (adaptive)" {
		t.Fatalf("reported state = %q, want the model's adaptive default called out as a default", got)
	}

	// Adaptive stays hidden from suggestions only when the model lacks it, so
	// check both listings.
	candidates, err := completeThinking(t.Context(), autocomplete.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Value != "adaptive" || candidates[1].Value != "default" {
		t.Fatalf("adaptive-only suggestions = %+v", candidates)
	}
}

func TestThinkingToggleStillOffersOnlyToggle(t *testing.T) {
	r := thinkingCommandsWithModel(t, thinkingPreviewBody)

	if err := r.Execute("/thinking adaptive"); err == nil || !strings.Contains(err.Error(), "adaptive thinking is not supported") {
		t.Fatalf("adaptive on a toggle-only model: %v", err)
	}
	candidates, err := completeThinking(t.Context(), autocomplete.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || candidates[0].Value != "on" || candidates[1].Value != "off" || candidates[2].Value != "default" {
		t.Fatalf("toggle model suggestions = %+v", candidates)
	}
}

func TestThinkingReportsModelDefault(t *testing.T) {
	// The daemon resolves the model default into thinking_default; a session with
	// no override should report it rather than claiming the provider decides.
	r := thinkingCommandsWithModel(t, `{"model":{"id":"with-default","name":"with-default","thinking":[{"type":"toggle"},{"type":"budget_tokens","min":100,"max":1000}],"thinking_default":{"type":"toggle","enabled":true}}}`)

	if got := report(t, r, "/thinking"); got != "Thinking: default (on)" {
		t.Fatalf("reported state = %q", got)
	}
	if got := report(t, r, "/budget"); got != "Thinking budget: provider default" {
		t.Fatalf("budget state = %q, want the provider default when the model default is a toggle", got)
	}
}

func TestBudgetReportsDefaultRange(t *testing.T) {
	r := thinkingCommandsWithModel(t, `{"model":{"id":"budget-default","name":"budget-default","thinking":[{"type":"budget_tokens","min":100,"max":1000,"default":"500"}],"thinking_default":{"type":"budget","budget":500}}}`)

	if got := report(t, r, "/budget"); got != "Thinking budget: default (500 tokens) — range 100–1000" {
		t.Fatalf("budget state = %q", got)
	}
	if err := r.Execute("/budget 500"); err != nil {
		t.Fatalf("in-range budget rejected: %v", err)
	}
	if got := report(t, r, "/budget"); got != "Thinking budget: 500" {
		t.Fatalf("budget after setting = %q", got)
	}
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

func TestThinkingOffersEveryStrategyTheModelDeclares(t *testing.T) {
	// A model declaring both must accept either spelling and suggest all of them.
	r := thinkingCommandsWithModel(t, `{"model":{"id":"both","name":"both","thinking":[{"type":"toggle"},{"type":"adaptive"}]}}`)

	if err := r.Execute("/thinking on"); err != nil {
		t.Fatalf("toggle on a model supporting both: %v", err)
	}
	if err := r.Execute("/thinking adaptive"); err != nil {
		t.Fatalf("adaptive on a model supporting both: %v", err)
	}
	if got := store.GetSession().Config.Thinking; got == nil || got.Type != "adaptive" {
		t.Fatalf("thinking: %+v", got)
	}

	candidates, err := completeThinking(t.Context(), autocomplete.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 4 || candidates[2].Value != "adaptive" {
		t.Fatalf("suggestions for a model supporting both = %+v", candidates)
	}
}

func TestThinkingFallsBackToStrategyDefault(t *testing.T) {
	// An older daemon may report strategies without resolving a thinking_default.
	// The strategy's own default still has to be reported rather than pretending
	// the provider decides.
	r := thinkingCommandsWithModel(t, `{"model":{"id":"toggle-default","name":"toggle-default","thinking":[{"type":"toggle","default":"true"}]}}`)

	if got := report(t, r, "/thinking"); got != "Thinking: on" {
		t.Fatalf("reported state = %q, want the toggle's own default", got)
	}
}
