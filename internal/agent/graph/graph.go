package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/stream"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/mcp"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

// AgentState represents the state passed between nodes in the agent loop.
type AgentState struct {
	Messages              message.MessageList                 `json:"messages"`
	Todos                 []tools.Todo                        `json:"todos"`
	ActivatedSkills       []string                            `json:"activated_skills"`
	PendingAuthorizations []permissions.AuthorizationRequest  `json:"pending_authorizations,omitempty"`
	Decisions             []permissions.AuthorizationDecision `json:"decisions,omitempty"`
	ExecutionCancelled    bool                                `json:"execution_cancelled,omitempty"`
}

// Copy performs a deep copy of AgentState to satisfy the loom graph.State interface.
func (s AgentState) Copy() AgentState {
	copied := AgentState{}
	if s.Messages != nil {
		copied.Messages = make(message.MessageList, len(s.Messages))
		copy(copied.Messages, s.Messages)
	}
	if s.Todos != nil {
		copied.Todos = make([]tools.Todo, len(s.Todos))
		copy(copied.Todos, s.Todos)
	}
	if s.ActivatedSkills != nil {
		copied.ActivatedSkills = make([]string, len(s.ActivatedSkills))
		copy(copied.ActivatedSkills, s.ActivatedSkills)
	}
	if s.PendingAuthorizations != nil {
		copied.PendingAuthorizations = make([]permissions.AuthorizationRequest, len(s.PendingAuthorizations))
		copy(copied.PendingAuthorizations, s.PendingAuthorizations)
	}
	if s.Decisions != nil {
		copied.Decisions = make([]permissions.AuthorizationDecision, len(s.Decisions))
		copy(copied.Decisions, s.Decisions)
	}
	copied.ExecutionCancelled = s.ExecutionCancelled
	return copied
}

// LLM defines the interface for model invocations.
type LLM interface {
	Invoke(ctx context.Context, messages []message.Message) (*message.Assistant, error)
}

// LLMModel defines the interface for a model that can bind tools.
type LLMModel interface {
	LLM
	BindTools(tools ...*tool.Tool) LLMModel
}

type loomModel struct {
	model *llm.Model
}

func (m loomModel) Invoke(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
	return m.model.Invoke(ctx, messages)
}

func (m loomModel) BindTools(tools ...*tool.Tool) LLMModel {
	return loomModel{model: m.model.BindTools(tools...)}
}

// NewLoomModel wraps a *llm.Model into an LLMModel interface.
func NewLoomModel(m *llm.Model) LLMModel {
	return loomModel{model: m}
}

// InboxProvider defines the interface to retrieve pending user messages.
type InboxProvider = tools.InboxProvider

// AgentGraph orchestrates the flow of model invocation.
type AgentGraph struct {
	model             LLM
	container         *tool.Container
	inbox             InboxProvider
	systemPrompt      string
	agentName         string
	onTodosUpdated    func(ctx context.Context, todos []tools.Todo) error
	permissionManager permissions.PermissionManager
	cwd               string
	lspManager        *lsp.Manager
	storage           tools.FileStorage
	fileTracker       filetrack.FileTracker
}

// Options defines the configurations and dependencies to initialize the AgentGraph.
type Options struct {
	Model             LLMModel
	Workspace         *workspace.Workspace
	Storage           tools.FileStorage
	Inbox             InboxProvider
	TaskManager       *tools.TaskManager
	SessionID         string
	SystemPrompt      string
	AgentName         string
	OnTodosUpdated    func(ctx context.Context, todos []tools.Todo) error
	PermissionManager permissions.PermissionManager
	LspManager        *lsp.Manager
	FileTracker       filetrack.FileTracker
	McpManager        *mcp.Manager
}

