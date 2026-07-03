package resolver

import "strings"

// Prefixes maps trigger strings to their ResourceType.
// Used by both autocomplete config and the submit-time parser.
var Prefixes = map[string]ResourceType{
	"@file:":  TypeFile,
	"@sym:":   TypeSymbol,
	"@skill:": TypeSkill,
}

// PrefixToSourceMap converts the shared Prefixes map into the
// map[string]string format expected by the autocomplete Config.Prefixes.
func PrefixToSourceMap() map[string]string {
	m := make(map[string]string, len(Prefixes))
	for prefix, rt := range Prefixes {
		m[prefix] = string(rt)
	}
	return m
}

// IsPrefix checks if the given text starts with any registered trigger prefix.
func IsPrefix(text string) bool {
	for p := range Prefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// Reference represents a context reference from user input.
type Reference struct {
	Type        ResourceType // e.g. TypeFile, TypeSymbol, TypeSkill
	Value       string       // full path/name (e.g. "internal/app/app.go")
	InsertText  string       // token as it appears in text (e.g. "@file:app.go")
	FromTracker bool         // true = autocomplete, false = manual parse
}

// ExtractReferences scans text for prefix-tagged tokens and returns
// references NOT already present in the tracked set.
func ExtractReferences(text string, tracked []Reference) []Reference {
	trackedSet := make(map[string]bool)
	for _, ref := range tracked {
		trackedSet[ref.InsertText] = true
	}

	var refs []Reference
	for prefix, resType := range Prefixes {
		offset := 0
		for {
			idx := strings.Index(text[offset:], prefix)
			if idx == -1 {
				break
			}
			start := offset + idx
			end := start + len(prefix)
			for end < len(text) && !isWhitespace(text[end]) {
				end++
			}
			token := text[start:end]
			value := text[start+len(prefix) : end]

			if !trackedSet[token] && value != "" {
				refs = append(refs, Reference{
					Type:        resType,
					Value:       value,
					InsertText:  token,
					FromTracker: false,
				})
			}
			offset = end
		}
	}
	return refs
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
