package autocomplete

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func testEngine(t testing.TB) *Engine {
	t.Helper()
	engine := New()
	for _, source := range []Source{
		{
			Name: "commands", Trigger: "/", StartOnly: true,
			Complete: Static(
				Candidate{Value: "mcp", Aliases: []string{"mc"}},
				Candidate{Value: "mode", Label: "Permission mode"},
				Candidate{Value: "new", Label: "New session"},
			),
		},
		{
			Name: "modes", Trigger: "/mode ", StartOnly: true,
			Complete: Static(
				Candidate{Value: "auto"}, Candidate{Value: "ask"},
				Candidate{Value: "accept_edits"}, Candidate{Value: "default"},
			),
		},
		{
			Name: "mcp", Trigger: "/mcp ", StartOnly: true,
			Complete: Static(
				Candidate{Value: "list"},
				Candidate{Value: "restart", Aliases: []string{"reboot"}},
			),
		},
		{
			Name: "mcp-targets", Trigger: "/mcp restart ", StartOnly: true,
			Complete: Static(Candidate{Value: "all"}, Candidate{Value: "server-a"}),
		},
		{
			Name: "mcp-alias-targets", Trigger: "/mc reboot ", StartOnly: true,
			Complete: Static(Candidate{Value: "all"}, Candidate{Value: "server-a"}),
		},
		{
			Name: "files", Trigger: "@", CaseSensitive: true,
			Complete: Static(
				Candidate{Value: "src/main.go"}, Candidate{Value: "src/model.go"},
				Candidate{Value: "src/file name.go"}, Candidate{Value: "\u6587\u6863/\u8bf4\u660e.md"},
			),
		},
	} {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}
	return engine
}

func candidateValues(items []Candidate) []string {
	values := make([]string, len(items))
	for i, item := range items {
		values[i] = item.Value
	}
	return values
}

