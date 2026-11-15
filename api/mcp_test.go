package api

import (
	"strings"
	"testing"
)

func TestCleanMCPErrorStripsNestedTransportWrappers(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "streamable http rejection",
			in:   `connect MCP server "github": sending "initialize": rejected by transport: 401 Unauthorized`,
			want: "401 Unauthorized",
		},
		{
			name: "calling wrapper",
			in:   `connect MCP server "github": calling "initialize": unauthorized`,
			want: "unauthorized",
		},
		{
			name: "auth sentinel and handler wrappers",
			in:   `connect MCP server "github": failed to get auth handler: mcp authentication required: missing credential environment variable "GITHUB_TOKEN"`,
			want: `missing credential environment variable "GITHUB_TOKEN"`,
		},
		{
			name: "daemon static auth wrapper",
			in:   `MCP server "github": missing credential environment variable "GITHUB_TOKEN"`,
			want: `missing credential environment variable "GITHUB_TOKEN"`,
		},
		{
			name: "api error envelope wrapping transport error",
			in:   `api: bad_gateway (502): connect MCP server "notes": sending "initialize": connection refused`,
			want: "connection refused",
		},
		{name: "already clean", in: "connection refused", want: "connection refused"},
		{name: "empty", in: "   ", want: ""},
		{
			name: "unterminated quote is left alone",
			in:   `connect MCP server "github`,
			want: `connect MCP server "github`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CleanMCPError(tc.in); got != tc.want {
				t.Errorf("CleanMCPError(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanMCPErrorIsIdempotent(t *testing.T) {
	once := CleanMCPError(`connect MCP server "github": sending "initialize": rejected by transport: 401 Unauthorized`)
	if twice := CleanMCPError(once); twice != once {
		t.Errorf("second pass changed the message: %q -> %q", once, twice)
	}
}

func TestMCPSchemeUsesOAuth(t *testing.T) {
	oauth := []string{"oauth2", "client-credentials", "enterprise", " OAuth2 "}
	for _, scheme := range oauth {
		if !MCPSchemeUsesOAuth(scheme) {
			t.Errorf("MCPSchemeUsesOAuth(%q) = false, want true", scheme)
		}
	}
	static := []string{"", "bearer", "api-key", "basic"}
	for _, scheme := range static {
		if MCPSchemeUsesOAuth(scheme) {
			t.Errorf("MCPSchemeUsesOAuth(%q) = true, want false", scheme)
		}
	}
}

func TestMCPServerWarning(t *testing.T) {
	authServer := MCPServer{
		Name:     "linear",
		Status:   MCPStatusAuthRequired,
		AuthType: MCPAuthOAuth2,
	}
	if got := MCPServerWarning(authServer); got != `MCP server "linear" requires authentication — run /mcp auth linear` {
		t.Errorf("oauth auth_required warning = %q", got)
	}

	pendingURL := MCPServer{Name: "linear", Status: MCPStatusAuthRequired, AuthURL: "https://auth.example/authorize"}
	if got := MCPServerWarning(pendingURL); !strings.Contains(got, "run /mcp auth linear") {
		t.Errorf("pending authorization URL warning = %q", got)
	}

	missingToken := MCPServer{
		Name:   "github",
		Status: MCPStatusAuthRequired,
		Error:  `connect MCP server "github": failed to get auth handler: mcp authentication required: missing credential environment variable "GITHUB_TOKEN"`,
	}
	got := MCPServerWarning(missingToken)
	if !strings.Contains(got, `missing credential environment variable "GITHUB_TOKEN"`) {
		t.Errorf("static credential warning should name the credential: %q", got)
	}
	if !strings.Contains(got, "run /mcp restart github") {
		t.Errorf("static credential warning should advise a restart: %q", got)
	}
	if strings.Contains(got, "rejected by transport") || strings.Contains(got, "connect MCP server") {
		t.Errorf("warning leaked transport wrappers: %q", got)
	}

	bareAuth := MCPServer{Name: "legacy", Status: MCPStatusAuthRequired}
	if got := MCPServerWarning(bareAuth); !strings.Contains(got, "run /mcp to inspect") {
		t.Errorf("bare auth_required warning = %q", got)
	}

	broken := MCPServer{Name: "notes", Status: MCPStatusError, Error: `connect MCP server "notes": calling "initialize": connection refused`}
	if got := MCPServerWarning(broken); got != `MCP server "notes" failed to connect (connection refused) — run /mcp to inspect` {
		t.Errorf("error warning = %q", got)
	}

	for _, status := range []MCPStatus{MCPStatusConnected, MCPStatusConnecting, MCPStatusDisconnected, ""} {
		if got := MCPServerWarning(MCPServer{Name: "notes", Status: status}); got != "" {
			t.Errorf("status %q should not warn, got %q", status, got)
		}
	}
}

func TestMCPServerWarningsSkipsHealthyServers(t *testing.T) {
	servers := []MCPServer{
		{Name: "healthy", Status: MCPStatusConnected},
		{Name: "github", Status: MCPStatusAuthRequired},
		{Name: "notes", Status: MCPStatusError, Error: "connection refused"},
	}
	warnings := MCPServerWarnings(servers)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "github") || !strings.Contains(warnings[1], "notes") {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if got := MCPServerWarnings(nil); got != nil {
		t.Errorf("no servers should produce no warnings, got %v", got)
	}
}
