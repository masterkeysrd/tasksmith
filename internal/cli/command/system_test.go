package command

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

// modelsTransport answers the daemon endpoints the model picker touches, so the
// picker can be rendered without a live daemon. defaultModel is the workspace
// default, which is the model a draft session runs before anyone chooses one.
type modelsTransport struct {
	models       []api.Model
	defaultModel *api.Model
}

func (tr *modelsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := "{}"
	switch r.URL.Path {
	case "/v1/models":
		data, err := json.Marshal(tr.models)
		if err != nil {
			return nil, err
		}
		body = string(data)
	case "/v1/workspace":
		data, err := json.Marshal(api.Workspace{Name: "test", RootDir: "/workspace", DefaultAgent: "main", DefaultModel: tr.defaultModel})
		if err != nil {
			return nil, err
		}
		body = string(data)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func modelListing() []api.Model {
	return []api.Model{
		{ID: "openai/gpt-5.5", Provider: "openai", Name: "GPT-5.5", Aliases: []string{"fast", "default"}},
		{ID: "openai/embed-model", Provider: "openai", Name: "Embeddings", Capabilities: api.ModelCapabilities{Completion: new(false)}},
		{ID: "local/plain", Name: "Plain"},
	}
}

func TestModelPickerOptionsSkipsNonCompletionModels(t *testing.T) {
	opts := modelPickerOptions(modelListing(), "openai/gpt-5.5")
	if len(opts) != 2 {
		t.Fatalf("options = %+v, want the two chat-capable models", opts)
	}
	if opts[0].Value != "openai/gpt-5.5" || opts[0].Label != "GPT-5.5" {
		t.Fatalf("first option = %+v", opts[0])
	}
	if opts[0].Description != "openai · @fast, @default" {
		t.Fatalf("aliases = %q, want provider and @-prefixed aliases", opts[0].Description)
	}
	if !opts[0].Current {
		t.Fatal("resolved active model should be marked current")
	}
	if opts[1].Value != "local/plain" || opts[1].Description != "" || opts[1].Current {
		t.Fatalf("second option = %+v, want no description and no current mark", opts[1])
	}
}

func TestModelPickerOptionsWithoutCurrentModel(t *testing.T) {
	for _, current := range []string{"", "openai/unknown", "@fast"} {
		for _, opt := range modelPickerOptions(modelListing(), current) {
			if opt.Current {
				t.Fatalf("current model %q must not match a model id, got %+v", current, opt)
			}
		}
	}
}

// modelPickerFixture drives the picker on a synchronous engine, so frames are only
// painted when the test asks for them.
type modelPickerFixture struct {
	inst *trail.Instance
	b    *mock.Backend
}

// mountModelPicker renders ModelPicker against a stub daemon. With no default
// model the session is a draft that has nothing resolved yet.
func mountModelPicker(t *testing.T, models []api.Model, defaultModel *api.Model) *modelPickerFixture {
	t.Helper()
	store.ResetSession()
	store.ClearSlot()
	store.ClearTranscript()
	t.Cleanup(store.ResetSession)
	t.Cleanup(store.ClearSlot)
	t.Cleanup(store.ClearTranscript)

	c, err := client.New("test.sock", client.WithCWD("."),
		client.WithHTTPClient(&http.Client{Transport: &modelsTransport{models: models, defaultModel: defaultModel}}))
	if err != nil {
		t.Fatal(err)
	}
	store.InitSession(t.Context(), c, store.InitSessionOptions{})

	app := trail.SimpleFC("ModelPickerTest", func() trail.Node { return ModelPicker("slot-model") })
	b := mock.New(100, 24)
	inst, err := trail.New(wind.Provider(wind.ProviderProps{Client: wind.NewClient()}, app()), trail.WithBackend(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	return &modelPickerFixture{inst: inst, b: b}
}

// awaitText paints frames until cond accepts the screen, since the model listing
// arrives asynchronously. It never fails, so callers assert with their own message.
func (f *modelPickerFixture) awaitText(cond func(string) bool) string {
	deadline := time.Now().Add(3 * time.Second)
	text := testutils.SurfaceText(f.b)
	for time.Now().Before(deadline) {
		f.inst.Frame()
		text = testutils.SurfaceText(f.b)
		if cond(text) {
			return text
		}
		time.Sleep(time.Millisecond)
	}
	return text
}

// typeText delivers printable characters one at a time and returns the last frame.
func (f *modelPickerFixture) typeText(s string) string {
	for _, r := range s {
		f.inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: r, Text: string(r)}})
	}
	return f.awaitText(func(string) bool { return true })
}

func TestModelPickerRendersAliasesAndCurrentModel(t *testing.T) {
	// A draft session has no chosen model: the workspace default it resolves to is
	// still the active model, so its row must be marked current.
	picker := mountModelPicker(t, modelListing(), &api.Model{ID: "local/plain", Name: "Plain"})

	text := picker.awaitText(func(s string) bool { return strings.Contains(s, "GPT-5.5") })
	if strings.Contains(text, "Embeddings") {
		t.Fatalf("embedding-only model offered:\n%s", text)
	}
	if !strings.Contains(text, "@fast") {
		t.Fatalf("aliases missing from the listing:\n%s", text)
	}
	if !strings.Contains(text, "Plain") || !strings.Contains(text, "current") {
		t.Fatalf("resolved default model not highlighted:\n%s", text)
	}
}

func TestModelPickerMarksAliasSelectionCurrent(t *testing.T) {
	picker := mountModelPicker(t, modelListing(), nil)

	// An active session configured with an alias still runs the resolved model, so
	// the resolved id is what the picker has to highlight.
	store.SetSession(&api.Session{
		Config: api.SessionConfig{Model: "@fast"},
		Model:  &api.Model{ID: "openai/gpt-5.5"},
	})

	text := picker.awaitText(func(s string) bool {
		return strings.Contains(s, "GPT-5.5") && strings.Contains(s, "current")
	})
	if !strings.Contains(text, "current") {
		t.Fatalf("alias-backed model not highlighted:\n%s", text)
	}
}

func TestModelPickerFiltersOnAliases(t *testing.T) {
	picker := mountModelPicker(t, modelListing(), nil)
	picker.awaitText(func(s string) bool { return strings.Contains(s, "GPT-5.5") })

	// The picker's search box only sees what a row displays, so an alias has to be
	// part of the row for `/model` to be searchable by the name users type.
	text := picker.typeText("fast")
	if strings.Contains(text, "Plain") {
		t.Fatalf("filter left a non-matching model:\n%s", text)
	}
	if !strings.Contains(text, "GPT-5.5") {
		t.Fatalf("alias search dropped the aliased model:\n%s", text)
	}
}

func TestModelPickerNoticesWhenNoChatCapableModels(t *testing.T) {
	picker := mountModelPicker(t, []api.Model{
		{ID: "openai/embed-model", Provider: "openai", Name: "Embeddings", Capabilities: api.ModelCapabilities{Completion: new(false)}},
	}, nil)

	picker.awaitText(func(string) bool { return slices.Contains(noticeTexts(), "No chat-capable models available.") })
	if !slices.Contains(noticeTexts(), "No chat-capable models available.") {
		t.Fatalf("picker offered no explanation for an embedding-only listing, notices: %v", noticeTexts())
	}
}

// noticeTexts returns every notice currently in the transcript scrollback.
func noticeTexts() []string {
	var texts []string
	state := store.GetTranscript()
	if state.Items == nil {
		return nil
	}
	for _, item := range *state.Items {
		if item.Kind != store.TranscriptKindNotice {
			continue
		}
		if notice, ok := item.Data.(store.NoticeData); ok {
			texts = append(texts, notice.Text)
		}
	}
	return texts
}

func TestModelCompletionOffersChatCapableModelsByAlias(t *testing.T) {
	store.ResetSession()
	defer store.ResetSession()

	c, err := client.New("test.sock", client.WithCWD("."),
		client.WithHTTPClient(&http.Client{Transport: &modelsTransport{models: modelListing()}}))
	if err != nil {
		t.Fatal(err)
	}
	store.InitSession(t.Context(), c, store.InitSessionOptions{})

	candidates, err := completeModels(t.Context(), autocomplete.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %+v, want the two chat-capable models", candidates)
	}
	if candidates[0].Value != "openai/gpt-5.5" || !slices.Contains(candidates[0].Aliases, "@fast") {
		t.Fatalf("aliases = %+v, want the alias users type", candidates[0].Aliases)
	}
	if candidates[1].Value != "local/plain" || len(candidates[1].Aliases) != 0 {
		t.Fatalf("candidate without aliases = %+v", candidates[1])
	}
}
