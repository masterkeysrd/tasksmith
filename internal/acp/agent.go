package acp

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/version"
)

type agent struct {
	base       *client.Client
	conn       *sdk.AgentSideConnection
	mu         sync.Mutex
	sessions   map[sdk.SessionId]*loadedSession
	turns      map[sdk.SessionId]*activeTurn
	output     *sessionOutput
	clientCaps sdk.ClientCapabilities
	closing    bool
}

type loadedSession struct {
	*client.Client
	usage sdk.SessionUsageUpdate
}

type activeTurn struct {
	cancel context.CancelFunc
	done   chan struct{}
}

const cleanupTimeout = 5 * time.Second

func Serve(ctx context.Context, c *client.Client, in io.Reader, out io.Writer) error {
	a := &agent{
		base:     c,
		sessions: make(map[sdk.SessionId]*loadedSession),
		turns:    make(map[sdk.SessionId]*activeTurn),
		output: &sessionOutput{
			Writer:  out,
			pending: make(map[sdk.SessionId][]byte),
		},
	}

	ready := make(chan struct{})
	a.conn = sdk.NewAgentSideConnection(a, a.output, sessionInput{
		Reader: in,
		ready:  ready,
	})
	close(ready)

	select {
	case <-ctx.Done():
	case <-a.conn.Done():
	}

	a.mu.Lock()
	a.closing = true
	var pending []*activeTurn
	for _, turn := range a.turns {
		turn.cancel()
		pending = append(pending, turn)
	}
	a.mu.Unlock()

	deadline, stop := context.WithTimeout(context.Background(), cleanupTimeout)
	defer stop()
	for _, turn := range pending {
		select {
		case <-turn.done:
		case <-deadline.Done():
			return fmt.Errorf("ACP shutdown: %w", deadline.Err())
		}
	}

	return ctx.Err()
}

func (a *agent) notify(ctx context.Context, id sdk.SessionId, u sdk.SessionUpdate) error {
	return a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: id,
		Update:    u,
	})
}

func (a *agent) capabilities() sdk.ClientCapabilities {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.clientCaps
}

func (a *agent) Initialize(_ context.Context, r sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	if r.ProtocolVersion < 1 {
		return sdk.InitializeResponse{}, sdk.NewInvalidParams("ACP protocol version 1 or later required")
	}

	a.mu.Lock()
	a.clientCaps = r.ClientCapabilities
	a.mu.Unlock()

	return sdk.InitializeResponse{
		ProtocolVersion: sdk.ProtocolVersionNumber,
		AgentInfo: &sdk.Implementation{
			Name:    "tasksmith",
			Title:   new("TaskSmith"),
			Version: version.Get(),
		},
		AuthMethods: []sdk.AuthMethod{},
		AgentCapabilities: sdk.AgentCapabilities{
			LoadSession: true,
			PromptCapabilities: sdk.PromptCapabilities{
				Audio:           true,
				Image:           true,
				EmbeddedContext: true,
			},
			SessionCapabilities: sdk.SessionCapabilities{
				Resume: &sdk.SessionResumeCapabilities{},
				Close:  &sdk.SessionCloseCapabilities{},
				Delete: &sdk.SessionDeleteCapabilities{},
				List:   &sdk.SessionListCapabilities{},
			},
		},
	}, nil
}

func (a *agent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, sdk.NewMethodNotFound("authenticate")
}

func (a *agent) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound("logout")
}

func notFound(id sdk.SessionId) error {
	return sdk.NewRequestError(-32002, "Session not found", string(id))
}

func sessionError(id sdk.SessionId, err error) error {
	if e, ok := errors.AsType[*api.APIError](err); ok && e.StatusCode == http.StatusNotFound {
		return notFound(id)
	}

	return err
}

