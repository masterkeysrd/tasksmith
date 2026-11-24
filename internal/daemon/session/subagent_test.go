package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

func TestSessionParentChildPersistenceAndScan(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })
	st, err := NewStore(ctx, db)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	rootSess, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Root Session",
		WorkDir:     ws.RootDir(),
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Catalog:     nil,
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New root session failed: %v", err)
	}

	if err := rootSess.CheckpointTo(ctx, st); err != nil {
		t.Fatalf("CheckpointTo failed: %v", err)
	}

	childSess, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Child Session",
		ParentID:    rootSess.ID,
		WorkDir:     ws.RootDir(),
		Parent:      rootSess,
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New child session failed: %v", err)
	}

	if err := childSess.CheckpointTo(ctx, st); err != nil {
		t.Fatalf("CheckpointTo child failed: %v", err)
	}

	// Read child back from store
	loadedChild, err := st.Get(ctx, childSess.ID)
	if err != nil {
		t.Fatalf("st.Get child failed: %v", err)
	}
	if loadedChild.ParentID != rootSess.ID {
		t.Errorf("loadedChild.ParentID = %q, want %q", loadedChild.ParentID, rootSess.ID)
	}
	if loadedChild.WorkDir != ws.RootDir() {
		t.Errorf("loadedChild.WorkDir = %q, want %q", loadedChild.WorkDir, ws.RootDir())
	}

	// Read all from store
	all, err := st.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("st.List failed: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(all))
	}

	// Filter by parent_id
	children, err := st.List(ctx, ListFilter{ParentID: new(rootSess.ID)})
	if err != nil {
		t.Fatalf("st.List with ParentID failed: %v", err)
	}
	if len(children) != 1 || children[0].ID != childSess.ID {
		t.Fatalf("expected 1 child session with id %q, got %d", childSess.ID, len(children))
	}

	roots, err := st.List(ctx, ListFilter{ParentID: new("")})
	if err != nil {
		t.Fatalf("st.List with empty ParentID failed: %v", err)
	}
	if len(roots) != 1 || roots[0].ID != rootSess.ID {
		t.Fatalf("expected 1 root session with id %q, got %d", rootSess.ID, len(roots))
	}

	none, err := st.List(ctx, ListFilter{ParentID: new("nonexistent")})
	if err != nil {
		t.Fatalf("st.List with nonexistent ParentID failed: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected 0 sessions for nonexistent parent, got %d", len(none))
	}
}

func TestSessionChildManagement(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}

	child1, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Child 1",
		Parent:      parent,
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New child1 failed: %v", err)
	}

	// Check shared PermManager state with isolated AgentPolicies view.
	if child1.PermManager == nil {
		t.Fatalf("expected non-nil child.PermManager")
	}
	if child1.PermManager == parent.PermManager {
		t.Errorf("expected child.PermManager to be a scoped ForSubagent view, got identical pointer")
	}
	if err := parent.PermManager.AddAllow("bash(go test *)"); err != nil {
		t.Fatalf("parent.PermManager.AddAllow: %v", err)
	}
	if got := child1.PermManager.Permissions().Allow; len(got) != 1 || got[0] != "bash(go test *)" {
		t.Errorf("expected child.PermManager to share session allow rules with parent, got %v", got)
	}

	parent.AddChild(child1)
	if len(parent.Children()) != 1 {
		t.Fatalf("expected 1 child, got %d", len(parent.Children()))
	}

	retrieved, ok := parent.Child(child1.ID)
	if !ok || retrieved.ID != child1.ID {
		t.Fatalf("Child(%q) not found or mismatch", child1.ID)
	}

	if child1.Parent() != parent {
		t.Errorf("child1.Parent() != parent")
	}
	if child1.ParentID != parent.ID {
		t.Errorf("child1.ParentID = %q, want %q", child1.ParentID, parent.ID)
	}

	// Remove child
	parent.RemoveChild(child1.ID)
	if len(parent.Children()) != 0 {
		t.Fatalf("expected 0 children after remove, got %d", len(parent.Children()))
	}
}

func TestToAPISessionSubagents(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer parent.Close()

	child, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Subagent Session",
		Parent:      parent,
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New child failed: %v", err)
	}
	parent.AddChild(child)

	apiParent := ToAPISession(parent)
	if len(apiParent.Subagents) != 1 {
		t.Fatalf("expected 1 subagent in API session, got %d", len(apiParent.Subagents))
	}
	if apiParent.Subagents[0].ID != child.ID {
		t.Errorf("apiParent.Subagents[0].ID = %q, want %q", apiParent.Subagents[0].ID, child.ID)
	}
	if apiParent.Subagents[0].ParentID != parent.ID {
		t.Errorf("apiParent.Subagents[0].ParentID = %q, want %q", apiParent.Subagents[0].ParentID, parent.ID)
	}
}

func TestRenderSystemPromptSubagentContext(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })

	rootSess, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Root",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New root failed: %v", err)
	}

	rootPrompt := rootSess.renderSystemPrompt("main")
	if strings.Contains(rootPrompt, "<subagent_context>") {
		t.Errorf("root session prompt should not contain <subagent_context>")
	}

	childSess, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Delegated Task",
		Parent:      rootSess,
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New child failed: %v", err)
	}

	childPrompt := childSess.renderSystemPrompt("explore")
	if !strings.Contains(childPrompt, "<subagent_context>") {
		t.Fatalf("child session prompt must contain <subagent_context>, got:\n%s", childPrompt)
	}
	if !strings.Contains(childPrompt, childSess.ID) {
		t.Errorf("child prompt should contain session ID %q", childSess.ID)
	}
	if !strings.Contains(childPrompt, "explore") {
		t.Errorf("child prompt should contain agent name 'explore'")
	}
}

func TestRenderSystemPromptExploreProjectContext(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })

	// A workspace with an AGENTS.md project context file whose Markdown body
	// populates the Context instructions.
	const agentsMd = "---\napiVersion: warp/v1alpha1\nkind: Context\nmetadata:\n  name: root-context\n---\nUse tabs for Go indentation and always run gofmt.\n"
	if err := os.WriteFile(filepath.Join(ws.RootDir(), "AGENTS.md"), []byte(agentsMd), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}

	rootSess, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Root",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New root failed: %v", err)
	}

	childSess, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Delegated Task",
		Parent:      rootSess,
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New child failed: %v", err)
	}

	childPrompt := childSess.renderSystemPrompt("explore")
	if !strings.Contains(childPrompt, "<project_context") {
		t.Fatalf("explore prompt must contain <project_context> block, got:\n%s", childPrompt)
	}
	if !strings.Contains(childPrompt, "Use tabs for Go indentation and always run gofmt.") {
		t.Errorf("explore prompt must contain AGENTS.md instructions, got:\n%s", childPrompt)
	}
}

