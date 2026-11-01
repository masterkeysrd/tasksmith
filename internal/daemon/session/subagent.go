package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

// AddChild registers or updates a child session on this parent session.
func (s *Session) AddChild(child *Session) {
	if child == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	for i, c := range s.children {
		if c.ID == child.ID {
			s.children[i] = child
			return
		}
	}
	s.children = append(s.children, child)
}

// RemoveChild unregisters a child session by ID from this session.
func (s *Session) RemoveChild(id string) {
	s.Lock()
	defer s.Unlock()
	for i, c := range s.children {
		if c.ID == id {
			s.children = append(s.children[:i], s.children[i+1:]...)
			return
		}
	}
}

// Child looks up a child session by ID.
func (s *Session) Child(id string) (*Session, bool) {
	s.Lock()
	defer s.Unlock()
	for _, c := range s.children {
		if c.ID == id {
			return c, true
		}
	}
	return nil, false
}

// Children returns a cloned list of the child sessions registered with this session.
func (s *Session) Children() []*Session {
	s.Lock()
	defer s.Unlock()
	return slices.Clone(s.children)
}

// SetParent associates this session with a parent session.
func (s *Session) SetParent(parent *Session) {
	s.Lock()
	defer s.Unlock()
	s.parent = parent
	if parent != nil {
		s.ParentID = parent.ID
	}
}

// Parent returns the parent session if this session is a delegated child.
func (s *Session) Parent() *Session {
	s.Lock()
	defer s.Unlock()
	return s.parent
}

// InvokeSubagent spawns a child subagent asynchronously in an isolated child session.
func (s *Session) InvokeSubagent(ctx context.Context, in api.InvokeAgentInput) (api.InvokeAgentOutput, error) {
	agent := strings.TrimSpace(in.Agent)
	title := strings.TrimSpace(in.Title)
	prompt := strings.TrimSpace(in.Prompt)
	if agent == "" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: agent is required", std.ErrInvalidArgument)
	}
	if title == "" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: title is required", std.ErrInvalidArgument)
	}
	if prompt == "" {
		return api.InvokeAgentOutput{}, fmt.Errorf("%w: prompt is required", std.ErrInvalidArgument)
	}

	childID := uuid.NewString()
	workDir := s.WorkDir
	if in.Workspace == "worktree" {
		wsDataDir, err := xdg.WorkspaceDir(s.workspace.RootDir())
		if err != nil {
			return api.InvokeAgentOutput{}, fmt.Errorf("resolve workspace data dir: %w", err)
		}
		treeDir := filepath.Join(wsDataDir, "trees", childID)
		if err := os.MkdirAll(filepath.Dir(treeDir), 0755); err != nil {
			return api.InvokeAgentOutput{}, fmt.Errorf("create worktree dir: %w", err)
		}
		cmd := exec.CommandContext(ctx, "git", "-C", s.WorkDir, "worktree", "add", "--detach", treeDir, "HEAD")
		if out, err := cmd.CombinedOutput(); err != nil {
			return api.InvokeAgentOutput{}, fmt.Errorf("git worktree add: %w: %s", err, string(out))
		}
		workDir = treeDir
	}

	cfg := Config{
		ID:          childID,
		Title:       title,
		ParentID:    s.ID,
		WorkDir:     workDir,
		Parent:      s,
		Session:     api.SessionConfig{Agent: agent, Mode: s.Config.Mode},
		DB:          s.db,
		Workspace:   s.workspace,
		Gateway:     s.gateway,
		Catalog:     s.catalog,
		Permissions: s.permRoot,
		Publisher:   s.publisher,
		Logger:      s.logger,
		Metrics:     s.metrics,
		ReloadLock:  s.reloadLock,
		Context:     s.bgCtx,
		WaitGroup:   s.wg,
		OnUpdate:    s.onUpdate,
	}

	child, err := New(ctx, cfg)
	if err != nil {
		return api.InvokeAgentOutput{}, err
	}

	if s.onUpdate != nil {
		if err := s.onUpdate(ctx, child); err != nil {
			_ = child.Close()
			return api.InvokeAgentOutput{}, err
		}
	}
	s.AddChild(child)

	s.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(child),
	})
	child.notifyParentUpdate()

	if err := child.Send(ctx, api.SendRequest{Content: prompt}); err != nil {
		return api.InvokeAgentOutput{}, err
	}

	return api.InvokeAgentOutput{
		SessionID: child.ID,
		Agent:     child.Config.Agent,
		Title:     child.Title,
		WorkDir:   child.WorkDir,
		Status:    "running",
		Message:   fmt.Sprintf("Subagent %s (%s) spawned asynchronously in %s", child.Config.Agent, child.ID, child.WorkDir),
	}, nil
}

