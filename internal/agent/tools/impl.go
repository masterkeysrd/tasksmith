package tools

import (
	"fmt"
	"strings"
)

const MaxEditDiffLines = 500

func truncateDiff(diffStr string) (string, string) {
	lines := strings.Split(diffStr, "\n")
	if len(lines) <= MaxEditDiffLines {
		return diffStr, ""
	}
	truncated := strings.Join(lines[:MaxEditDiffLines], "\n")
	return fmt.Sprintf("%s\n\n[SYSTEM NOTE: Diff truncated to save tokens. Showing first %d of %d lines of diff. The full diff was successfully applied to the file.]", truncated, MaxEditDiffLines, len(lines)), diffStr
}