func TestInvokeSubagentAndManageSubagents(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Subagent answer"}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer parent.Close()

	// InvokeSubagent with validation errors
	_, err = parent.InvokeSubagent(ctx, api.InvokeAgentInput{Title: "Title", Prompt: "Prompt"})
	if err == nil {
		t.Fatal("expected error when agent is empty")
	}
	_, err = parent.InvokeSubagent(ctx, api.InvokeAgentInput{Agent: "explore", Prompt: "Prompt"})
	if err == nil {
		t.Fatal("expected error when title is empty")
	}
	_, err = parent.InvokeSubagent(ctx, api.InvokeAgentInput{Agent: "explore", Title: "Title"})
	if err == nil {
		t.Fatal("expected error when prompt is empty")
	}

	// Invoke valid subagent
	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:     "explore",
		Title:     "Explore codebase",
		Prompt:    "Find all interfaces",
		Workspace: "inherit",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent failed: %v", err)
	}
	if out.SessionID == "" || out.Status != "running" {
		t.Fatalf("unexpected InvokeSubagent output: %+v", out)
	}

	// Child is registered in parent.Children()
	children := parent.Children()
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	child := children[0]
	if child.ID != out.SessionID {
		t.Errorf("child.ID = %q, want %q", child.ID, out.SessionID)
	}

	// Wait for child turn to complete
	if err := child.Wait(ctx); err != nil {
		t.Fatalf("child.Wait failed: %v", err)
	}
	// Wait for parent turn processing the completion notification
	_ = parent.Wait(ctx)

	// ManageSubagents: list
	listOut, err := parent.ManageSubagents(ctx, api.ManageAgentsInput{Action: "list"})
	if err != nil {
		t.Fatalf("ManageSubagents list failed: %v", err)
	}
	if len(listOut.Agents) != 1 || listOut.Agents[0].SessionID != child.ID {
		t.Fatalf("unexpected list output: %+v", listOut)
	}

	// ManageSubagents: status
	statusOut, err := parent.ManageSubagents(ctx, api.ManageAgentsInput{Action: "status", SessionID: child.ID})
	if err != nil {
		t.Fatalf("ManageSubagents status failed: %v", err)
	}
	if statusOut.SessionID != child.ID || statusOut.Status != string(api.StatusIdle) {
		t.Errorf("unexpected status output: %+v", statusOut)
	}

	// ManageSubagents: kill
	killOut, err := parent.ManageSubagents(ctx, api.ManageAgentsInput{Action: "kill", SessionID: child.ID})
	if err != nil {
		t.Fatalf("ManageSubagents kill failed: %v", err)
	}
	if killOut.SessionID != child.ID {
		t.Errorf("killOut.SessionID = %q, want %q", killOut.SessionID, child.ID)
	}

	// ManageSubagents: not found
	_, err = parent.ManageSubagents(ctx, api.ManageAgentsInput{Action: "status", SessionID: "unknown"})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestChildTurnCompletionNotification(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Found 5 components."}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer parent.Close()

	// Spawn subagent
	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Find components",
		Prompt: "List components",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent failed: %v", err)
	}

	child, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child not found in parent")
	}

	// Wait for child to complete
	if err := child.Wait(ctx); err != nil {
		t.Fatalf("child.Wait failed: %v", err)
	}

	// Wait for parent turn to process the completion message
	if err := parent.Wait(ctx); err != nil {
		t.Fatalf("parent.Wait failed: %v", err)
	}

	state, err := parent.Harness.LoadState(ctx)
	if err != nil {
		t.Fatalf("parent.Harness.LoadState failed: %v", err)
	}

	var foundSubagentCompleted bool
	for _, m := range state.Messages {
		if user, ok := m.(*message.User); ok && harness.SystemKindOf(user) == harness.SystemKindSubagent {
			foundSubagentCompleted = true
			text := user.GetContent().Text()
			if !strings.Contains(text, "<subagent_completed") {
				t.Errorf("expected <subagent_completed> XML tag, got: %s", text)
			}
			if !strings.Contains(text, child.ID) {
				t.Errorf("expected session_id %q in completion text: %s", child.ID, text)
			}
			if !strings.Contains(text, "Found 5 components.") {
				t.Errorf("expected child output in completion text: %s", text)
			}
		}
	}
	if !foundSubagentCompleted {
		t.Fatal("expected parent harness state messages to receive subagent completion message")
	}
}

func TestParentCancelPreservesInvokeAgentSubagents(t *testing.T) {
	ctx := context.Background()
	childStarted := make(chan struct{})
	releaseChild := make(chan struct{})
	parentStarted := make(chan struct{})

	var childOnce sync.Once
	var blockedParent atomic.Bool
	prov := &fakeProvider{
		handler: func(callCtx context.Context, req *llm.Request) ([]string, string, error) {
			var promptText string
			for _, m := range req.Messages {
				promptText += m.GetContent().Text() + "\n"
			}
			if strings.Contains(promptText, "SUBAGENT_BG_WORK") {
				childOnce.Do(func() { close(childStarted) })
				select {
				case <-releaseChild:
					return []string{"Background exploration finished."}, uuid.NewString(), nil
				case <-callCtx.Done():
					return nil, "", callCtx.Err()
				}
			}
			if strings.Contains(promptText, "PARENT_CANCEL_TARGET") && !strings.Contains(promptText, "<subagent_completed") && !blockedParent.Swap(true) {
				close(parentStarted)
				<-callCtx.Done()
				return nil, "", callCtx.Err()
			}
			return []string{"Processed background subagent result."}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer func() { _ = parent.Close() }()

	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Explore repo",
		Prompt: "SUBAGENT_BG_WORK",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent failed: %v", err)
	}
	<-childStarted

	child, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child not found in parent")
	}
	if child.IsCommandDelegated() {
		t.Fatal("invoke_agent child must not be marked CommandDelegated")
	}

	if err := parent.Send(ctx, api.SendRequest{Content: "PARENT_CANCEL_TARGET"}); err != nil {
		t.Fatalf("parent.Send: %v", err)
	}
	<-parentStarted

	// Pressing Esc cancels the parent turn, NOT the invoke_agent background subagent.
	if err := parent.Cancel(ctx); err != nil {
		t.Fatalf("parent.Cancel: %v", err)
	}

	child.Lock()
	childRunning := child.Running
	child.Unlock()
	if !childRunning {
		t.Fatal("expected invoke_agent subagent to keep running after parent.Cancel")
	}

	close(releaseChild)
	if err := child.Wait(ctx); err != nil {
		t.Fatalf("child.Wait: %v", err)
	}
	if err := parent.Wait(ctx); err != nil {
		t.Fatalf("parent.Wait: %v", err)
	}

	state, err := parent.Harness.LoadState(ctx)
	if err != nil {
		t.Fatalf("parent.Harness.LoadState: %v", err)
	}
	var sawCompleted bool
	for _, m := range state.Messages {
		if u, ok := m.(*message.User); ok && harness.SystemKindOf(u) == harness.SystemKindSubagent &&
			strings.Contains(u.GetContent().Text(), "Background exploration finished.") {
			sawCompleted = true
		}
	}
	if !sawCompleted {
		t.Fatal("expected surviving invoke_agent subagent to deliver <subagent_completed> to parent")
	}
}

