package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/geom"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

type appTestClient struct {
	tuiapi.Client
}

func (m *appTestClient) ListProjects(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error) {
	return &api.ListProjectsResponse{
		Projects: []api.Project{
			{Name: "tasksmith", DisplayName: "TaskSmith", Path: "/Users/test/tasksmith"},
		},
	}, nil
}

func (m *appTestClient) ListAgents(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
	return &api.ListAgentsResponse{
		Agents: []api.Agent{
			{Name: "main", Description: "Main agent"},
		},
	}, nil
}

func (m *appTestClient) ListProviders(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
	return &api.ListProvidersResponse{
		Providers: []api.Provider{
			{
				Name:        "anthropic",
				DisplayName: "Anthropic",
				Models: []api.Model{
					{ID: "claude-sonnet-5", Name: "claude-sonnet-5", Label: "Claude Sonnet 5"},
				},
			},
		},
	}, nil
}

func (m *appTestClient) ListProvidersPresets(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error) {
	return &api.ListProvidersPresetsResponse{
		Providers: []api.Provider{
			{
				Name:         "anthropic",
				DisplayName:  "Anthropic",
				Endpoint:     "https://api.anthropic.com",
				DefaultModel: "claude-sonnet-5",
				Models: []api.Model{
					{ID: "claude-sonnet-5", Name: "claude-sonnet-5", Label: "Claude Sonnet 5"},
				},
			},
		},
	}, nil
}

func (m *appTestClient) ListToolsPresets(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error) {
	return &api.ListToolsPresetsResponse{}, nil
}

func (m *appTestClient) GetWorkspaceConfig(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error) {
	return &api.GetWorkspaceConfigResponse{
		Name:            "tasksmith",
		DefaultProvider: "anthropic",
		HasManifest:     true,
		IsTrusted:       false,
		AuthorizedTools: map[string]bool{"bash": true},
	}, nil
}

func (m *appTestClient) ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
	return &api.ListSessionsResponse{}, nil
}

func renderAppForTest(t *testing.T) *testenv.Environment {
	t.Helper()

	active.SetSessionID("")
	active.SetScreen("chat")
	active.SetModal("")

	env := testenv.Default(140, 50)
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(App(AppProps{Client: &appTestClient{}}), container)
	return env
}

func flushUntilContains(t *testing.T, env *testenv.Environment, want string) {
	t.Helper()

	for range 12 {
		env.Flush()
		if strings.Contains(env.Document().TextContent(), want) {
			return
		}
	}

	t.Fatalf("expected screen to contain %q, got:\n%s", want, env.Document().TextContent())
}

func findButtonByText(node dom.Node, text string) element.Element {
	if el, ok := node.(element.Element); ok && strings.EqualFold(el.TagName(), "button") && strings.Contains(el.TextContent(), text) {
		return el
	}

	for child := range node.ChildNodes() {
		if found := findButtonByText(child, text); found != nil {
			return found
		}
	}

	return nil
}

func clickButtonByText(t *testing.T, env *testenv.Environment, text string) {
	t.Helper()

	btn := findButtonByText(env.Document(), text)
	if btn == nil {
		t.Fatalf("could not find button containing %q in:\n%s", text, env.Document().TextContent())
	}

	btn.DispatchEvent(event.NewMouseEvent(event.EventClick, geom.Point{}, event.ButtonLeft, 0))
	env.Flush()
}

func TestTrustImportRunSetupWizardRemainsInteractive(t *testing.T) {
	env := renderAppForTest(t)
	defer env.Close()

	flushUntilContains(t, env, "WORKSPACE_TRUST_VERIFICATION")
	clickButtonByText(t, env, "RUN SETUP WIZARD")
	flushUntilContains(t, env, "CONTINUE SETUP")

	clickButtonByText(t, env, "CONTINUE SETUP")
	flushUntilContains(t, env, "CONFIGURE MODEL PROVIDERS")
}

func TestDeclineToWelcomeRemainsInteractive(t *testing.T) {
	env := renderAppForTest(t)
	defer env.Close()

	flushUntilContains(t, env, "WORKSPACE_TRUST_VERIFICATION")
	clickButtonByText(t, env, "DECLINE")
	flushUntilContains(t, env, "BOOT_SEQUENCE_DENIED")

	clickButtonByText(t, env, "IGNORE & RUN AD-HOC")
	flushUntilContains(t, env, "TaskSmith")

	clickButtonByText(t, env, "Setup Wizard")
	flushUntilContains(t, env, "WORKSPACE_TRUST_VERIFICATION")
}
