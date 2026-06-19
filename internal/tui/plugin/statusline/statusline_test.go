package statusline

import (
	"encoding/json"
	"testing"
)

func TestState(t *testing.T) {
	// Verify that default config is loaded
	state := store.Get()
	if len(state.Config.Left) == 0 {
		t.Error("expected default left config to be initialized")
	}
	if len(state.Config.Right) == 0 {
		t.Error("expected default right config to be initialized")
	}

	// Verify fragment structure marshaling/unmarshaling matches JSON keys
	bytes, err := json.Marshal(state.Config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var parsed Config
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if len(parsed.Left) != len(state.Config.Left) {
		t.Errorf("expected left config length %d, got %d", len(state.Config.Left), len(parsed.Left))
	}
}
