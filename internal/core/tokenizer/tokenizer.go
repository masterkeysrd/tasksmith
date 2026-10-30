// Package tokenizer provides lightweight, code-calibrated token estimation for
// TaskSmith. It approximates BPE token counts from raw byte lengths without
// invoking a real tokenizer, which is sufficient for context-window budgeting,
// compaction triggers, and skill/UI display.
//
// The default density, [DefaultCharsPerToken], is tuned for code, JSON payloads,
// tool definitions, and diffs where BPE tokenizers average roughly 3.0
// characters per token. Callers that need a different density can use
// [EstimateCustom].
package tokenizer

import "math"

// DefaultCharsPerToken is calibrated for code, JSON payloads, tool definitions,
// and diffs where BPE tokenizers average ~3.0 characters per token.
const DefaultCharsPerToken = 3.0

// Estimate returns the approximate token count for s using the default
// characters-per-token density: ⌈len(s) / DefaultCharsPerToken⌉.
func Estimate(s string) int {
	return EstimateCustom(s, DefaultCharsPerToken)
}

// EstimateBytes returns the approximate token count for b using the default
// characters-per-token density: ⌈len(b) / DefaultCharsPerToken⌉.
func EstimateBytes(b []byte) int {
	return EstimateCustom(string(b), DefaultCharsPerToken)
}

// EstimateCustom returns the approximate token count for s using a
// caller-supplied characters-per-token density: ⌈len(s) / charsPerToken⌉. A
// non-positive density falls back to [DefaultCharsPerToken].
func EstimateCustom(s string, charsPerToken float64) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	if charsPerToken <= 0 {
		charsPerToken = DefaultCharsPerToken
	}
	return int(math.Ceil(float64(n) / charsPerToken))
}
