package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// SmartReplace attempts to find and replace `target` with `replacement` in `content`.
// It uses a multi-stage cascade to handle common agent formatting errors (like whitespace/indentation).
func SmartReplace(content, target, replacement string, replaceAll bool) (string, int, error) {
	// Normalize line endings
	targetNorm := strings.ReplaceAll(target, "\r\n", "\n")
	contentNorm := strings.ReplaceAll(content, "\r\n", "\n")
	replacementNorm := strings.ReplaceAll(replacement, "\r\n", "\n")

	if strings.TrimSpace(targetNorm) == "" {
		return content, 0, fmt.Errorf("target block cannot be empty or all-whitespace")
	}

	// Detect original line endings
	isCRLF := strings.Contains(content, "\r\n")

	finalize := func(result string, count int, err error) (string, int, error) {
		if err != nil {
			return content, count, err
		}
		if isCRLF {
			lines := strings.Split(result, "\n")
			for idx, l := range lines {
				lines[idx] = strings.TrimSuffix(l, "\r")
			}
			result = strings.Join(lines, "\r\n")
		}
		return result, count, nil
	}

	// STAGE 1: Exact Match (Fast Path)
	count := strings.Count(contentNorm, targetNorm)
	if count > 0 {
		if count > 1 && !replaceAll {
			return content, count, fmt.Errorf("target block matches %d occurrences (must be unique or replace_all must be true)", count)
		}
		var newContent string
		if replaceAll {
			newContent = strings.ReplaceAll(contentNorm, targetNorm, replacementNorm)
		} else {
			newContent = strings.Replace(contentNorm, targetNorm, replacementNorm, 1)
		}
		return finalize(newContent, count, nil)
	}

	// STAGE 2: Normalized Whitespace Match (Line-by-line)
	// This catches 90% of agent errors: wrong indentation, missing trailing spaces, etc.
	contentLines := strings.Split(contentNorm, "\n")
	rawTargetLines := strings.Split(targetNorm, "\n")

	// Strip leading/trailing empty lines from both target and replacement to prevent multiplication
	stripEmpty := func(lines []string) []string {
		s, e := 0, len(lines)
		for s < e && strings.TrimSpace(lines[s]) == "" {
			s++
		}
		for e > s && strings.TrimSpace(lines[e-1]) == "" {
			e--
		}
		return lines[s:e]
	}
	targetLines := stripEmpty(rawTargetLines)
	replacementNorm = strings.Join(stripEmpty(strings.Split(replacementNorm, "\n")), "\n")

	var matchIndices []int
	for i := 0; i <= len(contentLines)-len(targetLines); {
		match := true
		for j := 0; j < len(targetLines); j++ {
			if collapseWhitespace(contentLines[i+j]) != collapseWhitespace(targetLines[j]) {
				match = false
				break
			}
		}
		if match {
			matchIndices = append(matchIndices, i)
			if replaceAll {
				// Advance past the match to skip overlapping occurrences in replaceAll mode
				i += len(targetLines)
			} else {
				// Increment by 1 when replaceAll is false to scan all indices
				// for accurate error-reporting count of duplicate/ambiguous matches
				i++
			}
		} else {
			i++
		}
	}

	if len(matchIndices) > 1 && !replaceAll {
		return content, len(matchIndices), fmt.Errorf("target block matches %d occurrences", len(matchIndices))
	}

	if len(matchIndices) > 0 {
		// We found Stage 2 matches! Replace them from bottom to top to avoid shifting indices.
		newLines := make([]string, len(contentLines))
		copy(newLines, contentLines)

		for i := len(matchIndices) - 1; i >= 0; i-- {
			matchStart := matchIndices[i]
			matchEnd := matchStart + len(targetLines)

			// Determine original base indentation from the first matched line
			origIndent := getIndentation(newLines[matchStart])

			// Adjust the replacement block's indentation to match origIndent
			adjustedReplacement := adjustIndentation(replacementNorm, origIndent)
			var adjustedReplLines []string
			if adjustedReplacement != "" {
				adjustedReplLines = strings.Split(adjustedReplacement, "\n")
			}

			// Splice the adjusted replacement lines into the array
			spliced := append(newLines[:matchStart:matchStart], adjustedReplLines...)
			newLines = append(spliced, newLines[matchEnd:]...)
		}

		return finalize(strings.Join(newLines, "\n"), len(matchIndices), nil)
	}

	// STAGE 3: Fuzzy SequenceMatcher (Sliding Window)
	// Handles drifted code, minor hallucinations, or typos within lines using difflib.

	// We want to compare against trimmed lines to ignore indentation drift
	trimmedContentLines := make([]string, len(contentLines))
	for i, l := range contentLines {
		trimmedContentLines[i] = collapseWhitespace(l)
	}
	trimmedTargetLines := make([]string, len(targetLines))
	for i, l := range targetLines {
		trimmedTargetLines[i] = collapseWhitespace(l)
	}

	targetLineIndex := make(map[string]int)
	var targetLineCounts []int
	for _, tl := range trimmedTargetLines {
		if idx, exists := targetLineIndex[tl]; exists {
			targetLineCounts[idx]++
		} else {
			targetLineIndex[tl] = len(targetLineCounts)
			targetLineCounts = append(targetLineCounts, 1)
		}
	}

	bestRatio := 0.0

	// Slide a window across the file.
	// We allow the window to be slightly larger than the target to account for insertions/deletions.
	windowSize := len(targetLines) + 4
	if windowSize > len(contentLines) {
		windowSize = len(contentLines)
	}

	var candidates []scanCandidate
	threshold := 0.65 // Strict threshold requiring 65% match of target lines (equivalent to original 0.60 difflib ratio but clean)

	windowCounts := make([]int, len(targetLineCounts))

	for i := 0; i < len(contentLines); i++ {
		endIdx := i + windowSize
		if endIdx > len(contentLines) {
			endIdx = len(contentLines)
		}

		windowContent := trimmedContentLines[i:endIdx]

		// Early-exit pre-filter: skip windows that cannot possibly reach the similarity threshold.
		// Track frequencies using a pre-allocated slice to avoid heap allocations inside the loop.
		for j := range windowCounts {
			windowCounts[j] = 0
		}
		for _, wl := range windowContent {
			if idx, exists := targetLineIndex[wl]; exists {
				windowCounts[idx]++
			}
		}
		possibleMatches := 0
		for idx, count := range windowCounts {
			tc := targetLineCounts[idx]
			if count > tc {
				possibleMatches += tc
			} else {
				possibleMatches += count
			}
		}
		if float64(possibleMatches)/float64(len(trimmedTargetLines)) < threshold {
			continue
		}

		m := difflib.NewMatcher(windowContent, trimmedTargetLines)

		// Calculate ratio relative only to target length to avoid window size penalty
		matches := 0
		blocks := m.GetMatchingBlocks()
		for _, b := range blocks {
			matches += b.Size
		}
		r := float64(matches) / float64(len(trimmedTargetLines))
		if r > 1.0 {
			r = 1.0
		}

		if r > bestRatio {
			bestRatio = r
		}

		if len(blocks) >= 2 {
			first := blocks[0]
			last := blocks[len(blocks)-2] // second to last is the final actual match block

			// Reconstruct boundaries with projection to catch typos at the edges,
			// but strictly clamp them to the current sliding window to prevent massive over-deletion.
			start := i + (first.A - first.B)
			if start < i {
				start = i
			}
			end := i + last.A + last.Size + (len(targetLines) - (last.B + last.Size))
			endLimit := i + windowSize
			if endLimit > len(contentLines) {
				endLimit = len(contentLines)
			}
			if end > endLimit {
				end = endLimit
			}

			// Validate prefix similarity to prevent deleting unrelated code
			if first.B > 0 {
				prefixTarget := strings.Join(trimmedTargetLines[0:first.B], "\n")
				prefixEnd := start + first.B
				if prefixEnd > len(trimmedContentLines) {
					prefixEnd = len(trimmedContentLines)
				}
				prefixContent := strings.Join(trimmedContentLines[start:prefixEnd], "\n")
				if stringSimilarity(prefixTarget, prefixContent) < 0.5 {
					continue // Reject candidate due to dissimilar prefix
				}
			}

			// Validate suffix similarity to prevent deleting unrelated code
			suffixTargetLen := len(targetLines) - (last.B + last.Size)
			if suffixTargetLen > 0 {
				suffixTarget := strings.Join(trimmedTargetLines[last.B+last.Size:len(targetLines)], "\n")
				suffixStart := end - suffixTargetLen
				if suffixStart < 0 {
					suffixStart = 0
				}
				suffixContent := strings.Join(trimmedContentLines[suffixStart:end], "\n")
				if stringSimilarity(suffixTarget, suffixContent) < 0.5 {
					continue // Reject candidate due to dissimilar suffix
				}
			}

			// Deduplicate candidates by exact (start, end) ranges
			found := false
			for idx, existing := range candidates {
				if existing.start == start && existing.end == end {
					if r > existing.ratio {
						candidates[idx].ratio = r
					}
					found = true
					break
				}
			}
			if !found {
				candidates = append(candidates, scanCandidate{
					start: start,
					end:   end,
					ratio: r,
				})
			}
		}
	}

	if bestRatio >= threshold {
		// Sort candidates by ratio descending so that highest quality matches
		// are evaluated first and win overlap conflicts during filtering
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].ratio > candidates[j].ratio
		})

		type fuzzyMatch struct {
			start, end int
		}
		var matches []fuzzyMatch

		for _, cand := range candidates {
			// Match must be within 0.02 of the best ratio
			if cand.ratio >= bestRatio-0.02 {
				// Avoid overlapping matches
				overlap := false
				for _, existing := range matches {
					if cand.start < existing.end && cand.end > existing.start {
						overlap = true
						break
					}
				}
				if !overlap {
					matches = append(matches, fuzzyMatch{start: cand.start, end: cand.end})
				}
			}
		}

		if len(matches) > 1 && !replaceAll {
			return content, len(matches), fmt.Errorf("target block matches %d occurrences", len(matches))
		}

		if len(matches) > 0 {
			// Sort matches by start index descending to safely apply replacements
			// bottom-to-top without shifting remaining target indices
			sort.Slice(matches, func(i, j int) bool {
				return matches[i].start > matches[j].start
			})

			newLines := make([]string, len(contentLines))
			copy(newLines, contentLines)

			for idx := range matches {
				m := matches[idx]
				origIndent := getIndentation(newLines[m.start])
				adjustedReplacement := adjustIndentation(replacementNorm, origIndent)
				var adjustedReplLines []string
				if adjustedReplacement != "" {
					adjustedReplLines = strings.Split(adjustedReplacement, "\n")
				}
				spliced := append(newLines[:m.start:m.start], adjustedReplLines...)
				newLines = append(spliced, newLines[m.end:]...)
			}
			return finalize(strings.Join(newLines, "\n"), len(matches), nil)
		}
	}

	return content, 0, nil // 0 matches found in all stages
}

