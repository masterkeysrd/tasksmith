package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

const baseURL = "http://unix"

// Sentinel errors returned by the client package.
var (
	ErrInvalidSocketPath = errors.New("client: socket path must not be empty")
	ErrEmptyWorkspace    = errors.New("client: workspace path must not be empty")
	ErrEmptySessionID    = errors.New("client: session ID must not be empty")
)

// Option configures a [Client].
type Option func(*Client)

// WithHTTPClient overrides the underlying [*http.Client] used by the client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// Client communicates with the TaskSmith Brain Daemon over a Unix domain socket.
type Client struct {
	socketPath string
	httpClient *http.Client
}

// New constructs a new [Client] connected to socketPath.
func New(socketPath string, opts ...Option) (*Client, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, ErrInvalidSocketPath
	}

	var dialer net.Dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	c := &Client{
		socketPath: socketPath,
		httpClient: &http.Client{
			Transport: transport,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c, nil
}

// SocketPath returns the Unix domain socket path configured on the client.
func (c *Client) SocketPath() string {
	return c.socketPath
}

// Health queries the daemon's `/v1/health` endpoint.
func (c *Client) Health(ctx context.Context) (*api.HealthResponse, error) {
	var out api.HealthResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/health", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Shutdown requests a graceful daemon shutdown via `POST /v1/shutdown`.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/shutdown", nil, nil, nil)
}

// CreateSession creates a new session in workspace.
func (c *Client) CreateSession(ctx context.Context, workspace string, req api.CreateSessionRequest) (*api.Session, error) {
	q, err := workspaceQuery(workspace)
	if err != nil {
		return nil, err
	}
	var out api.Session
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", q, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSessions lists all sessions in workspace ordered by most recently updated.
func (c *Client) ListSessions(ctx context.Context, workspace string) ([]api.Session, error) {
	q, err := workspaceQuery(workspace)
	if err != nil {
		return nil, err
	}
	var out []api.Session
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSession retrieves session metadata and live status by ID.
func (c *Client) GetSession(ctx context.Context, workspace, sessionID string) (*api.Session, error) {
	path, q, err := sessionEndpoint(workspace, sessionID, "")
	if err != nil {
		return nil, err
	}
	var out api.Session
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSession deletes a session and its associated history and change journals.
func (c *Client) DeleteSession(ctx context.Context, workspace, sessionID string) error {
	path, q, err := sessionEndpoint(workspace, sessionID, "")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, path, q, nil, nil)
}

// History returns the chronological UI message history for a session.
func (c *Client) History(ctx context.Context, workspace, sessionID string) ([]api.Message, error) {
	path, q, err := sessionEndpoint(workspace, sessionID, "/messages")
	if err != nil {
		return nil, err
	}
	var out []api.Message
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Send submits a user message to a session and triggers a conversational turn
// in the daemon's background runtime.
func (c *Client) Send(ctx context.Context, workspace, sessionID string, req api.SendRequest) error {
	path, q, err := sessionEndpoint(workspace, sessionID, "/messages")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, q, req, nil)
}

// Cancel aborts an in-progress conversational turn on a session.
func (c *Client) Cancel(ctx context.Context, workspace, sessionID string) error {
	path, q, err := sessionEndpoint(workspace, sessionID, "/cancel")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, q, nil, nil)
}

// PendingPermissions returns all unresolved permission requests for a session.
func (c *Client) PendingPermissions(ctx context.Context, workspace, sessionID string) ([]api.PermissionRequest, error) {
	path, q, err := sessionEndpoint(workspace, sessionID, "/permissions")
	if err != nil {
		return nil, err
	}
	var out []api.PermissionRequest
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RespondPermission submits a user decision for a pending permission request.
func (c *Client) RespondPermission(ctx context.Context, workspace, sessionID string, decision api.PermissionDecision) error {
	path, q, err := sessionEndpoint(workspace, sessionID, "/permissions")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, q, decision, nil)
}

// PermissionMode returns the active [api.PermissionMode] for a session.
func (c *Client) PermissionMode(ctx context.Context, workspace, sessionID string) (api.PermissionMode, error) {
	path, q, err := sessionEndpoint(workspace, sessionID, "/mode")
	if err != nil {
		return "", err
	}
	var out api.ModeResponse
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return "", err
	}
	return out.Mode, nil
}

