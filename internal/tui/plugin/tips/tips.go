package tips

import (
	"math/rand"

	"github.com/masterkeysrd/kite/extras/kitex"
)

var agentTips = []string{
	"Press 'i' to enter Insert Mode, and 'Esc' to return to Normal Mode.",
	"Use the arrow keys or 'j'/'k' to navigate and scroll through the chat logs.",
	"Press ':' to open the TUI command bar and run system actions.",
	"Need a thorough run? Type '/goal' in the chat to launch an overnight task.",
	"Unsure of the design? Run '/grill-me' to align with the agent via an interactive interview.",
	"Taught the agent something new? Use '/learn' to persist the rule for future sessions.",
	"Working on a massive project? Try '/teamwork-preview' to orchestrate multiple subagents.",
	"You can approve, deny, or customize tool arguments directly from the TUI widget.",
	"The sidebar displays active LSP server diagnostics and file modification track logs.",
}

// Use returns a random tip that updates whenever sending transitions to true.
func Use(sending bool) string {
	activeTip, setActiveTip := kitex.UseState("")

	kitex.UseEffect(func() {
		if sending {
			idx := rand.Intn(len(agentTips))
			setActiveTip(agentTips[idx])
		} else {
			setActiveTip("")
		}
	}, []any{sending})

	return activeTip()
}