func TestChildTurnErrorNotification(t *testing.T) {
	ctx := context.Background()
	var callCount atomic.Int32
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			count := callCount.Add(1)
			if count == 1 {
				return nil, "", errors.New("simulated subagent failure")
			}
			return []string{"Noted subagent failure"}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer parent.Close()

	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Failing task",
		Prompt: "This will fail",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent failed: %v", err)
	}

	child, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child not found in parent")
	}

	// Wait for child to finish with error
	_ = child.Wait(ctx)

	// Wait for parent turn to process the failure notification
	if err := parent.Wait(ctx); err != nil {
		t.Fatalf("parent.Wait failed: %v", err)
	}

	state, err := parent.Harness.LoadState(ctx)
	if err != nil {
		t.Fatalf("parent.Harness.LoadState failed: %v", err)
	}

	var foundSubagentFailed bool
	for _, m := range state.Messages {
		if user, ok := m.(*message.User); ok && harness.SystemKindOf(user) == harness.SystemKindSubagent {
			foundSubagentFailed = true
			text := user.GetContent().Text()
			if !strings.Contains(text, "<subagent_completed") {
				t.Errorf("expected <subagent_completed> XML tag, got: %s", text)
			}
			if !strings.Contains(text, `status="failed"`) {
				t.Errorf("expected status=\"failed\" in completion text: %s", text)
			}
			if !strings.Contains(text, child.ID) {
				t.Errorf("expected session_id %q in completion text: %s", child.ID, text)
			}
			if !strings.Contains(text, "simulated subagent failure") {
				t.Errorf("expected error details in completion text: %s", text)
			}
		}
	}
	if !foundSubagentFailed {
		t.Fatal("expected parent harness state messages to receive subagent error message")
	}
}

func TestInvokeSubagentWorktree(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Worktree subagent completed"}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	// Initialize git repo in workspace root so git worktree add succeeds
	wsDir := ws.RootDir()
	cmds := [][]string{
		{"git", "-C", wsDir, "init"},
		{"git", "-C", wsDir, "config", "user.name", "Test"},
		{"git", "-C", wsDir, "config", "user.email", "test@example.com"},
		{"git", "-C", wsDir, "add", "."},
		{"git", "-C", wsDir, "commit", "-m", "initial commit"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v: %s", args, err, string(out))
		}
	}

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer parent.Close()

	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:     "explore",
		Title:     "Worktree Task",
		Prompt:    "Perform git task",
		Workspace: "worktree",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent worktree failed: %v", err)
	}

	if out.WorkDir == wsDir {
		t.Fatalf("expected isolated worktree dir, got workspace root %s", out.WorkDir)
	}
	if !strings.Contains(out.WorkDir, "trees") {
		t.Fatalf("expected worktree path to contain 'trees', got %s", out.WorkDir)
	}
	if fi, err := os.Stat(out.WorkDir); err != nil || !fi.IsDir() {
		t.Fatalf("worktree dir %s does not exist or is not a dir: %v", out.WorkDir, err)
	}

	child, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child not found in parent")
	}
	if child.WorkDir != out.WorkDir {
		t.Errorf("child.WorkDir = %s, want %s", child.WorkDir, out.WorkDir)
	}

	_ = child.Wait(ctx)
	_ = parent.Wait(ctx)
}

func wsPermManager(t *testing.T, ws *workspace.Workspace) permissions.Manager {
	t.Helper()
	m, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	return m
}

func TestUpdateActivityFromThoughtAndGenerating(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)
	t.Cleanup(func() { _ = mgr.Close() })

	root, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Root",
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New root failed: %v", err)
	}
	defer root.Close()

	// Root session should immediately return without setting Activity
	root.updateActivityFromThought("Thinking about steps...")
	if root.Activity != "" {
		t.Fatalf("expected root session Activity to remain empty, got %q", root.Activity)
	}

	child, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Child",
		ParentID:    root.ID,
		Parent:      root,
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New child failed: %v", err)
	}
	defer child.Close()

	// Only the first line (with ... if multiline) is kept for thought activity
	child.updateActivityFromThought("Inspecting store schema.\nSecond line of thought")
	child.Lock()
	act := child.Activity
	child.Unlock()

	if act != "Inspecting store schema..." {
		t.Fatalf("expected child Activity 'Inspecting store schema...', got %q", act)
	}

	// Transitioning to StatusGenerating sets Activity to "Generating"
	child.setStatus(api.StatusGenerating)
	child.Lock()
	genAct := child.Activity
	child.Unlock()
	if genAct != "Generating" {
		t.Fatalf("expected child Activity 'Generating', got %q", genAct)
	}
}

