package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// PendingRequest holds an active authentication or elicitation prompt.
type PendingRequest struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"` // "oauth" or "elicitation"
	ServerName   string           `json:"server_name"`
	Message      string           `json:"message"`
	URL          string           `json:"url,omitempty"`
	Schema       interface{}      `json:"schema,omitempty"`
	ResponseChan chan interface{} `json:"-"`
}

// Registry manages thread-safe storage of active MCP interactive requests.
type Registry struct {
	mu       sync.RWMutex
	requests map[string]*PendingRequest
}

// ActiveRequests is the global registry of all active MCP authentication/elicitation requests.
var ActiveRequests = &Registry{
	requests: make(map[string]*PendingRequest),
}

// Add inserts a new request.
func (r *Registry) Add(req *PendingRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests[req.ID] = req
}

// Remove deletes a request.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.requests, id)
}

// Get retrieves a request by ID.
func (r *Registry) Get(id string) *PendingRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.requests[id]
}

// List returns all active requests.
func (r *Registry) List() []*PendingRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*PendingRequest, 0, len(r.requests))
	for _, req := range r.requests {
		list = append(list, req)
	}
	return list
}

// OpenBrowser launches the user's default browser for the given URL.
func OpenBrowser(url string) error {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return err
}

type tuiCodeReceiver struct {
	serverName string
	port       int
	authChan   chan *auth.AuthorizationResult
	errChan    chan error
	server     *http.Server
}

func newTuiCodeReceiver(serverName string, port int) *tuiCodeReceiver {
	if port == 0 {
		port = 3142 // DefaultRedirectPort
	}
	return &tuiCodeReceiver{
		serverName: serverName,
		port:       port,
		authChan:   make(chan *auth.AuthorizationResult),
		errChan:    make(chan error),
	}
}

func (r *tuiCodeReceiver) serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", r.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", r.port, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		r.authChan <- &auth.AuthorizationResult{
			Code:  req.URL.Query().Get("code"),
			State: req.URL.Query().Get("state"),
		}
		fmt.Fprint(w, "Authentication successful. You can close this window.")
	})

	r.server = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.errChan <- err
		}
	}()

	return nil
}

func (r *tuiCodeReceiver) getAuthorizationCode(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	// Automatically try to open the browser
	_ = OpenBrowser(args.URL)

	reqID := uuid.New().String()
	respChan := make(chan interface{}, 1)
	pending := &PendingRequest{
		ID:           reqID,
		Type:         "oauth",
		ServerName:   r.serverName,
		Message:      fmt.Sprintf("Authentication required for MCP server %q. Please log in using the opened browser window or follow the link.", r.serverName),
		URL:          args.URL,
		ResponseChan: respChan,
	}
	ActiveRequests.Add(pending)
	defer ActiveRequests.Remove(reqID)

	select {
	case authRes := <-r.authChan:
		return authRes, nil
	case val := <-respChan:
		if authRes, ok := val.(*auth.AuthorizationResult); ok {
			return authRes, nil
		}
		return nil, fmt.Errorf("authentication cancelled by user")
	case err := <-r.errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *tuiCodeReceiver) close() {
	if r.server != nil {
		r.server.Close()
	}
}

// TaskSmithOAuthProvider implements AuthProvider for standard OAuth2, integrating with the TUI.
type TaskSmithOAuthProvider struct {
	ServerName   string
	ClientID     string
	ClientSecret string
	RedirectPort int
}

// GetHandler starts a redirect callback server and returns an OAuthHandler.
func (p *TaskSmithOAuthProvider) GetHandler(ctx context.Context) (auth.OAuthHandler, error) {
	receiver := newTuiCodeReceiver(p.ServerName, p.RedirectPort)
	if err := receiver.serve(ctx); err != nil {
		return nil, err
	}

	config := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              fmt.Sprintf("http://localhost:%d", receiver.port),
		AuthorizationCodeFetcher: receiver.getAuthorizationCode,
	}

	if p.ClientID != "" {
		config.PreregisteredClient = &oauthex.ClientCredentials{
			ClientID: p.ClientID,
		}
		if p.ClientSecret != "" {
			config.PreregisteredClient.ClientSecretAuth = &oauthex.ClientSecretAuth{
				ClientSecret: p.ClientSecret,
			}
		}
	}

	handler, err := auth.NewAuthorizationCodeHandler(config)
	if err != nil {
		receiver.close()
		return nil, err
	}

	return handler, nil
}

// TaskSmithElicitationProvider implements ElicitationProvider for interactive input requested by the MCP server.
type TaskSmithElicitationProvider struct {
	ServerName string
}

// HandleElicit registers the elicitation request and blocks until the TUI submits a result.
func (p *TaskSmithElicitationProvider) HandleElicit(ctx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	if params.URL != "" {
		_ = OpenBrowser(params.URL)
	}

	reqID := uuid.New().String()
	respChan := make(chan interface{}, 1)
	pending := &PendingRequest{
		ID:           reqID,
		Type:         "elicitation",
		ServerName:   p.ServerName,
		Message:      params.Message,
		URL:          params.URL,
		Schema:       params.RequestedSchema,
		ResponseChan: respChan,
	}
	ActiveRequests.Add(pending)
	defer ActiveRequests.Remove(reqID)

	select {
	case val := <-respChan:
		if result, ok := val.(*mcp.ElicitResult); ok {
			return result, nil
		}
		if err, ok := val.(error); ok {
			return nil, err
		}
		return nil, fmt.Errorf("elicitation cancelled by user")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// HandleElicitComplete is called when the server indicates elicitation is finished.
func (p *TaskSmithElicitationProvider) HandleElicitComplete(ctx context.Context, params *mcp.ElicitationCompleteParams) {
	// Noop
}
