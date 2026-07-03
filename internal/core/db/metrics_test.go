package db_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestMetricsDB(t *testing.T) {
	metricsDB, err := db.InitMetricsDB()
	if err != nil {
		t.Fatalf("failed to init metrics db: %v", err)
	}
	defer metricsDB.Close()

	// Verify the database file was created globally
	dataDir, err := xdg.SubDataDir()
	if err != nil {
		t.Fatalf("failed to get data dir: %v", err)
	}
	dbPath := filepath.Join(dataDir, "metrics.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected database file to be created at %s, but it was not found", dbPath)
	}

	event := db.MetricsEvent{
		SessionID:     "sess-123",
		WorkspacePath: "/path/to/workspace",
		ProjectName:   "my-project",
		AgentName:     "coder",
		CreatedAt:     time.Now(),
	}

	llmPayload := db.LLMCallPayload{
		Provider:         "openai",
		Model:            "gpt-4o",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		EstimatedCostUSD: 0.015,
	}

	if err := db.LogLLMCall(metricsDB, event, llmPayload); err != nil {
		t.Fatalf("failed to log LLM call: %v", err)
	}

	toolPayload := db.ToolCallPayload{
		ToolName:        "run_command",
		ExecutionTimeMs: 120,
		Status:          "success",
		OutputBytes:     500,
		OutputTokens:    125,
	}

	if err := db.LogToolCall(metricsDB, event, toolPayload); err != nil {
		t.Fatalf("failed to log tool call: %v", err)
	}

	// Verify insertion
	var count int
	if err := metricsDB.Get(&count, "SELECT COUNT(*) FROM metrics_events"); err != nil {
		t.Fatalf("failed to query metrics events: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}
}
