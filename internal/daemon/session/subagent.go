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
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
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

	model, err := s.resolveSubagentModel(agent, in.Model)
	if err != nil {
		return api.InvokeAgentOutput{}, err
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

	parentCfg := s.configSnapshot()
	cfg := Config{
		ID:              childID,
		Title:           title,
		ParentID:        s.ID,
		WorkDir:         workDir,
		Parent:          s,
		Session:         api.SessionConfig{Agent: agent, Model: model, Mode: parentCfg.Mode, Sandbox: s.Sandbox.Mode()},
		DB:              s.db,
		Workspace:       s.workspace,
		Gateway:         s.gateway,
		Catalog:         s.catalog,
		SandboxSettings: s.sandboxCfg,
		Permissions:     s.permRoot,
		Publisher:       s.publisher,
		Logger:          s.logger,
		Metrics:         s.metrics,
		ReloadLock:      s.reloadLock,
		Context:         s.bgCtx,
		WaitGroup:       s.wg,
		OnUpdate:        s.onUpdate,
		MCPCaller:       s.mcpCaller,
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

// configSnapshot returns a copy of the session's current configuration taken
// under the session lock, so callers running on tool-execution goroutines can
// read model/mode settings without racing concurrent Update calls.
func (s *Session) configSnapshot() api.SessionConfig {
	if s.State != nil {
		s.Lock()
		defer s.Unlock()
	}
	return s.Config
}

// resolveSubagentModel resolves the effective model reference a parent session
// passes to a delegated subagent, enforcing the target agent's spec.models
// allowlist and returning a canonical provider/model ID whenever the reference
// resolves through the workspace catalog.
//
// The reserved "@inherit" alias resolves to the parent's own active model,
// both when the caller requests it explicitly and when it is the first
// resolvable entry in the subagent's spec.models. An explicit request outside
// the allowlist, or one that cannot resolve to a known model while the agent
// restricts spec.models, is rejected synchronously. A subagent that declares no
// spec.models accepts any model reference (unresolvable ones pass through for
// providers that accept ad-hoc model IDs). When nothing is requested and no
// entry resolves, an empty string lets the child session fall back to the
// workspace defaults.
func (s *Session) resolveSubagentModel(agentName, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	parentModel := strings.TrimSpace(s.configSnapshot().Model)

	cat := s.workspace.Catalog()
	resolved, err := cat.ResolveAgent(agentName)
	if err != nil || resolved == nil || resolved.Agent == nil {
		// Unknown agents fail later with a clearer error when the child
		// session is constructed; do not invent a model verdict here.
		return requested, nil
	}
	allow := resolved.Agent.Spec.Models

	if isRequestedInherit(requested) {
		if len(allow) > 0 && !allowlistAllowsInherit(allow) {
			return "", fmt.Errorf("%w: model %q is not allowed for subagent %q (allowed: %v)",
				ErrInvalidConfig, requested, agentName, allow)
		}
		return parentModel, nil
	}

	if len(allow) == 0 {
		if requested == "" {
			return "", nil
		}
		if id, ok := s.resolveModelRef(requested, parentModel); ok {
			return id, nil
		}
		if isAliasRequest(requested) {
			return "", fmt.Errorf("%w: model %q does not resolve to a model available in this workspace",
				ErrInvalidConfig, requested)
		}
		return requested, nil
	}

	if requested != "" {
		if !subagentModelAllowed(s.catalog(), cat, allow, requested, parentModel) {
			return "", fmt.Errorf("%w: model %q is not allowed for subagent %q (allowed: %v)",
				ErrInvalidConfig, requested, agentName, allow)
		}
		if id, ok := s.resolveModelRef(requested, parentModel); ok {
			return id, nil
		}
		// An alias-shaped request must resolve: passing a literal "@fast" to
		// the child would fail asynchronously on the first turn instead of
		// reporting a usable error here. Non-alias references may still be
		// ad-hoc model IDs a provider accepts, so they pass through.
		if isAliasRequest(requested) {
			return "", fmt.Errorf("%w: model %q does not resolve to a model available in this workspace",
				ErrInvalidConfig, requested)
		}
		return requested, nil
	}

	parentProvider := models.ID(parentModel).Provider()
	for _, entry := range allow {
		if isRequestedInherit(entry) {
			return parentModel, nil
		}
		if id, ok := preferredModelCandidate(cat.ResolveModels(warp.ModelRoleCompletion, entry), parentProvider); ok {
			return id, nil
		}
	}
	return "", nil
}

// resolveModelRef resolves a model reference to its canonical provider/model
// ID when the workspace catalog recognizes it, preferring a candidate owned by
// the parent session's active provider so a delegated subagent stays on the
// same provider as its parent.
func (s *Session) resolveModelRef(ref, parentModel string) (string, bool) {
	ref = strings.TrimSpace(ref)
	cat := s.workspace.Catalog()
	parentProvider := models.ID(parentModel).Provider()
	if id, ok := preferredModelCandidate(cat.ResolveModels(warp.ModelRoleCompletion, ref), parentProvider); ok {
		return id, true
	}
	if modelsCat := s.catalog(); modelsCat != nil {
		if m, ok := modelsCat.Get(models.ID(ref)); ok && m.ID.Valid() {
			return string(m.ID), true
		}
	}
	return "", false
}

// isAliasRequest reports whether a reference is alias-shaped ("@fast",
// "provider/@fast"), which must resolve through the workspace catalog and can
// never be an ad-hoc provider model identifier.
func isAliasRequest(ref string) bool {
	return strings.HasPrefix(ref, "@") || strings.Contains(ref, "/@")
}

// subagentModelAllowed reports whether a caller-requested model reference is
// permitted by a subagent's spec.models allowlist. A request is allowed when it
// matches an entry textually (case-insensitively), when it resolves through any
// registered provider to the same canonical model as an allowlist entry (so
// multi-provider tier aliases like "anthropic/@fast" match "@fast"), or when it
// names the parent's active model and the allowlist grants "@inherit".
func subagentModelAllowed(modelsCat *models.Catalog, cat *workspace.Catalog, allow []string, requested, parentModel string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}
	if isRequestedInherit(requested) {
		return allowlistAllowsInherit(allow)
	}
	for _, entry := range allow {
		if isRequestedInherit(entry) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(entry), requested) {
			return true
		}
	}

	parentProvider := models.ID(parentModel).Provider()
	if parentProvider != "" && allowlistAllowsInherit(allow) {
		if strings.EqualFold(requested, parentModel) {
			return true
		}
	}

	requestedIDs := modelCandidateIDs(modelsCat, cat, requested)
	for _, entry := range allow {
		if isRequestedInherit(entry) {
			continue
		}
		for _, entryID := range modelCandidateIDs(modelsCat, cat, entry) {
			if slices.Contains(requestedIDs, entryID) {
				return true
			}
		}
	}
	return false
}

