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
	StartLine   int          // 1-indexed start line (optional, defaults to 1 for TypeFile)
	EndLine     int          // 1-indexed end line (optional, defaults to 0 for TypeFile)
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
				var cleanVal string
				var startLine, endLine int
				if resType == TypeFile {
					cleanVal, startLine, endLine = parseLineRange(value)
				} else {
					cleanVal = value
				}
				refs = append(refs, Reference{
					Type:        resType,
					Value:       cleanVal,
					StartLine:   startLine,
					EndLine:     endLine,
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

// ReferencePayload is the JSON-serializable representation of a Reference
// used for API communication between the TUI and the API service.
type ReferencePayload struct {
	Type        ResourceType `json:"type"`
	Value       string       `json:"value"`
	StartLine   int          `json:"start_line,omitempty"`
	EndLine     int          `json:"end_line,omitempty"`
	InsertText  string       `json:"insert_text"`
	FromTracker bool         `json:"from_tracker"`
}

// ToPayload converts a Reference to its JSON-serializable payload form.
func (r Reference) ToPayload() ReferencePayload {
	return ReferencePayload{
		Type:        r.Type,
		Value:       r.Value,
		StartLine:   r.StartLine,
		EndLine:     r.EndLine,
		InsertText:  r.InsertText,
		FromTracker: r.FromTracker,
	}
}

// FromPayload converts a JSON-serializable payload back to a Reference.
func (p ReferencePayload) FromPayload() Reference {
	return Reference{
		Type:        p.Type,
		Value:       p.Value,
		StartLine:   p.StartLine,
		EndLine:     p.EndLine,
		InsertText:  p.InsertText,
		FromTracker: p.FromTracker,
	}
}
