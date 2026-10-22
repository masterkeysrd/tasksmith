package command

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func TestCommandSources(t *testing.T) {
	reg := NewRegistry()
	engine := commandCompletionEngine(t, reg.Sources())

	for _, test := range []struct {
		input  string
		source string
		values []string
	}{
		{input: "/mode", source: "commands", values: []string{"mode"}},
		{input: "/h", source: "commands", values: []string{"help"}},
		{input: "/?", source: "commands", values: []string{"help"}},
		{input: "/ls", source: "commands", values: []string{"sessions"}},
		{
			input:  "/mode ",
			source: "commands:mode",
			values: []string{"default", "accept_edits", "plan", "ask", "auto"},
		},
		{input: "/mode a", source: "commands:mode", values: []string{"accept_edits", "ask", "auto"}},
		{input: "/MoDe\tAS", source: "commands:mode", values: []string{"ask"}},
		{input: "/mode unknown", source: "commands:mode"},
		{input: "/mode auto "},
		{input: "/help "},
		{input: "message /mode "},
		{input: "message /mode"},
	} {
		t.Run(test.input, func(t *testing.T) {
			result, err := engine.Complete(t.Context(), test.input, len(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if result.Source != test.source {
				t.Errorf("source = %q, want %q", result.Source, test.source)
			}
			if got := completionValues(result); !slices.Equal(got, test.values) {
				t.Errorf("values = %v, want %v", got, test.values)
			}
		})
	}

	result, err := engine.Complete(t.Context(), "/", 1)
	if err != nil {
		t.Fatal(err)
	}
	commands := reg.List()
	if len(result.Candidates) != len(commands) {
		t.Fatalf("got %d candidates, want %d", len(result.Candidates), len(commands))
	}
	for i, candidate := range result.Candidates {
		cmd := commands[i]
		if candidate.Value != cmd.Name || candidate.Label != cmd.Usage || candidate.Description != cmd.Description {
			t.Errorf("candidate %d = %+v, want metadata for /%s", i, candidate, cmd.Name)
		}
		data, ok := candidate.Data.(Command)
		if !ok || data.Name != cmd.Name || data.Usage != cmd.Usage {
			t.Errorf("candidate %d data = %#v, want Command for /%s", i, candidate.Data, cmd.Name)
		}
	}

	result, err = engine.Complete(t.Context(), "/mode ", len("/mode "))
	if err != nil {
		t.Fatal(err)
	}
	for i, option := range modeOptionsWithActive("") {
		candidate := result.Candidates[i]
		if candidate.Label != option.Label || candidate.Description != option.Description {
			t.Errorf("mode candidate %d = %+v, want metadata for %s", i, candidate, option.Value)
		}
		if mode, ok := candidate.Data.(api.PermissionMode); !ok || string(mode) != option.Value {
			t.Errorf("mode candidate %d data = %#v, want PermissionMode(%q)", i, candidate.Data, option.Value)
		}
	}
}

func TestCommandArgumentCompleter(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "request context")
	reg := NewRegistry()
	calls := 0
	reg.Register(Command{
		Name:    "mcp",
		Aliases: []string{" M ", "/MCP", "m"},
		Handler: func(Env, []string) error {
			t.Fatal("completion must not execute commands")
			return nil
		},
		Complete: func(gotCtx context.Context, req autocomplete.Request) ([]autocomplete.Candidate, error) {
			calls++
			if gotCtx.Value(contextKey{}) != "request context" {
				t.Error("completer did not receive the caller context")
			}
			if req.Query != "r" {
				t.Errorf("query = %q, want r", req.Query)
			}
			return []autocomplete.Candidate{
				{Value: "list"},
				{Value: "restart", Description: "Restart an MCP server", Data: "restart metadata"},
			}, nil
		},
	})
	engine := commandCompletionEngine(t, reg.Sources())

	for _, input := range []string{"/mcp", "/mc", "/help "} {
		if _, err := engine.Complete(ctx, input, len(input)); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatalf("argument completer called %d times before an argument trigger", calls)
	}

	for _, input := range []string{"/mcp r", "/m r", "/MCP r"} {
		result, err := engine.Complete(ctx, input, len(input))
		if err != nil {
			t.Fatal(err)
		}
		if got := completionValues(result); !slices.Equal(got, []string{"restart"}) {
			t.Fatalf("Complete(%q) = %v, want restart", input, got)
		}
		if result.Candidates[0].Data != "restart metadata" {
			t.Errorf("Complete(%q) lost argument metadata", input)
		}
		completed, _, err := result.Apply(0)
		if err != nil {
			t.Fatal(err)
		}
		if want := input[:len(input)-1] + "restart"; completed != want {
			t.Errorf("Apply(%q) = %q, want %q", input, completed, want)
		}
	}
	if calls != 3 {
		t.Errorf("argument completer called %d times, want 3", calls)
	}
}

func TestCommandSourcesSnapshotAndReplacement(t *testing.T) {
	reg := NewRegistry()
	original := commandCompletionEngine(t, reg.Sources())
	reg.Register(Command{Name: "mode", Description: "Replacement without argument completion"})
	reg.Register(Command{Name: "custom"})
	updated := commandCompletionEngine(t, reg.Sources())

	for _, test := range []struct {
		engine *autocomplete.Engine
		input  string
		source string
		values []string
	}{
		{engine: original, input: "/mode a", source: "commands:mode", values: []string{"accept_edits", "ask", "auto"}},
		{engine: original, input: "/custom", source: "commands"},
		{engine: updated, input: "/mode a"},
		{engine: updated, input: "/custom", source: "commands", values: []string{"custom"}},
	} {
		result, err := test.engine.Complete(t.Context(), test.input, len(test.input))
		if err != nil {
			t.Fatal(err)
		}
		if result.Source != test.source || !slices.Equal(completionValues(result), test.values) {
			t.Errorf("Complete(%q) = (%q, %v), want (%q, %v)",
				test.input, result.Source, completionValues(result), test.source, test.values)
		}
	}
}

func TestCommandCompletionApply(t *testing.T) {
	engine := commandCompletionEngine(t, NewRegistry().Sources())
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "/mo", want: "/mode "},
		{input: "/h", want: "/help"},
		{input: "/ls", want: "/sessions"},
		{input: "/mode as", want: "/mode ask"},
	} {
		result, err := engine.Complete(t.Context(), test.input, len(test.input))
		if err != nil {
			t.Fatal(err)
		}
		got, cursor, err := result.Apply(0)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want || cursor != len(test.want) {
			t.Errorf("Apply(%q) = (%q, %d), want (%q, %d)", test.input, got, cursor, test.want, len(test.want))
		}
	}
}

func TestCommandCompleterErrors(t *testing.T) {
	wantErr := errors.New("completion failed")
	reg := NewRegistry()
	reg.Register(Command{
		Name: "broken",
		Complete: func(context.Context, autocomplete.Request) ([]autocomplete.Candidate, error) {
			return nil, wantErr
		},
	})
	engine := commandCompletionEngine(t, reg.Sources())
	if _, err := engine.Complete(t.Context(), "/broken ", len("/broken ")); !errors.Is(err, wantErr) {
		t.Errorf("completion error = %v, want %v", err, wantErr)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, source := range NewRegistry().Sources() {
		if _, err := source.Complete(ctx, autocomplete.Request{}); !errors.Is(err, context.Canceled) {
			t.Errorf("source %q cancellation error = %v", source.Name, err)
		}
	}
}

func commandCompletionEngine(t *testing.T, sources []autocomplete.Source) *autocomplete.Engine {
	t.Helper()
	engine := autocomplete.New()
	for _, source := range sources {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}
	return engine
}

func completionValues(result autocomplete.Result) []string {
	values := make([]string, len(result.Candidates))
	for i, candidate := range result.Candidates {
		values[i] = candidate.Value
	}
	return values
}