// New creates a new AgentGraph orchestrator by loading/binding tools outside of the execution nodes.
func New(ctx context.Context, opts Options) (*AgentGraph, error) {
	var allowedTools map[string]bool
	if opts.Workspace != nil {
		cfg, err := opts.Workspace.GetWorkspaceConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace config: %w", err)
		}
		allowedTools = cfg.AuthorizedTools
	}

	if opts.Workspace != nil && opts.AgentName != "" {
		if agent, err := opts.Workspace.ResolveAgent(ctx, opts.AgentName); err == nil && agent != nil && agent.Agent != nil {
			if agent.Agent.Spec.Policies != nil && agent.Agent.Spec.Policies.Tools != nil {
				agentAllowed := make(map[string]bool)
				for _, tName := range agent.Agent.Spec.Policies.Tools.Include {
					if allowedTools == nil || tools.IsToolAllowed(tName, allowedTools) {
						agentAllowed[tName] = true
					}
				}
				allowedTools = agentAllowed
			}
		}
	}

	var cwd string
	if opts.Workspace != nil {
		cwd = opts.Workspace.CWD()
	}

	pm := opts.PermissionManager
	if pm == nil {
		var err error
		pm, err = permissions.NewFSManager(cwd, opts.SessionID)
		if err != nil {
			return nil, fmt.Errorf("failed to create permission manager: %w", err)
		}
	}
	if fsm, ok := pm.(*permissions.FSManager); ok {
		fsm.SetWorkspaceInitializedFn(func() bool {
			if opts.Workspace == nil {
				return false
			}
			cfg, err := opts.Workspace.GetWorkspaceConfig(ctx)
			if err != nil {
				return false
			}
			return cfg.IsConfigured
		})
	}

	var mcps []*warp.MCP
	if opts.Workspace != nil {
		mcps = opts.Workspace.MCPs()
	}

	r := resolver.New(resolver.Config{
		Lsp:         opts.LspManager,
		Cwd:         cwd,
		FileTracker: opts.FileTracker,
		Storage:     opts.Storage,
		Workspace:   opts.Workspace,
	})

	handlers := tools.NewHandlers(opts.Storage, cwd).
		WithTaskManager(opts.TaskManager, opts.SessionID).
		WithResolver(r).
		WithAgentName(opts.AgentName).
		WithPermissionManager(pm).
		WithLspManager(opts.LspManager).
		WithFileTracker(opts.FileTracker).
		WithMcpManager(opts.McpManager)

	activeTools, err := tools.LoadActiveTools(ctx, handlers, allowedTools, mcps)
	if err != nil {
		return nil, fmt.Errorf("failed to load active tools: %w", err)
	}

	var boundModel LLM = opts.Model
	if len(activeTools) > 0 && opts.Model != nil {
		boundModel = opts.Model.BindTools(activeTools...)
	}

	var container *tool.Container
	if len(activeTools) > 0 {
		container = tool.NewContainer(activeTools...)
	}

	return &AgentGraph{
		model:             boundModel,
		container:         container,
		inbox:             opts.Inbox,
		systemPrompt:      opts.SystemPrompt,
		agentName:         opts.AgentName,
		onTodosUpdated:    opts.OnTodosUpdated,
		permissionManager: pm,
		cwd:               cwd,
		lspManager:        opts.LspManager,
		storage:           opts.Storage,
		fileTracker:       opts.FileTracker,
	}, nil
}

// Build compiles the graph using the loom builder.
func (a *AgentGraph) Build(cp graph.Checkpointer) (*graph.Graph[AgentState], error) {
	builder := graph.New[AgentState]().
		WithName("agent_loop").
		AddNode("check_inbox", graph.NodeFunc(a.checkInbox)).
		AddNode("think", graph.NodeFunc(a.think)).
		AddNode("execute_tools", graph.NodeFunc(a.executeTools)).
		AddEdge(graph.START, "check_inbox")

	builder.AddRouteEdge("check_inbox", func(s AgentState) (string, error) {
		log.Info(fmt.Sprintf("[AgentGraph] check_inbox router: s.ExecutionCancelled=%t s.Messages=%d", s.ExecutionCancelled, len(s.Messages)))
		if s.ExecutionCancelled {
			log.Info("[AgentGraph] check_inbox router: returning graph.END because s.ExecutionCancelled is true")
			return graph.END, nil
		}
		if len(s.Messages) == 0 {
			log.Info("[AgentGraph] check_inbox router: returning graph.END because s.Messages is empty")
			return graph.END, nil
		}
		lastMsg := s.Messages[len(s.Messages)-1]
		if lastMsg.Role() == message.RoleAssistant {
			log.Info("[AgentGraph] check_inbox router: returning graph.END because lastMsg is Assistant")
			return graph.END, nil
		}
		log.Info("[AgentGraph] check_inbox router: returning think")
		return "think", nil
	}, map[string]string{
		"think":   "think",
		graph.END: graph.END,
	})

	builder.AddRouteEdge("think", func(s AgentState) (string, error) {
		if len(s.Messages) == 0 {
			return "check_inbox", nil
		}
		lastMsg := s.Messages[len(s.Messages)-1]
		for _, block := range lastMsg.GetContent() {
			if _, ok := block.(*message.ToolCall); ok {
				return "execute_tools", nil
			}
		}
		return "check_inbox", nil
	}, map[string]string{
		"execute_tools": "execute_tools",
		"check_inbox":   "check_inbox",
	})

	builder.AddEdge("execute_tools", "check_inbox")

	if cp != nil {
		builder.WithCheckpointer(cp)
	}

	return builder.Build()
}