func (a *agent) detach(ctx context.Context, id sdk.SessionId) (*client.Client, error) {
	a.mu.Lock()
	c := a.sessions[id]
	turn := a.turns[id]
	delete(a.sessions, id)
	a.mu.Unlock()

	if turn != nil {
		turn.cancel()
		ctx, stop := context.WithTimeout(ctx, cleanupTimeout)
		defer stop()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-turn.done:
		}
	}

	if c == nil {
		return nil, nil
	}
	return c.Client, nil
}

func (a *agent) CloseSession(
	ctx context.Context,
	r sdk.CloseSessionRequest,
) (out sdk.CloseSessionResponse, err error) {
	c, err := a.detach(ctx, r.SessionId)
	if err == nil && c == nil {
		err = notFound(r.SessionId)
	}

	return
}

func (a *agent) DeleteSession(
	ctx context.Context,
	r sdk.DeleteSessionRequest,
) (out sdk.DeleteSessionResponse, err error) {
	c, err := a.detach(ctx, r.SessionId)
	if err != nil {
		return
	}
	if c == nil {
		c = a.base
	}

	return out, sessionError(r.SessionId, c.DeleteSession(ctx, string(r.SessionId)))
}

func (a *agent) ResumeSession(
	ctx context.Context,
	r sdk.ResumeSessionRequest,
) (out sdk.ResumeSessionResponse, err error) {
	c, err := a.workspace(r.Cwd)
	if err != nil {
		return
	}
	if len(r.McpServers) > 0 {
		return out, sdk.NewInvalidParams("configure MCP servers in the TaskSmith workspace")
	}

	s, err := c.GetSession(ctx, string(r.SessionId))
	if err != nil {
		return out, sessionError(r.SessionId, err)
	}

	out.Modes, out.ConfigOptions, err = a.attach(ctx, c, s, false)
	return
}

func (a *agent) workspace(cwd string) (*client.Client, error) {
	if !filepath.IsAbs(cwd) {
		return nil, sdk.NewInvalidParams("cwd must be absolute")
	}

	return a.base.WithCWD(cwd), nil
}

func (a *agent) session(id sdk.SessionId) (*client.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	c := a.sessions[id]
	if c == nil {
		return nil, notFound(id)
	}

	return c.Client, nil
}

func (a *agent) ListSessions(
	ctx context.Context,
	r sdk.ListSessionsRequest,
) (out sdk.ListSessionsResponse, err error) {
	c := a.base
	if r.Cwd != nil {
		c, err = a.workspace(*r.Cwd)
		if err != nil {
			return
		}
	}

	ss, err := c.ListSessions(ctx)
	if err != nil {
		return
	}

	const pageSize = 50
	start := 0
	if r.Cursor != nil {
		b, decodeErr := base64.RawURLEncoding.Strict().DecodeString(*r.Cursor)
		n, parseErr := strconv.Atoi(string(b))
		if decodeErr != nil || parseErr != nil || n < 0 || n >= len(ss) || strconv.Itoa(n) != string(b) {
			return out, sdk.NewInvalidParams("invalid session cursor")
		}
		start = n
	}

	end := start + min(pageSize, len(ss)-start)
	out.Sessions = make([]sdk.SessionInfo, 0, end-start)
	for _, s := range ss[start:end] {
		info := sdk.SessionInfo{
			SessionId: sdk.SessionId(s.ID),
			Title:     &s.Title,
			Cwd:       cmp.Or(s.WorkDir, c.CWD()),
		}
		if !s.UpdatedAt.IsZero() {
			info.UpdatedAt = new(s.UpdatedAt.Format(time.RFC3339))
		}
		out.Sessions = append(out.Sessions, info)
	}
	if end < len(ss) {
		out.NextCursor = new(base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end))))
	}

	return
}

func (a *agent) NewSession(
	ctx context.Context,
	r sdk.NewSessionRequest,
) (out sdk.NewSessionResponse, err error) {
	c, err := a.workspace(r.Cwd)
	if err != nil {
		return
	}
	if len(r.McpServers) > 0 {
		return out, sdk.NewInvalidParams("configure MCP servers in the TaskSmith workspace")
	}

	s, err := c.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		return
	}

	out.SessionId = sdk.SessionId(s.ID)
	out.Modes, out.ConfigOptions, err = a.attach(ctx, c, s, true)
	return
}

