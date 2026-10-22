package autocomplete

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Errors returned for invalid declarations, input, or completion selection.
var (
	ErrInvalidSource     = errors.New("autocomplete: invalid source")
	ErrDuplicateSource   = errors.New("autocomplete: duplicate source")
	ErrInvalidInput      = errors.New("autocomplete: invalid input")
	ErrInvalidCursor     = errors.New("autocomplete: invalid cursor")
	ErrInvalidCandidates = errors.New("autocomplete: invalid candidates")
	ErrInvalidSelection  = errors.New("autocomplete: invalid selection")
	ErrInvalidRange      = errors.New("autocomplete: invalid replacement range")
)

// Engine resolves input against registered sources. Its zero value is ready to
// use. Registration and completion may run concurrently.
type Engine struct {
	mu      sync.RWMutex
	sources []registeredSource
}

type registeredSource struct {
	Source
	parts         []string
	trailingSpace bool
	key           string
}

// New returns an engine with no registered sources.
func New() *Engine {
	return &Engine{}
}

// Register adds a source. Names and equivalent triggers must be unique.
// Triggers must be valid UTF-8, begin with a non-whitespace character, and
// contain no quotes or escape characters.
func (e *Engine) Register(source Source) error {
	if strings.TrimSpace(source.Name) == "" || !utf8.ValidString(source.Name) {
		return fmt.Errorf("%w: name is required and must be valid UTF-8", ErrInvalidSource)
	}
	if source.Trigger == "" || !utf8.ValidString(source.Trigger) ||
		strings.ContainsAny(source.Trigger, `"'\\`) {
		return fmt.Errorf("%w: invalid trigger %q", ErrInvalidSource, source.Trigger)
	}
	if source.Complete == nil {
		return fmt.Errorf("%w: source %q requires a completer", ErrInvalidSource, source.Name)
	}
	first, _ := utf8.DecodeRuneInString(source.Trigger)
	if unicode.IsSpace(first) {
		return fmt.Errorf("%w: trigger must not begin with whitespace", ErrInvalidSource)
	}
	last, _ := utf8.DecodeLastRuneInString(source.Trigger)
	registered := registeredSource{
		Source: source, parts: strings.Fields(source.Trigger),
		trailingSpace: unicode.IsSpace(last),
	}
	registered.key = strings.Join(registered.parts, " ")
	if registered.trailingSpace {
		registered.key += " "
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, existing := range e.sources {
		caseSensitive := existing.CaseSensitive && source.CaseSensitive
		if existing.Name == source.Name || normalize(existing.key, caseSensitive) == normalize(registered.key, caseSensitive) {
			return fmt.Errorf("%w: name %q or trigger %q is already registered", ErrDuplicateSource, source.Name, source.Trigger)
		}
	}
	e.sources = append(e.sources, registered)
	return nil
}

// Complete resolves suggestions at cursor, a UTF-8 byte offset in input.
// Triggers match unquoted token prefixes. The nearest trigger wins, with the
// longest trigger breaking ties. A whitespace-terminated trigger such as
// "/mode " activates only after that token's separator. Quotes and backslash
// escapes group whitespace into tokens; unfinished quotes/escapes are accepted.
func (e *Engine) Complete(ctx context.Context, input string, cursor int) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("%w: context is required", ErrInvalidInput)
	}
	if err := validateInput(input, cursor); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	req, source := e.match(input, cursor)
	if req.Source == "" {
		return Result{}, nil
	}
	items, err := source.Complete(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("autocomplete: source %q: %w", source.Name, err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := validateCandidates(items, source.CaseSensitive); err != nil {
		return Result{}, fmt.Errorf("autocomplete: source %q: %w", source.Name, err)
	}
	return Result{Request: req, Candidates: filter(items, req.Query, source.CaseSensitive)}, nil
}

// Match resolves the active source without invoking its completer. Source is
// empty when no trigger matches. Cursor is a UTF-8 byte offset.
func (e *Engine) Match(input string, cursor int) (Request, error) {
	if err := validateInput(input, cursor); err != nil {
		return Request{}, err
	}
	req, _ := e.match(input, cursor)
	return req, nil
}

func (e *Engine) match(input string, cursor int) (Request, Source) {
	tokens := tokenize(input)
	active, index := tokenAt(tokens, cursor)
	e.mu.RLock()
	sources := append([]registeredSource(nil), e.sources...)
	e.mu.RUnlock()
	source, replacement, ok := findSource(sources, tokens, active, index, input, cursor)
	if !ok {
		return Request{}, Source{}
	}

	req := Request{
		Input: input, Cursor: cursor, Source: source.Name, Trigger: source.Trigger,
		Query: decode(input[replacement.Start:cursor]), Range: replacement,
	}
	return req, source
}

// Apply replaces the active token with the selected candidate and returns the
// new input and byte cursor. Text outside Range, including the trigger and later
// arguments, is preserved. Values containing whitespace, quotes, or backslashes
// are quoted unless the source supplies InsertText. No separator is added.
func (r Result) Apply(index int) (string, int, error) {
	if index < 0 || index >= len(r.Candidates) {
		return "", 0, fmt.Errorf("%w: index %d", ErrInvalidSelection, index)
	}
	if err := validateInput(r.Input, r.Cursor); err != nil {
		return "", 0, err
	}
	if r.Range.Start < 0 || r.Range.Start > r.Cursor || r.Range.End < r.Cursor ||
		r.Range.End > len(r.Input) || !boundary(r.Input, r.Range.Start) || !boundary(r.Input, r.Range.End) {
		return "", 0, ErrInvalidRange
	}
	candidate := r.Candidates[index]
	text := candidate.InsertText
	if text == "" {
		text = quote(candidate.Value)
	}
	if candidate.Value == "" || !utf8.ValidString(text) {
		return "", 0, ErrInvalidCandidates
	}
	return r.Input[:r.Range.Start] + text + r.Input[r.Range.End:], r.Range.Start + len(text), nil
}

func validateInput(input string, cursor int) error {
	if !utf8.ValidString(input) {
		return fmt.Errorf("%w: input must be valid UTF-8", ErrInvalidInput)
	}
	if cursor < 0 || cursor > len(input) || !boundary(input, cursor) {
		return fmt.Errorf("%w: byte offset %d", ErrInvalidCursor, cursor)
	}
	return nil
}

func boundary(input string, offset int) bool {
	return offset == len(input) || utf8.RuneStart(input[offset])
}

func normalize(value string, caseSensitive bool) string {
	if caseSensitive {
		return value
	}
	return strings.ToLower(value)
}

func validateCandidates(items []Candidate, caseSensitive bool) error {
	seen := make(map[string]int)
	for i, item := range items {
		if item.Value == "" || !utf8.ValidString(item.Value) || !utf8.ValidString(item.InsertText) {
			return fmt.Errorf("%w: candidate %d requires a valid UTF-8 value and insertion text", ErrInvalidCandidates, i)
		}
		values := append([]string{item.Value}, item.Aliases...)
		for _, value := range values {
			if value == "" || !utf8.ValidString(value) {
				return fmt.Errorf("%w: candidate %q has an invalid alias", ErrInvalidCandidates, item.Value)
			}
			key := normalize(value, caseSensitive)
			if previous, ok := seen[key]; ok && previous != i {
				return fmt.Errorf("%w: ambiguous value or alias %q", ErrInvalidCandidates, value)
			}
			seen[key] = i
		}
	}
	return nil
}

func filter(items []Candidate, query string, caseSensitive bool) []Candidate {
	query = normalize(query, caseSensitive)
	var matches []Candidate
	for _, item := range items {
		match := strings.HasPrefix(normalize(item.Value, caseSensitive), query) ||
			(item.Label != "" && strings.HasPrefix(normalize(item.Label, caseSensitive), query))
		for _, alias := range item.Aliases {
			match = match || strings.HasPrefix(normalize(alias, caseSensitive), query)
		}
		if match {
			matches = append(matches, item)
		}
	}
	return matches
}

type token struct {
	start int
	end   int
}

func tokenize(input string) []token {
	var tokens []token
	start, quote, escaped := -1, rune(0), false
	for i, r := range input {
		if start < 0 {
			if unicode.IsSpace(r) {
				continue
			}
			start = i
		}
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			tokens = append(tokens, token{start: start, end: i})
			start = -1
		}
	}
	if start >= 0 {
		tokens = append(tokens, token{start: start, end: len(input)})
	}
	return tokens
}

