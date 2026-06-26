package chat

import (
	"context"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type mockClient struct{}

func (m *mockClient) ListProjects(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error) {
	return &api.ListProjectsResponse{}, nil
}

func (m *mockClient) ListAgents(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
	return &api.ListAgentsResponse{}, nil
}

func (m *mockClient) ListProviders(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
	return &api.ListProvidersResponse{}, nil
}

func (m *mockClient) ListProvidersPresets(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error) {
	return &api.ListProvidersPresetsResponse{}, nil
}

func (m *mockClient) ListToolsPresets(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error) {
	return &api.ListToolsPresetsResponse{}, nil
}

func (m *mockClient) InitializeWorkspace(ctx context.Context, req api.InitializeWorkspaceRequest) (*api.InitializeWorkspaceResponse, error) {
	return &api.InitializeWorkspaceResponse{Success: true}, nil
}

func (m *mockClient) GetWorkspaceConfig(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error) {
	return &api.GetWorkspaceConfigResponse{}, nil
}

func (m *mockClient) ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
	return &api.ListSessionsResponse{
		Sessions: []api.Session{
			{ID: "test-session-id", Title: "Test Session"},
		},
	}, nil
}

func (m *mockClient) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error) {
	return &api.CreateSessionResponse{}, nil
}

func (m *mockClient) DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error) {
	return &api.DeleteSessionResponse{Success: true}, nil
}

func (m *mockClient) RenameSession(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error) {
	return &api.RenameSessionResponse{Success: true}, nil
}

func (m *mockClient) ArchiveSession(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error) {
	return &api.ArchiveSessionResponse{Success: true}, nil
}

func (m *mockClient) SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error) {
	return &api.SendMessageResponse{Success: true}, nil
}

func (m *mockClient) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{
		Messages: []string{
			`{"role":"user","content":[{"type":"text","text":"hello"}]}`,
		},
	}, nil
}

func (m *mockClient) GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	return &api.GetSessionStateResponse{Status: "idle"}, nil
}

func (m *mockClient) SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error) {
	return &api.SubmitAuthorizationDecisionResponse{Success: true}, nil
}

func (m *mockClient) ResolveMcpRequest(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error) {
	return &api.ResolveMcpResponse{Success: true}, nil
}

func (m *mockClient) GetTokenAnalytics(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error) {
	return &api.GetTokenAnalyticsResponse{}, nil
}

func (m *mockClient) ConfigureLsp(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error) {
	return &api.ConfigureLspResponse{Success: true}, nil
}

func (m *mockClient) DismissLspSuggestion(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error) {
	return &api.DismissLspSuggestionResponse{Success: true}, nil
}

func (m *mockClient) GetLspStatus(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error) {
	return &api.GetLspStatusResponse{}, nil
}

func (m *mockClient) RestartLsp(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error) {
	return &api.RestartLspResponse{Success: true}, nil
}

func (m *mockClient) RestartMcp(ctx context.Context, req api.RestartMcpRequest) (*api.RestartMcpResponse, error) {
	return &api.RestartMcpResponse{Success: true}, nil
}

func (m *mockClient) GetLspDiagnosticCounts(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error) {
	return &api.GetLspDiagnosticCountsResponse{}, nil
}

func (m *mockClient) GetLspDiagnostics(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error) {
	return &api.GetLspDiagnosticsResponse{}, nil
}

func (m *mockClient) LspSearch(ctx context.Context, req api.LspSearchRequest) (*api.LspSearchResponse, error) {
	return &api.LspSearchResponse{}, nil
}

func (m *mockClient) GetFileChanges(ctx context.Context, req api.GetFileChangesRequest) (*api.GetFileChangesResponse, error) {
	return &api.GetFileChangesResponse{}, nil
}

func (m *mockClient) GetFileJournal(ctx context.Context, req api.GetFileJournalRequest) (*api.GetFileJournalResponse, error) {
	return &api.GetFileJournalResponse{}, nil
}

func (m *mockClient) RevertFile(ctx context.Context, req api.RevertFileRequest) (*api.RevertFileResponse, error) {
	return &api.RevertFileResponse{Success: true}, nil
}

func (m *mockClient) GetCachedFile(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error) {
	return &api.GetCachedFileResponse{Content: "mock cached file content"}, nil
}

func (m *mockClient) GetMcpStatus(ctx context.Context, req api.GetMcpStatusRequest) (*api.GetMcpStatusResponse, error) {
	return &api.GetMcpStatusResponse{}, nil
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
			Value:    "hello",
			Disabled: false,
			IsInsert: true,
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
}

type mockClientWithTools struct {
	mockClient
}

func (m *mockClientWithTools) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{
		Messages: []string{
			`{"role":"user","content":[{"type":"text","text":"Run tool"}]}`,
			`{"role":"assistant","content":[{"type":"text","text":"Thinking..."},{"type":"tool_call","id":"call-1","name":"bash","args":{"CommandLine":"echo hello"}},{"type":"tool_call","id":"call-2","name":"view_file","args":{"AbsolutePath":"/path/to/file.go"}}]}`,
			`{"role":"tool","tool_call_id":"call-1","name":"bash","content":[{"type":"text","text":"hello\n"}]}`,
			`{"role":"tool","tool_call_id":"call-2","name":"view_file","content":[{"type":"text","text":"package main"}]}`,
			`{"role":"user","content":[{"type":"text","text":"Wake up"}],"metadata":{"is_system_notification":true,"task_id":"task-123","task_name":"run tests","task_status":"completed","exit_code":0}}`,
		},
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