func TestInvokeSubagentDoesNotLeakAgentPoliciesToParent(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Exploration finished"}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Main Parent Session",
		Session:     api.SessionConfig{Agent: "main", Mode: api.ModeAuto},
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent failed: %v", err)
	}
	defer func() { _ = parent.Close() }()

	bashPlan := &tools.Plan{ToolName: "bash", Action: api.ActionExec, Safety: api.SafetySafe, Target: "go version"}
	invokePlan := &tools.Plan{ToolName: "invoke_agent", Action: api.ActionRead, Safety: api.SafetySafe, Target: "general", ReadOnly: true}

	if dec, _, err := parent.PermManager.Evaluate(bashPlan); err != nil || dec != tools.AccessAllow {
		t.Fatalf("expected parent to allow bash before spawning explore, got %v (err=%v)", dec, err)
	}

	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Explore workspace",
		Prompt: "Inspect files",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent failed: %v", err)
	}

	child, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child session %q not found", out.SessionID)
	}
	_ = child.Wait(ctx)
	_ = parent.Wait(ctx)

	// Child (explore) must deny bash due to explore's agent policy ceiling.
	if dec, _, err := child.PermManager.Evaluate(bashPlan); err != nil || dec != tools.AccessDeny {
		t.Fatalf("expected explore child to deny bash, got %v (err=%v)", dec, err)
	}

	// Parent (main) must still allow bash and invoke_agent after spawning explore.
	if dec, _, err := parent.PermManager.Evaluate(bashPlan); err != nil || dec != tools.AccessAllow {
		t.Fatalf("expected parent to still allow bash after spawning explore, got %v (err=%v)", dec, err)
	}
	if dec, _, err := parent.PermManager.Evaluate(invokePlan); err != nil || dec != tools.AccessAllow {
		t.Fatalf("expected parent to still allow invoke_agent after spawning explore, got %v (err=%v)", dec, err)
	}
}

