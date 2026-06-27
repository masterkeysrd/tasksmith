package tools

import (
	"fmt"
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
		return newContent, count, nil
	}

	// STAGE 2: Normalized Whitespace Match (Line-by-line)
	// This catches 90% of agent errors: wrong indentation, missing trailing spaces, etc.
	contentLines := strings.Split(contentNorm, "\n")
	rawTargetLines := strings.Split(targetNorm, "\n")

	// Strip leading/trailing empty lines from target to be forgiving
	startIdx := 0
	for startIdx < len(rawTargetLines) && strings.TrimSpace(rawTargetLines[startIdx]) == "" {
		startIdx++
	}
	endIdx := len(rawTargetLines)
	for endIdx > startIdx && strings.TrimSpace(rawTargetLines[endIdx-1]) == "" {
		endIdx--
	}
	if startIdx >= endIdx {
		return content, 0, nil // Target is effectively empty, 0 matches
	}
	targetLines := rawTargetLines[startIdx:endIdx]

	// Find matches by comparing TrimSpace on each line
	var matchIndices []int
	for i := 0; i <= len(contentLines)-len(targetLines); i++ {
		match := true
		for j := 0; j < len(targetLines); j++ {
			if strings.TrimSpace(contentLines[i+j]) != strings.TrimSpace(targetLines[j]) {
				match = false
				break
			}
		}
		if match {
			matchIndices = append(matchIndices, i)
		}
	}

	if len(matchIndices) > 1 && !replaceAll {
		return content, len(matchIndices), fmt.Errorf("normalized target block matches %d occurrences", len(matchIndices))
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
			adjustedReplLines := strings.Split(adjustedReplacement, "\n")

			// Splice the adjusted replacement lines into the array
			spliced := append(newLines[:matchStart], adjustedReplLines...)
			newLines = append(spliced, newLines[matchEnd:]...)
		}

		return strings.Join(newLines, "\n"), len(matchIndices), nil
	}

	// STAGE 3: Fuzzy SequenceMatcher (Sliding Window)
	// Handles drifted code, minor hallucinations, or typos within lines using difflib.

	// We want to compare against trimmed lines to ignore indentation drift
	trimmedContentLines := make([]string, len(contentLines))
	for i, l := range contentLines {
		trimmedContentLines[i] = strings.TrimSpace(l)
	}
	trimmedTargetLines := make([]string, len(targetLines))
	for i, l := range targetLines {
		trimmedTargetLines[i] = strings.TrimSpace(l)
	}

	bestRatio := 0.0
	bestStart := -1
	bestEnd := -1

	// Slide a window across the file.
	// We allow the window to be slightly larger than the target to account for insertions/deletions.
	windowSize := len(targetLines) + 4
	if windowSize > len(contentLines) {
		windowSize = len(contentLines)
	}

	for i := 0; i < len(contentLines); i++ {
		endIdx := i + windowSize
		if endIdx > len(contentLines) {
			endIdx = len(contentLines)
		}

		windowContent := trimmedContentLines[i:endIdx]
		m := difflib.NewMatcher(windowContent, trimmedTargetLines)
		r := m.Ratio()

		if r > bestRatio {
			bestRatio = r

			// Find exactly where the match starts and ends within this window
			blocks := m.GetMatchingBlocks()
			if len(blocks) >= 2 {
				first := blocks[0]
				last := blocks[len(blocks)-2] // second to last is the final actual match block
				bestStart = i + first.A
				bestEnd = i + last.A + last.Size
			}
		}
	}

	// 0.6 ratio threshold is a safe standard for block replacement confidence
	if bestRatio >= 0.6 && bestStart != -1 {
		origIndent := getIndentation(contentLines[bestStart])
		adjustedReplacement := adjustIndentation(replacementNorm, origIndent)
		adjustedReplLines := strings.Split(adjustedReplacement, "\n")

		newLines := make([]string, len(contentLines))
		copy(newLines, contentLines)

		spliced := append(newLines[:bestStart], adjustedReplLines...)
		newLines = append(spliced, newLines[bestEnd:]...)

		return strings.Join(newLines, "\n"), 1, nil
	}

	fmt.Printf("DEBUG: bestRatio=%v bestStart=%v len(matchIndices)=%v\n", bestRatio, bestStart, len(matchIndices))
	return content, 0, nil // 0 matches found in all stages
}

func getIndentation(line string) string {
	for i, c := range line {
		if c != ' ' && c != '\t' {
			return line[:i]
		}
	}
	return line // entirely whitespace
}

func adjustIndentation(block string, targetBaseIndent string) string {
	lines := strings.Split(block, "\n")
	if len(lines) == 0 {
		return block
	}

	// Find the base indentation of the agent's replacement block
	var agentBaseIndent string
	found := false
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			agentBaseIndent = getIndentation(l)
			found = true
			break
		}
	}

	if !found {
		return block // Block is entirely empty lines
	}

	// If the agent's base indent matches what we want, do nothing
	if agentBaseIndent == targetBaseIndent {
		return block
	}

	// Adjust all lines
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = "" // clear empty lines
			continue
		}
		// If the line starts with the agent's base indent, swap it for targetBaseIndent
		if strings.HasPrefix(l, agentBaseIndent) {
			lines[i] = targetBaseIndent + strings.TrimPrefix(l, agentBaseIndent)
		} else {
			// Best effort fallback: just prepend targetBaseIndent
			lines[i] = targetBaseIndent + strings.TrimLeft(l, " \t")
		}
	}

	return strings.Join(lines, "\n")
}
