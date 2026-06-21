package api

type ListProjectsRequest struct {
}

type ListProjectsResponse struct {
	Projects []Project `json:"projects"`
}

type Project struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Path        string `json:"path"`
}

type ListAgentsRequest struct {
}

type ListAgentsResponse struct {
	Agents []Agent `json:"agents"`
}

type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ListProvidersRequest struct {
}

type ListProvidersResponse struct {
	Providers []Provider `json:"providers"`
}

type Provider struct {
	Name         string  `json:"name"`
	DisplayName  string  `json:"display_name"`
	Description  string  `json:"description"`
	DefaultModel string  `json:"default_model"`
	Endpoint     string  `json:"endpoint"`
	AuthEnv      string  `json:"auth_env"`
	APIKey       string  `json:"api_key"`
	Models       []Model `json:"models"`
}

type Model struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
}

type ListProvidersPresetsRequest struct {
}

type ListProvidersPresetsResponse struct {
	Providers []Provider `json:"providers"`
}

type ListToolsPresetsRequest struct {
}

type ListToolsPresetsResponse struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Labels      map[string]string `json:"labels"`
}

type InitializeWorkspaceRequest struct {
	ProjectName      string          `json:"project_name"`
	SelectedProvider string          `json:"selected_provider"`
	APIKey           string          `json:"api_key"`
	Endpoint         string          `json:"endpoint"`
	DefaultModel     string          `json:"default_model"`
	Theme            string          `json:"theme"`
	AuthorizedTools  map[string]bool `json:"authorized_tools"`
}

type InitializeWorkspaceResponse struct {
	Success bool `json:"success"`
}

type GetWorkspaceConfigRequest struct {
}

type GetWorkspaceConfigResponse struct {
	Name            string          `json:"name"`
	DefaultProvider string          `json:"default_provider"`
	AuthorizedTools map[string]bool `json:"authorized_tools"`
	IsConfigured    bool            `json:"is_configured"`
	CWD             string          `json:"cwd"`
}

type ListSessionsRequest struct {
}

type ListSessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateSessionRequest struct {
	Title string `json:"title"`
}

type CreateSessionResponse struct {
	Session Session `json:"session"`
}

type DeleteSessionRequest struct {
	ID string `json:"id"`
}

type DeleteSessionResponse struct {
	Success bool `json:"success"`
}

type RenameSessionRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type RenameSessionResponse struct {
	Success bool `json:"success"`
}

type ArchiveSessionRequest struct {
	ID string `json:"id"`
}

type ArchiveSessionResponse struct {
	Success bool `json:"success"`
}

type SendMessageRequest struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

type SendMessageResponse struct {
	Success bool `json:"success"`
}

type GetSessionMessagesRequest struct {
	SessionID string `json:"session_id"`
}

type GetSessionMessagesResponse struct {
	Messages       []string `json:"messages"`                  // Serialized JSON messages
	QueuedMessages []string `json:"queued_messages,omitempty"` // Serialized JSON queued messages
}

type GetSessionStateRequest struct {
	SessionID string `json:"session_id"`
}

type GetSessionStateResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