// setupTierTestEnv builds a workspace whose fake provider declares the
// standard tier aliases (@fast, @balanced) plus a project subagent with a
// narrow spec.models allowlist, alongside the built-in explore (@fast,
// @inherit) and general (@inherit, *) subagents.
func setupTierTestEnv(t *testing.T) (*workspace.Workspace, *models.Catalog) {
	t.Helper()

	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-tier-test")
	t.Cleanup(func() { xdg.ClearCache() })

	wsDir := t.TempDir()
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: tiers\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake/fake-balanced-model\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	provDir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prov := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: fake
spec:
  type: fake
  defaultModels:
    completion: fake-balanced-model
  models:
    - id: fake-fast-model
      label: Fake Fast
      aliases: [fast]
      capabilities:
        completion: true
    - id: fake-balanced-model
      label: Fake Balanced
      aliases: [balanced]
      capabilities:
        completion: true
`
	if err := os.WriteFile(filepath.Join(provDir, "fake.provider.yaml"), []byte(prov), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), wsDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	cat, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build: %v", err)
	}
	return ws, cat
}

func TestResolveSubagentModelTiers(t *testing.T) {
	ws, cat := setupTierTestEnv(t)

	parent := &Session{workspace: ws, catalog: func() *models.Catalog { return cat }}
	parent.Config.Model = "fake/fake-balanced-model"

	tests := []struct {
		name      string
		agent     string
		requested string
		want      string
		wantErr   bool
	}{
		{name: "explore defaults to @fast", agent: "explore", want: "fake/fake-fast-model"},
		{name: "explicit @inherit uses parent model", agent: "explore", requested: "@inherit", want: "fake/fake-balanced-model"},
		{name: "@inherit is case-insensitive", agent: "explore", requested: "@INHERIT", want: "fake/fake-balanced-model"},
		{name: "alias in allowlist canonicalizes", agent: "explore", requested: "@fast", want: "fake/fake-fast-model"},
		{name: "provider-scoped alias canonicalizes", agent: "explore", requested: "fake/@fast", want: "fake/fake-fast-model"},
		{name: "canonical id matches allowlist entry", agent: "explore", requested: "fake/fake-fast-model", want: "fake/fake-fast-model"},
		{name: "tier outside allowlist is rejected", agent: "explore", requested: "@balanced", wantErr: true},
		{name: "general accepts explicit override via wildcard", agent: "general", requested: "@fast", want: "fake/fake-fast-model"},
		{name: "general defaults to @inherit", agent: "general", want: "fake/fake-balanced-model"},
		{name: "unknown agent defers", agent: "nope", requested: "@whatever", want: "@whatever"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parent.resolveSubagentModel(tc.agent, tc.requested)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("resolveSubagentModel(%q, %q) err = %v, want ErrInvalidConfig", tc.agent, tc.requested, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSubagentModel(%q, %q) unexpected err: %v", tc.agent, tc.requested, err)
			}
			if got != tc.want {
				t.Fatalf("resolveSubagentModel(%q, %q) = %q, want %q", tc.agent, tc.requested, got, tc.want)
			}
		})
	}

	// Missing catalog getter must degrade cleanly without panicking.
	bareParent := &Session{workspace: ws}
	bareParent.Config.Model = "fake/fake-balanced-model"
	got, err := bareParent.resolveSubagentModel("explore", "@fast")
	if err != nil || got != "fake/fake-fast-model" {
		t.Fatalf("bareParent resolveSubagentModel(@fast) = (%q, %v), want (%q, nil)", got, err, "fake/fake-fast-model")
	}
}

func TestResolveSubagentModelInheritRejectedWhenNotAllowed(t *testing.T) {
	ws, cat := setupTierTestEnv(t)

	agDir := filepath.Join(ws.RootDir(), ".agents", "agents")
	if err := os.MkdirAll(agDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fastOnly := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: fastonly\nspec:\n  triggers: [agent]\n  models: [\"@fast\"]\n---\nfast only\n"
	if err := os.WriteFile(filepath.Join(agDir, "fastonly.agent.md"), []byte(fastOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	inheritFirst := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: inheritfirst\nspec:\n  triggers: [agent]\n  models: [\"@inherit\", \"@fast\"]\n---\ninherit first\n"
	if err := os.WriteFile(filepath.Join(agDir, "inheritfirst.agent.md"), []byte(inheritFirst), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	parent := &Session{workspace: ws, catalog: func() *models.Catalog { return cat }}
	parent.Config.Model = "fake/fake-balanced-model"

	if _, err := parent.resolveSubagentModel("fastonly", "@inherit"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("@inherit on allowlist without @inherit entry: err = %v, want ErrInvalidConfig", err)
	}
	if _, err := parent.resolveSubagentModel("fastonly", "@fast"); err != nil {
		t.Fatalf("@fast on fastonly: unexpected err: %v", err)
	}
	// The parent's own model must never widen the allowlist: running the
	// parent on an allowed model does not make disallowed requests legal.
	parent.Config.Model = "fake/fake-fast-model"
	if _, err := parent.resolveSubagentModel("fastonly", "@balanced"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("@balanced on fastonly with parent on allowed model: err = %v, want ErrInvalidConfig", err)
	}
	if _, err := parent.resolveSubagentModel("explore", "@balanced"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("@balanced on explore with parent on allowed model: err = %v, want ErrInvalidConfig", err)
	}
	if _, err := parent.resolveSubagentModel("explore", "fake/fake-balanced-model"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("disallowed provider/model on explore with parent on allowed model: err = %v, want ErrInvalidConfig", err)
	}
	parent.Config.Model = "fake/fake-balanced-model"
	// @inherit as the leading spec.models entry makes the parent's model the
	// default even without an explicit request.
	got, err := parent.resolveSubagentModel("inheritfirst", "")
	if err != nil {
		t.Fatalf("inheritfirst default: unexpected err: %v", err)
	}
	if got != "fake/fake-balanced-model" {
		t.Fatalf("inheritfirst default model = %q, want fake/fake-balanced-model", got)
	}
}

func TestInvokeSubagentAppliesResolvedModel(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"done"}, uuid.NewString(), nil
		},
	}

	db, _, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	ws, cat := setupTierTestEnv(t)

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Tier Parent",
		Session:     api.SessionConfig{Agent: "main", Model: "fake/fake-balanced-model"},
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Catalog:     func() *models.Catalog { return cat },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent: %v", err)
	}
	defer func() { _ = parent.Close() }()

	// Default delegation routes explore onto its @fast tier model.
	out, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Fast exploration",
		Prompt: "Find all interfaces",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent: %v", err)
	}
	child, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child %q not registered", out.SessionID)
	}
	if got := ToAPISession(child); got.Config.Model != out.Model || got.Model == nil || got.Model.ID != out.Model || got.Model.Label != out.ModelLabel {
		t.Errorf("child model = %q (%+v), output model = %q (%q)", got.Config.Model, got.Model, out.Model, out.ModelLabel)
	}
	if out.Model != "fake/fake-fast-model" || out.ModelLabel != "Fake Fast" {
		t.Errorf("output model = %q (%q), want fake/fake-fast-model (Fake Fast)", out.Model, out.ModelLabel)
	}
	_ = child.Wait(ctx)

	// @inherit runs the child on the parent's model.
	out, err = parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Inherited exploration",
		Prompt: "Find all interfaces",
		Model:  "@inherit",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent(@inherit): %v", err)
	}
	child2, ok := parent.Child(out.SessionID)
	if !ok {
		t.Fatalf("child %q not registered", out.SessionID)
	}
	if child2.Config.Model != "fake/fake-balanced-model" {
		t.Errorf("inherited child model = %q, want fake/fake-balanced-model", child2.Config.Model)
	}
	if out.Model != "fake/fake-balanced-model" || out.ModelLabel != "Fake Balanced" {
		t.Errorf("output model = %q (%q), want fake/fake-balanced-model (Fake Balanced)", out.Model, out.ModelLabel)
	}
	_ = child2.Wait(ctx)

	// A tier outside explore's allowlist is rejected without spawning a child.
	before := len(parent.Children())
	if _, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "explore",
		Title:  "Rejected tier",
		Prompt: "Find all interfaces",
		Model:  "@balanced",
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("InvokeSubagent(@balanced) err = %v, want ErrInvalidConfig", err)
	}
	if after := len(parent.Children()); after != before {
		t.Fatalf("rejected invoke spawned a child: %d -> %d children", before, after)
	}
}

func TestDelegatedCommandReportsResolvedModel(t *testing.T) {
	ctx := t.Context()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"done"}, uuid.NewString(), nil
		},
	}
	_, ws, _, pub, mgr := setupTwoModelEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	cmdDir := filepath.Join(ws.RootDir(), ".agents", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	command := `---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: delegated-model
spec:
  context: clean
  models: [fast-model]
---
Run on the fast model.`
	if err := os.WriteFile(filepath.Join(cmdDir, "delegated-model.command.md"), []byte(command), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	const parentModel = "fake/fake-default-model"
	const commandModel = "fake/fast-model"
	parent, _, err := mgr.Create(ctx, api.CreateSessionRequest{
		Title:  "Parent",
		Config: api.SessionConfig{Agent: "main", Model: parentModel},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Send(ctx, api.SendRequest{Content: "/delegated-model"}); err != nil {
		t.Fatal(err)
	}
	children := parent.Children()
	if len(children) != 1 {
		t.Fatalf("got %d children, want 1", len(children))
	}
	child := children[0]
	if err := child.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	assertModel := func(stage string, snapshot api.Session) {
		t.Helper()
		if snapshot.Config.Model != commandModel || snapshot.Model == nil || snapshot.Model.ID != commandModel {
			t.Errorf("%s: Config.Model = %q, Model = %+v, want %s", stage, snapshot.Config.Model, snapshot.Model, commandModel)
		}
	}

	var created, generating, idle bool
	for _, rec := range pub.recordedRecords() {
		if rec.topic != api.TopicSessions {
			continue
		}
		switch ev := rec.event.(type) {
		case api.SessionCreatedEvent:
			if ev.Session.ID == child.ID {
				created = true
				assertModel("created event", ev.Session)
			}
		case api.SessionUpdatedEvent:
			if ev.Session.ID != child.ID {
				continue
			}
			assertModel(string(ev.Session.Status)+" update", ev.Session)
			generating = generating || ev.Session.Status == api.StatusGenerating
			idle = idle || (generating && ev.Session.Status == api.StatusIdle)
		}
	}
	if !created || !generating || !idle {
		t.Errorf("missing lifecycle snapshots: created=%t generating=%t idle after generation=%t", created, generating, idle)
	}
	assertModel("idle snapshot", ToAPISession(child))
	if got := parent.configSnapshot().Model; got != parentModel {
		t.Errorf("parent model = %q, want %s", got, parentModel)
	}
	if turns := prov.recordedTurns(); len(turns) == 0 || turns[0].model != commandModel {
		t.Errorf("first provider turn did not use %s: %+v", commandModel, turns)
	}
}

func TestMainAgentPromptSurfacesSubagentModelTiers(t *testing.T) {
	ws, _ := setupTierTestEnv(t)

	cat := ws.Catalog()
	resolved, err := cat.ResolveAgent("main")
	if err != nil {
		t.Fatalf("ResolveAgent(main): %v", err)
	}
	prompt, err := resolved.Agent.Render(&warp.AgentRenderOptions{
		Workspace: cat.WorkspaceSpec(),
		Project:   cat.Project(),
		Resolved:  resolved,
		Globals:   cat.AgentRenderGlobals(resolved),
	})
	if err != nil {
		t.Fatalf("Render(main): %v", err)
	}
	if !strings.Contains(prompt, "<default_model>@fast</default_model>") {
		t.Errorf("main prompt missing explore default_model:\n%s", prompt)
	}
	if !strings.Contains(prompt, "<allowed_models> @fast @inherit</allowed_models>") {
		t.Errorf("main prompt missing explore allowed_models:\n%s", prompt)
	}
	if strings.Contains(prompt, "### `invoke_agent` tool") || strings.Contains(prompt, "### `manage_agents` tool") {
		t.Errorf("main prompt should not duplicate subagent tool mechanics already on *.tool.md")
	}

	invokeTool, ok := cat.GetTool("invoke_agent")
	if !ok || !strings.Contains(invokeTool.Spec.Instructions, "Model Routing") {
		t.Errorf("invoke_agent.tool.md missing Model Routing guidance: %+v", invokeTool)
	}
	manageTool, ok := cat.GetTool("manage_agents")
	if !ok || !strings.Contains(manageTool.Spec.Instructions, `action: "models"`) {
		t.Errorf("manage_agents.tool.md missing models discovery guidance: %+v", manageTool)
	}
}

// setupMultiTierTestEnv builds a workspace with two providers that both
// declare the standard tier aliases, so a bare "@fast" resolves once per
// provider and provider-scoped references must match allowlist entries across
// both providers.
func setupMultiTierTestEnv(t *testing.T) (*workspace.Workspace, *models.Catalog) {
	t.Helper()

	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-multitier-test")
	t.Cleanup(func() { xdg.ClearCache() })

	wsDir := t.TempDir()
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: multitiers\nspec:\n  defaultProvider: alpha\n  defaultModels:\n    completion: alpha/alpha-balanced-model\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	provDir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prov := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: %s
spec:
  type: fake
  defaultModels:
    completion: %s-balanced-model
  models:
    - id: %s-fast-model
      aliases: [fast]
      capabilities:
        completion: true
    - id: %s-balanced-model
      aliases: [balanced]
      capabilities:
        completion: true
`
	for _, name := range []string{"alpha", "beta"} {
		body := fmt.Sprintf(prov, name, name, name, name)
		if err := os.WriteFile(filepath.Join(provDir, name+".provider.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	agentsDir := filepath.Join(wsDir, ".agents", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globAgent := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: globalpha\nspec:\n  triggers: [agent]\n  models: [\"alpha/*\"]\n---\nglob alpha\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "globalpha.agent.md"), []byte(globAgent), 0o644); err != nil {
		t.Fatal(err)
	}
	anyAgent := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: anymodel\nspec:\n  triggers: [agent]\n---\nany model\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "anymodel.agent.md"), []byte(anyAgent), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), wsDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	cat, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build: %v", err)
	}
	return ws, cat
}