// think node queries the LLM and appends the result to the history.
func (a *AgentGraph) think(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	if a.model == nil {
		return nil, fmt.Errorf("no model configured in graph")
	}

	rehydratedMessages := tools.RehydrateMessagesForLLM(s.Messages)

	sp := a.systemPrompt

	if sp != "" {
		systemMsg := message.NewSystemText(sp)
		rehydratedMessages = append([]message.Message{systemMsg}, rehydratedMessages...)
	}

	msg, err := a.model.Invoke(ctx, rehydratedMessages)
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}

	if a.agentName != "" {
		meta := msg.GetMetadata()
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["agent_name"] = a.agentName
		msg.SetMetadata(meta)
	}

	sw, hasWriter := stream.WriterFromContext(ctx)
	if hasWriter {
		_ = sw.Write(ctx, stream.Event{
			Name: "agent_message",
			Data: msg,
		})
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		state.Messages = append(state.Messages, msg)
		return state
	}), nil
}

// interruptUpdate implements graph.Command and graph.Interruptable.
type interruptUpdate struct {
	fn func(AgentState) AgentState
}

func (c interruptUpdate) Apply(s AgentState) AgentState {
	return c.fn(s)
}

func (c interruptUpdate) Interrupt() {}

// executeTools executes any tool calls from the last assistant message.
func (a *AgentGraph) executeTools(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	if a.container == nil {
		return nil, fmt.Errorf("no tools container configured in graph")
	}

	lastMsg := s.Messages[len(s.Messages)-1]
	var toolResults []message.Message

	sw, hasWriter := stream.WriterFromContext(ctx)

	var pendingRequests []permissions.AuthorizationRequest
	interruptRequired := false

	decisionMap := make(map[string]permissions.AuthorizationDecision)
	log.Info(fmt.Sprintf("[AgentGraph] executeTools: s.Decisions count=%d", len(s.Decisions)))
	for i, dec := range s.Decisions {
		log.Info(fmt.Sprintf("[AgentGraph] decision %d: ToolCallID=%q, Approved=%t, CancelExecution=%t, Reason=%q", i, dec.ToolCallID, dec.Approved, dec.CancelExecution, dec.Reason))
		decisionMap[dec.ToolCallID] = dec
	}

	// Check for hard cancel: if any decision requests execution cancellation,
	// generate cancellation messages for all tool calls and halt the graph.
	for _, dec := range s.Decisions {
		if dec.CancelExecution {
			for _, block := range lastMsg.GetContent() {
				if tc, ok := block.(*message.ToolCall); ok {
					toolMsg := &message.Tool{
						ToolCallID: tc.ID,
						Name:       tc.Name,
						IsError:    true,
						Content:    message.Content{&message.TextBlock{Text: "Execution cancelled by user."}},
					}
					toolResults = append(toolResults, toolMsg)
					if hasWriter {
						_ = sw.Write(ctx, stream.Event{
							Name: "tool_message",
							Data: toolMsg,
						})
					}
				}
			}
			return graph.Update[AgentState](func(state AgentState) AgentState {
				log.Info("[AgentGraph] executeTools update function running: setting ExecutionCancelled = true")
				state.Messages = append(state.Messages, toolResults...)
				state.PendingAuthorizations = nil
				state.Decisions = nil
				state.ExecutionCancelled = true
				return state
			}), nil
		}
	}

	completedResults := make(map[string]message.Message)
	for _, msg := range s.Messages {
		if tMsg, ok := msg.(*message.Tool); ok {
			completedResults[tMsg.ToolCallID] = tMsg
		}
	}

	type evalResult struct {
		toolCall   *message.ToolCall
		permState  permissions.PermissionState
		finalArgs  map[string]any
		targetTool *tool.Tool
	}
	var toExecute []evalResult

	for _, block := range lastMsg.GetContent() {
		tc, ok := block.(*message.ToolCall)
		if !ok {
			continue
		}

		if existingResult, ok := completedResults[tc.ID]; ok {
			toolResults = append(toolResults, existingResult)
			continue
		}

		args := tc.Args
		var decision *permissions.AuthorizationDecision
		if dec, ok := decisionMap[tc.ID]; ok {
			decision = &dec
		}

		var targetTool *tool.Tool
		for _, t := range a.container.ListTools() {
			if t.Definition.Name == tc.Name {
				targetTool = t
				break
			}
		}
		if targetTool == nil {
			toolMsg := &message.Tool{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				IsError:    true,
				Content:    message.Content{&message.TextBlock{Text: fmt.Sprintf("tool %q not found in container", tc.Name)}},
			}
			toolResults = append(toolResults, toolMsg)
			continue
		}

		if err := targetTool.Validate(args); err != nil {
			toolMsg := &message.Tool{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				IsError:    true,
				Content:    message.Content{&message.TextBlock{Text: fmt.Sprintf("invalid arguments for tool %q: %v", tc.Name, err)}},
			}
			toolResults = append(toolResults, toolMsg)
			continue
		}

		userHint := targetTool.Annotation.UserHint
		if d, ok := args["description"].(string); ok && d != "" {
			userHint = d
		}

		permState, finalArgs, _, authReq := permissions.EvaluateToolCall(
			permissions.ContextWithWorkspaceCWD(ctx, a.cwd),
			a.permissionManager,
			permissions.ToolCallRequest{
				ToolName:    tc.Name,
				Args:        args,
				Description: targetTool.Definition.Description,
				UserHint:    userHint,
				IsDangerous: targetTool.Annotation.IsDangerous,
				IsOpenWorld: targetTool.Annotation.IsOpenWorld,
				IsReadOnly:  targetTool.Annotation.IsReadOnly,
			},
			decision,
		)
		if authReq != nil {
			authReq.ToolCallID = tc.ID
		}

		if permState == permissions.StateRequiresAuth {
			if authReq != nil {
				pendingRequests = append(pendingRequests, *authReq)
			}
			interruptRequired = true
		} else {
			toExecute = append(toExecute, evalResult{
				toolCall:   tc,
				permState:  permState,
				finalArgs:  finalArgs,
				targetTool: targetTool,
			})
		}
	}

	if !interruptRequired {
		for _, er := range toExecute {
			tc := er.toolCall
			switch er.permState {
			case permissions.StateExplicitDeny:
				text := fmt.Sprintf("Authorization denied by user for tool %q", tc.Name)
				dec := decisionMap[tc.ID]
				if dec.Reason != "" {
					text = fmt.Sprintf("Authorization denied by user for tool %q: %s", tc.Name, dec.Reason)
				}
				toolMsg := &message.Tool{
					ToolCallID: tc.ID,
					Name:       tc.Name,
					IsError:    true,
					Content:    message.Content{&message.TextBlock{Text: text}},
				}
				if dec.Reason != "" {
					meta := toolMsg.GetMetadata()
					if meta == nil {
						meta = make(map[string]any)
					}
					meta["deny_reason"] = dec.Reason
					toolMsg.SetMetadata(meta)
				}
				toolResults = append(toolResults, toolMsg)
				if hasWriter {
					_ = sw.Write(ctx, stream.Event{
						Name: "tool_message",
						Data: toolMsg,
					})
				}

			default: // StateExplicitAllow / StateAuto
				tc.Args = er.finalArgs
				var toolMsg *message.Tool
				toolCallCtx := context.WithValue(ctx, "tool_call_id", tc.ID)

				start := time.Now()
				toolResp, err := a.container.Call(toolCallCtx, tc)
				execTime := time.Since(start).Milliseconds()

				if err != nil {
					toolMsg = &message.Tool{
						ToolCallID: tc.ID,
						Name:       tc.Name,
						IsError:    true,
						Content:    message.Content{&message.TextBlock{Text: err.Error()}},
					}
				} else {
					toolMsg = tools.ProcessMcpOutput(ctx, tc, toolResp, a.storage)
				}

				meta := toolMsg.GetMetadata()
				if meta == nil {
					meta = make(map[string]any)
				}
				meta["execution_time_ms"] = execTime
				if er.permState == permissions.StateAuto {
					meta["auto_approved"] = true
				}
				toolMsg.SetMetadata(meta)

				toolResults = append(toolResults, toolMsg)

				if hasWriter {
					_ = sw.Write(ctx, stream.Event{
						Name: "tool_message",
						Data: toolMsg,
					})
				}
			}
		}
	}

	if interruptRequired {
		return interruptUpdate{
			fn: func(state AgentState) AgentState {
				for _, res := range toolResults {
					found := false
					for _, msg := range state.Messages {
						if tMsg, ok := msg.(*message.Tool); ok && tMsg.ToolCallID == res.(*message.Tool).ToolCallID {
							found = true
							break
						}
					}
					if !found {
						state.Messages = append(state.Messages, res)
					}
				}

				state.PendingAuthorizations = pendingRequests

				// Filter out decisions that were handled
				var remainingDecisions []permissions.AuthorizationDecision
				for _, dec := range state.Decisions {
					resolved := false
					for _, req := range pendingRequests {
						if req.ToolCallID == dec.ToolCallID {
							resolved = true
							break
						}
					}
					for _, res := range toolResults {
						if res.(*message.Tool).ToolCallID == dec.ToolCallID {
							resolved = true
							break
						}
					}
					if !resolved {
						remainingDecisions = append(remainingDecisions, dec)
					}
				}
				state.Decisions = remainingDecisions

				return state
			},
		}, nil
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		for _, res := range toolResults {
			found := false
			for _, msg := range state.Messages {
				if tMsg, ok := msg.(*message.Tool); ok && tMsg.ToolCallID == res.(*message.Tool).ToolCallID {
					found = true
					break
				}
			}
			if !found {
				state.Messages = append(state.Messages, res)
			}
		}

		state.PendingAuthorizations = nil
		state.Decisions = nil

		for _, res := range toolResults {
			if tMsg, ok := res.(*message.Tool); ok {
				if tMsg.IsError {
					continue
				}
				for _, block := range lastMsg.GetContent() {
					if tc, ok := block.(*message.ToolCall); ok && tc.ID == tMsg.ToolCallID {
						if hook, ok := toolStateHooks[tc.Name]; ok {
							if updateFn, err := hook(ctx, tc.Args, a); err == nil && updateFn != nil {
								state = updateFn(state)
							}
						}
					}
				}
			}
		}

		return state
	}), nil
}

