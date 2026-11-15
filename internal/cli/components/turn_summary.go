package components

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// turnVerbs is the small rotating set of verbs for completed turns.
var turnVerbs = []string{"Smithed", "Forged", "Crafted", "Tempered", "Worked"}

// turnVerb picks a verb deterministically from the entry ID so a given line
// renders the same way after a reload.
func turnVerb(entryID string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(entryID))
	return turnVerbs[int(h.Sum32())%len(turnVerbs)]
}

// FormatTurnSummary renders the one-line summary of a finished turn, for
// example "Smithed for 42s · ↑12.4k ↓1.8k · $0.03 · done 2:15 PM".
func FormatTurnSummary(rec api.TurnCompletedRecord, entryID string, at time.Time) string {
	elapsed := format.Duration(time.Duration(rec.ElapsedMs) * time.Millisecond)
	var lead string
	switch rec.Status {
	case api.TurnStatusCanceled:
		lead = "Stopped after " + elapsed
	case api.TurnStatusFailed:
		lead = "Failed after " + elapsed
	default:
		lead = turnVerb(entryID) + " for " + elapsed
	}
	parts := []string{lead}
	if rec.InputTokens > 0 || rec.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("↑%s ↓%s", format.Tokens(int(rec.InputTokens)), format.Tokens(int(rec.OutputTokens))))
		parts = append(parts, format.FormatCostNano(rec.CostNano, rec.Unpriced))
	} else if rec.CostNano > 0 || rec.Unpriced {
		parts = append(parts, format.FormatCostNano(rec.CostNano, rec.Unpriced))
	}
	if !at.IsZero() {
		parts = append(parts, "done "+at.Local().Format("3:04 PM"))
	}
	return strings.Join(parts, " · ")
}
