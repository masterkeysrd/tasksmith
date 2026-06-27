package tips

import (
	"testing"
)

func TestTips(t *testing.T) {
	if len(agentTips) == 0 {
		t.Error("expected agentTips registry to be populated")
	}
}