// TestResolveSubagentModelMultiProvider verifies allowlist matching against
// every provider's tier candidates (not just the first resolution), provider
// preference for the parent's provider on defaults, and synchronous rejection
// of references that cannot resolve while the agent restricts spec.models.
func TestResolveSubagentModelMultiProvider(t *testing.T) {
	ws, cat := setupMultiTierTestEnv(t)

	parent := &Session{workspace: ws, catalog: func() *models.Catalog { return cat }}
	parent.Config.Model = "beta/beta-balanced-model"

	tests := []struct {
		name      string
		agent     string
		requested string
		want      string
		wantErr   bool
	}{
		{name: "default prefers parent provider", agent: "explore", want: "beta/beta-fast-model"},
		{name: "provider-scoped second provider allowed", agent: "explore", requested: "beta/@fast", want: "beta/beta-fast-model"},
		{name: "second provider canonical id allowed", agent: "explore", requested: "beta/beta-fast-model", want: "beta/beta-fast-model"},
		{name: "first provider canonical id allowed", agent: "explore", requested: "alpha/alpha-fast-model", want: "alpha/alpha-fast-model"},
		{name: "parent id allowed when inherit listed", agent: "explore", requested: "beta/beta-balanced-model", want: "beta/beta-balanced-model"},
		{name: "tier outside allowlist rejected", agent: "explore", requested: "beta/@balanced", wantErr: true},
		{name: "unknown model rejected when restricted", agent: "explore", requested: "gamma/gamma-model", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parent.resolveSubagentModel(tc.agent, tc.requested)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("resolveSubagentModel(%q, %q) err = %v, want ErrInvalidConfig", tc.agent, tc.requested, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSubagentModel(%q, %q) unexpected err: %v", tc.agent, tc.requested, err)
			}
			if got != tc.want {
				t.Fatalf("resolveSubagentModel(%q, %q) = %q, want %q", tc.agent, tc.requested, got, tc.want)
			}
		})
	}
}