func collapseWhitespace(s string) string {
	var sb strings.Builder
	inWhitespace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			if !inWhitespace {
				sb.WriteRune(' ')
				inWhitespace = true
			}
		} else {
			sb.WriteRune(r)
			inWhitespace = false
		}
	}
	return strings.TrimSpace(sb.String())
}

func getIndentation(line string) string {
	for i, c := range line {
		if c != ' ' && c != '\t' {
			return line[:i]
		}
	}
	return line // entirely whitespace
}

func stringSimilarity(s1, s2 string) float64 {
	a := strings.Split(s1, "")
	b := strings.Split(s2, "")
	m := difflib.NewMatcher(a, b)
	matches := 0
	for _, block := range m.GetMatchingBlocks() {
		matches += block.Size
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 1.0
	}
	return float64(matches) / float64(maxLen)
}

func normalizeIndent(indent string, useTabs bool) string {
	if useTabs {
		// Replace every 4 spaces with a tab, keeping remaining spaces intact
		return strings.ReplaceAll(indent, "    ", "\t")
	}
	// Convert tabs to spaces.
	return strings.ReplaceAll(indent, "\t", "    ")
}

type scanCandidate struct {
	start, end int
	ratio      float64
}

// isTrulyFlat returns true if the trimmed line is a comment or directive
// that is meant to remain unindented (flat) at column 0.
func isTrulyFlat(trimmed string, agentBaseIndent string) bool {
	// If the base block itself is not heavily indented, we treat flat lines as agent formatting errors.
	if len(agentBaseIndent) < 4 && !strings.Contains(agentBaseIndent, "\t") {
		return false
	}
	return strings.HasPrefix(trimmed, "//") ||
		strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "* ") ||
		strings.HasPrefix(trimmed, "*/") ||
		strings.HasPrefix(trimmed, "package ") ||
		strings.HasPrefix(trimmed, "import ")
}

