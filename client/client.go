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
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
)

const baseURL = "http://unix"

// Sentinel errors returned by the client package.
var (
	ErrInvalidSocketPath = errors.New("client: socket path must not be empty")
	ErrEmptyWorkspace    = errors.New("client: workspace path must not be empty")
	ErrEmptyCWD          = ErrEmptyWorkspace
	ErrEmptySessionID    = errors.New("client: session ID must not be empty")
	ErrEmptySubscriberID = errors.New("client: subscriber ID must not be empty")
	ErrNeedSync          = errors.New("client: stream out of sync")
)

// IsNeedSync reports whether err represents a stream cursor desync requiring a full snapshot reload.
func IsNeedSync(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNeedSync) {
		return true
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Code == api.ErrCodeNeedSync {
		return true
	}
	return false
}

// NewCursor returns a fresh canonical UUIDv7 string cursor for event streaming.
func NewCursor() string {
	return uuid.Must(uuid.NewV7()).String()
}

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

// WithCWD configures the default client working directory sent via [api.HeaderCWD].
func WithCWD(cwd string) Option {
	return func(c *Client) {
		c.cwd = strings.TrimSpace(cwd)
	}
}

// Client communicates with the TaskSmith Brain Daemon over a Unix domain socket.
type Client struct {
	socketPath string
	cwd        string
	httpClient *http.Client
	mux        *eventMux
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
		mux: newEventMux(),
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

// CWD returns the configured client working directory.
func (c *Client) CWD() string {
	return c.cwd
}

// WithCWD returns a shallow copy of [Client] with its working directory set to cwd.
func (c *Client) WithCWD(cwd string) *Client {
	cp := *c
	cp.cwd = strings.TrimSpace(cwd)
	return &cp
}

func (c *Client) requireCWD() error {
	if strings.TrimSpace(c.cwd) == "" {
		return ErrEmptyWorkspace
	}
	return nil
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

// GetWorkspace retrieves the resolved configuration metadata for the workspace.
func (c *Client) GetWorkspace(ctx context.Context) (*api.Workspace, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out api.Workspace
	if err := c.doJSON(ctx, http.MethodGet, "/v1/workspace", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PreviewSession resolves a proposed draft session configuration without creating it.
func (c *Client) PreviewSession(ctx context.Context, req api.PreviewSessionRequest) (*api.SessionPreview, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out api.SessionPreview
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/preview", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReloadWorkspace requests the daemon to reload workspace resources from disk.
func (c *Client) ReloadWorkspace(ctx context.Context) error {
	if err := c.requireCWD(); err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, "/v1/workspace/reload", nil, nil, nil)
}

// ListAgents returns all effective agents in the workspace scope.
func (c *Client) ListAgents(ctx context.Context) ([]api.Agent, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out []api.Agent
	if err := c.doJSON(ctx, http.MethodGet, "/v1/agents", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAgent retrieves an effective agent configuration by name.
func (c *Client) GetAgent(ctx context.Context, name string) (*api.Agent, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("client: agent name must not be empty")
	}
	path := "/v1/agents/" + url.PathEscape(name)
	var out api.Agent
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCommands returns all effective command resources in the workspace.
func (c *Client) ListCommands(ctx context.Context) ([]api.Command, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out []api.Command
	if err := c.doJSON(ctx, http.MethodGet, "/v1/commands", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCommand retrieves a command resource by name.
func (c *Client) GetCommand(ctx context.Context, name string) (*api.Command, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("client: command name must not be empty")
	}
	path := "/v1/commands/" + url.PathEscape(name)
	var out api.Command
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSkills returns all effective skill resources in the workspace.
func (c *Client) ListSkills(ctx context.Context) ([]api.Skill, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out []api.Skill
	if err := c.doJSON(ctx, http.MethodGet, "/v1/skills", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSkill retrieves a skill resource by name.
func (c *Client) GetSkill(ctx context.Context, name string) (*api.Skill, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("client: skill name must not be empty")
	}
	path := "/v1/skills/" + url.PathEscape(name)
	var out api.Skill
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListModels returns all resolved models across providers in the workspace.
func (c *Client) ListModels(ctx context.Context) ([]api.Model, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out []api.Model
	if err := c.doJSON(ctx, http.MethodGet, "/v1/models", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModel retrieves a model by ID or qualified identifier (e.g. "ollama/qwen3.8").
func (c *Client) GetModel(ctx context.Context, id string) (*api.Model, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("client: model ID must not be empty")
	}
	parts := strings.Split(id, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	path := "/v1/models/" + strings.Join(parts, "/")
	var out api.Model
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Permissions retrieves configured project and global permission rules.
func (c *Client) Permissions(ctx context.Context) (*api.PermissionsResponse, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out api.PermissionsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/permissions", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddPermissionRule appends an allow or deny rule to project or global permissions.
func (c *Client) AddPermissionRule(ctx context.Context, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out api.PermissionsResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/permissions", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemovePermissionRule deletes a rule from project or global permissions.
func (c *Client) RemovePermissionRule(ctx context.Context, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out api.PermissionsResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/permissions", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSession creates a new session in workspace.
func (c *Client) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.Session, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out api.Session
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSessions lists all sessions in workspace ordered by most recently updated.
func (c *Client) ListSessions(ctx context.Context) ([]api.Session, error) {
	if err := c.requireCWD(); err != nil {
		return nil, err
	}
	var out []api.Session
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSession retrieves session metadata and live status by ID.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*api.Session, error) {
	path, err := c.sessionEndpoint(sessionID, "")
	if err != nil {
		return nil, err
	}
	var out api.Session
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSession modifies session metadata (title, agent, model) on an existing session.
func (c *Client) UpdateSession(ctx context.Context, sessionID string, req api.UpdateSessionRequest) (*api.Session, error) {
	path, err := c.sessionEndpoint(sessionID, "")
	if err != nil {
		return nil, err
	}
	var out api.Session
	if err := c.doJSON(ctx, http.MethodPatch, path, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSession deletes a session and its associated history and change journals.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	path, err := c.sessionEndpoint(sessionID, "")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil, nil)
}

// Timeline returns the chronological session timeline for a session.
func (c *Client) Timeline(ctx context.Context, sessionID string) ([]api.TimelineEntry, error) {
	path, err := c.sessionEndpoint(sessionID, "/timeline")
	if err != nil {
		return nil, err
	}
	var out []api.TimelineEntry
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Send submits a user message to a session and triggers a conversational turn
// in the daemon's background runtime.
func (c *Client) Send(ctx context.Context, sessionID string, req api.SendRequest) error {
	path, err := c.sessionEndpoint(sessionID, "/send")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, nil, req, nil)
}

// Cancel aborts an in-progress conversational turn on a session.
func (c *Client) Cancel(ctx context.Context, sessionID string) error {
	path, err := c.sessionEndpoint(sessionID, "/cancel")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, nil, nil, nil)
}

// Changes returns all recorded file mutation entries for a session.
func (c *Client) Changes(ctx context.Context, sessionID string) ([]api.FileChange, error) {
	path, err := c.sessionEndpoint(sessionID, "/changes")
	if err != nil {
		return nil, err
	}
	var out []api.FileChange
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DiffChange computes the unified diff for a specific journal entry on a path.
func (c *Client) DiffChange(ctx context.Context, sessionID, filePath, entryID string) (string, error) {
	path, err := c.sessionEndpoint(sessionID, "/diff")
	if err != nil {
		return "", err
	}
	q := url.Values{}
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
func (c *Client) Revert(ctx context.Context, sessionID string, req api.RevertRequest) error {
	path, err := c.sessionEndpoint(sessionID, "/revert")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, nil, req, nil)
}

// Tasks returns all background tasks for a session.
func (c *Client) Tasks(ctx context.Context, sessionID string) ([]api.Task, error) {
	path, err := c.sessionEndpoint(sessionID, "/tasks")
	if err != nil {
		return nil, err
	}
	var out api.TasksResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

// KillTask requests termination of an active background task.
func (c *Client) KillTask(ctx context.Context, sessionID, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("client: task ID must not be empty")
	}
	path, err := c.sessionEndpoint(sessionID, "/tasks/"+url.PathEscape(taskID)+"/kill")
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodPost, path, nil, nil, nil)
}

// StreamTaskOutput streams live task output (both backlog and live updates) for a task.
func (c *Client) StreamTaskOutput(ctx context.Context, sessionID, taskID string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			yield("", errors.New("client: task ID must not be empty"))
			return
		}
		path, err := c.sessionEndpoint(sessionID, "/tasks/"+url.PathEscape(taskID)+"/output")
		if err != nil {
			yield("", err)
			return
		}

		u := baseURL + path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			yield("", fmt.Errorf("client: build request: %w", err))
			return
		}
		req.Header.Set("Accept", "text/plain")
		if c.cwd != "" {
			req.Header.Set(api.HeaderCWD, c.cwd)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			yield("", err)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			yield("", decodeErrorResponse(resp))
			return
		}

		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if !yield(string(buf[:n]), nil) {
					return
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				yield("", readErr)
				return
			}
		}
	}
}

// Subscribe subscribes an active SSE stream (identified by subscriberID) to additional topics.
func (c *Client) Subscribe(ctx context.Context, subscriberID, since string, topics ...string) (*api.SubscriptionResponse, error) {
	subID := strings.TrimSpace(subscriberID)
	if subID == "" {
		return nil, ErrEmptySubscriberID
	}
	headers := http.Header{
		api.HeaderSubscriberID: []string{subID},
	}
	var out api.SubscriptionResponse
	if err := c.doJSONWithHeaders(ctx, http.MethodPost, "/v1/events/subscribe", nil, headers, api.SubscriptionRequest{
		Topics: topics,
		Since:  strings.TrimSpace(since),
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Unsubscribe unsubscribes an active SSE stream (identified by subscriberID) from topics.
func (c *Client) Unsubscribe(ctx context.Context, subscriberID string, topics ...string) (*api.SubscriptionResponse, error) {
	subID := strings.TrimSpace(subscriberID)
	if subID == "" {
		return nil, ErrEmptySubscriberID
	}
	headers := http.Header{
		api.HeaderSubscriberID: []string{subID},
	}
	var out api.SubscriptionResponse
	if err := c.doJSONWithHeaders(ctx, http.MethodPost, "/v1/events/unsubscribe", nil, headers, api.SubscriptionRequest{Topics: topics}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Events opens a real-time Server-Sent Events (SSE) stream with optional subscriberID, since cursor, and initial topics.
func (c *Client) Events(ctx context.Context, subscriberID, since string, topics ...string) iter.Seq2[api.Event, error] {
	return c.EventsWithReady(ctx, subscriberID, since, nil, topics...)
}

// EventsWithReady opens an SSE stream and invokes ready with the server-assigned (or echoed) subscriber ID once connected.
func (c *Client) EventsWithReady(ctx context.Context, subscriberID, since string, ready func(subscriberID string), topics ...string) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		q := url.Values{}
		for _, t := range topics {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				q.Add("topic", trimmed)
			}
		}
		if trimmedSince := strings.TrimSpace(since); trimmedSince != "" {
			q.Set("since", trimmedSince)
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
		if subID := strings.TrimSpace(subscriberID); subID != "" {
			req.Header.Set(api.HeaderSubscriberID, subID)
		}
		if c.cwd != "" {
			req.Header.Set(api.HeaderCWD, c.cwd)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			yield(api.Event{}, err)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errResp := decodeErrorResponse(resp)
			var apiErr *api.APIError
			if errors.As(errResp, &apiErr) && apiErr.Code == api.ErrCodeNeedSync {
				errResp = fmt.Errorf("%w: %w", ErrNeedSync, errResp)
			}
			yield(api.Event{}, errResp)
			return
		}

		if ready != nil {
			ready(strings.TrimSpace(resp.Header.Get(api.HeaderSubscriberID)))
		}

		streamSSE(ctx, resp.Body, yield)
	}
}

func streamSSE(ctx context.Context, body io.Reader, yield func(api.Event, error) bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var dataBuf bytes.Buffer

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, ":") {
			continue
		}
		if line == "" {
			if dataBuf.Len() == 0 {
				continue
			}
			var ev api.Event
			if err := json.Unmarshal(dataBuf.Bytes(), &ev); err != nil {
				dataBuf.Reset()
				if !yield(api.Event{}, fmt.Errorf("client: decode sse event: %w", err)) {
					return
				}
				continue
			}
			dataBuf.Reset()
			if !yield(ev, nil) {
				return
			}
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

func (c *Client) sessionEndpoint(sessionID, suffix string) (string, error) {
	if err := c.requireCWD(); err != nil {
		return "", err
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return "", ErrEmptySessionID
	}
	path := "/v1/sessions/" + url.PathEscape(sid) + suffix
	return path, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, reqBody any, respBody any) error {
	return c.doJSONWithHeaders(ctx, method, path, query, nil, reqBody, respBody)
}

func (c *Client) doJSONWithHeaders(ctx context.Context, method, path string, query url.Values, headers http.Header, reqBody any, respBody any) error {
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
	if c.cwd != "" {
		req.Header.Set(api.HeaderCWD, c.cwd)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
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

// SessionUsage returns the token, cost and tool usage of a session.
func (c *Client) SessionUsage(ctx context.Context, sessionID string) (api.SessionUsage, error) {
	path, err := c.sessionEndpoint(sessionID, "/usage")
	if err != nil {
		return api.SessionUsage{}, err
	}
	var out api.SessionUsage
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return api.SessionUsage{}, err
	}
	return out, nil
}

// Usage returns the usage summary of the workspace or of all workspaces.
func (c *Client) Usage(ctx context.Context, req api.UsageRequest) (api.UsageSummary, error) {
	if err := c.requireCWD(); err != nil {
		return api.UsageSummary{}, err
	}
	query := url.Values{}
	if req.Scope != "" {
		query.Set("scope", string(req.Scope))
	}
	if req.GroupBy != "" {
		query.Set("group_by", string(req.GroupBy))
	}
	if !req.Since.IsZero() {
		query.Set("since", req.Since.UTC().Format(time.RFC3339))
	}
	if !req.Until.IsZero() {
		query.Set("until", req.Until.UTC().Format(time.RFC3339))
	}
	var out api.UsageSummary
	if err := c.doJSON(ctx, http.MethodGet, "/v1/usage", query, nil, &out); err != nil {
		return api.UsageSummary{}, err
	}
	return out, nil
}

// QueryMetrics runs a generic metrics query.
func (c *Client) QueryMetrics(ctx context.Context, q api.MetricsQuery) (api.MetricsResult, error) {
	if err := c.requireCWD(); err != nil {
		return api.MetricsResult{}, err
	}
	var out api.MetricsResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/metrics/query", nil, q, &out); err != nil {
		return api.MetricsResult{}, err
	}
	return out, nil
}