// TestResolveSubagentModelUnresolvableAliasRejected verifies that requesting a
// tier alias the workspace cannot resolve fails synchronously with
// ErrInvalidConfig instead of spawning a child bound to a literal "@fast".
func TestResolveSubagentModelUnresolvableAliasRejected(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-notier-test")
	t.Cleanup(func() { xdg.ClearCache() })

	wsDir := t.TempDir()
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: notiers\nspec:\n  defaultProvider: plain\n  defaultModels:\n    completion: plain/plain-model\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	provDir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prov := "apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: plain\nspec:\n  type: fake\n  models:\n    - id: plain-model\n      capabilities:\n        completion: true\n---\n"
	if err := os.WriteFile(filepath.Join(provDir, "plain.provider.yaml"), []byte(prov), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Open(context.Background(), wsDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	cat, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build: %v", err)
	}

	parent := &Session{workspace: ws, catalog: func() *models.Catalog { return cat }}
	parent.Config.Model = "plain/plain-model"

	if _, err := parent.resolveSubagentModel("explore", "@fast"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("explicit @fast with no fast alias: err = %v, want ErrInvalidConfig", err)
	}
	// With nothing resolvable in the allowlist, the default falls back to the
	// empty string so the child uses workspace defaults, never a literal alias.
	got, err := parent.resolveSubagentModel("explore", "")
	if err != nil {
		t.Fatalf("explore default: unexpected err: %v", err)
	}
	if strings.HasPrefix(got, "@") {
		t.Fatalf("explore default model = %q, want resolvable reference or empty", got)
	}
}

// TestManageSubagentsModelsDiscovery verifies the manage_agents "models"
// action: full listing, provider filtering, fuzzy query ranking, intersection
// with a subagent's spec.models allowlist, bounded limit, and unknown-agent
// rejection.
func TestManageSubagentsModelsDiscovery(t *testing.T) {
	ws, cat := setupMultiTierTestEnv(t)

	parent := &Session{workspace: ws, catalog: func() *models.Catalog { return cat }}
	parent.Config.Model = "beta/beta-balanced-model"

	modelIDs := func(out api.ManageAgentsOutput) map[string]bool {
		set := make(map[string]bool, len(out.Models))
		for _, m := range out.Models {
			set[m.ID] = true
		}
		return set
	}

	t.Run("list all", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models"})
		if err != nil {
			t.Fatalf("models list: %v", err)
		}
		if len(out.Models) != 4 {
			t.Fatalf("models = %d, want 4: %+v", len(out.Models), out.Models)
		}
		if out.Message != "Found 4 models" {
			t.Errorf("message = %q, want %q", out.Message, "Found 4 models")
		}
		for _, m := range out.Models {
			if m.Provider == "" || m.ID == "" {
				t.Errorf("summary missing id/provider: %+v", m)
			}
		}
	})

	t.Run("provider filter", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Provider: "beta"})
		if err != nil {
			t.Fatalf("models provider filter: %v", err)
		}
		if len(out.Models) != 2 {
			t.Fatalf("models = %d, want 2: %+v", len(out.Models), out.Models)
		}
		for _, m := range out.Models {
			if m.Provider != "beta" {
				t.Errorf("model %q outside provider filter", m.ID)
			}
		}
	})

	t.Run("fuzzy query matches aliases", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Query: "fast"})
		if err != nil {
			t.Fatalf("models query: %v", err)
		}
		got := modelIDs(out)
		if !got["alpha/alpha-fast-model"] || !got["beta/beta-fast-model"] {
			t.Fatalf("fast-tier models missing: %+v", out.Models)
		}
		if len(out.Models) != 2 {
			t.Fatalf("models = %d, want 2: %+v", len(out.Models), out.Models)
		}
		if !strings.Contains(out.Message, `matching "fast"`) {
			t.Errorf("message = %q, want matching query", out.Message)
		}
	})

	t.Run("agent allowlist intersection", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Agent: "explore"})
		if err != nil {
			t.Fatalf("models agent filter: %v", err)
		}
		got := modelIDs(out)
		if !got["alpha/alpha-fast-model"] || !got["beta/beta-fast-model"] || !got["beta/beta-balanced-model"] {
			t.Fatalf("allowlisted models missing: %+v", out.Models)
		}
		if got["alpha/alpha-balanced-model"] {
			t.Fatalf("alpha/alpha-balanced-model outside explore allowlist: %+v", out.Models)
		}
	})

	t.Run("agent with wildcard spec.models", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Agent: "general"})
		if err != nil {
			t.Fatalf("models general agent: %v", err)
		}
		if len(out.Models) != 4 {
			t.Fatalf("models = %d, want 4 (general allows @inherit + \"*\"): %+v", len(out.Models), out.Models)
		}
	})

	t.Run("agent with glob-only spec.models", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Agent: "globalpha"})
		if err != nil {
			t.Fatalf("models glob agent: %v", err)
		}
		if len(out.Models) != 2 {
			t.Fatalf("models = %d, want 2 (alpha/* only): %+v", len(out.Models), out.Models)
		}
		for _, m := range out.Models {
			if m.Provider != "alpha" {
				t.Errorf("model %q outside alpha/* glob", m.ID)
			}
		}
	})

	t.Run("agent with empty spec.models allows all", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Agent: "anymodel"})
		if err != nil {
			t.Fatalf("models empty-allowlist agent: %v", err)
		}
		if len(out.Models) != 4 {
			t.Fatalf("models = %d, want 4 (empty spec.models allows any): %+v", len(out.Models), out.Models)
		}
	})

	t.Run("aliases returned as @refs", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Query: "fast"})
		if err != nil {
			t.Fatalf("models query: %v", err)
		}
		for _, m := range out.Models {
			for _, alias := range m.Aliases {
				if !strings.HasPrefix(alias, "@") {
					t.Errorf("model %q alias %q missing @ prefix", m.ID, alias)
				}
			}
		}
	})

	t.Run("specific query does not over-match aliases", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Query: "deepseek-fast-coder"})
		if err != nil {
			t.Fatalf("models specific query: %v", err)
		}
		if len(out.Models) != 0 {
			t.Fatalf("models = %+v, want no matches (query must not match via reverse containment)", out.Models)
		}
	})

	t.Run("bounded limit", func(t *testing.T) {
		out, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Limit: 1})
		if err != nil {
			t.Fatalf("models limit: %v", err)
		}
		if len(out.Models) != 1 {
			t.Fatalf("models = %d, want 1: %+v", len(out.Models), out.Models)
		}
		if !strings.Contains(out.Message, "(showing first 1)") {
			t.Errorf("message = %q, want truncation notice", out.Message)
		}
	})

	t.Run("unknown agent rejected", func(t *testing.T) {
		if _, err := parent.ManageSubagents(context.Background(), api.ManageAgentsInput{Action: "models", Agent: "nope"}); !errors.Is(err, std.ErrInvalidArgument) {
			t.Fatalf("unknown agent err = %v, want ErrInvalidArgument", err)
		}
	})
}

// TestResolveSubagentModelWildcardsAndFuzzyHints verifies:
// - general defaults to @inherit (parent's active model) when model is omitted,
// - general accepts non-tiered models via "*",
// - provider-scoped globs ("ollama/*") permit matching models and reject others,
// - unknown/typo model requests return ErrInvalidConfig with up to 3 "did you mean" hints.
func TestResolveSubagentModelWildcardsAndFuzzyHints(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-glob-test")
	t.Cleanup(func() { xdg.ClearCache() })

	wsDir := t.TempDir()
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: globs\nspec:\n  defaultProvider: alpha\n  defaultModels:\n    completion: alpha/alpha-balanced\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	provDir := filepath.Join(wsDir, ".agents", "providers")
	agentsDir := filepath.Join(wsDir, ".agents", "agents")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	alphaProv := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: alpha
spec:
  type: fake
  models:
    - id: alpha-fast
      aliases: [fast]
      capabilities: {completion: true}
    - id: alpha-balanced
      aliases: [balanced]
      capabilities: {completion: true}
`
	ollamaProv := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: ollama
spec:
  type: ollama
  models:
    - id: qwen3.8
      capabilities: {completion: true}
    - id: mimo-v2.6
      capabilities: {completion: true}
    - id: nemotron-3.5
      capabilities: {completion: true}
`
	if err := os.WriteFile(filepath.Join(provDir, "alpha.provider.yaml"), []byte(alphaProv), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provDir, "ollama.provider.yaml"), []byte(ollamaProv), 0o644); err != nil {
		t.Fatal(err)
	}

	localAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: localcoder