// checkInbox checks for new user messages in the inbox and appends them to the execution state.
func (a *AgentGraph) checkInbox(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	log.Info(fmt.Sprintf("[AgentGraph] checkInbox node: s.ExecutionCancelled=%t s.Messages=%d", s.ExecutionCancelled, len(s.Messages)))
	if s.ExecutionCancelled {
		log.Info("[AgentGraph] checkInbox node: returning immediately because s.ExecutionCancelled is true")
		return graph.Update[AgentState](func(state AgentState) AgentState {
			return state
		}), nil
	}
	if a.inbox == nil {
		return graph.Update[AgentState](func(state AgentState) AgentState {
			return state
		}), nil
	}

	msgs := a.inbox.PopMessages()
	if len(msgs) == 0 {
		return graph.Update[AgentState](func(state AgentState) AgentState {
			return state
		}), nil
	}

	sw, hasWriter := stream.WriterFromContext(ctx)
	if hasWriter {
		for _, msg := range msgs {
			_ = sw.Write(ctx, stream.Event{
				Name: "user_message",
				Data: msg,
			})
		}
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		if len(msgs) > 0 {
			InjectReminders(ctx, msgs[len(msgs)-1], state, a.lspManager, a.cwd)
		}
		state.Messages = append(state.Messages, msgs...)
		return state
	}), nil
}

