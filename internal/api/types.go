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
