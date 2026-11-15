package autocomplete

import (
	"context"
	"errors"
	"testing"
)

func TestMatchDoesNotComplete(t *testing.T) {
	engine := New()
	calls := 0
	complete := func(context.Context, Request) ([]Candidate, error) {
		calls++
		return []Candidate{{Value: "mode"}, {Value: "auto"}, {Value: "file"}}, nil
	}
	for _, source := range []Source{
		{Name: "commands", Trigger: "/", StartOnly: true, Complete: complete},
		{Name: "modes", Trigger: "/mode ", StartOnly: true, Complete: complete},
		{Name: "files", Trigger: "@", Complete: complete},
	} {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{"/", "/mode", "/mode ", "/mode a", "message @file", "message", "/mode auto "} {
		before := calls
		request, err := engine.Match(input, len(input))
		if err != nil {
			t.Fatal(err)
		}
		if calls != before {
			t.Fatal("Match invoked a completer")
		}
		result, err := engine.Complete(t.Context(), input, len(input))
		if err != nil {
			t.Fatal(err)
		}
		if request != result.Request {
			t.Errorf("Match(%q) = %+v, Complete request = %+v", input, request, result.Request)
		}
	}
	if _, err := engine.Match("@", 2); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("invalid cursor error = %v", err)
	}
}
