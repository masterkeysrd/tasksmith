package api

import (
	"fmt"
	"strings"
)

// MCP authentication scheme names accepted by spec.auth.type in an MCP manifest.
const (
	MCPAuthBearer            = "bearer"
	MCPAuthAPIKey            = "api-key"
	MCPAuthBasic             = "basic"
	MCPAuthOAuth2            = "oauth2"
	MCPAuthClientCredentials = "client-credentials"
	MCPAuthEnterprise        = "enterprise"
)

// ErrCodeMCPAuthStatic is the API error code returned when an interactive
// authorization is requested for an MCP server that authenticates with static
// credentials or machine-to-machine client credentials. The message names the
// credential the user must supply, because no browser flow can help.
const ErrCodeMCPAuthStatic = "mcp_auth_static"

// MCPSchemeUsesOAuth reports whether an authentication scheme obtains its token
// from an authorization server rather than sending a credential the manifest
// already holds. Enterprise is included because it delegates to an identity
// provider even though TaskSmith cannot yet drive that handshake.
func MCPSchemeUsesOAuth(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case MCPAuthOAuth2, MCPAuthClientCredentials, MCPAuthEnterprise:
		return true
	default:
		return false
	}
}

// mcpErrorPrefixes are redundant wrappers the MCP SDK and the daemon stack around
// the real reason a server failed, innermost first in the chain. Stripping them
// turns
// `connect MCP server "github": sending "initialize": rejected by transport: 401 Unauthorized`
// into `401 Unauthorized`.
var mcpErrorPrefixes = []string{
	"mcp authentication required: ",
	"connect MCP server ",
	"MCP server ",
	"failed to get auth handler: ",
	"failed to authorize: ",
	"rejected by transport: ",
	"calling ",
	"sending ",
}

// CleanMCPError reduces a connection failure to the reason it happened by
// stripping the nested transport wrappers the MCP SDK adds around it, so a
// listing can show `401 Unauthorized` or `missing credential environment
// variable "GITHUB_TOKEN"` instead of the whole call chain. It is idempotent: an
// already-clean message is returned unchanged.
func CleanMCPError(errText string) string {
	text := strings.TrimSpace(errText)
	if text == "" {
		return ""
	}
	for {
		next := stripOneMCPErrorPrefix(text)
		if next == text {
			return text
		}
		text = next
	}
}

// stripOneMCPErrorPrefix removes the first wrapper prefix it finds, including the
// quoted method, server name, or error text that follows it.
func stripOneMCPErrorPrefix(text string) string {
	if rest, ok := stripAPIErrorPrefix(text); ok {
		return rest
	}
	for _, prefix := range mcpErrorPrefixes {
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		rest := text[len(prefix):]
		if prefix == "calling " || prefix == "sending " || prefix == "connect MCP server " || prefix == "MCP server " {
			remainder, ok := cutQuotedSuffix(rest)
			if !ok {
				continue
			}
			return remainder
		}
		return strings.TrimSpace(rest)
	}
	return text
}

// stripAPIErrorPrefix drops a leading `api: <code> (<status>): ` or
// `api: status <status>: ` envelope formatted by [APIError.Error].
func stripAPIErrorPrefix(text string) (string, bool) {
	if !strings.HasPrefix(text, "api: ") {
		return "", false
	}
	rest := strings.TrimPrefix(text, "api: ")
	if strings.HasPrefix(rest, "status ") {
		_, after, ok := strings.Cut(rest, ": ")
		if !ok {
			return "", false
		}
		return strings.TrimSpace(after), true
	}
	_, after, ok := strings.Cut(rest, "): ")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(after), true
}

// cutQuotedSuffix drops a leading quoted token and the colon that follows it,
// returning what the wrapper was wrapping. The colon is required because a
// wrapper always reads `prefix "token": reason`; without it, a real message such
// as `MCP server "notes" is not connected` would be mistaken for a wrapper.
func cutQuotedSuffix(text string) (string, bool) {
	if !strings.HasPrefix(text, "\"") {
		return "", false
	}
	end := strings.Index(text[1:], "\"")
	if end < 0 {
		return "", false
	}
	remainder := text[1+end+1:]
	if !strings.HasPrefix(remainder, ":") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(remainder, ":")), true
}

// MCPServerWarning explains in one line what a user must do about an unhealthy
// MCP server, or returns an empty string for a server that needs nothing. The
// advice differs by why the server is unhealthy: an interactive login can be
// started, while a missing static credential has to be supplied by the user.
func MCPServerWarning(server MCPServer) string {
	switch server.Status {
	case MCPStatusAuthRequired:
		switch {
		case server.AuthURL != "" || MCPSchemeUsesOAuth(server.AuthType):
			return fmt.Sprintf("MCP server %q requires authentication — run /mcp auth %s", server.Name, server.Name)
		case strings.TrimSpace(server.Error) != "":
			return fmt.Sprintf("MCP server %q requires authentication (%s) — run /mcp restart %s",
				server.Name, CleanMCPError(server.Error), server.Name)
		default:
			return fmt.Sprintf("MCP server %q requires authentication — run /mcp to inspect", server.Name)
		}
	case MCPStatusError:
		if reason := strings.TrimSpace(CleanMCPError(server.Error)); reason != "" {
			return fmt.Sprintf("MCP server %q failed to connect (%s) — run /mcp to inspect", server.Name, reason)
		}
		return fmt.Sprintf("MCP server %q failed to connect — run /mcp to inspect", server.Name)
	default:
		return ""
	}
}

// MCPServerWarnings renders the actionable warning for every MCP server that is
// unauthenticated or failing to connect, skipping healthy and in-flight servers.
func MCPServerWarnings(servers []MCPServer) []string {
	var warnings []string
	for _, server := range servers {
		if warning := MCPServerWarning(server); warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}
