// Package fuzzy provides subsequence fuzzy matching and scoring utilities for workspace resources.
package fuzzy

import (
	"strings"
)

// Match checks if query is a subsequence of target.
// It trims leading slashes from the query, performs case-insensitive
// subsequence matching, and returns a boolean match status and an integer score.
// A higher score indicates a better, closer match.
func Match(target, query string) (bool, int) {
	target = strings.ToLower(target)
	query = strings.ToLower(strings.TrimPrefix(query, "/"))

	if query == "" {
		return true, 0
	}

	tIdx, qIdx := 0, 0
	score := 0
	lastMatchIdx := -1

	for tIdx < len(target) && qIdx < len(query) {
		if target[tIdx] == query[qIdx] {
			// Consecutive character bonus
			if lastMatchIdx != -1 && tIdx == lastMatchIdx+1 {
				score += 5
			}
			// Boundary bonus (right after '/', '_', '-', or at the start)
			if tIdx == 0 || target[tIdx-1] == '/' || target[tIdx-1] == '_' || target[tIdx-1] == '-' {
				score += 10
			}
			// Prefix match bonus
			if qIdx == tIdx {
				score += 3
			}

			score += 1
			lastMatchIdx = tIdx
			qIdx++
		}
		tIdx++
	}

	if qIdx == len(query) {
		// Prefer shorter matching paths
		score -= len(target) - len(query)
		return true, score
	}

	return false, 0
}