func adjustIndentation(block string, targetBaseIndent string) string {
	lines := strings.Split(block, "\n")

	useTabs := strings.Contains(targetBaseIndent, "\t") || (targetBaseIndent == "" && strings.Contains(block, "\t"))

	// Find the minimum base indentation of the agent's replacement block, ignoring intentionally flat lines
	var agentBaseIndent string
	found := false
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		if trimmed != "" {
			if isTrulyFlat(trimmed, "        ") {
				continue
			}
			indent := getIndentation(l)
			if !found || len(indent) < len(agentBaseIndent) {
				agentBaseIndent = indent
				found = true
			}
		}
	}
	if !found {
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				agentBaseIndent = getIndentation(l)
				break
			}
		}
	}

	if !found {
		return block // Block is entirely empty lines
	}

	// Adjust all lines
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = "" // clear empty lines
			continue
		}
		trimmed := strings.TrimLeft(l, " \t")
		lineIndent := l[:len(l)-len(trimmed)]

		if lineIndent == "" && agentBaseIndent != "" && isTrulyFlat(trimmed, agentBaseIndent) {
			// Keep intentionally flat lines unindented
			lines[i] = trimmed
		} else if strings.HasPrefix(lineIndent, agentBaseIndent) {
			relIndent := strings.TrimPrefix(lineIndent, agentBaseIndent)
			normRelIndent := normalizeIndent(relIndent, useTabs)
			lines[i] = targetBaseIndent + normRelIndent + trimmed
		} else {
			normIndent := normalizeIndent(lineIndent, useTabs)
			lines[i] = targetBaseIndent + normIndent + trimmed
		}
	}

	return strings.Join(lines, "\n")
}
