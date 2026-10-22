package components

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func completionEngine(t *testing.T, complete autocomplete.Completer) *autocomplete.Engine {
	t.Helper()
	engine := autocomplete.New()
	if err := engine.Register(autocomplete.Source{Name: "files", Trigger: "@", Complete: complete}); err != nil {
		t.Fatal(err)
	}
	return engine
}

func mountAutocomplete(t *testing.T, initial AutocompleteOptions) (*trail.Instance, func() AutocompleteState, func(AutocompleteOptions)) {
	t.Helper()
	var latest AutocompleteState
	var update func(AutocompleteOptions)
	component := trail.SimpleFC("CompletionTest", func() trail.Node {
		opts, setOpts := kitex.UseState(initial)
		update = setOpts
		latest = UseAutocomplete(opts())
		return Autocomplete(AutocompleteProps{State: latest})
	})
	cache := wind.NewClient()
	cache.GcTime = time.Millisecond
	inst, err := trail.New(
		wind.Provider(wind.ProviderProps{Client: cache}, component()),
		trail.WithBackend(mock.New(80, 24)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	return inst, func() AutocompleteState { return latest }, func(opts AutocompleteOptions) { update(opts) }
}

func awaitCompletion(t *testing.T, inst *trail.Instance, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inst.Frame()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for autocomplete")
}

func TestUseAutocompleteSelectionAndDismissal(t *testing.T) {
	engine := completionEngine(t, autocomplete.Static(
		autocomplete.Candidate{Value: "alpha"},
		autocomplete.Candidate{Value: "alpine"},
	))
	opts := AutocompleteOptions{Engine: engine, Value: "@a", Cursor: 2}
	inst, latest, update := mountAutocomplete(t, opts)
	awaitCompletion(t, inst, func() bool { return latest().IsOpen && !latest().IsLoading })
	if !latest().Down() {
		t.Fatal("Down did not consume navigation")
	}
	inst.Frame()
	if latest().Selected != 1 {
		t.Fatalf("selection = %d, want 1", latest().Selected)
	}
	edit, err := latest().Accept()
	if err != nil || edit.Value != "@alpine" || edit.Cursor != len(edit.Value) {
		t.Fatalf("Accept = %+v, %v", edit, err)
	}
	opts.Value, opts.Cursor = edit.Value, edit.Cursor
	update(opts)
	inst.Frame()
	if latest().IsOpen {
		t.Fatal("accepted suggestions reopened")
	}

	opts.Value, opts.Cursor = "@al", 3
	update(opts)
	awaitCompletion(t, inst, func() bool { return latest().IsOpen && !latest().IsLoading })
	if latest().Selected != 0 {
		t.Errorf("new context retained selection %d", latest().Selected)
	}
	if !latest().Dismiss() {
		t.Fatal("Dismiss did not consume Escape")
	}
	inst.Frame()
	if latest().IsOpen || latest().Dismiss() {
		t.Fatal("dismissed suggestions reopened or consumed Escape again")
	}
	opts.Value, opts.Cursor = "text", 4
	update(opts)
	inst.Frame()
	opts.Value, opts.Cursor = "@al", 3
	update(opts)
	awaitCompletion(t, inst, func() bool { return latest().IsOpen && !latest().IsLoading })
	opts.Value, opts.Cursor = "@z", 2
	update(opts)
	awaitCompletion(t, inst, func() bool { return !latest().IsLoading })
	if latest().IsOpen || latest().Dismiss() {
		t.Fatal("no-match results consumed Escape")
	}
}

func TestUseAutocompleteCancelsOutdatedWork(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	engine := completionEngine(t, func(ctx context.Context, req autocomplete.Request) ([]autocomplete.Candidate, error) {
		if req.Query == "a" {
			started <- ctx
			<-release
			return []autocomplete.Candidate{{Value: "alpha"}}, nil
		}
		return []autocomplete.Candidate{{Value: "beta"}}, nil
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	opts := AutocompleteOptions{Engine: engine, Value: "@a", Cursor: 2}
	inst, latest, update := mountAutocomplete(t, opts)
	var oldContext context.Context
	awaitCompletion(t, inst, func() bool {
		select {
		case oldContext = <-started:
			return true
		default:
			return false
		}
	})
	if !latest().IsLoading || !latest().IsOpen {
		t.Fatalf("pending state = %+v", latest())
	}
	opts.Value = "@b"
	update(opts)
	awaitCompletion(t, inst, func() bool {
		return !latest().IsLoading && len(latest().Result.Candidates) == 1 &&
			latest().Result.Candidates[0].Value == "beta"
	})
	select {
	case <-oldContext.Done():
	default:
		t.Fatal("changing the draft did not cancel the previous request")
	}
	close(release)
	inst.Frame()
	if latest().Result.Candidates[0].Value != "beta" {
		t.Fatal("outdated work replaced the active result")
	}

	replacement := completionEngine(t, autocomplete.Static(autocomplete.Candidate{Value: "bravo"}))
	opts.Engine = replacement
	update(opts)
	inst.Frame()
	if candidates := latest().Result.Candidates; len(candidates) > 0 && candidates[0].Value != "bravo" {
		t.Fatalf("new engine snapshot exposed old candidates: %+v", candidates)
	}
	awaitCompletion(t, inst, func() bool {
		return !latest().IsLoading && len(latest().Result.Candidates) == 1 &&
			latest().Result.Candidates[0].Value == "bravo"
	})
}

func TestUseAutocompleteErrorsAndInactiveInput(t *testing.T) {
	wantErr := errors.New("source unavailable")
	var calls atomic.Int32
	engine := completionEngine(t, func(context.Context, autocomplete.Request) ([]autocomplete.Candidate, error) {
		calls.Add(1)
		return nil, wantErr
	})
	opts := AutocompleteOptions{Engine: engine, Value: "ordinary text", Cursor: len("ordinary text")}
	inst, latest, update := mountAutocomplete(t, opts)
	if latest().IsOpen || calls.Load() != 0 {
		t.Fatal("ordinary text activated completion")
	}
	opts.Value, opts.Cursor, opts.Disabled = "@", 1, true
	update(opts)
	inst.Frame()
	if latest().IsOpen || calls.Load() != 0 {
		t.Fatal("disabled input activated completion")
	}
	opts.Disabled = false
	update(opts)
	awaitCompletion(t, inst, func() bool { return latest().Error != nil })
	if !errors.Is(latest().Error, wantErr) || !latest().IsOpen {
		t.Fatalf("error state = %+v", latest())
	}
	if latest().Up() || latest().Down() {
		t.Fatal("error state consumed list navigation")
	}
	opts.Engine = completionEngine(t, autocomplete.Static(autocomplete.Candidate{Value: "available"}))
	update(opts)
	inst.Frame()
	if latest().Error != nil {
		t.Fatal("old error leaked into a new engine snapshot")
	}
	awaitCompletion(t, inst, func() bool { return len(latest().Result.Candidates) == 1 && !latest().IsLoading })
	opts.Value, opts.Cursor = "text", 4
	update(opts)
	inst.Frame()
	if latest().Error != nil || latest().IsOpen {
		t.Fatal("old error leaked into inactive input")
	}
}

func TestUseAutocompleteUnmountCancellation(t *testing.T) {
	started := make(chan context.Context, 1)
	engine := completionEngine(t, func(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
		started <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	})
	inst, _, _ := mountAutocomplete(t, AutocompleteOptions{Engine: engine, Value: "@", Cursor: 1})
	var requestContext context.Context
	awaitCompletion(t, inst, func() bool {
		select {
		case requestContext = <-started:
			return true
		default:
			return false
		}
	})
	inst.Unmount()
	select {
	case <-requestContext.Done():
	case <-time.After(time.Second):
		t.Fatal("unmount did not cancel completion")
	}
}

func TestAutocompletePresentation(t *testing.T) {
	type fileData struct{ Prefix, Kind, Path string }
	for _, test := range []struct {
		name  string
		state AutocompleteState
		want  string
	}{
		{name: "closed", want: ""},
		{name: "loading", state: AutocompleteState{IsOpen: true, IsLoading: true}, want: "Completing..."},
		{name: "error", state: AutocompleteState{IsOpen: true, Error: errors.New("source unavailable")}, want: "source unavailable"},
		{
			name: "default", want: "fallback-label",
			state: AutocompleteState{IsOpen: true, Result: autocomplete.Result{
				Candidates: []autocomplete.Candidate{{Value: "fallback-label"}},
			}},
		},
		{
			name: "columns", want: "source.go",
			state: AutocompleteState{IsOpen: true, Result: autocomplete.Result{
				Request: autocomplete.Request{Source: "files"},
				Candidates: []autocomplete.Candidate{{
					Value: "source.go", Data: fileData{Prefix: "src", Kind: "Go", Path: "source.go"},
				}},
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := mock.New(80, 24)
			component := trail.SimpleFC("PresentationTest", func() trail.Node {
				return Autocomplete(AutocompleteProps{
					State: test.state,
					Views: map[string]AutocompleteView{"files": func(result autocomplete.Result, selected int) trail.Node {
						data := result.Candidates[0].Data.(fileData)
						return OptionList(OptionListProps{
							Options: []Option{{Cells: []string{data.Prefix, data.Kind, data.Path}}},
							Columns: []Column{{Width: 8}, {Width: 8}, {}}, Selected: selected,
						})
					}},
				})
			})
			inst, err := trail.New(component(), trail.WithBackend(backend))
			if err != nil {
				t.Fatal(err)
			}
			defer inst.Unmount()
			inst.Frame()
			var output strings.Builder
			frame := backend.LastFrame().Surface
			if frame != nil {
				for y := 0; y < frame.Height; y++ {
					for x := 0; x < frame.Width; x++ {
						output.WriteString(frame.CellAt(x, y).Content)
					}
				}
			}
			if !strings.Contains(output.String(), test.want) {
				t.Errorf("output %q does not contain %q", output.String(), test.want)
			}
			if test.name == "columns" && (!strings.Contains(output.String(), "src") || !strings.Contains(output.String(), "Go")) {
				t.Errorf("missing file columns in %q", output.String())
			}
		})
	}
}