// modelCandidateIDs returns every canonical provider/model ID a reference
// resolves to across registered providers (a bare tier alias can resolve once
// per provider that declares it).
func modelCandidateIDs(modelsCat *models.Catalog, cat *workspace.Catalog, ref string) []string {
	ref = strings.TrimSpace(ref)
	var ids []string
	for _, c := range cat.ResolveModels(warp.ModelRoleCompletion, ref) {
		if id := models.ID(c.QualifiedID); id.Valid() && c.Provider != nil {
			ids = appendUniqueID(ids, c.QualifiedID)
		}
	}
	if modelsCat != nil {
		if m, ok := modelsCat.Get(models.ID(ref)); ok && m.ID.Valid() {
			ids = appendUniqueID(ids, string(m.ID))
		}
	}
	return ids
}

func appendUniqueID(ids []string, id string) []string {
	if !slices.Contains(ids, id) {
		ids = append(ids, id)
	}
	return ids
}

// preferredModelCandidate selects the canonical ID of the first resolvable
// candidate, preferring one owned by the parent session's provider so tier
// aliases keep subagents on the provider the parent is actively using.
func preferredModelCandidate(candidates []warp.ResolvedModel, parentProvider string) (string, bool) {
	if parentProvider != "" {
		for _, c := range candidates {
			if c.Provider == nil || !models.ID(c.QualifiedID).Valid() {
				continue
			}
			if c.Provider.Metadata.Name == parentProvider {
				return c.QualifiedID, true
			}
		}
	}
	for _, c := range candidates {
		if c.Provider == nil || !models.ID(c.QualifiedID).Valid() {
			continue
		}
		return c.QualifiedID, true
	}
	return "", false
}

