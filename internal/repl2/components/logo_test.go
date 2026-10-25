package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
)

func TestLogoRender(t *testing.T) {
	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Logo(), trail.WithBackend(b))
	}()

	var text string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text = surfaceText(b)
		if strings.Contains(text, "◥██████████◣") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	for _, want := range []string{
		"◥██████████◣",
		"▐██▌",
		"◢████◣",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("logo output missing %q, got:\n%s", want, text)
		}
	}

	frame := b.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected rendered surface")
	}

	// Verify line 0 has top bar color
	cell0 := frame.CellAt(2, 0)
	if cell0.Fg != logoColorTop {
		t.Errorf("expected cell (2, 0) foreground %v, got %v", logoColorTop, cell0.Fg)
	}

	// Verify line 1 has center pillar color
	cell1 := frame.CellAt(6, 1)
	if cell1.Fg != logoColorCenter {
		t.Errorf("expected cell (6, 1) foreground %v, got %v", logoColorCenter, cell1.Fg)
	}

	// Verify line 2 has base color
	cell2 := frame.CellAt(6, 2)
	if cell2.Fg != logoColorBase {
		t.Errorf("expected cell (6, 2) foreground %v, got %v", logoColorBase, cell2.Fg)
	}
}
