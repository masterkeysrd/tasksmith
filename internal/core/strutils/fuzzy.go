package strutils

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// ClosestMatch represents a ranked candidate identifier returned by fuzzy matching.
// Lower Tier and lower Score represent closer matches (Tier 0 = exact, 1 = substring,
// 2 = bounded edit distance).
type ClosestMatch struct {
	ID    string
	Tier  int
	Score int
}

// BestRank accumulates the best (lowest Tier, then lowest Score) fuzzy match
// across multiple candidate fields of a single item.
type BestRank struct {
	tier  int
	score int
	ok    bool
}

// Consider records (tier, score) when ok is true and it improves on the best
// match recorded so far.
func (b *BestRank) Consider(tier, score int, ok bool) {
	if !ok {
		return
	}
	if !b.ok || tier < b.tier || (tier == b.tier && score < b.score) {
		b.tier = tier
		b.score = score
		b.ok = true
	}
}

// Result returns the best (tier, score, true) recorded, or (0, 0, false) when
// no candidate matched.
func (b BestRank) Result() (tier, score int, ok bool) {
	return b.tier, b.score, b.ok
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

// DefaultEditThreshold returns a conservative maximum Levenshtein edit distance
// for typo recovery between query and target (one-third of the shorter string's
// rune length, with a floor of 1 for non-empty inputs).
func DefaultEditThreshold(query, target string) int {
	n := min(utf8.RuneCountInString(query), utf8.RuneCountInString(target))
	if n <= 0 {
		return 0
	}
	return max(1, n/3)
}

// RankMatch evaluates how closely target matches query (case-insensitively),
// classifying matches across three tiers:
//   - Tier 0: exact equality (score 0)
//   - Tier 1: target contains query as a substring (score = Levenshtein(q, t))
//   - Tier 2: bounded edit distance <= maxDist between comparably sized strings
//     (score = Levenshtein(q, t)); pass maxDist < 0 to disable edit-distance matching.
func RankMatch(query, target string, maxDist int) (tier, score int, ok bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	t := strings.ToLower(strings.TrimSpace(target))
	if q == "" || t == "" {
		return 0, 0, false
	}
	if q == t {
		return 0, 0, true
	}
	if strings.Contains(t, q) {
		return 1, Levenshtein(q, t), true
	}
	if maxDist < 0 {
		return 0, 0, false
	}
	ql := utf8.RuneCountInString(q)
	tl := utf8.RuneCountInString(t)
	if max(ql, tl)-min(ql, tl) > max(2, min(ql, tl)/2) {
		return 0, 0, false
	}
	if dist := Levenshtein(q, t); dist <= maxDist {
		return 2, dist, true
	}
	return 0, 0, false
}

// RankBy filters items to those for which match returns ok == true and orders
// them by ascending (tier, score, key(item)). If limit > 0, the returned slice
// is capped to limit elements; total always reports the full match count prior
// to truncation.
func RankBy[T any](items []T, limit int, key func(T) string, match func(T) (tier, score int, ok bool)) (ranked []T, total int) {
	type entry struct {
		item  T
		key   string
		tier  int
		score int
	}
	var matched []entry
	for _, it := range items {
		tier, score, ok := match(it)
		if !ok {
			continue
		}
		matched = append(matched, entry{
			item:  it,
			key:   key(it),
			tier:  tier,
			score: score,
		})
	}
	slices.SortFunc(matched, func(a, b entry) int {
		if a.tier != b.tier {
			return a.tier - b.tier
		}
		if a.score != b.score {
			return a.score - b.score
		}
		return strings.Compare(a.key, b.key)
	})
	total = len(matched)
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	if len(matched) == 0 {
		return nil, 0
	}
	out := make([]T, len(matched))
	for i, e := range matched {
		out[i] = e.item
	}
	return out, total
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
	unique := make([]ClosestMatch, 0, len(bestByID))
	for _, m := range bestByID {
		unique = append(unique, m)
	}
	ranked, _ := RankBy(unique, limit,
		func(m ClosestMatch) string { return m.ID },
		func(m ClosestMatch) (int, int, bool) { return m.Tier, m.Score, true },
	)
	out := make([]string, len(ranked))
	for i, m := range ranked {
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
	ranked, _ := RankBy(targets, limit,
		func(s string) string { return s },
		func(target string) (int, int, bool) {
			return RankMatch(q, target, DefaultEditThreshold(q, target))
		},
	)
	return ranked
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