func isRequestedInherit(ref string) bool {
	return strings.EqualFold(strings.TrimSpace(ref), workspace.ModelAliasInherit)
}

func allowlistAllowsInherit(allow []string) bool {
	return slices.ContainsFunc(allow, isRequestedInherit)
}

// dispatchCommandSubagent executes a slash command in a forked child session.
// It is used when a command requests clean/fork context or targets a different
// agent than the invoking session. The child inherits the parent's runtime
// dependencies; for fork context the parent's conversation history is seeded
// into the child's harness before the command turn starts.
func (s *Session) dispatchCommandSubagent(ctx context.Context, exec *commandExecution) error {
	if exec == nil || exec.Message == nil {
		return nil
	}

	childID := uuid.NewString()
	parentCfg := s.configSnapshot()
	targetAgent := exec.TargetAgent
	if targetAgent == "" {
		targetAgent = parentCfg.Agent
	}
	targetModel := exec.TargetModel
	if isRequestedInherit(targetModel) {
		// A command may explicitly ask to run on the invoking session's model.
		targetModel = parentCfg.Model
	} else if targetModel == "" && targetAgent != parentCfg.Agent {
		// A command delegating to another agent still honors that agent's
		// spec.models defaults (including the reserved "@inherit").
		resolved, err := s.resolveSubagentModel(targetAgent, "")
		if err != nil {
			return err
		}
		targetModel = resolved
	}

	cfg := Config{
		ID:              childID,
		Title:           "/" + exec.Name,
		ParentID:        s.ID,
		WorkDir:         s.WorkDir,
		Parent:          s,
		Session:         api.SessionConfig{Agent: targetAgent, Model: targetModel, Mode: parentCfg.Mode, Sandbox: s.Sandbox.Mode()},
		DB:              s.db,
		Workspace:       s.workspace,
		Gateway:         s.gateway,
		Catalog:         s.catalog,
		SandboxSettings: s.sandboxCfg,
		Permissions:     s.permRoot,
		Publisher:       s.publisher,
		Logger:          s.logger,
		Metrics:         s.metrics,
		ReloadLock:      s.reloadLock,
		Context:         s.bgCtx,
		WaitGroup:       s.wg,
		OnUpdate:        s.onUpdate,
		MCPCaller:       s.mcpCaller,
	}

	child, err := New(ctx, cfg)
	if err != nil {
		return err
	}

	if s.onUpdate != nil {
		if err := s.onUpdate(ctx, child); err != nil {
			_ = child.Close()
			return err
		}
	}
	s.AddChild(child)

	s.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(child),
	})
	child.notifyParentUpdate()

	// For fork context, fork the child's checkpoint from the parent's latest
	// checkpoint so the command runs with the parent's full state (all channels).
	if exec.ContextMode == warp.CommandContextFork {
		if err := child.Harness.ForkFrom(ctx, s.ID); err != nil {
			_ = child.Close()
			return err
		}
	}

	// Rebuild the command message's tool blocks against the child's own tool
	// container so deferred/eager tool activation matches targetAgent's partition.
	// The child runs its own conversation, so it gets a fresh message identity:
	// the message the caller sent is already recorded on the parent's timeline,
	// and timeline IDs are unique across sessions.
	cmdMsg := message.CloneMessage(exec.Message)
	cmdMsg.SetID(uuid.NewString())
	if exec.Resolved != nil && len(exec.Resolved.Tools) > 0 {
		content := make(message.Content, 0, len(exec.Resolved.Tools)+1)
		content = append(content, &message.TextBlock{Text: exec.Message.GetContent().Text()})
		content = append(content, tools.ActivationBlocks(child.Tools, s.workspace.Catalog(), exec.Resolved.Tools)...)
		cmdMsg = message.NewUser(content...)
		cmdMsg.SetID(uuid.NewString())
	}

	// Apply a per-turn model override when the command declares a model that
	// differs from the child's configured model.
	var hInput harness.Input
	hInput.Message = cmdMsg
	if targetModel != "" && !strings.EqualFold(targetModel, child.Config.Model) {
		hInput.ModelOverride = targetModel
	}

	return child.startTurn(hInput)
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
