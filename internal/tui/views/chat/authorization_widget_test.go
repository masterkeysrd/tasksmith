package chat

import (
	"context"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type recordingMockClient struct {
	mockClient
	LastRequest *api.SubmitAuthorizationDecisionRequest
}

func (m *recordingMockClient) SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error) {
	m.LastRequest = &req
	return &api.SubmitAuthorizationDecisionResponse{Success: true}, nil
}

func TestAuthorizationWidgetMultiPage(t *testing.T) {
	thm := &theme.Scheme{}
	client := &recordingMockClient{}
	windClient := wind.NewClient()

	req := permissions.AuthorizationRequest{
		ToolCallID:  "test-tool-call",
		ToolName:    "bash",
		Description: "Run multi-command pipeline",
		Payload:     map[string]any{"command": "git add . && git commit"},
		GrantRequests: []permissions.PermissionGrantRequest{
			{
				ID:          "cmd_1",
				Description: "git add .",
				Options: []permissions.PermissionOption{
					{
						Label:       "Allow exact: git add .",
						Target:      "git add .",
						MatchMethod: "exact",
						Action:      permissions.ActionAllow,
					},
				},
				AllowedScopes: []permissions.PermissionScope{permissions.ScopeOnce},
			},
			{
				ID:          "cmd_2",
				Description: "git commit",
				Options: []permissions.PermissionOption{
					{
						Label:       "Allow exact: git commit",
						Target:      "git commit",
						MatchMethod: "exact",
						Action:      permissions.ActionAllow,
					},
				},
				AllowedScopes: []permissions.PermissionScope{permissions.ScopeOnce},
			},
		},
	}

	// Create test environment
	env := testenv.Default(120, 40)
	defer env.Close()

	container := element.NewBox(env.Document())
	env.Mount(container)

	var lastDecision *permissions.AuthorizationDecision
	onDecision := func(dec permissions.AuthorizationDecision) {
		lastDecision = &dec
	}

	node := wind.Provider(wind.ProviderProps{Client: windClient},
		tuiapi.Provider(tuiapi.Props{Client: client},
			theme.Provider(theme.Props{Theme: thm},
				AuthorizationWidget(AuthorizationWidgetProps{
					Request:    req,
					SessionID:  "test-session",
					IsActive:   true,
					OnDecision: onDecision,
				}),
			),
		),
	)

	kitex.Render(node, container)
	env.Flush()

	// Initially, it should be active and display the first command
	if AuthCtrl.ActiveToolCallID != "test-tool-call" {
		t.Fatalf("expected active tool call ID %q, got %q", "test-tool-call", AuthCtrl.ActiveToolCallID)
	}

	// Simulate pressing Approve (which calls handleApprove)
	if AuthCtrl.Approve == nil {
		t.Fatal("expected Approve handler to be registered")
	}

	// Let's click approve for page 1
	AuthCtrl.Approve()
	env.Flush()

	// Since there is a second page, OnDecision should not have been called yet
	if lastDecision != nil {
		t.Fatal("expected OnDecision to not be called after first page approval")
	}

	// Now Approve is for page 2. Let's call it again
	AuthCtrl.Approve()
	env.Flush()

	// Now OnDecision should have been called with both decisions!
	if lastDecision == nil {
		t.Fatal("expected OnDecision to be called after last page approval")
	}

	if !lastDecision.Approved {
		t.Fatal("expected decision to be approved")
	}

	if len(lastDecision.GrantDecisions) != 2 {
		t.Fatalf("expected 2 grant decisions, got %d", len(lastDecision.GrantDecisions))
	}

	dec1 := lastDecision.GrantDecisions[0]
	if dec1.RequestID != "cmd_1" || dec1.SelectedTarget != "git add ." {
		t.Errorf("unexpected first grant decision: %+v", dec1)
	}

	dec2 := lastDecision.GrantDecisions[1]
	if dec2.RequestID != "cmd_2" || dec2.SelectedTarget != "git commit" {
		t.Errorf("unexpected second grant decision: %+v", dec2)
	}
}
