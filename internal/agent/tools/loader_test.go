package tools_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
)

type mockFileStorage struct {
	saved map[string]string
}

func (m *mockFileStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	if m.saved == nil {
		m.saved = make(map[string]string)
	}
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, r); err != nil {
		return "", err
	}
	m.saved[relativePath] = buf.String()
	return "/fake/path/" + relativePath, nil
}

func (m *mockFileStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	if content, ok := m.saved[relativePath]; ok {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	return nil, io.EOF
}

func TestIsToolAllowed(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		allowedMap map[string]bool
		expected   bool
	}{
		{
			name:       "nil allowed map allows everything",
			toolName:   "some_tool",
			allowedMap: nil,
			expected:   true,
		},
		{
			name:       "exact match allowed",
			toolName:   "some_tool",
			allowedMap: map[string]bool{"some_tool": true},
			expected:   true,
		},
		{
			name:       "wildcard match allowed",
			toolName:   "mcp__docker__run",
			allowedMap: map[string]bool{"mcp__docker__*": true},
			expected:   true,
		},
		{
			name:       "wildcard mismatch denied",
			toolName:   "mcp__other__run",
			allowedMap: map[string]bool{"mcp__docker__*": true},
			expected:   false,
		},
		{
			name:       "unlisted tool denied",
			toolName:   "other_tool",
			allowedMap: map[string]bool{"some_tool": true},
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tools.IsToolAllowed(tt.toolName, tt.allowedMap)
			if got != tt.expected {
				t.Errorf("IsToolAllowed(%q, %v) = %v; want %v", tt.toolName, tt.allowedMap, got, tt.expected)
			}
		})
	}
}

func TestProcessMcpOutput(t *testing.T) {
	storage := &mockFileStorage{}

	t.Run("non-mcp tool returns unchanged", func(t *testing.T) {
		tc := &message.ToolCall{ID: "tc-1", Name: "bash"}
		originalMsg := &message.Tool{
			ToolCallID: "tc-1",
			Name:       "bash",
			Content:    message.Content{&message.TextBlock{Text: "small output"}},
		}

		processed := tools.ProcessMcpOutput(context.Background(), tc, originalMsg, storage)
		if processed != originalMsg {
			t.Error("expected original message pointer to be returned unchanged")
		}
	})

	t.Run("small mcp output returned unchanged", func(t *testing.T) {
		tc := &message.ToolCall{ID: "tc-2", Name: "mcp__server__tool"}
		originalMsg := &message.Tool{
			ToolCallID: "tc-2",
			Name:       "mcp__server__tool",
			Content:    message.Content{&message.TextBlock{Text: "small output"}},
		}

		processed := tools.ProcessMcpOutput(context.Background(), tc, originalMsg, storage)
		if processed != originalMsg {
			t.Error("expected original message pointer to be returned unchanged")
		}
	})

	t.Run("large mcp output is truncated and stored", func(t *testing.T) {
		tc := &message.ToolCall{ID: "tc-3", Name: "mcp__server__tool"}
		largeText := strings.Repeat("A", 10000)
		originalMsg := &message.Tool{
			ToolCallID: "tc-3",
			Name:       "mcp__server__tool",
			Content:    message.Content{&message.TextBlock{Text: largeText}},
		}

		processed := tools.ProcessMcpOutput(context.Background(), tc, originalMsg, storage)
		if processed == nil {
			t.Fatal("expected non-nil processed output")
		}

		meta := processed.GetMetadata()
		if meta == nil {
			t.Fatal("expected metadata to be set on truncated message")
		}
		if truncated, ok := meta["truncated"].(bool); !ok || !truncated {
			t.Errorf("expected metadata to mark truncated = true, got: %v", meta["truncated"])
		}
		if path, ok := meta["full_content_path"].(string); !ok || path != "/fake/path/tc-3_mcp_output.txt" {
			t.Errorf("expected metadata full_content_path, got: %v", meta["full_content_path"])
		}

		savedContent := storage.saved["tc-3_mcp_output.txt"]
		if savedContent != largeText {
			t.Errorf("saved content length = %d; want %d", len(savedContent), len(largeText))
		}

		// Ensure content has truncated text block + note
		if len(processed.Content) < 2 {
			t.Fatalf("expected at least 2 content blocks, got: %d", len(processed.Content))
		}
		tb, ok := processed.Content[0].(*message.TextBlock)
		if !ok {
			t.Fatal("expected first block to be a TextBlock")
		}
		if len(tb.Text) != 8000 {
			t.Errorf("truncated text length = %d; want 8000", len(tb.Text))
		}
	})
}