func tokenAt(tokens []token, cursor int) (token, int) {
	for i, tok := range tokens {
		if cursor < tok.start {
			return token{start: cursor, end: cursor}, i
		}
		if cursor <= tok.end {
			return tok, i
		}
	}
	return token{start: cursor, end: cursor}, len(tokens)
}

func findSource(sources []registeredSource, tokens []token, active token, index int, input string, cursor int) (Source, Range, bool) {
	var match registeredSource
	replacement := Range{Start: active.start, End: active.end}
	bestRoot := -1
	for _, source := range sources {
		completed := len(source.parts)
		prefix := ""
		if !source.trailingSpace {
			completed--
			prefix = source.parts[completed]
		}
		root := index - completed
		if root < 0 || (source.StartOnly && root != 0) {
			continue
		}
		matches := true
		for i := range completed {
			tok := tokens[root+i]
			raw := input[tok.start:tok.end]
			if normalize(decode(raw), source.CaseSensitive) != normalize(source.parts[i], source.CaseSensitive) ||
				(i == 0 && !strings.HasPrefix(normalize(raw, source.CaseSensitive), normalize(source.parts[0], source.CaseSensitive))) {
				matches = false
				break
			}
		}
		length, prefixMatches := prefixLength(input[active.start:cursor], prefix, source.CaseSensitive)
		if !matches || !prefixMatches {
			continue
		}
		if root > bestRoot || (root == bestRoot && len(source.key) > len(match.key)) {
			match = source
			bestRoot = root
			replacement.Start = active.start + length
		}
	}
	return match.Source, replacement, bestRoot >= 0
}

func prefixLength(value, prefix string, caseSensitive bool) (int, bool) {
	end := 0
	for _, wanted := range prefix {
		if end == len(value) {
			return 0, false
		}
		r, size := utf8.DecodeRuneInString(value[end:])
		equal := r == wanted
		if !caseSensitive {
			equal = unicode.ToLower(r) == unicode.ToLower(wanted)
		}
		if !equal {
			return 0, false
		}
		end += size
	}
	return end, true
}

func decode(input string) string {
	var out strings.Builder
	quote, escaped := rune(0), false
	for _, r := range input {
		switch {
		case escaped:
			out.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				out.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func quote(value string) string {
	if strings.IndexFunc(value, unicode.IsSpace) < 0 && !strings.ContainsAny(value, `"'\\`) {
		return value
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