func (a *agent) LoadSession(
	ctx context.Context,
	r sdk.LoadSessionRequest,
) (out sdk.LoadSessionResponse, err error) {
	c, err := a.workspace(r.Cwd)
	if err != nil {
		return
	}
	if len(r.McpServers) > 0 {
		return out, sdk.NewInvalidParams("configure MCP servers in the TaskSmith workspace")
	}

	s, err := c.GetSession(ctx, string(r.SessionId))
	if err != nil {
		return out, sessionError(r.SessionId, err)
	}

	entries, err := c.Timeline(ctx, s.ID)
	if err != nil {
		return out, sessionError(r.SessionId, err)
	}

	t := newTranslation(c.CWD())
	t.caps = a.capabilities()
	for _, e := range entries {
		for _, u := range t.entry(e, true) {
			if err = a.notify(ctx, r.SessionId, u); err != nil {
				return
			}
		}
	}

	out.Modes, out.ConfigOptions, err = a.attach(ctx, c, s, false)
	return
}

func (a *agent) attach(
	ctx context.Context,
	c *client.Client,
	s *api.Session,
	newlyCreated bool,
) (*sdk.SessionModeState, []sdk.SessionConfigOption, error) {
	id := sdk.SessionId(s.ID)
	opts, err := configuration(ctx, c, s)
	if err != nil {
		return nil, nil, err
	}

	cmds, err := c.ListCommands(ctx)
	if err != nil {
		return nil, nil, err
	}

	commands := []sdk.AvailableCommand{
		{
			Name:        "compact",
			Description: "Compact session history",
		},
	}
	for _, cmd := range cmds {
		commands = append(commands, sdk.AvailableCommand{
			Name:        cmd.Name,
			Description: cmd.Description,
		})
	}

	update := sdk.SessionUpdate{
		AvailableCommandsUpdate: &sdk.SessionAvailableCommandsUpdate{AvailableCommands: commands},
	}
	updates := []sdk.SessionUpdate{update}
	if caps := a.capabilities(); caps.Session != nil && caps.Session.Notices != nil {
		for _, d := range s.Diagnostics {
			updates = append(updates, warning(d.Message))
		}
	}
	for _, u := range updates {
		if newlyCreated {
			err = a.output.afterNew(id, u)
		} else {
			err = a.notify(ctx, id, u)
		}
		if err != nil {
			return nil, nil, err
		}
	}

	a.mu.Lock()
	a.sessions[id] = &loadedSession{
		Client: c,
		usage: sdk.SessionUsageUpdate{
			Used: int64(s.Context.UsedTokens),
			Size: int64(s.Context.MaxTokens),
		},
	}
	a.mu.Unlock()

	m := &sdk.SessionModeState{CurrentModeId: sdk.SessionModeId(cmp.Or(s.Config.Mode, "default"))}
	for _, name := range modes {
		m.AvailableModes = append(m.AvailableModes, sdk.SessionMode{
			Id:   sdk.SessionModeId(name),
			Name: name,
		})
	}

	return m, opts, nil
}

func modelOptions(ctx context.Context, c *client.Client, s *api.Session) ([]sdk.SessionConfigOption, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	current := s.Config.Model
	if s.Model != nil {
		current = s.Model.ID
	}
	if current == "" {
		w, err := c.GetWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		if w.DefaultModel != nil {
			current = w.DefaultModel.ID
		}
	}

	values := sdk.SessionConfigSelectOptionsUngrouped{}
	for _, m := range models {
		if !m.Capabilities.SupportsCompletion() {
			continue
		}
		values = append(values, sdk.SessionConfigSelectOption{
			Value: sdk.SessionConfigValueId(m.ID),
			Name:  cmp.Or(m.Label, m.Name, m.ID),
		})
	}
	if len(values) == 0 {
		return nil, nil
	}

	o := sdk.NewSessionConfigOptionSelect(
		sdk.SessionConfigValueId(current),
		sdk.SessionConfigSelectOptions{Ungrouped: &values},
	)
	o.Select.Id = "model"
	o.Select.Name = "Model"
	o.Select.Category = sdk.Ptr(sdk.SessionConfigOptionCategoryModel)

	return []sdk.SessionConfigOption{o}, nil
}

