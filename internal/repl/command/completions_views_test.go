package command

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestCompletionViewsReactToActiveMode(t *testing.T) {
	engine := commandCompletionEngine(t, NewRegistry().Sources())
	result, err := engine.Complete(t.Context(), "/mode ", len("/mode "))
	if err != nil {
		t.Fatal(err)
	}
	if CompletionViews(api.ModePlan)[result.Source] == nil {
		t.Fatalf("no view registered for source %q", result.Source)
	}

	var updateMode func(api.PermissionMode)
	component := trail.SimpleFC("CommandCompletionViews", func() trail.Node {
		mode, setMode := kitex.UseState(api.ModePlan)
		updateMode = setMode
		return CompletionViews(mode())[result.Source](result, 2)
	})
	backend := mock.New(120, 24)
	inst, err := trail.New(component(), trail.WithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)

	for _, mode := range []api.PermissionMode{api.ModePlan, api.ModeAuto} {
		if updateMode != nil {
			updateMode(mode)
		}
		inst.Frame()
		frame := backend.LastFrame().Surface
		var output strings.Builder
		for y := 0; y < frame.Height; y++ {
			for x := 0; x < frame.Width; x++ {
				output.WriteString(frame.CellAt(x, y).Content)
			}
			output.WriteByte('\n')
		}
		currentRows := 0
		for row := range strings.SplitSeq(output.String(), "\n") {
			if strings.Contains(row, "current") {
				currentRows++
				if !strings.Contains(row, string(mode)) {
					t.Errorf("current marker is not on %q: %s", mode, row)
				}
			}
		}
		if currentRows != 1 {
			t.Errorf("got %d current markers for mode %q:\n%s", currentRows, mode, output.String())
		}
		if !strings.Contains(output.String(), "\u276f plan") {
			t.Errorf("view did not preserve the selected candidate:\n%s", output.String())
		}
	}
}
