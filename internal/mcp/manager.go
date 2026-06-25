package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	loom "github.com/masterkeysrd/loom/mcp"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/warp"
)

// Manager wraps loom's MultiClient and handles resource mappings and auth setup.
type Manager struct {
	client      *loom.MultiClient
	serverNames []string
}

// NewManager constructs an MCP Manager from warp.MCP resource specifications.
func NewManager(mcps []*warp.MCP) *Manager {
	configs := make(map[string]loom.Config)
	var names []string
	for _, m := range mcps {
		name := m.GetName()
		spec := m.Spec

		var transport string
		if strings.ToLower(spec.Type) == "sse" {
			transport = "http"
		} else {
			transport = "stdio"
		}

		var cmd string
		var args []string
		if len(spec.Command) > 0 {
			cmd = spec.Command[0]
			args = spec.Command[1:]
		}

		// Look for Client ID and Secret in spec environment, fallback to system environment variables
		clientID := spec.Env["CLIENT_ID"]
		if clientID == "" {
			clientID = os.Getenv(fmt.Sprintf("MCP_%s_CLIENT_ID", strings.ToUpper(strings.ReplaceAll(name, "-", "_"))))
		}
		clientSecret := spec.Env["CLIENT_SECRET"]
		if clientSecret == "" {
			clientSecret = os.Getenv(fmt.Sprintf("MCP_%s_CLIENT_SECRET", strings.ToUpper(strings.ReplaceAll(name, "-", "_"))))
		}

		cfg := loom.Config{
			Transport: transport,
			Command:   cmd,
			Args:      args,
			Env:       spec.Env,
			URL:       spec.Endpoint,
			Auth: &TaskSmithOAuthProvider{
				ServerName:   name,
				ClientID:     clientID,
				ClientSecret: clientSecret,
			},
			Elicitation: &TaskSmithElicitationProvider{
				ServerName: name,
			},
		}

		configs[name] = cfg
		names = append(names, name)
	}

	return &Manager{
		client:      loom.NewMultiClient(configs),
		serverNames: names,
	}
}

// ServerNames returns the list of configured MCP server names.
func (m *Manager) ServerNames() []string {
	return m.serverNames
}

// MultiClient returns the underlying loom MultiClient.
func (m *Manager) MultiClient() *loom.MultiClient {
	return m.client
}

// GetClientByName returns the specific loom client by server name.
func (m *Manager) GetClientByName(serverName string) (*loom.Client, bool) {
	return nil, false
}

// DiscoverTools queries all configured MCP servers, namespaces names, maps safety annotations, and returns the merged tool list.
func (m *Manager) DiscoverTools(ctx context.Context, mcps []*warp.MCP) ([]*tool.Tool, error) {
	var mcpTools []*tool.Tool
	multiClient := m.MultiClient()

	for _, mcpResource := range mcps {
		serverName := mcpResource.GetName()
		fetchedTools, err := multiClient.Tools(ctx, serverName)
		if err != nil {
			log.ForComponent("mcp").Error("failed to list MCP tools", log.String("server", serverName), log.Err(err))
			continue
		}

		cleanName := func(s string) string {
			return strings.ReplaceAll(s, "-", "_")
		}
		sNameCleaned := cleanName(serverName)

		for _, lt := range fetchedTools {
			var anno tool.Annotation
			if mcpResource.Spec.Annotations != nil {
				anno.IsDangerous = mcpResource.Spec.Annotations.IsDangerous
				anno.IsOpenWorld = mcpResource.Spec.Annotations.IsOpenWorld
				anno.IsReadOnly = mcpResource.Spec.Annotations.IsReadOnly
				anno.IsIdempotent = mcpResource.Spec.Annotations.IsIdempotent
				anno.UserHint = mcpResource.Spec.Annotations.UserHint
			}
			if override, ok := mcpResource.Spec.Overrides[lt.Definition.Name]; ok {
				anno.IsDangerous = override.IsDangerous
				anno.IsOpenWorld = override.IsOpenWorld
				anno.IsReadOnly = override.IsReadOnly
				anno.IsIdempotent = override.IsIdempotent
				anno.UserHint = override.UserHint
			}

			lt.Definition.Name = fmt.Sprintf("mcp__%s__%s", sNameCleaned, lt.Definition.Name)
			lt.Definition.Title = fmt.Sprintf("%s - %s", sNameCleaned, lt.Definition.Title)
			lt.Annotation = anno
			lt.Definition.Annotation = anno

			mcpTools = append(mcpTools, lt)
		}
	}

	return mcpTools, nil
}

// Close closes all managed MCP clients.
func (m *Manager) Close() error {
	return m.client.Close()
}
