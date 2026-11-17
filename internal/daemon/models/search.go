package models

import (
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

// AliasRefs returns m.Aliases formatted with a leading "@" prefix ("@fast"),
// matching the syntax accepted by model resolution. Returns nil when m has no
// aliases.
func (m Model) AliasRefs() []string {
	if len(m.Aliases) == 0 {
		return nil
	}
	refs := make([]string, 0, len(m.Aliases))
	for _, alias := range m.Aliases {
		if alias = strings.TrimSpace(alias); alias != "" {
			refs = append(refs, "@"+strings.TrimPrefix(alias, "@"))
		}
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

// RankMatch scores how closely rawQuery matches m across its qualified ID, bare
// model slug, display name/label, @aliases, and description (Tier 0 exact,
// Tier 1 substring containment within the target, Tier 2 bounded edit distance;
// descriptions match only by substring containment). Lower (tier, score) sorts
// first.
func RankMatch(rawQuery string, m Model) (tier, score int, ok bool) {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	queryNoAt := strings.TrimPrefix(query, "@")
	if queryNoAt == "" {
		return 0, 0, false
	}

	var best strutils.BestRank
	considerField := func(q, target string) {
		t := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(target)), "@")
		if q == "" || t == "" {
			return
		}
		best.Consider(strutils.RankMatch(q, t, strutils.DefaultEditThreshold(q, t)))
	}

	considerField(queryNoAt, string(m.ID))
	considerField(queryNoAt, m.ID.Name())
	considerField(queryNoAt, m.Name)
	considerField(queryNoAt, m.Label)
	for _, alias := range m.Aliases {
		considerField(queryNoAt, alias)
	}

	// Support provider-scoped queries such as "ollama/@fast" or "ollama/qwen3".
	if reqID := ID(query); reqID.Valid() && strings.EqualFold(m.ID.Provider(), reqID.Provider()) {
		reqName := strings.TrimPrefix(reqID.Name(), "@")
		considerField(reqName, m.ID.Name())
		for _, alias := range m.Aliases {
			considerField(reqName, alias)
		}
	}

	best.Consider(strutils.RankMatch(queryNoAt, m.Description, -1))
	return best.Result()
}

// SuggestClosest returns up to limit closest model IDs from listed for typo
// recovery on an unresolved model or @alias reference. When requested carries
// a valid "<provider>/" prefix, models owned by that provider also qualify as
// a fallback tier if no closer lexical match exists.
func SuggestClosest(listed []Model, requested string, limit int) []string {
	q := strings.TrimSpace(requested)
	if q == "" || len(listed) == 0 || limit <= 0 {
		return nil
	}
	reqProv := ""
	if reqID := ID(q); reqID.Valid() {
		reqProv = reqID.Provider()
	}
	lowerQ := strings.ToLower(q)
	ranked, _ := strutils.RankBy(listed, limit,
		func(m Model) string { return string(m.ID) },
		func(m Model) (int, int, bool) {
			if tier, score, ok := RankMatch(q, m); ok {
				return tier, score, true
			}
			if reqProv != "" && strings.EqualFold(m.ID.Provider(), reqProv) {
				return 3, strutils.Levenshtein(lowerQ, strings.ToLower(string(m.ID))), true
			}
			return 0, 0, false
		},
	)
	if len(ranked) == 0 {
		return nil
	}
	out := make([]string, len(ranked))
	for i, m := range ranked {
		out[i] = string(m.ID)
	}
	return out
}