spec:
  triggers: [agent]
  models: ["ollama/*", "!ollama/nemotron-3.5"]
---
local coder
`
	if err := os.WriteFile(filepath.Join(agentsDir, "localcoder.agent.md"), []byte(localAgent), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), wsDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	cat, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build: %v", err)
	}

	parent := &Session{workspace: ws, catalog: func() *models.Catalog { return cat }}
	// Parent is on a non-default model; general must inherit it when model is omitted.
	parent.Config.Model = "alpha/alpha-fast"

	got, err := parent.resolveSubagentModel("general", "")
	if err != nil {
		t.Fatalf("general default: unexpected err: %v", err)
	}
	if got != "alpha/alpha-fast" {
		t.Errorf("general default = %q, want parent model alpha/alpha-fast", got)
	}

	// general accepts arbitrary non-tiered models via "*".
	got, err = parent.resolveSubagentModel("general", "ollama/mimo-v2.6")
	if err != nil {
		t.Fatalf("general non-tiered override: unexpected err: %v", err)
	}
	if got != "ollama/mimo-v2.6" {
		t.Errorf("general override = %q, want ollama/mimo-v2.6", got)
	}

	// Provider-scoped glob "ollama/*" accepts matching models and rejects others/negated ones.
	got, err = parent.resolveSubagentModel("localcoder", "ollama/qwen3.8")
	if err != nil {
		t.Fatalf("localcoder ollama/qwen3.8: unexpected err: %v", err)
	}
	if got != "ollama/qwen3.8" {
		t.Errorf("localcoder = %q, want ollama/qwen3.8", got)
	}
	if _, err := parent.resolveSubagentModel("localcoder", "alpha/alpha-fast"); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("localcoder alpha/alpha-fast err = %v, want ErrInvalidConfig", err)
	}
	if _, err := parent.resolveSubagentModel("localcoder", "ollama/nemotron-3.5"); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("localcoder negated model err = %v, want ErrInvalidConfig", err)
	}

	// Unknown / typo requests return ErrInvalidConfig with "did you mean" hints (<= 3).
	for _, tc := range []struct {
		req      string
		wantHint string
	}{
		{req: "@fas", wantHint: "alpha/alpha-fast"},
		{req: "qwen3", wantHint: "ollama/qwen3.8"},
		{req: "ollam/mimo-v2.6", wantHint: "ollama/mimo-v2.6"},
	} {
		_, err := parent.resolveSubagentModel("general", tc.req)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("resolveSubagentModel(general, %q) err = %v, want ErrInvalidConfig", tc.req, err)
		}
		if !strings.Contains(err.Error(), "did you mean") || !strings.Contains(err.Error(), tc.wantHint) {
			t.Errorf("resolveSubagentModel(general, %q) err = %q, want 'did you mean' with %q", tc.req, err.Error(), tc.wantHint)
		}
	}
}

// TestResolveConfigRejectsInherit verifies a top-level session config cannot
// bind directly to the reserved "@inherit" alias.
func TestResolveConfigRejectsInherit(t *testing.T) {
	ws, cat := setupTierTestEnv(t)

	if _, err := resolveConfig(ws, cat, api.SessionConfig{Agent: "main", Model: workspace.ModelAliasInherit}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("resolveConfig(@inherit) err = %v, want ErrInvalidConfig", err)
	}
	if _, _, _, _, err := Preview(context.Background(), ws, cat, api.SessionConfig{Agent: "main", Model: "@inherit"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Preview(@inherit) err = %v, want ErrInvalidConfig", err)
	}
}

func TestUserIntentPreservedAcrossSlashCommandAndSubagent(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"ok"}, uuid.NewString(), nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)
	t.Cleanup(func() { _ = mgr.Close() })

	cmdDir := filepath.Join(ws.RootDir(), ".agents", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdManifest := "---\napiVersion: warp/v1alpha1\nkind: Command\nmetadata:\n  name: ship\n  description: Commit staged work\nspec:\n  context: clean\n---\nExpanded instructions body that should not replace user_intent.\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "ship.command.md"), []byte(cmdManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}

	parent, err := New(ctx, Config{
		ID:          uuid.NewString(),
		Title:       "Parent Session",
		Session:     api.SessionConfig{Mode: api.ModeAuto},
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Permissions: wsPermManager(t, ws),
		Publisher:   pub,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New parent: %v", err)
	}
	defer func() { _ = parent.Close() }()

	// 1. Slash command execution stamps raw human "/ship ..." invocation instead of rendered body.
	execPlan, err := parent.resolveCommandExecution(message.NewUserText("/ship"))
	if err != nil || execPlan == nil {
		t.Fatalf("resolveCommandExecution(/ship) = %+v, %v", execPlan, err)
	}
	if got := harness.UserIntentOf(execPlan.Message); got != "/ship — Commit staged work" {
		t.Fatalf("command UserIntent = %q, want %q", got, "/ship — Commit staged work")
	}

	// 2. Parent human prompt propagates to an invoke_agent subagent instead of the LLM's subagent prompt.
	if err := parent.Send(ctx, api.SendRequest{Content: "Format and verify the repository"}); err != nil {
		t.Fatalf("parent.Send: %v", err)
	}
	if err := parent.Wait(ctx); err != nil {
		t.Fatalf("parent.Wait: %v", err)
	}

	inv, err := parent.InvokeSubagent(ctx, api.InvokeAgentInput{
		Agent:  "general",
		Title:  "Run helper task",
		Prompt: "Internal delegated instructions written by parent LLM",
	})
	if err != nil {
		t.Fatalf("InvokeSubagent: %v", err)
	}
	child, ok := parent.Child(inv.SessionID)
	if !ok {
		t.Fatal("child session not registered")
	}
	if err := child.Wait(ctx); err != nil {
		t.Fatalf("child.Wait: %v", err)
	}
	_ = parent.Wait(ctx)

	if got := child.lastTurnUserIntent(); got != "Format and verify the repository" {
		t.Fatalf("child lastTurnUserIntent = %q, want parent human prompt", got)
	}
}