func (a *agent) SetSessionMode(
	ctx context.Context,
	r sdk.SetSessionModeRequest,
) (out sdk.SetSessionModeResponse, err error) {
	res, err := a.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{
		ValueId: &sdk.SetSessionConfigOptionValueId{
			SessionId: r.SessionId,
			ConfigId:  "mode",
			Value:     sdk.SessionConfigValueId(r.ModeId),
		},
	})
	if err != nil {
		return out, err
	}

	return out, a.notify(ctx, r.SessionId, sdk.SessionUpdate{
		ConfigOptionUpdate: &sdk.SessionConfigOptionUpdate{ConfigOptions: res.ConfigOptions},
	})
}

func (a *agent) SetSessionConfigOption(
	ctx context.Context,
	r sdk.SetSessionConfigOptionRequest,
) (out sdk.SetSessionConfigOptionResponse, err error) {
	v := r.ValueId
	if v == nil || v.Value == "" {
		return out, sdk.NewInvalidParams("expected a session configuration selector")
	}

	c, err := a.session(v.SessionId)
	if err != nil {
		return
	}
	cfg, err := configPatch(string(v.ConfigId), string(v.Value))
	if err != nil {
		return
	}

	s, err := c.UpdateSession(ctx, string(v.SessionId), api.UpdateSessionRequest{Config: cfg})
	if err != nil {
		if e, ok := errors.AsType[*api.APIError](err); ok && e.StatusCode == http.StatusBadRequest {
			return out, sdk.NewInvalidParams(e.Message)
		}
		return out, sessionError(v.SessionId, err)
	}
	if cfg.Mode != "" {
		err = a.notify(ctx, v.SessionId, sdk.SessionUpdate{
			CurrentModeUpdate: &sdk.SessionCurrentModeUpdate{CurrentModeId: sdk.SessionModeId(s.Config.Mode)},
		})
		if err != nil {
			return
		}
	}

	out.ConfigOptions, err = configuration(ctx, c, s)
	return
}

type sessionInput struct {
	io.Reader
	ready <-chan struct{}
}

func (in sessionInput) Read(p []byte) (int, error) {
	<-in.ready
	return in.Reader.Read(p)
}

type sessionOutput struct {
	io.Writer
	mu      sync.Mutex
	pending map[sdk.SessionId][]byte
}

func (w *sessionOutput) afterNew(id sdk.SessionId, commands sdk.SessionUpdate) error {
	frame, err := json.Marshal(struct {
		Version string                  `json:"jsonrpc"`
		Method  string                  `json:"method"`
		Params  sdk.SessionNotification `json:"params"`
	}{
		"2.0",
		sdk.ClientMethodSessionUpdate,
		sdk.SessionNotification{
			SessionId: id,
			Update:    commands,
		},
	})
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.pending[id] = append(append(w.pending[id], frame...), '\n')
	w.mu.Unlock()

	return nil
}

func (w *sessionOutput) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}

	if frame := w.takeAnnouncement(p); frame != nil {
		size, e := w.Writer.Write(frame)
		if e != nil {
			return n, e
		}
		if size != len(frame) {
			return n, io.ErrShortWrite
		}
	}

	return n, nil
}

func (w *sessionOutput) takeAnnouncement(p []byte) []byte {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) == 0 {
		return nil
	}

	var response struct {
		Result struct {
			SessionID sdk.SessionId `json:"sessionId"`
		} `json:"result"`
	}
	if json.Unmarshal(p, &response) != nil {
		return nil
	}

	id := response.Result.SessionID
	frame := w.pending[id]
	delete(w.pending, id)
	return frame
}