// SetPermissionMode updates the active [api.PermissionMode] for a session.
func (c *Client) SetPermissionMode(ctx context.Context, workspace, sessionID string, mode api.PermissionMode) error {
	path, q, err := sessionEndpoint(workspace, sessionID, "/mode")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPut, path, q, api.SetModeRequest{Mode: mode}, nil)
}

// Changes returns all recorded file mutation entries for a session.
func (c *Client) Changes(ctx context.Context, workspace, sessionID string) ([]api.FileChange, error) {
	path, q, err := sessionEndpoint(workspace, sessionID, "/changes")
	if err != nil {
		return nil, err
	}
	var out []api.FileChange
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DiffChange computes the unified diff for a specific journal entry on a path.
func (c *Client) DiffChange(ctx context.Context, workspace, sessionID, filePath, entryID string) (string, error) {
	path, q, err := sessionEndpoint(workspace, sessionID, "/diff")
	if err != nil {
		return "", err
	}
	q.Set("path", filePath)
	q.Set("entry_id", entryID)
	var out api.DiffResponse
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return "", err
	}
	return out.Diff, nil
}

// Revert reverts a single journal entry, a single file to baseline, or all
// tracked files in the session.
func (c *Client) Revert(ctx context.Context, workspace, sessionID string, req api.RevertRequest) error {
	path, q, err := sessionEndpoint(workspace, sessionID, "/revert")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, q, req, nil)
}

// Subscribe opens a real-time Server-Sent Events (SSE) stream for workspace and
// optional sessionID, yielding [api.Event] items as they arrive.
func (c *Client) Subscribe(ctx context.Context, workspace, sessionID string) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		q := url.Values{}
		if ws := strings.TrimSpace(workspace); ws != "" {
			q.Set("workspace", ws)
		}
		if sid := strings.TrimSpace(sessionID); sid != "" {
			q.Set("session_id", sid)
		}

		u := baseURL + "/v1/events"
		if encoded := q.Encode(); encoded != "" {
			u += "?" + encoded
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			yield(api.Event{}, fmt.Errorf("client: build events request: %w", err))
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			yield(api.Event{}, err)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			yield(api.Event{}, decodeErrorResponse(resp))
			return
		}

		streamSSE(ctx, resp.Body, yield)
	}
}

func streamSSE(ctx context.Context, body io.Reader, yield func(api.Event, error) bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		eventType string
		dataBuf   bytes.Buffer
	)

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, ":") {
			continue
		}
		if line == "" {
			if dataBuf.Len() == 0 {
				eventType = ""
				continue
			}
			var ev api.Event
			if err := json.Unmarshal(dataBuf.Bytes(), &ev); err != nil {
				dataBuf.Reset()
				eventType = ""
				if !yield(api.Event{}, fmt.Errorf("client: decode sse event: %w", err)) {
					return
				}
				continue
			}
			if ev.Type == "" && eventType != "" {
				ev.Type = api.EventType(eventType)
			}
			dataBuf.Reset()
			eventType = ""
			if !yield(ev, nil) {
				return
			}
			continue
		}
		if after, ok := strings.CutPrefix(line, "event:"); ok {
			eventType = strings.TrimSpace(after)
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			val := strings.TrimPrefix(after, " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(val)
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		yield(api.Event{}, err)
	}
}

func workspaceQuery(workspace string) (url.Values, error) {
	ws := strings.TrimSpace(workspace)
	if ws == "" {
		return nil, ErrEmptyWorkspace
	}
	q := url.Values{}
	q.Set("workspace", ws)
	return q, nil
}

func sessionEndpoint(workspace, sessionID, suffix string) (string, url.Values, error) {
	q, err := workspaceQuery(workspace)
	if err != nil {
		return "", nil, err
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return "", nil, ErrEmptySessionID
	}
	path := "/v1/sessions/" + url.PathEscape(sid) + suffix
	return path, q, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, reqBody any, respBody any) error {
	u := baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("client: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("client: build request %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeErrorResponse(resp)
	}

	if respBody == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("client: decode response %s %s: %w", method, path, err)
	}
	return nil
}

func decodeErrorResponse(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var errResp api.ErrorResponse
	if len(raw) > 0 && json.Unmarshal(raw, &errResp) == nil && (errResp.Code != "" || errResp.Message != "") {
		return &api.APIError{
			StatusCode: resp.StatusCode,
			Code:       errResp.Code,
			Message:    errResp.Message,
		}
	}

	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &api.APIError{
		StatusCode: resp.StatusCode,
		Message:    msg,
	}
}
