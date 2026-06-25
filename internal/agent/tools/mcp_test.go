package tools

import (
	"context"
	"testing"
)

func TestMcpListResourcesNilManager(t *testing.T) {
	ctx := context.Background()
	handlers := NewHandlers(nil, "")

	out, err := handlers.McpListResources(ctx, McpListResourcesArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Success {
		t.Error("expected success to be false when manager is nil")
	}
	if out.Error != "No MCP manager configured" {
		t.Errorf("expected error 'No MCP manager configured', got %q", out.Error)
	}
}

func TestMcpReadResourcesNilManager(t *testing.T) {
	ctx := context.Background()
	handlers := NewHandlers(nil, "")

	out, err := handlers.McpReadResources(ctx, McpReadResourcesArgs{Uri: "myserver://some/resource"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Success {
		t.Error("expected success to be false when manager is nil")
	}
	if out.Content != "No MCP manager configured" {
		t.Errorf("expected content 'No MCP manager configured', got %q", out.Content)
	}
}
