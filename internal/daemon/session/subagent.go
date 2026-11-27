package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
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
func (s *Session) InvokeSubagent(ctx context.Context, in api.InvokeAgentInput) (_ api.InvokeAgentOutput, retErr error) {
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
		defer func() {
			if retErr == nil {
				return
			}
			cleanup := exec.CommandContext(context.WithoutCancel(ctx), "git", "-C", s.WorkDir, "worktree", "remove", "--force", treeDir)
			retErr = errors.Join(retErr, cleanup.Run(), os.RemoveAll(treeDir))
		}()
		workDir = treeDir
	}

	parentCfg, err := s.childConfig()
	if err != nil {
		return api.InvokeAgentOutput{}, err
	}
	cfg := Config{
		ID:              childID,
		Title:           title,
		ParentID:        s.ID,
		WorkDir:         workDir,
		Parent:          s,
		Session:         api.SessionConfig{Agent: agent, Model: model, Mode: parentCfg.Mode, Sandbox: parentCfg.Sandbox},
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
		OnDiscard:       s.onDiscard,
		MCPCaller:       s.mcpCaller,
	}

	childRoot, err := SessionDir(s.workspace.RootDir(), childID)
	if err != nil {
		return api.InvokeAgentOutput{}, err
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, os.RemoveAll(childRoot))
		}
	}()
	child, err := New(ctx, cfg)
	if err != nil {
		return api.InvokeAgentOutput{}, err
	}

	published := false
	defer func() {
		if retErr == nil {
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)
		s.RemoveChild(childID)
		retErr = errors.Join(retErr, child.Close(), child.Timeline.Delete(cleanupCtx), child.Journal.Delete(cleanupCtx))
		if s.onUpdate != nil {
			if err := s.onDiscard(cleanupCtx, childID); err != nil && !errors.Is(err, ErrSessionNotFound) {
				retErr = errors.Join(retErr, err)
			}
		}
		if published {
			s.publisher.Publish(api.TopicSessions, api.SessionDeletedEvent{Workspace: s.workspace.RootDir(), SessionID: childID})
			s.publisher.DeleteTopic(api.SessionStreamTopic(childID))
		}
	}()
	if s.onUpdate != nil {
		if err := s.onUpdate(ctx, child); err != nil {
			return api.InvokeAgentOutput{}, err
		}
	}
	s.AddChild(child)

	snapshot := ToAPISession(child)

	s.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   snapshot,
	})
	child.notifyParentUpdate()

	published = true
	if err := child.sendWithUserIntent(ctx, api.SendRequest{Content: prompt}, s.lastTurnUserIntent()); err != nil {
		return api.InvokeAgentOutput{}, err
	}

	out := api.InvokeAgentOutput{
		SessionID: snapshot.ID,
		Agent:     snapshot.Config.Agent,
		Title:     snapshot.Title,
		WorkDir:   snapshot.WorkDir,
		Status:    "running",
		Message:   fmt.Sprintf("Subagent %s (%s) spawned asynchronously in %s", snapshot.Config.Agent, snapshot.ID, snapshot.WorkDir),
	}
	if snapshot.Model != nil {
		out.Model = snapshot.Model.ID
		out.ModelLabel = snapshot.Model.Label
	}
	return out, nil
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

func (s *Session) effectiveConfig() api.SessionConfig {
	if s.State != nil {
		s.Lock()
		defer s.Unlock()
	}
	cfg := s.Config
	cfg.Model = cmp.Or(s.turnOverride, cfg.Model)
	return cfg
}

func (s *Session) childConfig() (api.SessionConfig, error) {
	s.Lock()
	defer s.Unlock()
	if s.closing {
		return api.SessionConfig{}, context.Canceled
	}
	cfg := s.Config
	cfg.Sandbox = s.Sandbox.Mode()
	return cfg, nil
}

