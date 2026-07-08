package graph

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	loomsqlite "github.com/masterkeysrd/loom/checkpoint/sqlite"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/tasksmith/internal/agent/prompt"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

type subagentRunnerImpl struct{}

func init() {
	tools.SubagentRunner = &subagentRunnerImpl{}
}

func (r *subagentRunnerImpl) Run(ctx context.Context, opts tools.SubagentOptions) (string, error) {
	// 1. Resolve workspace and defaults
	w := opts.ParentHandlers.Resolver.Workspace
	wsConcrete, _ := w.(*workspace.Workspace)

	resolvedAgent, err := w.ResolveAgent(ctx, opts.AgentRef)
	if err != nil {
		return "", fmt.Errorf("failed to resolve agent %q: %w", opts.AgentRef, err)
	}

	// Render agent system prompt
	var systemPrompt string
	if resolvedAgent != nil {
		var contextInstructions string
		var contextPath string
		contexts := w.Contexts()
		if len(contexts) > 0 {
			var parts []string
			for _, ctxRes := range contexts {
				if inst := ctxRes.Spec.Instructions; inst != "" {
					parts = append(parts, inst)
				}
			}
			contextInstructions = strings.Join(parts, "\n\n")
			contextPath = contexts[0].Directory
		}

		rendered, err := prompt.RenderAgent(
			resolvedAgent,
			w.WorkspaceSpec(),
			w.Project(),
			map[string]any{
				"Context":     contextInstructions,
				"ContextPath": contextPath,
			},
		)
		if err != nil {
			return "", fmt.Errorf("failed to render system prompt template: %w", err)
		}
		systemPrompt = rendered
	}

	// 2. Resolve model and provider
	var providerName, modelName string
	var matchingProvider *warp.ModelProvider

	providers := w.Providers()
	if resolvedAgent != nil && resolvedAgent.Agent != nil && len(resolvedAgent.Agent.Spec.Models) > 0 {
		for _, modelID := range resolvedAgent.Agent.Spec.Models {
			for _, p := range providers {
				for _, mInfo := range p.Spec.Models {
					if mInfo.ID == modelID {
						modelName = modelID
						providerName = p.GetName()
						matchingProvider = p
						break
					}
				}
				if modelName != "" {
					break
				}
			}
			if modelName != "" {
				break
			}
		}

		if modelName == "" {
			modelName = resolvedAgent.Agent.Spec.Models[0]
			spec := w.WorkspaceSpec()
			if spec != nil && spec.Def != nil && spec.Def.Spec.DefaultProvider != "" {
				providerName = spec.Def.Spec.DefaultProvider
				for _, p := range providers {
					if p.GetName() == providerName {
						matchingProvider = p
						break
					}
				}
			}
			if providerName == "" && len(providers) > 0 {
				providerName = providers[0].GetName()
				matchingProvider = providers[0]
			}
		}
	}

	// Fallback to workspace defaults
	if modelName == "" {
		_, defPName, defMName, err := w.ResolveDefaults(ctx)
		if err != nil {
			return "", fmt.Errorf("no default model/provider resolved: %w", err)
		}
		providerName = defPName
		modelName = defMName

		for _, p := range providers {
			if p.GetName() == providerName {
				matchingProvider = p
				break
			}
		}
	}

	var provider llm.Provider
	if matchingProvider == nil {
		return "", fmt.Errorf("provider %q not found in workspace", providerName)
	}
	provider, err = model.CreateProvider(ctx, matchingProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create provider %q: %w", providerName, err)
	}

	if _, found := provider.GetProfile(modelName); !found {
		cleanedName := modelName
		if idx := strings.Index(modelName, "/"); idx != -1 {
			cleanedName = modelName[idx+1:]
		}
		baseName := cleanedName
		if before, _, ok := strings.Cut(cleanedName, ":"); ok {
			baseName = before
		}

		if _, foundCleaned := provider.GetProfile(cleanedName); foundCleaned {
			modelName = cleanedName
		} else if prof, foundBase := provider.GetProfile(baseName); foundBase {
			prof.ID = modelName
			prof.Name = modelName
			provider.OverrideProfile(modelName, prof)
		} else {
			provider.OverrideProfile(modelName, llm.ModelProfile{
				ID:   modelName,
				Name: modelName,
			})
		}
	}

	var agentSettings model.SessionSettings
	loomModelObj, err := model.New(ctx, model.Config{
		Provider:      provider,
		ModelName:     modelName,
		ModelProvider: matchingProvider,
		Agent:         resolvedAgent,
		Settings:      agentSettings,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create model: %w", err)
	}

	// 3. Open checkpoints database
	dbConn, err := coredb.Open(opts.ParentHandlers.CWD, "checkpoints.db")
	if err != nil {
		return "", fmt.Errorf("failed to open checkpoints database: %w", err)
	}
	defer dbConn.Close()

	cp, err := loomsqlite.NewCheckpointer(dbConn.DB)
	if err != nil {
		return "", fmt.Errorf("failed to create checkpointer: %w", err)
	}

	// 4. Initialize storage and compile options
	storage := &subagentFileStorage{
		workspacePath: opts.ParentHandlers.CWD,
		sessionID:     opts.SessionID,
	}

	graphOpts := Options{
		Model:             NewLoomModel(loomModelObj),
		Workspace:         wsConcrete,
		Storage:           storage,
		Inbox:             opts.Inbox,
		TaskManager:       opts.ParentHandlers.TaskManager,
		SessionID:         opts.SessionID,
		SystemPrompt:      systemPrompt,
		AgentName:         opts.AgentRef,
		OnTodosUpdated:    func(ctx context.Context, todos []tools.Todo) error { return nil },
		PermissionManager: opts.ParentHandlers.PermissionManager, // Bubble-up permission cache
		LspManager:        opts.ParentHandlers.LspManager,
		FileTracker:       opts.ParentHandlers.FileTracker,
		McpManager:        opts.ParentHandlers.McpManager,
	}

	ag, err := New(ctx, graphOpts)
	if err != nil {
		return "", fmt.Errorf("failed to create subagent graph: %w", err)
	}

	g, err := ag.Build(cp)
	if err != nil {
		return "", fmt.Errorf("failed to build subagent graph: %w", err)
	}

	loc := &graph.Location{ThreadID: opts.SessionID}

	// Setup initial message injection
	initialMsgs := opts.Inbox.PopMessages()
	inputCmd := graph.Update[AgentState](func(state AgentState) AgentState {
		state.Messages = append(state.Messages, initialMsgs...)
		return state
	})

	var finalOutput string
	for {
		seq, err := g.Stream(ctx, inputCmd, loc)
		if err != nil {
			return "", err
		}

		for ev, err := range seq {
			if err != nil {
				return "", err
			}

			// Pipe thinking and text to task stdout
			switch ev.Event {
			case graph.EventLLMChunk:
				var textChunk string
				var thinkingChunk string
				hasTypedChunks := false
				switch d := ev.Data.(type) {
				case message.AssistantChunk:
					hasTypedChunks = true
					textChunk = message.Content(d.Content).Text()
					thinkingChunk = message.Content(d.Content).Thought()
				case *message.AssistantChunk:
					hasTypedChunks = true
					textChunk = message.Content(d.Content).Text()
					thinkingChunk = message.Content(d.Content).Thought()
				case string:
					if !hasTypedChunks {
						textChunk = d
					}
				}

				if thinkingChunk != "" {
					fmt.Fprint(opts.Stdout, thinkingChunk)
				}
				if textChunk != "" {
					fmt.Fprint(opts.Stdout, textChunk)
				}
			case "agent_message":
				if msg, ok := ev.Data.(message.Message); ok {
					for _, block := range msg.GetContent() {
						if tc, ok := block.(*message.ToolCall); ok {
							fmt.Fprintf(opts.Stdout, "\n[Tool Call: %s(%v)]\n", tc.Name, tc.Args)
						}
					}
				}
			case "tool_message":
				if msg, ok := ev.Data.(message.Message); ok {
					if tMsg, ok := msg.(*message.Tool); ok {
						var text string
						for _, block := range tMsg.GetContent() {
							if tb, ok := block.(*message.TextBlock); ok {
								text += tb.Text
							}
						}
						if tMsg.IsError {
							fmt.Fprintf(opts.Stderr, "\n[Tool Error: %s -> %s]\n", tMsg.Name, text)
						} else {
							fmt.Fprintf(opts.Stdout, "\n[Tool Response: %s -> %s]\n", tMsg.Name, text)
						}
					}
				}
			}
		}

		if snap, err := g.Load(ctx, *loc); err == nil && len(snap.State.Messages) > 0 {
			lastMsg := snap.State.Messages[len(snap.State.Messages)-1]
			if lastMsg.Role() == message.RoleAssistant {
				hasToolCalls := false
				for _, block := range lastMsg.GetContent() {
					if _, ok := block.(*message.ToolCall); ok {
						hasToolCalls = true
						break
					}
				}
				if !hasToolCalls {
					var textParts []string
					for _, block := range lastMsg.GetContent() {
						if tb, ok := block.(*message.TextBlock); ok {
							textParts = append(textParts, tb.Text)
						}
					}
					finalOutput = strings.Join(textParts, "")
					if runner, ok := opts.Inbox.(*tools.AgentRunner); ok {
						runner.SetResult(finalOutput)
					}
				}
			}
		}

		if opts.Mode == "transient" {
			break
		}

		// Suspend: interactive mode wait loop for follow-up inputs
		var newMsgs []message.Message
		ticker := time.NewTicker(100 * time.Millisecond)
		for len(newMsgs) == 0 {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return finalOutput, ctx.Err()
			case <-ticker.C:
				newMsgs = opts.Inbox.PopMessages()
			}
		}
		ticker.Stop()

		inputCmd = graph.Update[AgentState](func(state AgentState) AgentState {
			state.Messages = append(state.Messages, newMsgs...)
			return state
		})
	}

	return finalOutput, nil
}

type subagentFileStorage struct {
	workspacePath string
	sessionID     string
}

func (l *subagentFileStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	wsDir, err := xdg.WorkspaceDir(l.workspacePath)
	if err != nil {
		return "", err
	}

	destPath := filepath.Join(wsDir, "sessions", l.sessionID, relativePath)
	destDir := filepath.Dir(destPath)

	if err := fsutil.EnsureDir(destDir); err != nil {
		return "", err
	}

	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return "", err
	}

	return destPath, nil
}

func (l *subagentFileStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	wsDir, err := xdg.WorkspaceDir(l.workspacePath)
	if err != nil {
		return nil, err
	}

	destPath := filepath.Join(wsDir, "sessions", l.sessionID, relativePath)
	return os.Open(destPath)
}
