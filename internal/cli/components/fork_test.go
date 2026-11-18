package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestTimelineEventForked(t *testing.T) {
	fromEntry := api.TimelineEntry{
		ID:   "tl-fork-1",
		Kind: api.TimelineKindForked,
		Forked: &api.ForkRecord{
			Direction: api.ForkDirectionFrom,
			SessionID: "sess-src",
			Title:     "Refactor parser",
		},
	}
	text := testutils.Render(t, TimelineEvent(fromEntry), 80, 24)
	if !strings.Contains(text, `Forked from "Refactor parser"`) {
		t.Fatalf("expected forked_from notice text, got:\n%s", text)
	}

	intoEntry := api.TimelineEntry{
		ID:   "tl-fork-2",
		Kind: api.TimelineKindForked,
		Forked: &api.ForkRecord{
			Direction: api.ForkDirectionInto,
			SessionID: "sess-fork",
			Title:     "Refactor parser (fork)",
		},
	}
	text = testutils.Render(t, TimelineEvent(intoEntry), 80, 24)
	if !strings.Contains(text, `Forked into "Refactor parser (fork)"`) {
		t.Fatalf("expected forked_into notice text, got:\n%s", text)
	}

	untitled := api.TimelineEntry{
		ID:   "tl-fork-3",
		Kind: api.TimelineKindForked,
		Forked: &api.ForkRecord{
			Direction: api.ForkDirectionInto,
			SessionID: "sess-fork",
		},
	}
	text = testutils.Render(t, TimelineEvent(untitled), 80, 24)
	if !strings.Contains(text, "Forked into a new session") {
		t.Fatalf("expected untitled fork fallback text, got:\n%s", text)
	}

	empty := testutils.Render(t, TimelineEvent(api.TimelineEntry{
		ID:   "tl-fork-4",
		Kind: api.TimelineKindForked,
	}), 80, 24)
	if strings.TrimSpace(empty) != "" {
		t.Fatalf("expected empty render for payload-less fork entry, got:\n%s", empty)
	}
}
