// Package format provides human-readable string formatting helpers for the
// TaskSmith terminal UI. It converts raw numeric and duration values into
// compact, display-ready strings suitable for status lines, meters, and
// informational output.
//
// # Cost
//
// [FormatCostNano] renders a nano-USD amount as a dollar figure. Amounts under
// one cent keep four decimal places so small costs stay visible, while larger
// amounts use two decimals. When unpriced is true the amount is treated as a
// lower bound and prefixed with "≥ ".
//
// # Duration
//
// [Duration] renders a [time.Duration] in its two largest units (for example
// "3h 52m" or "45ms"), clamping negative durations to zero. [DurationMilis]
// provides the same rendering from a millisecond count.
//
// # Text
//
// [CompactSingleLine] collapses consecutive whitespace and newlines into single
// spaces, trims the ends, and truncates the result to maxRunes runes with an
// ellipsis ("…") when it is longer; a maxRunes of zero or less disables
// truncation.
//
// # Tokens
//
// [Tokens] formats a token count into a human-readable string: raw integers
// under 1,000, a "k" suffix for thousands, and an "M" suffix for millions,
// each formatted to one decimal place. Negative counts are prefixed with a
// minus sign.
package format