// resolveSubagentModel resolves the effective model reference a parent session
// passes to a delegated subagent, delegating spec.models allowlist matching
// (literal IDs, @aliases, @inherit, globs like "*" and "<provider>/*", and "!"
// negations) to WARP while preserving parent-provider preference and canonical
// normalization through the models catalog.
//
// When a requested model or alias fails to resolve to a registered provider or
// known model, up to 3 closest matches from the catalog are included as
// "did you mean ...?" hints.
func (s *Session) resolveSubagentModel(agentName, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	parentModel := strings.TrimSpace(s.effectiveConfig().Model)
	parentProvider := models.ID(parentModel).Provider()

	cat := s.workspace.Catalog()
	resolved, err := cat.ResolveAgent(agentName)
	if err != nil || resolved == nil || resolved.Agent == nil {
		// Unknown agents fail later with a clearer error when the child
		// session is constructed; do not invent a model verdict here.
		return requested, nil
	}

	modelsCat := s.modelCatalog()
	normalize := func(id string) string {
		if modelsCat == nil || id == "" {
			return id
		}
		if m, ok := modelsCat.Get(models.ID(id)); ok && m.ID.Valid() {
			return string(m.ID)
		}
		return id
	}

	if requested == "" {
		resolver := parentProviderResolver{Resolver: cat.Resolver(), provider: parentProvider}
		candidates, err := resolved.ResolveModelsWithParent(resolver, parentModel)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
		if id, ok := preferredModelCandidate(candidates, ""); ok {
			return normalize(id), nil
		}
		if len(resolved.Agent.Spec.Models) > 0 && isRequestedInherit(resolved.Agent.Spec.Models[0]) && parentModel != "" {
			return normalize(parentModel), nil
		}
		return "", nil
	}

	var listed []models.Model
	if modelsCat != nil {
		listed = modelsCat.List()
	}

	lookupRef := canonicalizeListedRequest(listed, requested)
	effectiveParent := ""
	if isRequestedInherit(lookupRef) || (parentModel != "" && strings.EqualFold(lookupRef, parentModel)) {
		effectiveParent = parentModel
	}
	candidates, err := resolved.ResolveModelsWithParent(cat.Resolver(), effectiveParent, lookupRef)
	if err != nil {
		if hint := unresolvedModelHint(cat, listed, requested, lookupRef); hint != "" {
			return "", fmt.Errorf("%w: %w%s", ErrInvalidConfig, err, hint)
		}
		return "", fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	id, ok := preferredModelCandidate(candidates, parentProvider)
	if !ok {
		if isRequestedInherit(requested) && parentModel != "" {
			return normalize(parentModel), nil
		}
		return "", fmt.Errorf("%w: model %q does not resolve to a registered model provider%s",
			ErrInvalidConfig, requested, suggestClosestModels(listed, requested))
	}

	// Bare non-alias references (no "provider/" prefix) must match a declared
	// model rather than silently attaching defaultProvider to a typo.
	if !strings.Contains(lookupRef, "/") && !isAliasRequest(lookupRef) && !isRequestedInherit(lookupRef) {
		if !hasDeclaredModelCandidate(candidates) {
			return "", fmt.Errorf("%w: unknown model %q%s",
				ErrInvalidConfig, requested, suggestClosestModels(listed, requested))
		}
	}

	return normalize(id), nil
}

type parentProviderResolver struct {
	warp.Resolver
	provider string
}

func (r parentProviderResolver) ResolveModels(role warp.ModelRole, refs ...string) []warp.ResolvedModel {
	candidates := r.Resolver.ResolveModels(role, refs...)
	if len(candidates) < 2 || r.provider == "" {
		return candidates
	}
	priority := func(c warp.ResolvedModel) int {
		if c.Provider != nil && c.Provider.Metadata.Name == r.provider {
			return 0
		}
		return 1
	}
	ordered := slices.Clone(candidates)
	slices.SortStableFunc(ordered, func(a, b warp.ResolvedModel) int { return cmp.Compare(priority(a), priority(b)) })
	return ordered
}

func (s *Session) modelCatalog() *models.Catalog {
	if s.catalog == nil {
		return nil
	}
	return s.catalog()
}

func canonicalizeListedRequest(listed []models.Model, requested string) string {
	if requested == "" || isRequestedInherit(requested) || isAliasRequest(requested) {
		return requested
	}
	for _, m := range listed {
		if strings.EqualFold(string(m.ID), requested) {
			return string(m.ID)
		}
	}
	return requested
}

func hasDeclaredModelCandidate(candidates []warp.ResolvedModel) bool {
	for _, c := range candidates {
		if c.Provider != nil && c.Model != nil {
			return true
		}
	}
	return false
}

// unresolvedModelHint returns a "did you mean" suffix only when lookupRef fails
// to resolve to a known model in the workspace (so explicit policy rejections
// of valid models do not suggest unrelated alternatives).
func unresolvedModelHint(cat *workspace.Catalog, listed []models.Model, requested, lookupRef string) string {
	if isRequestedInherit(lookupRef) {
		return ""
	}
	if id, ok := preferredModelCandidate(cat.ResolveModels(warp.ModelRoleCompletion, lookupRef), ""); ok && id != "" {
		return ""
	}
	return suggestClosestModels(listed, requested)
}

// suggestClosestModels returns up to 3 closest model IDs from listed for typo
// recovery, or an empty string if no reasonable matches exist.
func suggestClosestModels(listed []models.Model, requested string) string {
	return strutils.FormatDidYouMean(models.SuggestClosest(listed, requested, 3))
}

// isAliasRequest reports whether a reference is alias-shaped ("@fast",
// "provider/@fast"), which must resolve through the workspace catalog and can
// never be an ad-hoc provider model identifier.
func isAliasRequest(ref string) bool {
	return strings.HasPrefix(ref, "@") || strings.Contains(ref, "/@")
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
	parentCfg, err := s.childConfig()
	if err != nil {
		return err
	}
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
		ID:               childID,
		Title:            "/" + exec.Name,
		ParentID:         s.ID,
		WorkDir:          s.WorkDir,
		Parent:           s,
		CommandDelegated: true,
		Session:          api.SessionConfig{Agent: targetAgent, Model: targetModel, Mode: parentCfg.Mode, Sandbox: parentCfg.Sandbox},
		DB:               s.db,
		Workspace:        s.workspace,
		Gateway:          s.gateway,
		Catalog:          s.catalog,
		SandboxSettings:  s.sandboxCfg,
		Permissions:      s.permRoot,
		Publisher:        s.publisher,
		Logger:           s.logger,
		Metrics:          s.metrics,
		ReloadLock:       s.reloadLock,
		Context:          s.bgCtx,
		WaitGroup:        s.wg,
		OnUpdate:         s.onUpdate,
		OnDiscard:        s.onDiscard,
		MCPCaller:        s.mcpCaller,
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
		if err := child.mediaStore.LinkAllFrom(s.mediaDir()); err != nil {
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
		rebuilt := message.NewUser(content...)
		rebuilt.SetID(uuid.NewString())
		if intent := harness.UserIntentOf(exec.Message); intent != "" {
			harness.SetUserIntent(rebuilt, intent)
		}
		cmdMsg = rebuilt
	}

	// Apply a per-turn model override when the command declares a model that
	// differs from the child's configured model.
	var hInput harness.Input
	hInput.Message = cmdMsg
	if targetModel != "" && !strings.EqualFold(targetModel, child.configSnapshot().Model) {
		hInput.ModelOverride = targetModel
	}

	return child.startTurn(hInput)
}

// ManageSubagents handles management operations (list, status, send, kill,
// models) on child subagent sessions and model discovery.
func (s *Session) ManageSubagents(ctx context.Context, in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
	switch in.Action {
	case "models":
		return s.listSubagentModels(in)

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
		if err := child.sendWithUserIntent(ctx, api.SendRequest{Content: in.Message}, s.lastTurnUserIntent()); err != nil {
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

const (
	subagentModelListDefaultLimit = 20
	subagentModelListMaxLimit     = 50
)

// listSubagentModels backs the manage_agents "models" action: it returns
// bounded summaries of completion-capable models subagents can run on,
// optionally fuzzy-filtered by query and provider and intersected with a
// specific subagent's spec.models allowlist, so orchestrators can discover
// valid invoke_agent model overrides without the full catalog living in the
// system prompt.
func (s *Session) listSubagentModels(in api.ManageAgentsInput) (api.ManageAgentsOutput, error) {
	limit := in.Limit
	switch {
	case limit <= 0:
		limit = subagentModelListDefaultLimit
	case limit > subagentModelListMaxLimit:
		limit = subagentModelListMaxLimit
	}

	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	query := strings.ToLower(strings.TrimSpace(in.Query))

	// An agent filter intersects candidates against that subagent's resolved
	// spec.models allowlist. ResolveModelsWithParent with an explicit requested
	// reference enforces the same literal/alias/@inherit/glob/negation
	// semantics invoke_agent applies, per candidate; agents with an empty
	// spec.models accept any model and stay unrestricted.
	var isAllowed func(id string) bool
	if agent := strings.TrimSpace(in.Agent); agent != "" {
		cat := s.workspace.Catalog()
		resolved, err := cat.ResolveAgent(agent)
		if err != nil || resolved == nil || resolved.Agent == nil {
			return api.ManageAgentsOutput{}, fmt.Errorf("%w: unknown subagent %q", std.ErrInvalidArgument, agent)
		}
		parentModel := strings.TrimSpace(s.effectiveConfig().Model)
		if len(resolved.Agent.Spec.Models) > 0 {
			isAllowed = func(id string) bool {
				effectiveParent := ""
				if parentModel != "" && strings.EqualFold(id, parentModel) {
					effectiveParent = parentModel
				}
				candidates, err := resolved.ResolveModelsWithParent(cat.Resolver(), effectiveParent, id)
				if err != nil {
					return false
				}
				return hasDeclaredModelCandidate(candidates)
			}
		}
	}

	var listed []models.Model
	if modelsCat := s.modelCatalog(); modelsCat != nil {
		listed = modelsCat.List()
	}

	var candidates []models.Model
	for _, m := range listed {
		if !m.SupportsCompletion() {
			continue
		}
		if provider != "" && !strings.EqualFold(m.ID.Provider(), provider) {
			continue
		}
		if isAllowed != nil && !isAllowed(string(m.ID)) {
			continue
		}
		candidates = append(candidates, m)
	}

	matched, total := strutils.RankBy(candidates, limit,
		func(m models.Model) string { return string(m.ID) },
		func(m models.Model) (int, int, bool) {
			if query == "" {
				return 0, 0, true
			}
			return models.RankMatch(query, m)
		},
	)
	truncated := total > limit

	summaries := make([]api.ManageAgentsModel, len(matched))
	for i, m := range matched {
		summaries[i] = subagentModelSummary(m)
	}

	message := fmt.Sprintf("Found %d models", len(summaries))
	if query != "" {
		message = fmt.Sprintf("Found %d models matching %q", len(summaries), strings.TrimSpace(in.Query))
	}
	if truncated {
		message += fmt.Sprintf(" (showing first %d)", limit)
	}
	return api.ManageAgentsOutput{Models: summaries, Message: message}, nil
}

// subagentModelSummary projects a catalog model onto the compact summary the
// manage_agents "models" action returns.
func subagentModelSummary(m models.Model) api.ManageAgentsModel {
	out := api.ManageAgentsModel{
		ID:           string(m.ID),
		Provider:     m.ID.Provider(),
		Name:         cmp.Or(m.Name, string(m.ID.Name())),
		Description:  m.Description,
		Aliases:      m.AliasRefs(),
		ContextLimit: m.Limits.Context,
	}
	out.Tools = m.Capabilities.Tools != nil && *m.Capabilities.Tools
	out.Attachment = m.Capabilities.Attachment != nil && *m.Capabilities.Attachment
	out.StructuredOutput = m.Capabilities.StructuredOutput != nil && *m.Capabilities.StructuredOutput
	return out
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
	case "web_search":
		return "Searching the web"
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