// ManageSubagents handles management operations (list, status, send, kill) on child subagent sessions.
func (s *Session) ManageSubagents(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
	switch in.Action {
	case "list":
		children := s.Children()
		agents := make([]api.ManageAgentsAgent, 0, len(children))
		for _, ch := range children {
			ch.Lock()
			st := string(ch.Status)
			errMsg := ch.Error
			ch.Unlock()
			agents = append(agents, api.ManageAgentsAgent{
				SessionID: ch.ID,
				Agent:     ch.Config.Agent,
				Title:     ch.Title,
				Status:    st,
				WorkDir:   ch.WorkDir,
				Error:     errMsg,
			})
		}
		return api.ManageAgentsOutput{
			Agents:  agents,
			Message: fmt.Sprintf("Found %d subagents", len(agents)),
		}, nil

	case "status":
		sessionID := strings.TrimSpace(in.SessionID)
		if sessionID == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: session_id is required", std.ErrInvalidArgument)
		}
		child, ok := s.Child(sessionID)
		if !ok {
			return api.ManageAgentsOutput{}, ErrSessionNotFound
		}

		var output string
		entries, err := child.Timeline.List(ctx)
		if err == nil {
			for _, entrie := range slices.Backward(entries) {
				if asst, ok := entrie.Message.(*message.Assistant); ok {
					output = strings.TrimSpace(asst.GetContent().Text())
					break
				}
			}
		}

		child.Lock()
		st := string(child.Status)
		child.Unlock()

		return api.ManageAgentsOutput{
			SessionID: child.ID,
			Agent:     child.Config.Agent,
			Title:     child.Title,
			Status:    st,
			WorkDir:   child.WorkDir,
			Output:    output,
			Message:   fmt.Sprintf("Subagent %s status: %s", child.ID, st),
		}, nil

	case "send":
		sessionID := strings.TrimSpace(in.SessionID)
		if sessionID == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: session_id is required", std.ErrInvalidArgument)
		}
		child, ok := s.Child(sessionID)
		if !ok {
			return api.ManageAgentsOutput{}, ErrSessionNotFound
		}
		if err := child.Send(ctx, api.SendRequest{Content: in.Message}); err != nil {
			return api.ManageAgentsOutput{}, err
		}
		child.Lock()
		st := string(child.Status)
		child.Unlock()
		return api.ManageAgentsOutput{
			SessionID: child.ID,
			Agent:     child.Config.Agent,
			Title:     child.Title,
			Status:    st,
			WorkDir:   child.WorkDir,
			Message:   fmt.Sprintf("Delivered message to subagent %s", child.ID),
		}, nil

	case "kill":
		sessionID := strings.TrimSpace(in.SessionID)
		if sessionID == "" {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: session_id is required", std.ErrInvalidArgument)
		}
		child, ok := s.Child(sessionID)
		if !ok {
			return api.ManageAgentsOutput{}, ErrSessionNotFound
		}
		if err := child.Cancel(ctx); err != nil {
			return api.ManageAgentsOutput{}, err
		}
		child.Lock()
		st := string(child.Status)
		child.Unlock()
		return api.ManageAgentsOutput{
			SessionID: child.ID,
			Agent:     child.Config.Agent,
			Title:     child.Title,
			Status:    st,
			WorkDir:   child.WorkDir,
			Message:   fmt.Sprintf("Canceled subagent %s", child.ID),
		}, nil

	default:
		return api.ManageAgentsOutput{}, fmt.Errorf("%w: unsupported action %q", std.ErrInvalidArgument, in.Action)
	}
}

func (s *Session) updateActivityFromThought(thought string) {
	if s.ParentID == "" {
		return
	}
	thought = strings.TrimSpace(thought)
	if thought == "" {
		return
	}
	line, _, multiline := strings.Cut(thought, "\n")
	line = strings.TrimSpace(line)
	runes := []rune(line)
	if len(runes) > 60 {
		line = string(runes[:60]) + "..."
	} else if multiline {
		line = strings.TrimRight(line, ". ") + "..."
	}
	s.Lock()
	if s.Activity == line {
		s.Unlock()
		return
	}
	s.Activity = line
	s.Unlock()
	s.notifyParentUpdate()
}

func toolActivityLabel(tc message.ToolCall) string {
	switch tc.Name {
	case "edit", "write", "remove":
		return "Editing files"
	case "view", "ls":
		return "Reading files"
	case "grep", "glob":
		return "Searching codebase"
	case "bash":
		if desc, ok := tc.Args["description"].(string); ok && strings.TrimSpace(desc) != "" {
			return strings.TrimSpace(desc)
		}
		return "Running command"
	case "web_fetch", "download":
		return "Fetching web content"
	default:
		return "Running " + tc.Name
	}
}

func (s *Session) notifyParentUpdate() {
	if s.ParentID == "" {
		return
	}
	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})
	if s.parent != nil {
		s.publisher.Publish(api.SessionStreamTopic(s.ParentID), api.SessionUpdatedEvent{
			Workspace: s.workspace.RootDir(),
			Session:   ToAPISession(s.parent),
		})
	}
}
