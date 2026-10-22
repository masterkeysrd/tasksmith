// Package autocomplete provides a headless, source-driven completion engine for
// terminal UIs. It does not render suggestions or execute completed input.
package autocomplete

import "context"

// Range identifies a half-open byte range in the original input.
type Range struct {
	Start int
	End   int
}

// Request describes the token being resolved. Cursor and Range use UTF-8 byte
// offsets. Query is the decoded token prefix up to the cursor, excluding the
// matched trigger.
type Request struct {
	Input   string
	Cursor  int
	Source  string
	Trigger string
	Query   string
	Range   Range
}

// Completer supplies candidates for a matched trigger. Implementations must
// honor ctx and be safe for concurrent calls if their engine is used concurrently.
type Completer func(ctx context.Context, req Request) ([]Candidate, error)

// Candidate is a selectable value. Label and Description are presentation
// metadata. Aliases participate in matching but insert the canonical Value.
type Candidate struct {
	Value       string
	Label       string
	Description string
	Aliases     []string
	// InsertText overrides the default quoted Value when nonempty.
	InsertText string
	// Data is opaque, source-owned metadata. The engine preserves it without
	// inspecting or deep-copying its contents.
	Data any
}

// Source associates a trigger with its candidates. For example, "/" can suggest
// commands while "/mode " suggests mode values. Whitespace in a trigger matches
// token separators; trailing whitespace begins completion of the next token.
type Source struct {
	Name     string
	Trigger  string
	Complete Completer
	// StartOnly restricts the trigger to the first non-whitespace token.
	StartOnly bool
	// CaseSensitive applies to trigger, value, label, and alias matching.
	CaseSensitive bool
}

// Result contains the active completion context and matching candidates.
// Source is empty when no registered source can resolve the input context.
type Result struct {
	Request
	Candidates []Candidate
}

// Static declares a fixed set of candidates.
// It snapshots the supplied slice and aliases; each call returns its own copy.
// Opaque Data payloads are retained as supplied.
func Static(candidates ...Candidate) Completer {
	items := cloneCandidates(candidates)
	return func(ctx context.Context, _ Request) ([]Candidate, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return cloneCandidates(items), nil
	}
}

func cloneCandidates(items []Candidate) []Candidate {
	out := make([]Candidate, len(items))
	copy(out, items)
	for i := range out {
		out[i].Aliases = append([]string(nil), out[i].Aliases...)
	}
	return out
}
