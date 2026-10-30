package models

import "strings"

// ID is the qualified model identifier "<provider>/<model>".
//
// The model part may itself contain "/" (e.g. "openrouter/x-ai/grok-build-0.1");
// only the first "/" separates the provider from the model.
type ID string

// String returns the identifier as a string.
func (id ID) String() string { return string(id) }

// Provider returns the text before the first "/". When there is no "/", the whole
// identifier is returned.
func (id ID) Provider() string {
	s := string(id)
	if before, _, ok := strings.Cut(s, "/"); ok {
		return before
	}
	return s
}

// Name returns the text after the first "/". When there is no "/", it returns "".
func (id ID) Name() string {
	s := string(id)
	if _, after, ok := strings.Cut(s, "/"); ok {
		return after
	}
	return ""
}

// Valid reports whether the identifier has both a non-empty provider and a
// non-empty model name.
func (id ID) Valid() bool {
	s := string(id)
	i := strings.IndexByte(s, '/')
	return i > 0 && i < len(s)-1
}

// Normalize returns the qualified form of a provider/model pair, prefixing the
// model with the provider name when it is not already qualified.
func Normalize(provider, model string) ID {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return ID(model)
	}
	if model == "" {
		return ID(provider)
	}
	if strings.HasPrefix(model, provider+"/") {
		return ID(model)
	}
	return ID(provider + "/" + model)
}
