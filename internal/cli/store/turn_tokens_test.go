package store

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

// turnTokens returns the spinner's tokens for the current turn the same way
// the status components compute them.
func turnTokens() (input, output int64) {
	st := GetStream()
	totals := UsageTotals{}
	u := GetUsage().Totals
	totals.InputTokens, totals.OutputTokens = u.Tokens.Input, u.Tokens.Output
	input = max(0, totals.InputTokens-st.BaseInput) + st.CallInput
	output = max(0, totals.OutputTokens-st.BaseOutput) + st.CallOutput
	return input, output
}

// TestTurnTokensSumFullInputOfEveryCall replays five model calls of a 20k
// prompt each: the turn's input must be 100k, the sum of the full inputs.
func TestTurnTokensSumFullInputOfEveryCall(t *testing.T) {
	ResetStream()
	defer ResetStream()
	SetSession(&api.Session{ID: "sess-1"})
	globalConversation.setSessionID("sess-1")

	// Earlier turns already used 40k input / 1k output.
	SetUsage(api.SessionUsage{SessionID: "sess-1", Totals: api.UsageGroup{Calls: 2, Tokens: api.UsageTokens{Input: 40_000, Output: 1_000}}})

	// The turn starts: the user message is pending and the stream is running.
	globalConversation.applyEvent(api.Event{Type: api.EventStatusChanged, Payload: api.StatusChangedEvent{SessionID: "sess-1", Status: api.StatusThinking}})
	if in, out := turnTokens(); in != 0 || out != 0 {
		t.Fatalf("turn start = %d/%d, want 0/0", in, out)
	}

	calls, totalIn, totalOut := int64(2), int64(40_000), int64(1_000)
	for i := 1; i <= 5; i++ {
		id := "asst-" + string(rune('0'+i))
		// Mid-call chunks carry the call's cumulative usage.
		for _, outSoFar := range []int{100, 300} {
			globalConversation.applyEvent(api.Event{Type: api.EventChunk, Payload: api.ChunkEvent{
				SessionID: "sess-1",
				Chunk: api.Chunk{
					ID:      id,
					Content: []api.Block{{Type: api.BlockText, Text: "x"}},
					Usage:   &api.ChunkUsage{Input: 20_000, Output: outSoFar},
				},
			}})
			in, out := turnTokens()
			wantIn := int64(i) * 20_000
			wantOut := int64(i-1)*300 + int64(outSoFar)
			if in != wantIn || out != wantOut {
				t.Fatalf("call %d mid-stream (out %d) = %d/%d, want %d/%d", i, outSoFar, in, out, wantIn, wantOut)
			}
		}
		// The call finishes: its full 20k input joins the session totals.
		calls++
		totalIn += 20_000
		totalOut += 300
		SetUsage(api.SessionUsage{SessionID: "sess-1", Totals: api.UsageGroup{Calls: calls, Tokens: api.UsageTokens{Input: totalIn, Output: totalOut}}})
		globalConversation.applyEvent(api.Event{Type: api.EventUsageUpdated, Payload: api.UsageUpdatedEvent{
			SessionID: "sess-1",
			Usage:     api.SessionUsage{SessionID: "sess-1", Totals: api.UsageGroup{Calls: calls, Tokens: api.UsageTokens{Input: totalIn, Output: totalOut}}},
		}})
		in, out := turnTokens()
		if in != int64(i)*20_000 || out != int64(i)*300 {
			t.Fatalf("call %d finished = %d/%d, want %d/%d", i, in, out, int64(i)*20_000, int64(i)*300)
		}
	}

	if in, _ := turnTokens(); in != 100_000 {
		t.Fatalf("five 20k calls summed to %d, want 100000", in)
	}
}
