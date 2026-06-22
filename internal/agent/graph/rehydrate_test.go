package graph

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
)

func TestRehydrateMessagesForLLM(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-rehydrate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dummyBytes := []byte("image-content-data")
	filePath := filepath.Join(tmpDir, "logo.png")
	if err := os.WriteFile(filePath, dummyBytes, 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	userMsg := message.NewUserText("What is this image?")

	// A binary view tool message with data initially nil
	binaryToolMsg := &message.Tool{
		ToolCallID: "call-bin-1",
		Name:       "view",
		Content: message.Content{
			&message.ImageBlock{
				MIMEType: "image/png",
				Data:     nil,
			},
		},
		StructuredContent: map[string]any{
			"source":      filePath,
			"cached_path": filePath,
			"mime_type":   "image/png",
			"is_binary":   true,
		},
	}

	// A non-binary view tool message
	textToolMsg := &message.Tool{
		ToolCallID: "call-text-1",
		Name:       "view",
		Content: message.Content{
			&message.TextBlock{
				Text: "1 | some text line",
			},
		},
		StructuredContent: map[string]any{
			"source":    "info.txt",
			"mime_type": "text/plain",
			"is_binary": false,
		},
	}

	messages := []message.Message{userMsg, binaryToolMsg, textToolMsg}

	// Run re-hydration
	rehydrated := tools.RehydrateMessagesForLLM(messages)

	if len(rehydrated) != len(messages) {
		t.Fatalf("expected length %d, got %d", len(messages), len(rehydrated))
	}

	// 1. Verify user message is unchanged (pointer equality)
	if rehydrated[0] != userMsg {
		t.Errorf("expected user message to be unchanged, got different pointer")
	}

	// 2. Verify text tool message is unchanged (pointer equality)
	if rehydrated[2] != textToolMsg {
		t.Errorf("expected text tool message to be unchanged, got different pointer")
	}

	// 3. Verify binary tool message was cloned (different pointer)
	if rehydrated[1] == binaryToolMsg {
		t.Errorf("expected binary tool message to be cloned, but got same pointer")
	}

	clonedToolMsg, ok := rehydrated[1].(*message.Tool)
	if !ok {
		t.Fatalf("expected *message.Tool, got %T", rehydrated[1])
	}

	// Verify the original binary message was NOT modified (remains nil)
	origImageBlock := binaryToolMsg.Content[0].(*message.ImageBlock)
	if origImageBlock.Data != nil {
		t.Errorf("expected original image block data to remain nil, but it was mutated")
	}

	// Verify the cloned message has the data re-hydrated
	clonedImageBlock, ok := clonedToolMsg.Content[0].(*message.ImageBlock)
	if !ok {
		t.Fatalf("expected cloned block to be *message.ImageBlock, got %T", clonedToolMsg.Content[0])
	}
	if !bytes.Equal(clonedImageBlock.Data, dummyBytes) {
		t.Errorf("expected cloned image block to have re-hydrated data %q, got %q", string(dummyBytes), string(clonedImageBlock.Data))
	}
}

func TestRehydrateRemoveTool(t *testing.T) {
	removeToolMsg := &message.Tool{
		ToolCallID: "call-remove-1",
		Name:       "remove",
		Content: message.Content{
			&message.TextBlock{
				Text: "Successfully removed ./file.txt",
			},
		},
		StructuredContent: tools.RemoveOutput{
			Path:    "./file.txt",
			Success: true,
			Content: "file content that should be removed to save tokens",
		},
	}

	rehydrated := tools.RehydrateMessagesForLLM([]message.Message{removeToolMsg})
	if len(rehydrated) != 1 {
		t.Fatalf("expected 1 message, got %d", len(rehydrated))
	}

	cloned, ok := rehydrated[0].(*message.Tool)
	if !ok {
		t.Fatalf("expected *message.Tool, got %T", rehydrated[0])
	}

	out, ok := cloned.StructuredContent.(tools.RemoveOutput)
	if !ok {
		t.Fatalf("expected tools.RemoveOutput in StructuredContent, got %T", cloned.StructuredContent)
	}

	if out.Content != "" {
		t.Errorf("expected Content field to be blanked out, got %q", out.Content)
	}
	if out.Path != "./file.txt" {
		t.Errorf("expected Path to remain './file.txt', got %q", out.Path)
	}

	// Verify the original message was NOT modified (deep copy / no mutation of original structured content)
	origOut := removeToolMsg.StructuredContent.(tools.RemoveOutput)
	if origOut.Content == "" {
		t.Error("expected original message to preserve Content field, but it was mutated")
	}
}
