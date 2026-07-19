package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type mockClient struct {
	tuiapi.MockClient
}

func (m *mockClient) ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
	return &api.ListSessionsResponse{
		Sessions: []api.Session{
			{ID: "test-session-id", Title: "Test Session"},
		},
	}, nil
}

func (m *mockClient) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{
		Messages: message.MessageList{
			message.NewUserText("hello"),
		},
	}, nil
}

func (m *mockClient) GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	return &api.GetSessionStateResponse{Status: "idle"}, nil
}

func (m *mockClient) GetCachedFile(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error) {
	return &api.GetCachedFileResponse{Content: "mock cached file content"}, nil
}

func TestChatView(t *testing.T) {
	thm := &theme.Scheme{}
	client := &mockClient{}
	windClient := wind.NewClient()

	render := func(node kitex.Node) kitex.Node {
		return wind.Provider(wind.ProviderProps{Client: windClient},
			tuiapi.Provider(tuiapi.Props{Client: client},
				theme.Provider(theme.Props{Theme: thm}, node),
			),
		)
	}

	t.Run("RenderChatView", func(t *testing.T) {
		node := render(View(ViewProps{
			SessionID: "test-session-id",
		}))
		if node == nil {
			t.Fatal("Chat view returned nil node")
		}
	})

	t.Run("RenderComposer", func(t *testing.T) {
		node := render(Composer(ComposerProps{
			Value:     "hello",
			Disabled:  false,
			IsInsert:  true,
			SessionID: "test-session-id",
		}))
		if node == nil {
			t.Fatal("Composer returned nil node")
		}
	})

	t.Run("RenderChatViewWithTools", func(t *testing.T) {
		c := &mockClientWithTools{}
		renderWithTools := func(node kitex.Node) kitex.Node {
			return wind.Provider(wind.ProviderProps{Client: windClient},
				tuiapi.Provider(tuiapi.Props{Client: c},
					theme.Provider(theme.Props{Theme: thm}, node),
				),
			)
		}
		node := renderWithTools(View(ViewProps{
			SessionID: "test-session-id",
		}))
		if node == nil {
			t.Fatal("Chat view with tools returned nil node")
		}
	})

	t.Run("RenderChatViewWithDeniedTool", func(t *testing.T) {
		c := &mockClientWithDeniedTool{}
		renderWithDenied := func(node kitex.Node) kitex.Node {
			return wind.Provider(wind.ProviderProps{Client: windClient},
				tuiapi.Provider(tuiapi.Props{Client: c},
					theme.Provider(theme.Props{Theme: thm}, node),
				),
			)
		}
		node := renderWithDenied(View(ViewProps{
			SessionID: "test-session-id",
		}))
		if node == nil {
			t.Fatal("Chat view with denied tool returned nil node")
		}
	})
}

func parseJSONMessages(strs []string) message.MessageList {
	var list message.MessageList
	if len(strs) > 0 {
		rawArray := "[" + strings.Join(strs, ",") + "]"
		_ = json.Unmarshal([]byte(rawArray), &list)
	}
	return list
}

type mockClientWithTools struct {
	mockClient
}

func (m *mockClientWithTools) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{
		Messages: parseJSONMessages([]string{
			`{"role":"user","content":[{"type":"text","text":"Run tool"}]}`,
			`{"role":"assistant","content":[{"type":"text","text":"Thinking..."},{"type":"tool_call","id":"call-1","name":"bash","args":{"CommandLine":"echo hello"}},{"type":"tool_call","id":"call-2","name":"view_file","args":{"AbsolutePath":"/path/to/file.go"}}]}`,
			`{"role":"tool","tool_call_id":"call-1","name":"bash","content":[{"type":"text","text":"hello\n"}]}`,
			`{"role":"tool","tool_call_id":"call-2","name":"view_file","content":[{"type":"text","text":"package main"}]}`,
			`{"role":"user","content":[{"type":"text","text":"Wake up"}],"metadata":{"is_system_notification":true,"task_id":"task-123","task_name":"run tests","task_status":"completed","exit_code":0}}`,
		}),
	}, nil
}

