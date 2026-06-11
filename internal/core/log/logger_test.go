package log

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	t.Run("Info", func(t *testing.T) {
		buf.Reset()
		logger.Info("test message", String("key", "value"))
		
		var out map[string]any
		if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("failed to unmarshal log output: %v", err)
		}
		
		if out["msg"] != "test message" {
			t.Errorf("expected msg 'test message', got %v", out["msg"])
		}
		if out["key"] != "value" {
			t.Errorf("expected key 'value', got %v", out["key"])
		}
		if out["level"] != "INFO" {
			t.Errorf("expected level 'INFO', got %v", out["level"])
		}
	})

	t.Run("ForComponent", func(t *testing.T) {
		buf.Reset()
		compLogger := logger.ForComponent("test-comp")
		compLogger.Info("comp message")
		
		var out map[string]any
		if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("failed to unmarshal log output: %v", err)
		}
		
		if out["component"] != "test-comp" {
			t.Errorf("expected component 'test-comp', got %v", out["component"])
		}
	})

	t.Run("SetDefault", func(t *testing.T) {
		var defaultBuf bytes.Buffer
		SetDefault(&defaultBuf)
		Info("default message")

		var out map[string]any
		if err := json.Unmarshal(defaultBuf.Bytes(), &out); err != nil {
			t.Fatalf("failed to unmarshal log output: %v", err)
		}

		if out["msg"] != "default message" {
			t.Errorf("expected msg 'default message', got %v", out["msg"])
		}
	})
}
