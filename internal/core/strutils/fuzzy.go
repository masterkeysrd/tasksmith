package strutils

import (
	"fmt"
	"sort"
	"strings"
)

// ClosestMatch represents a ranked candidate identifier returned by fuzzy matching.
// Lower Tier and lower Score represent closer matches (Tier 0 = exact, 1 = substring,
// 2 = bounded edit distance).
type ClosestMatch struct {
	ID    string
	Tier  int
	Score int
}

// Levenshtein returns the rune-wise Levenshtein edit distance between a and b.
func Levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ra {
		curr[0] = i + 1
		for j, cb := range rb {
			cost := 1
			if ca == cb {
				cost = 0
			}
			curr[j+1] = min(prev[j+1]+1, min(curr[j]+1, prev[j]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

// RankMatch evaluates how closely target matches query (case-insensitively),
// classifying matches across three tiers:
//   - Tier 0: exact equality (score 0)
//   - Tier 1: non-empty substring containment (score = Levenshtein(q, t))
//   - Tier 2: bounded edit distance <= maxDist (score = Levenshtein(q, t))
func RankMatch(query, target string, maxDist int) (tier, score int, ok bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	t := strings.ToLower(strings.TrimSpace(target))
	if q == "" || t == "" {
		return 0, 0, false
	}
	if q == t {
		return 0, 0, true
	}
	dist := Levenshtein(q, t)
	if strings.Contains(t, q) || strings.Contains(q, t) {
		return 1, dist, true
	}
	if maxDist >= 0 && dist <= maxDist {
		return 2, dist, true
	}
	return 0, 0, false
}

// TopClosestIDs deduplicates matches by ID (keeping each ID's lowest Tier and
// Score), orders them by ascending (Tier, Score, ID), and returns up to limit IDs.
func TopClosestIDs(matches []ClosestMatch, limit int) []string {
	if len(matches) == 0 || limit <= 0 {
		return nil
	}
	bestByID := make(map[string]ClosestMatch, len(matches))
	for _, m := range matches {
		prev, exists := bestByID[m.ID]
		if !exists || m.Tier < prev.Tier || (m.Tier == prev.Tier && m.Score < prev.Score) {
			bestByID[m.ID] = m
		}
	}
	ordered := make([]ClosestMatch, 0, len(bestByID))
	for _, m := range bestByID {
		ordered = append(ordered, m)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Tier != ordered[j].Tier {
			return ordered[i].Tier < ordered[j].Tier
		}
		if ordered[i].Score != ordered[j].Score {
			return ordered[i].Score < ordered[j].Score
		}
		return ordered[i].ID < ordered[j].ID
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	out := make([]string, len(ordered))
	for i, m := range ordered {
		out[i] = m.ID
	}
	return out
}

// SuggestClosestStrings returns up to limit targets matching query using
// case-insensitive exact, substring, and bounded Levenshtein matching.
func SuggestClosestStrings(query string, targets []string, limit int) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || len(targets) == 0 || limit <= 0 {
		return nil
	}
	var matches []ClosestMatch
	for _, target := range targets {
		maxDist := max(2, max(len(q), len(target))/2+1)
		if tier, score, ok := RankMatch(q, target, maxDist); ok {
			matches = append(matches, ClosestMatch{ID: target, Tier: tier, Score: score})
		}
	}
	return TopClosestIDs(matches, limit)
}

// FormatDidYouMean formats ranked suggestions as " (did you mean \"a\", \"b\"?)"
// or returns "" when suggestions is empty.
func FormatDidYouMean(suggestions []string) string {
	if len(suggestions) == 0 {
		return ""
	}
	quoted := make([]string, len(suggestions))
	for i, s := range suggestions {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf(" (did you mean %s?)", strings.Join(quoted, ", "))
}