// InjectReminders appends system reminders to the user message.
func InjectReminders(ctx context.Context, msg message.Message, s AgentState, lspManager *lsp.Manager, cwd string) {
	userMsg, ok := msg.(*message.User)
	if !ok {
		return
	}

	meta := userMsg.GetMetadata()
	if meta != nil {
		if isSys, ok := meta["is_system_notification"].(bool); ok && isSys {
			return
		}
	}

	var reminders []string
	if len(s.Todos) == 0 {
		reminders = append(reminders, "Your todos list is currently empty. If a task is non-trivial, you must establish a plan first. Use the `todos` tool to create your initial task list before you do anything else—including reading project plans, fetching files, or exploring the codebase. You can always update the todos later as you discover more. Do not mention this reminder or the empty state to the user.")
	}

	if len(s.ActivatedSkills) == 0 {
		reminders = append(reminders, "You do not have any skills activated. If the user's request matches one of your available skills, you must activate it using the 'activate_skill' tool first.")
	}

	if lspManager != nil && cwd != "" {
		diagStr := tools.GetTopWorkspaceDiagnosticsString(ctx, lspManager, cwd)
		if diagStr != "" {
			reminders = append(reminders, "Current Workspace Diagnostics (if you fix any of these, do not repeatedly call LspDiagnostics unless necessary, trust the live feedback if available):\n"+diagStr)
		}
	}

	if len(reminders) > 0 {
		reminderBlock := fmt.Sprintf("<system_reminder>\n%s\n</system_reminder>", strings.Join(reminders, "\n\n"))
		userMsg.Content = append(userMsg.Content, &message.TextBlock{Text: "\n\n" + reminderBlock})
	}
}