type mockClientWithDeniedTool struct {
	mockClient
}

func (m *mockClientWithDeniedTool) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{
		Messages: parseJSONMessages([]string{
			`{"role":"user","content":[{"type":"text","text":"Run tool"}]}`,
			`{"role":"assistant","content":[{"type":"tool_call","id":"call-denied","name":"write","args":{"path":"test.txt"}}]}`,
			`{"role":"tool","tool_call_id":"call-denied","name":"write","is_error":true,"content":[{"type":"text","text":"Authorization denied by user for tool \"write\": security policy"}],"metadata":{"deny_reason":"security policy"}}`,
		}),
	}, nil
}

func TestParseRangeFromHeader(t *testing.T) {
	tests := []struct {
		input     string
		wantStart int
		wantEnd   int
	}{
		{
			input:     "README.md (1-100 of 100)\n1 | line1\n2 | line2",
			wantStart: 1,
			wantEnd:   100,
		},
		{
			input:     "main.go (15-45 of 200)\n15 | main",
			wantStart: 15,
			wantEnd:   45,
		},
		{
			input:     "no_paren_match\n",
			wantStart: 0,
			wantEnd:   0,
		},
	}

	for _, tc := range tests {
		gotStart, gotEnd := parseRangeFromHeader(tc.input)
		if gotStart != tc.wantStart || gotEnd != tc.wantEnd {
			t.Errorf("parseRangeFromHeader(%q) = (%d, %d), want (%d, %d)", tc.input, gotStart, gotEnd, tc.wantStart, tc.wantEnd)
		}
	}
}

func TestParseViewStructuredOutput(t *testing.T) {
	t.Run("tools.ViewOutput type assertion", func(t *testing.T) {
		val := tools.ViewOutput{
			Content:   "test",
			StartLine: 5,
		}
		got, ok := parseViewStructuredOutput(val)
		if !ok || got.Content != "test" || got.StartLine != 5 {
			t.Errorf("expected view output, got %v (ok: %v)", got, ok)
		}
	})

	t.Run("map representation conversion", func(t *testing.T) {
		val := map[string]any{
			"content":    "test2",
			"start_line": float64(10),
		}
		got, ok := parseViewStructuredOutput(val)
		if !ok || got.Content != "test2" || got.StartLine != 10 {
			t.Errorf("expected conversion, got %v (ok: %v)", got, ok)
		}
	})
}

func TestStripLinePrefixes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "1 | line1\n2 | line2\n3 | line3",
			want:  "line1\nline2\nline3",
		},
		{
			input: "15 | func main() {\n16 | \tprintln()\n17 | }",
			want:  "func main() {\n\tprintln()\n}",
		},
		{
			input: "no_prefix\n10 | prefixed",
			want:  "no_prefix\nprefixed",
		},
	}

	for _, tc := range tests {
		got := stripLinePrefixes(tc.input)
		if got != tc.want {
			t.Errorf("stripLinePrefixes(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func BenchmarkSwitchSessions(b *testing.B) {
	thm := &theme.Scheme{}
	client := &mockClient{}
	windClient := wind.NewClient()

	env := testenv.Default(120, 40)
	defer env.Close()

	container := element.NewBox(env.Document())
	env.Mount(container)

	activeSessionID := "session-1"

	node := wind.Provider(wind.ProviderProps{Client: windClient},
		tuiapi.Provider(tuiapi.Props{Client: client},
			theme.Provider(theme.Props{Theme: thm},
				kitex.SimpleFC("TestRoot", func() kitex.Node {
					sessID := active.UseSessionID()
					return View(ViewProps{SessionID: sessID})
				})(),
			),
		),
	)

	kitex.Render(node, container)
	env.Flush()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if activeSessionID == "session-1" {
			activeSessionID = "session-2"
		} else {
			activeSessionID = "session-1"
		}
		active.SetSessionID(activeSessionID)
		env.Flush()
	}
}