func TestComplete(t *testing.T) {
	modes := []string{"auto", "ask", "accept_edits", "default"}
	tests := []struct {
		input  string
		source string
		query  string
		want   []string
	}{
		{input: "/",
			source: "commands", want: []string{"mcp", "mode", "new"}},
		{input: "/mo",
			source: "commands", query: "mo", want: []string{"mode"}},
		{input: "/mode",
			source: "commands", query: "mode", want: []string{"mode"}},
		{input: "/mode ",
			source: "modes", want: modes},
		{input: "/mode a",
			source: "modes", query: "a", want: modes[:3]},
		{input: "/MODE A",
			source: "modes", query: "A", want: modes[:3]},
		{input: "/mode unknown",
			source: "modes", query: "unknown"},
		{input: "/mcp ",
			source: "mcp", want: []string{"list", "restart"}},
		{input: "/mcp r",
			source: "mcp", query: "r", want: []string{"restart"}},
		{input: "/mcp restart",
			source: "mcp", query: "restart", want: []string{"restart"}},
		{input: "/mcp restart ",
			source: "mcp-targets", want: []string{"all", "server-a"}},
		{input: "/mcp restart s",
			source: "mcp-targets", query: "s", want: []string{"server-a"}},
		{input: "/mc reboot ",
			source: "mcp-alias-targets", want: []string{"all", "server-a"}},
		{input: "\t /mode\t \u2003",
			source: "modes", want: modes},
		{input: "/mcp 'restart' ",
			source: "mcp-targets", want: []string{"all", "server-a"}},
		{input: "/mcp re\\start ",
			source: "mcp-targets", want: []string{"all", "server-a"}},
		{input: "inspect @src/",
			source: "files", query: "src/", want: []string{"src/main.go", "src/model.go", "src/file name.go"}},
		{input: "/mcp @src/ma",
			source: "files", query: "src/ma", want: []string{"src/main.go"}},
		{input: "@SRC/",
			source: "files", query: "SRC/"},
		{input: "@\u6587",
			source: "files", query: "\u6587", want: []string{"\u6587\u6863/\u8bf4\u660e.md"}},
		{input: `@"src/file n`,
			source: "files", query: "src/file n", want: []string{"src/file name.go"}},
		{input: `@src/file\ n`,
			source: "files", query: "src/file n", want: []string{"src/file name.go"}},
		{input: `@src/ma\`,
			source: "files", query: "src/ma", want: []string{"src/main.go"}},
		{input: "/Per",
			source: "commands", query: "Per", want: []string{"mode"}},
		{input: ""},
		{input: "ordinary input"},
		{input: "inspect /mode "},
		{input: "someone@example.com"},
		{input: `\@src/`},
		{input: `"@src/"`},
		{input: "/unknown "},
		{input: "/mcp unknown "},
		{input: "/mode auto "},
		{input: "inspect @src/main.go ordinary"},
	}
	engine := testEngine(t)
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := engine.Complete(t.Context(), test.input, len(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if result.Source != test.source || result.Query != test.query ||
				!slices.Equal(candidateValues(result.Candidates), test.want) {
				t.Errorf("Complete = source=%q query=%q candidates=%v; want source=%q query=%q candidates=%v",
					result.Source, result.Query, candidateValues(result.Candidates),
					test.source, test.query, test.want)
			}
		})
	}
}

func TestCursorAndReplacement(t *testing.T) {
	tests := []struct {
		input  string
		cursor int
		value  string
		want   string
		pos    int
	}{
		{"/modex later", len("/mo"), "mode", "/mode later", len("/mode")},
		{"/mode ax later", len("/mode a"), "auto", "/mode auto later", len("/mode auto")},
		{"/mode ask", len("/mode "), "auto", "/mode auto", len("/mode auto")},
		{"/mode  ask", len("/mode "), "auto", "/mode auto ask", len("/mode auto")},
		{"/mode   ", len("/mode   "), "ask", "/mode   ask", len("/mode   ask")},
		{`@"src/file n" later`, len(`@"src/file n`), "src/file name.go", `@"src/file name.go" later`, len(`@"src/file name.go"`)},
		{`@"src/file n`, len(`@"src/file n`), "src/file name.go", `@"src/file name.go"`, len(`@"src/file name.go"`)},
		{`@src/file\ n`, len(`@src/file\ n`), "src/file name.go", `@"src/file name.go"`, len(`@"src/file name.go"`)},
		{"/mcp 're' later", len("/mcp 're"), "restart", "/mcp restart later", len("/mcp restart")},
		{"@\u6587", len("@\u6587"), "\u6587\u6863/\u8bf4\u660e.md", "@\u6587\u6863/\u8bf4\u660e.md", len("@\u6587\u6863/\u8bf4\u660e.md")},
	}
	engine := testEngine(t)
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := engine.Complete(t.Context(), test.input, test.cursor)
			if err != nil {
				t.Fatal(err)
			}
			index := slices.Index(candidateValues(result.Candidates), test.value)
			if index < 0 {
				t.Fatalf("missing candidate %q in %v", test.value, candidateValues(result.Candidates))
			}
			got, pos, err := result.Apply(index)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || pos != test.pos {
				t.Errorf("Apply = (%q, %d), want (%q, %d)", got, pos, test.want, test.pos)
			}
		})
	}
	for _, cursor := range []int{0, 1, 2} {
		input := "  /mode"
		result, err := engine.Complete(t.Context(), input, cursor)
		if err != nil || result.Source != "" {
			t.Errorf("completion before trigger at %d = %+v, %v", cursor, result, err)
		}
	}
}

func TestSourcePrecedence(t *testing.T) {
	engine := New()
	for _, source := range []Source{
		{Name: "at", Trigger: "@", Complete: Static(Candidate{Value: "base"})},
		{Name: "double", Trigger: "@@", Complete: Static(Candidate{Value: "better"})},
		{Name: "commands", Trigger: "/", StartOnly: true, Complete: func(context.Context, Request) ([]Candidate, error) {
			t.Error("an earlier trigger must not run when a nearer source matches")
			return nil, nil
		}},
		{Name: "unicode", Trigger: "\u03bb", Complete: Static(Candidate{Value: "value"})},
	} {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}
	for input, source := range map[string]string{
		"/anything @@b": "double",
		"/anything @b":  "at",
		"\u03bbv":       "unicode",
	} {
		result, err := engine.Complete(t.Context(), input, len(input))
		if err != nil || result.Source != source || len(result.Candidates) != 1 {
			t.Errorf("Complete(%q) = %+v, %v", input, result, err)
		}
	}
}

func TestDynamicSourceReceivesContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "request-value")
	input := "/mode a"
	calls := 0
	engine := New()
	err := engine.Register(Source{Name: "modes", Trigger: "/mode ", Complete: func(ctx context.Context, req Request) ([]Candidate, error) {
		calls++
		if req.Input != input || req.Cursor != len(input) || req.Source != "modes" || req.Trigger != "/mode " ||
			req.Query != "a" || req.Range != (Range{Start: len("/mode "), End: len(input)}) {
			t.Errorf("incorrect request context: %+v", req)
		}
		if ctx.Value(contextKey{}) != "request-value" {
			t.Error("completer did not receive caller context")
		}
		return []Candidate{{Value: "auto"}, {Value: "other"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Complete(ctx, input, len(input))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !slices.Equal(candidateValues(result.Candidates), []string{"auto"}) {
		t.Errorf("unexpected dynamic completion: %+v", result)
	}
}

func TestRegistrationErrors(t *testing.T) {
	valid := Source{Name: "valid", Trigger: "/", Complete: Static()}
	for name, source := range map[string]Source{
		"empty name":       {Trigger: "/", Complete: Static()},
		"blank name":       {Name: " \t", Trigger: "/", Complete: Static()},
		"empty trigger":    {Name: "test", Complete: Static()},
		"leading space":    {Name: "test", Trigger: " /", Complete: Static()},
		"quote trigger":    {Name: "test", Trigger: `"`, Complete: Static()},
		"escape trigger":   {Name: "test", Trigger: `\`, Complete: Static()},
		"invalid UTF8":     {Name: "test", Trigger: "\xff", Complete: Static()},
		"missing resolver": {Name: "test", Trigger: "/"},
	} {
		t.Run(name, func(t *testing.T) {
			var engine Engine
			if err := engine.Register(source); !errors.Is(err, ErrInvalidSource) {
				t.Errorf("Register = %v, want ErrInvalidSource", err)
			}
			if err := engine.Register(valid); err != nil {
				t.Errorf("failed registration changed engine: %v", err)
			}
		})
	}
	var engine Engine
	if err := engine.Register(valid); err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []Source{
		{Name: "valid", Trigger: "@", Complete: Static()},
		{Name: "other", Trigger: "/", Complete: Static()},
	} {
		if err := engine.Register(duplicate); !errors.Is(err, ErrDuplicateSource) {
			t.Errorf("Register duplicate = %v, want ErrDuplicateSource", err)
		}
	}
	if err := engine.Register(Source{Name: "modes", Trigger: "/mode ", Complete: Static()}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Register(Source{Name: "same-context", Trigger: "/mode\t", Complete: Static()}); !errors.Is(err, ErrDuplicateSource) {
		t.Errorf("equivalent whitespace trigger = %v", err)
	}
}

func TestUnicodeTriggerByteOffsets(t *testing.T) {
	for _, test := range []struct{ trigger, input string }{
		{"K", "\u212Aval"},
		{"\u212A", "kval"},
	} {
		engine := New()
		if err := engine.Register(Source{Name: "unicode", Trigger: test.trigger, Complete: Static(Candidate{Value: "value"})}); err != nil {
			t.Fatal(err)
		}
		result, err := engine.Complete(t.Context(), test.input, len(test.input))
		if err != nil || result.Query != "val" || len(result.Candidates) != 1 {
			t.Fatalf("case-insensitive trigger = %+v, %v", result, err)
		}
		input, cursor, err := result.Apply(0)
		start := len(test.input) - len("val")
		want := test.input[:start] + "value"
		if err != nil || input != want || cursor != len(want) {
			t.Errorf("Apply = %q at %d, %v; want %q", input, cursor, err, want)
		}
	}
}

func TestInputAndSelectionErrors(t *testing.T) {
	engine := testEngine(t)
	for _, test := range []struct {
		input  string
		cursor int
		want   error
	}{
		{"/mode", -1, ErrInvalidCursor},
		{"/mode", 6, ErrInvalidCursor},
		{"@\u6587", 2, ErrInvalidCursor},
		{"\xff", 1, ErrInvalidInput},
	} {
		if _, err := engine.Complete(t.Context(), test.input, test.cursor); !errors.Is(err, test.want) {
			t.Errorf("Complete(%q, %d) = %v, want %v", test.input, test.cursor, err, test.want)
		}
	}
	var nilContext context.Context
	if _, err := engine.Complete(nilContext, "/", 1); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("nil context = %v", err)
	}
	result, err := engine.Complete(t.Context(), "/mode a", len("/mode a"))
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{-1, len(result.Candidates)} {
		if _, _, err := result.Apply(index); !errors.Is(err, ErrInvalidSelection) {
			t.Errorf("Apply(%d) = %v", index, err)
		}
	}
	for _, replacement := range []Range{{-1, 7}, {7, 6}, {0, 99}, {99, 99}} {
		result.Range = replacement
		if _, _, err := result.Apply(0); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("Apply range %+v = %v", replacement, err)
		}
	}
	result.Input, result.Cursor, result.Range = "@\u6587", len("@\u6587"), Range{2, len("@\u6587")}
	if _, _, err := result.Apply(0); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("split-rune replacement = %v", err)
	}
}

func TestSourceErrorsAndCancellation(t *testing.T) {
	failure := errors.New("source failed")
	for _, trigger := range []string{"/", "/mode "} {
		t.Run(trigger, func(t *testing.T) {
			fail := func(context.Context, Request) ([]Candidate, error) { return nil, failure }
			engine := New()
			if err := engine.Register(Source{Name: "test", Trigger: trigger, Complete: fail}); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Complete(t.Context(), trigger, len(trigger)); !errors.Is(err, failure) || !strings.Contains(err.Error(), `"test"`) {
				t.Errorf("source error = %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := testEngine(t).Complete(ctx, "/", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled request = %v", err)
	}
	engine := New()
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	if err := engine.Register(Source{Name: "cancel", Trigger: "/", Complete: func(context.Context, Request) ([]Candidate, error) {
		cancel()
		return []Candidate{{Value: "ignored"}}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Complete(ctx, "/", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation during completion = %v", err)
	}
}

func TestInvalidCandidates(t *testing.T) {
	for name, candidates := range map[string][]Candidate{
		"empty value":     {{Label: "no value"}},
		"invalid value":   {{Value: "\xff"}},
		"invalid insert":  {{Value: "valid", InsertText: "\xff"}},
		"empty alias":     {{Value: "valid", Aliases: []string{""}}},
		"duplicate":       {{Value: "same"}, {Value: "same"}},
		"case collision":  {{Value: "same"}, {Value: "SAME"}},
		"alias collision": {{Value: "first", Aliases: []string{"second"}}, {Value: "second"}},
	} {
		t.Run(name, func(t *testing.T) {
			engine := New()
			if err := engine.Register(Source{Name: "test", Trigger: "/", Complete: Static(candidates...)}); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Complete(t.Context(), "/", 1); !errors.Is(err, ErrInvalidCandidates) {
				t.Errorf("invalid candidates = %v", err)
			}
		})
	}
}

func TestStaticCopiesDeclarationsAndResults(t *testing.T) {
	items := []Candidate{{Value: "first", Aliases: []string{"alias"}}}
	complete := Static(items...)
	items[0].Value, items[0].Aliases[0] = "changed", "changed"
	first, err := complete(t.Context(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Value != "first" || first[0].Aliases[0] != "alias" {
		t.Fatalf("declaration was not copied: %+v", first)
	}
	first[0].Value, first[0].Aliases[0] = "mutated", "mutated"
	second, err := complete(t.Context(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Value != "first" || second[0].Aliases[0] != "alias" {
		t.Errorf("result was not copied: %+v", second)
	}
}

func TestCandidateDataPreserved(t *testing.T) {
	type fileData struct {
		Prefix string
		Kind   string
		Path   string
	}
	file := fileData{Prefix: "@", Kind: "file", Path: "src/main.go"}
	for name, data := range map[string]any{
		"value":   file,
		"pointer": &file,
		"map":     map[string]string{"Prefix": "@", "Kind": "file", "Path": "src/main.go"},
		"nil":     nil,
	} {
		for _, static := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/static=%v", name, static), func(t *testing.T) {
				items := []Candidate{
					{Value: "src/main.go", Data: data},
					{Value: "other.go", Data: file},
				}
				complete := Completer(func(context.Context, Request) ([]Candidate, error) {
					return items, nil
				})
				if static {
					complete = Static(items...)
				}
				engine := New()
				if err := engine.Register(Source{Name: "files", Trigger: "@", Complete: complete}); err != nil {
					t.Fatal(err)
				}
				input := "inspect @src/m"
				result, err := engine.Complete(t.Context(), input, len(input))
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Candidates) != 1 {
					t.Fatalf("metadata affected filtering: %+v", result.Candidates)
				}
				got := result.Candidates[0].Data
				if !reflect.DeepEqual(got, data) {
					t.Errorf("Data = %#v, want %#v", got, data)
				}
				if name == "pointer" && got != &file {
					t.Error("Data pointer identity was not preserved")
				}
				completed, cursor, err := result.Apply(0)
				want := "inspect @src/main.go"
				if err != nil || completed != want || cursor != len(want) {
					t.Errorf("metadata affected insertion: %q at %d, %v", completed, cursor, err)
				}
			})
		}
	}
}

func TestInsertionTextAndQuoting(t *testing.T) {
	for _, candidate := range []Candidate{
		{Value: `a "quoted" file\name`},
		{Value: "a'b"},
		{Value: "with\nnewline"},
		{Value: "file name", InsertText: `file\ name`},
		{Value: "mode", InsertText: "mode "},
	} {
		t.Run(candidate.Value, func(t *testing.T) {
			engine := New()
			if err := engine.Register(Source{Name: "test", Trigger: "@", Complete: Static(candidate)}); err != nil {
				t.Fatal(err)
			}
			result, err := engine.Complete(t.Context(), "@", 1)
			if err != nil {
				t.Fatal(err)
			}
			input, cursor, err := result.Apply(0)
			if err != nil {
				t.Fatal(err)
			}
			if candidate.InsertText != "" && input != "@"+candidate.InsertText {
				t.Errorf("InsertText not preserved: %q", input)
			}
			if candidate.InsertText == "mode " {
				return
			}
			resolved, err := engine.Complete(t.Context(), input, cursor)
			if err != nil || resolved.Query != candidate.Value || len(resolved.Candidates) != 1 {
				t.Errorf("insertion did not round trip: %q -> %+v, %v", input, resolved, err)
			}
		})
	}
}

func TestConcurrentRegistrationAndCompletion(t *testing.T) {
	engine := testEngine(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			name := fmt.Sprintf("source-%d", i)
			if err := engine.Register(Source{Name: name, Trigger: name, Complete: Static(Candidate{Value: "value"})}); err != nil {
				t.Errorf("Register: %v", err)
			}
			for range 20 {
				result, err := engine.Complete(t.Context(), "/mode ", len("/mode "))
				if err != nil || len(result.Candidates) != 4 {
					t.Errorf("concurrent completion = %+v, %v", result, err)
				}
			}
		})
	}
	wg.Wait()
}

func FuzzComplete(f *testing.F) {
	for _, input := range []string{"/mode", "/mode ", "/mcp restart s", "inspect @src/", `@"src/file n`, "@\u6587", "\xff"} {
		f.Add(input, len(input))
	}
	engine := testEngine(f)
	f.Fuzz(func(t *testing.T, input string, cursor int) {
		result, err := engine.Complete(t.Context(), input, cursor)
		if err != nil {
			return
		}
		for i := range result.Candidates {
			completed, pos, err := result.Apply(i)
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.ValidString(completed) || pos < 0 || pos > len(completed) {
				t.Fatalf("invalid insertion %q at %d", completed, pos)
			}
			if completed[:result.Range.Start] != input[:result.Range.Start] || completed[pos:] != input[result.Range.End:] {
				t.Fatalf("insertion changed text outside replacement range: %q -> %q", input, completed)
			}
		}
	})
}

func ExampleEngine_Complete() {
	engine := New()
	for _, source := range []Source{
		{Name: "commands", Trigger: "/", StartOnly: true,
			Complete: Static(Candidate{Value: "mode"}, Candidate{Value: "mcp"})},
		{Name: "modes", Trigger: "/mode ", StartOnly: true,
			Complete: Static(Candidate{Value: "auto"}, Candidate{Value: "ask"})},
		{Name: "mcp", Trigger: "/mcp ", StartOnly: true,
			Complete: Static(Candidate{Value: "list"}, Candidate{Value: "restart"})},
	} {
		if err := engine.Register(source); err != nil {
			panic(err)
		}
	}
	for _, input := range []string{"/mode", "/mode ", "/mcp r"} {
		result, err := engine.Complete(context.Background(), input, len(input))
		if err != nil {
			panic(err)
		}
		fmt.Printf("%q: %v\n", input, candidateValues(result.Candidates))
	}
	// Output:
	// "/mode": [mode]
	// "/mode ": [auto ask]
	// "/mcp r": [restart]
}
