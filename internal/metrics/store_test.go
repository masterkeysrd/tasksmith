package metrics_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
)

func TestGetTokenAnalytics(t *testing.T) {
	// Temporarily redirect XDG_DATA_HOME so we don't pollute the real DB
	tempDir := t.TempDir()
	os.Setenv("XDG_DATA_HOME", tempDir)
	defer os.Unsetenv("XDG_DATA_HOME")
	xdg.ClearCache() // Clear XDG cache so it picks up the temp dir

	metricsDB, err := db.InitMetricsDB()
	if err != nil {
		t.Fatalf("failed to init metrics db: %v", err)
	}
	defer metricsDB.Close()

	// Seed some metrics events
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

	store := metrics.NewStore(metricsDB)
	ctx := context.Background()

	res, err := store.GetTokenAnalytics(ctx, "7days", "ALL")
	if err != nil {
		t.Fatalf("failed to get token analytics: %v", err)
	}

	if res.GlobalStats.TotalCalls != 1 {
		t.Errorf("expected 1 global total call, got %d", res.GlobalStats.TotalCalls)
	}

	if res.GlobalStats.TotalSessions != 1 {
		t.Errorf("expected 1 global session, got %d", res.GlobalStats.TotalSessions)
	}

	if res.GlobalStats.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", res.GlobalStats.TotalTokens)
	}

	if len(res.ProvidersList) != 1 || res.ProvidersList[0] != "openai" {
		t.Errorf("expected ProvidersList [openai], got %v", res.ProvidersList)
	}

	if len(res.ByProject) != 1 || res.ByProject[0].ProjectName != "my-project" {
		t.Errorf("expected project my-project, got %v", res.ByProject)
	}

	if len(res.ByAgent) != 1 || res.ByAgent[0].AgentName != "coder" {
		t.Errorf("expected agent coder, got %v", res.ByAgent)
	}

	if len(res.Tools) != 1 || res.Tools[0].ToolName != "run_command" {
		t.Errorf("expected tool run_command, got %v", res.Tools)
	}
}
