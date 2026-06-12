package mode

import (
	"testing"
)

func TestModeString(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{Normal, "NORMAL"},
		{Insert, "INSERT"},
		{Command, "COMMAND"},
		{Mode(-1), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("Mode.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestStore(t *testing.T) {
	// Initial state
	if store.Get().current != Normal {
		t.Errorf("expected initial mode Normal, got %v", store.Get().current)
	}

	// Set mode
	Set(Insert)
	if store.Get().current != Insert {
		t.Errorf("expected mode Insert after Set, got %v", store.Get().current)
	}

	Set(Command)
	if store.Get().current != Command {
		t.Errorf("expected mode Command after Set, got %v", store.Get().current)
	}

	// Reset to Normal
	Set(Normal)
}
