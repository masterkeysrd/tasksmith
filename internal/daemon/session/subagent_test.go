package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
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
