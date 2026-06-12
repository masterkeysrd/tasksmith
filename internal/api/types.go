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
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}
