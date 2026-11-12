package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func TestRegistryBuiltinsAndLookup(t *testing.T) {
	r := NewRegistry()

	cmds := []string{"help", "mode", "model", "agent", "effort", "budget", "thinking", "theme", "new", "clear", "sessions", "changes", "revert", "exit", "permissions", "tasks", "daemon", "compact", "context", "workspace"}
	for _, name := range cmds {
		cmd, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("expected command /%s to be registered", name)
		}
		if cmd.Name != name {
			t.Errorf("expected cmd.Name=%q, got %q", name, cmd.Name)
		}
	}

	// Test aliases
	aliases := map[string]string{
		"h":       "help",
		"?":       "help",
		"session": "sessions",
		"ls":      "sessions",
		"quit":    "exit",
		"q":       "exit",
		"perm":    "permissions",
		"perms":   "permissions",
		"ws":      "workspace",
	}
	for alias, expectedName := range aliases {
		cmd, ok := r.Lookup(alias)
		if !ok {
			t.Errorf("expected alias /%s to be registered", alias)
			continue
		}
		if cmd.Name != expectedName {
			t.Errorf("alias %q resolved to %q, expected %q", alias, cmd.Name, expectedName)
		}
	}
}

func TestCommandExecute(t *testing.T) {
	r := NewRegistry()

	t.Run("exit", func(t *testing.T) {
		store.ResetApp()
		defer store.ResetApp()

		if err := r.Execute("/exit"); err != nil {
			t.Fatalf("Execute /exit: %v", err)
		}
		if !store.GetApp().ExitRequested {
			t.Errorf("expected ExitRequested=true")
		}
	})

	t.Run("mode with arg", func(t *testing.T) {
		store.ResetSession()
		defer store.ResetSession()

		if err := r.Execute("/mode auto"); err != nil {
			t.Fatalf("Execute /mode auto: %v", err)
		}
		if store.GetSession().Config.Mode != api.ModeAuto {
			t.Errorf("expected mode to be auto, got %q", store.GetSession().Config.Mode)
		}

		// Invalid mode returns error
		if err := r.Execute("/mode invalid_mode"); err == nil {
			t.Errorf("expected error for invalid mode")
		}
	})

	t.Run("help enqueues slot", func(t *testing.T) {
		store.ClearSlot()
		defer store.ClearSlot()

		if err := r.Execute("/help"); err != nil {
			t.Fatalf("Execute /help: %v", err)
		}
		if store.GetSlot().Active == nil {
			t.Fatalf("expected slot to be enqueued for /help")
		}
	})

	t.Run("permissions", func(t *testing.T) {
		store.ClearSlot()
		defer store.ClearSlot()
		defer store.ResetSession()

		if err := r.Execute("/permissions"); err != nil {
			t.Fatalf("Execute /permissions without client: %v", err)
		}
		if store.GetSlot().Active != nil {
			t.Errorf("expected no active slot when client is nil")
		}

		cli, _ := client.New("/tmp/dummy.sock")
		store.InitSession(context.Background(), cli, store.InitSessionOptions{})

		if err := r.Execute("/permissions"); err != nil {
			t.Fatalf("Execute /permissions: %v", err)
		}
		if store.GetSlot().Active == nil || store.GetSlot().Active.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker active slot after /permissions, got %+v", store.GetSlot().Active)
		}
		store.ClearSlot()

		if err := r.Execute("/perm"); err != nil {
			t.Fatalf("Execute /perm: %v", err)
		}
		if store.GetSlot().Active == nil || store.GetSlot().Active.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker active slot after /perm, got %+v", store.GetSlot().Active)
		}
		store.ClearSlot()

		if err := r.Execute("/perms"); err != nil {
			t.Fatalf("Execute /perms: %v", err)
		}
		if store.GetSlot().Active == nil || store.GetSlot().Active.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker active slot after /perms, got %+v", store.GetSlot().Active)
		}
	})

	t.Run("new session", func(t *testing.T) {
		store.ResetSession()
		store.ClearTranscript()
		defer store.ResetSession()
		defer store.ClearTranscript()

		store.SetHeaderMeta("v1.0.0", "/ws")
		store.SetModelInfo("test-model-1", "auto")

		epoch0 := store.GetTranscript().Epoch

		// 1. /new on clean transcript with same model should NOT increment epoch
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new: %v", err)
		}
		if store.GetTranscript().Epoch != epoch0 {
			t.Errorf("expected epoch to remain %d on clean transcript with same model, got %d", epoch0, store.GetTranscript().Epoch)
		}
		if store.GetSession().Config.Model != "test-model-1" {
			t.Errorf("expected model test-model-1, got %q", store.GetSession().Config.Model)
		}

		// 2. /new on draft transcript clears items without incrementing epoch
		store.AppendNotice("dirty notice")
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new on draft transcript: %v", err)
		}
		if store.GetTranscript().Epoch != epoch0 {
			t.Errorf("expected epoch to remain %d on draft transcript, got %d", epoch0, store.GetTranscript().Epoch)
		}
		if store.GetTranscript().Items != nil {
			t.Errorf("expected nil items after /new in draft, got %v", store.GetTranscript().Items)
		}

		// 3. /new on frozen transcript increments epoch and transitions to draft
		store.FreezeTranscript()
		store.AppendNotice("frozen notice")
		epochFrozenBefore := store.GetTranscript().Epoch
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new on frozen transcript: %v", err)
		}
		if store.GetTranscript().Epoch != epochFrozenBefore+1 {
			t.Errorf("expected epoch to increment on frozen transcript, was %d now %d", epochFrozenBefore, store.GetTranscript().Epoch)
		}
		if store.GetTranscript().Frozen {
			t.Errorf("expected Frozen=false after /new")
		}
		if store.GetTranscript().Items != nil {
			t.Errorf("expected nil items after /new, got %v", store.GetTranscript().Items)
		}

		// 4. /new with different model updates live header without incrementing epoch
		epoch1 := store.GetTranscript().Epoch
		if err := r.Execute("/new diff-model"); err != nil {
			t.Fatalf("Execute /new diff-model: %v", err)
		}
		if store.GetTranscript().Epoch != epoch1 {
			t.Errorf("expected epoch to remain %d on clean transcript, got %d", epoch1, store.GetTranscript().Epoch)
		}
		if store.GetSession().Config.Model != "diff-model" {
			t.Errorf("expected model diff-model, got %q", store.GetSession().Config.Model)
		}
		if store.CurrentHeaderData().Model != "default" {
			t.Errorf("expected unresolved model to remain default, got %q", store.CurrentHeaderData().Model)
		}
	})

	t.Run("tasks command", func(t *testing.T) {
		store.ResetSession()
		store.ClearSlot()
		defer store.ResetSession()
		defer store.ClearSlot()

		// Always enqueues slot
		if err := r.Execute("/tasks"); err != nil {
			t.Fatalf("Execute /tasks: %v", err)
		}
		if slot := store.GetSlot().Active; slot == nil || slot.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker slot active, got %+v", slot)
		}
	})
}

func TestNewClearsSessionTodos(t *testing.T) {
	store.ResetSession()
	store.ResetStream()
	store.ClearTranscript()
	defer store.ResetSession()
	defer store.ResetStream()
	defer store.ClearTranscript()

	store.SetSession(&api.Session{ID: "old-session"})
	store.SetMode(api.ModePlan)
	store.SeedTodos([]api.TodoItem{{Content: "Old task", ActiveText: "Working", Status: "in_progress"}})
	store.ApplyEvent(api.Event{Type: api.EventStatusChanged, Payload: api.StatusChangedEvent{SessionID: "old-session", Status: api.StatusThinking}})
	if store.GetStream().Todos == nil {
		t.Fatal("expected existing checklist")
	}
	if err := NewRegistry().Execute("/new"); err != nil {
		t.Fatal(err)
	}
	if stream := store.GetStream(); stream.Todos != nil || stream.Streaming || stream.Messages != nil || stream.Queue != nil {
		t.Fatalf("previous session stream remained after /new: %+v", stream)
	}
	if session := store.GetSession(); session.Session != nil || session.Config.Mode != api.ModePlan {
		t.Fatalf("expected draft session retaining mode: %+v", session)
	}
	store.ApplyEvent(api.Event{Type: api.EventStatusChanged, Payload: api.StatusChangedEvent{Status: api.StatusThinking}})
	if store.GetStream().Todos != nil {
		t.Fatal("previous checklist returned after a new event")
	}
}

func TestSources(t *testing.T) {
	r := NewRegistry()
	sources := r.Sources()
	if len(sources) == 0 {
		t.Fatalf("expected sources to be non-empty")
	}
	// First source should be root "/"
	if sources[0].Trigger != "/" {
		t.Errorf("expected first source trigger '/', got %q", sources[0].Trigger)
	}

	// Should have sub-sources for mode, model, agent, effort, theme
	foundMode := false
	for _, s := range sources {
		if s.Trigger == "/mode " {
			foundMode = true
			break
		}
	}
	if !foundMode {
		t.Errorf("expected /mode sub-source")
	}
}

func TestSkillAutocompleteOrderingAndCompletion(t *testing.T) {
	r := NewRegistry()
	baseline, err := r.Sources()[0].Complete(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for i, cmd := range r.List() {
		if baseline[i].Value != cmd.Name {
			t.Fatalf("command-only ordering changed at %d: got %q, want %q", i, baseline[i].Value, cmd.Name)
		}
	}

	r.RegisterWorkspace([]api.Command{{Name: "review"}}, []api.Skill{
		{Name: "tasksmith-customizations", Namespace: "system"},
		{Name: "deploy", Arguments: []api.Argument{{Name: "env", Required: true}}},
	})
	engine := autocomplete.New()
	for _, source := range r.Sources() {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}
	complete := func(input string) autocomplete.Result {
		t.Helper()
		result, err := engine.Complete(context.Background(), input, len(input))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := complete("/")
	if len(result.Candidates) != len(r.List()) {
		t.Fatalf("got %d candidates for %d registered commands", len(result.Candidates), len(r.List()))
	}
	for i, want := range []string{baseline[0].Value, "tasksmith-customizations", baseline[1].Value, "deploy"} {
		if result.Candidates[i].Value != want {
			t.Errorf("candidate %d = %q, want %q", i, result.Candidates[i].Value, want)
		}
	}
	seen := make(map[string]int)
	for _, candidate := range result.Candidates {
		seen[candidate.Value]++
	}
	for _, cmd := range r.List() {
		if seen[cmd.Name] != 1 {
			t.Errorf("candidate %q appears %d times", cmd.Name, seen[cmd.Name])
		}
	}

	result = complete("/tasksmith")
	if len(result.Candidates) != 1 || result.Candidates[0].Value != "tasksmith-customizations" {
		t.Fatalf("unexpected filtered candidates: %+v", result.Candidates)
	}
	if text, _, err := result.Apply(0); err != nil || text != "/tasksmith-customizations" {
		t.Fatalf("applying built-in skill: text=%q err=%v", text, err)
	}
	result = complete("/deploy")
	if len(result.Candidates) != 1 {
		t.Fatalf("unexpected deploy candidates: %+v", result.Candidates)
	}
	if text, _, err := result.Apply(0); err != nil || text != "/deploy " {
		t.Fatalf("applying skill with arguments: text=%q err=%v", text, err)
	}
	result = complete("/deploy ")
	if len(result.Candidates) != 1 || result.Candidates[0].Value != "env" {
		t.Fatalf("unexpected skill argument candidates: %+v", result.Candidates)
	}
}

func TestCompactAndContextCommands(t *testing.T) {
	r := NewRegistry()

	t.Run("compact in draft mode", func(t *testing.T) {
		store.ResetSession()
		store.ClearTranscript()
		defer store.ResetSession()
		defer store.ClearTranscript()

		if err := r.Execute("/compact some focus area"); err != nil {
			t.Fatalf("Execute /compact: %v", err)
		}

		tr := store.GetTranscript()
		if tr.Items == nil || len(*tr.Items) == 0 {
			t.Fatalf("expected transcript item for /compact in draft mode")
		}
		last := (*tr.Items)[len(*tr.Items)-1]
		if last.Kind != store.TranscriptKindNotice {
			t.Errorf("expected Notice transcript item, got %v", last.Kind)
		}
		if d, ok := last.Data.(store.NoticeData); !ok || d.Text != "Nothing to compact" {
			t.Errorf("expected 'Nothing to compact' notice, got %+v", last.Data)
		}
	})

	t.Run("context command", func(t *testing.T) {
		store.ResetSession()
		store.ClearTranscript()
		defer store.ResetSession()
		defer store.ClearTranscript()

		if err := r.Execute("/context"); err != nil {
			t.Fatalf("Execute /context: %v", err)
		}

		tr := store.GetTranscript()
		if tr.Items == nil || len(*tr.Items) == 0 {
			t.Fatalf("expected transcript item for /context")
		}
		last := (*tr.Items)[len(*tr.Items)-1]
		if last.Kind != store.TranscriptKindContextUsage {
			t.Fatalf("expected TranscriptKindContextUsage, got %v", last.Kind)
		}
		d, ok := last.Data.(store.ContextUsageData)
		if !ok {
			t.Fatalf("expected ContextUsageData, got %T", last.Data)
		}
		if d.ModelName == "" {
			t.Errorf("expected non-empty ModelName")
		}
	})
}

func TestWorkspaceCommandsAndSkillsSlashExecutionAndValidation(t *testing.T) {
	r := NewRegistry()

	cmds := []api.Command{
		{
			Name:        "review",
			Description: "Review a pull request",
			Arguments: []api.Argument{
				{Name: "pr_number", Required: true},
				{Name: "branch", Default: "main"},
			},
		},
		{
			Name:        "git/review",
			Description: "Plugin git review command",
			Arguments: []api.Argument{
				{Name: "target", Required: true},
			},
		},
	}
	skills := []api.Skill{
		{
			Name:        "deploy",
			Description: "Deploy application",
			Arguments: []api.Argument{
				{Name: "env", Required: true},
			},
		},
	}

	r.RegisterWorkspace(cmds, skills)

	// 1. Verify Lookups
	if _, ok := r.Lookup("review"); !ok {
		t.Fatal("expected /review command to be registered")
	}
	if _, ok := r.Lookup("/review"); !ok {
		t.Fatal("expected /review with slash prefix to resolve")
	}
	if _, ok := r.Lookup("git/review"); !ok {
		t.Fatal("expected /git/review plugin command to be registered")
	}
	if _, ok := r.Lookup("deploy"); !ok {
		t.Fatal("expected /deploy skill to be registered")
	}

	// 2. Client-side argument validation: Missing required argument fails immediately
	err := r.Execute("/review")
	if err == nil || !strings.Contains(err.Error(), "missing required argument \"pr_number\"") {
		t.Fatalf("expected missing required argument error for /review, got: %v", err)
	}

	// 3. Client-side argument validation: Passing positional argument satisfies required argument
	// Initialize fake store client to verify store.Send
	store.ResetSession()
	store.ClearTranscript()
	defer store.ResetSession()
	defer store.ClearTranscript()

	if err := r.Execute("/review 123"); err != nil {
		t.Fatalf("unexpected error for valid /review positional: %v", err)
	}

	// 4. Client-side argument validation: Passing named flag satisfies required argument
	if err := r.Execute("/review --pr_number=456"); err != nil {
		t.Fatalf("unexpected error for valid /review flag: %v", err)
	}

	// 5. Plugin namespaced command validation
	if err := r.Execute("/git/review"); err == nil || !strings.Contains(err.Error(), "missing required argument \"target\"") {
		t.Fatalf("expected missing required argument error for /git/review, got: %v", err)
	}
	if err := r.Execute("/git/review prod"); err != nil {
		t.Fatalf("unexpected error for valid /git/review: %v", err)
	}

	// 6. Skill validation
	if err := r.Execute("/deploy"); err == nil || !strings.Contains(err.Error(), "missing required argument \"env\"") {
		t.Fatalf("expected missing required argument error for /deploy, got: %v", err)
	}
	if err := r.Execute("/deploy production"); err != nil {
		t.Fatalf("unexpected error for valid /deploy: %v", err)
	}

	// 7. Verify Autocomplete Sources
	sources := r.Sources()
	if len(sources) == 0 {
		t.Fatal("expected autocomplete sources")
	}
	// Verify candidates contains review, git/review, deploy
	mainCandidates, err := sources[0].Complete(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("complete sources: %v", err)
	}
	var foundReview, foundPluginReview, foundDeploy bool
	for _, c := range mainCandidates {
		switch c.Value {
		case "review":
			foundReview = true
		case "git/review":
			foundPluginReview = true
		case "deploy":
			foundDeploy = true
		}
	}
	if !foundReview || !foundPluginReview || !foundDeploy {
		t.Errorf("missing candidates: review=%v, git/review=%v, deploy=%v", foundReview, foundPluginReview, foundDeploy)
	}
}

func TestAgentTriggerDisplayAndRestriction(t *testing.T) {
	// 1. The trigger badge and agent description surface trigger info in the
	// picker and autocomplete (both AgentPicker and completeAgents use these
	// helpers to build their descriptions).
	if got := triggerBadge(nil); got != "triggers: all" {
		t.Errorf("triggerBadge(nil) = %q, want %q", got, "triggers: all")
	}
	if got := triggerBadge([]string{"agent"}); got != "triggers: agent" {
		t.Errorf("triggerBadge([agent]) = %q, want %q", got, "triggers: agent")
	}
	if got := agentDescription(api.Agent{Name: "main", Description: "Default coding persona"}); got != "Default coding persona [triggers: all]" {
		t.Errorf("agentDescription (unrestricted) = %q", got)
	}
	if got := agentDescription(api.Agent{Name: "explore", Description: "Fast read-only explorer", Triggers: []string{"agent"}}); got != "Fast read-only explorer [triggers: agent]" {
		t.Errorf("agentDescription (restricted) = %q", got)
	}
	if got := agentDescription(api.Agent{Name: "x", Triggers: []string{"agent"}}); got != "[triggers: agent]" {
		t.Errorf("agentDescription without description = %q", got)
	}

	// 2. /agent <restricted> is rejected when the agent excludes the human trigger.
	store.ResetSession()
	store.ResetApp()
	defer store.ResetSession()
	defer store.ResetApp()

	store.SetWorkspaceAgents([]api.Agent{
		{Name: "main", Description: "Default coding persona"},
		{Name: "explore", Description: "Fast read-only explorer", Triggers: []string{"agent"}},
	})

	r := NewRegistry()
	if err := r.Execute("/agent explore"); err == nil || !strings.Contains(err.Error(), "cannot be triggered by human") {
		t.Fatalf("expected /agent explore to be rejected, got: %v", err)
	}

	// A human-triggerable agent is accepted and set on the draft session.
	if err := r.Execute("/agent main"); err != nil {
		t.Fatalf("expected /agent main to succeed, got: %v", err)
	}
	if store.GetSession().Config.Agent != "main" {
		t.Errorf("expected agent main to be set, got %q", store.GetSession().Config.Agent)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestRuleAndFileAutocomplete(t *testing.T) {
	store.SetWorkspaceRules(nil)
	store.ResetSession()
	defer store.SetWorkspaceRules(nil)
	defer store.ResetSession()

	var lastFileQuery string
	mockHC := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.Path, "/v1/workspace/rules") {
				rules := []api.Rule{
					{Name: "commit-hygiene", Description: "Strict commit rules"},
				}
				data, _ := json.Marshal(rules)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(data)),
					Header:     make(http.Header),
				}, nil
			}
			if strings.HasPrefix(req.URL.Path, "/v1/skills") {
				skills := []api.Skill{
					{Name: "release-runbook", Description: "Step-by-step release procedure"},
				}
				data, _ := json.Marshal(skills)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(data)),
					Header:     make(http.Header),
				}, nil
			}
			if strings.HasPrefix(req.URL.Path, "/v1/workspace/files") {
				lastFileQuery = req.URL.Query().Get("q")
				files := []api.WorkspaceFile{
					{
						Path:      "internal/cli/app.go",
						ShortPath: "cli/app.go",
						Name:      "app.go",
						Dir:       "internal/cli",
						IsDir:     false,
						Recent:    true,
						Allowed:   true,
					},
					{
						Path:      "internal/daemon/",
						ShortPath: "daemon/",
						Name:      "daemon",
						Dir:       "internal",
						IsDir:     true,
						Recent:    false,
						Allowed:   true,
					},
					{
						Path:      "secret.key",
						ShortPath: "secret.key",
						Name:      "secret.key",
						Dir:       "",
						IsDir:     false,
						Recent:    false,
						Allowed:   false,
					},
				}
				data, _ := json.Marshal(files)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(data)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("not found")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	testClient, err := client.New("/tmp/test.sock", client.WithHTTPClient(mockHC), client.WithCWD("/test/ws"))
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	store.SetClient(testClient)

	r := NewRegistry()
	engine := autocomplete.New()
	for _, source := range r.Sources() {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Test @rule: autocomplete
	input := "@rule:commit"
	res, err := engine.Complete(context.Background(), input, len(input))
	if err != nil {
		t.Fatalf("Complete @rule: error: %v", err)
	}
	if res.Source != "rules" {
		t.Fatalf("expected source rules, got %q", res.Source)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	if res.Candidates[0].InsertText != "commit-hygiene " {
		t.Errorf("expected InsertText %q, got %q", "commit-hygiene ", res.Candidates[0].InsertText)
	}
	finalText, _, err := res.Apply(0)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if finalText != "@rule:commit-hygiene " {
		t.Errorf("expected final text %q, got %q", "@rule:commit-hygiene ", finalText)
	}

	// 1b. Test @skill: autocomplete
	skillInput := "@skill:release"
	skillRes, err := engine.Complete(context.Background(), skillInput, len(skillInput))
	if err != nil {
		t.Fatalf("Complete @skill: error: %v", err)
	}
	if skillRes.Source != "skills" {
		t.Fatalf("expected source skills, got %q", skillRes.Source)
	}
	if len(skillRes.Candidates) != 1 || skillRes.Candidates[0].InsertText != "release-runbook " {
		t.Fatalf("unexpected @skill: candidates: %+v", skillRes.Candidates)
	}
	skillFinalText, _, err := skillRes.Apply(0)
	if err != nil {
		t.Fatalf("Apply @skill: error: %v", err)
	}
	if skillFinalText != "@skill:release-runbook " {
		t.Errorf("expected final skill text %q, got %q", "@skill:release-runbook ", skillFinalText)
	}

	// 2. Test @ file autocomplete
	fileInput := "@app"
	fileRes, err := engine.Complete(context.Background(), fileInput, len(fileInput))
	if err != nil {
		t.Fatalf("Complete @app error: %v", err)
	}
	if fileRes.Source != "files" {
		t.Fatalf("expected source files, got %q", fileRes.Source)
	}
	if len(fileRes.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(fileRes.Candidates))
	}
	// Check first candidate (app.go)
	if fileRes.Candidates[0].InsertText != "cli/app.go " {
		t.Errorf("expected InsertText %q, got %q", "cli/app.go ", fileRes.Candidates[0].InsertText)
	}
	if !strings.Contains(fileRes.Candidates[0].Description, "recent") {
		t.Errorf("expected recent in description, got %q", fileRes.Candidates[0].Description)
	}
	// Check directory candidate
	if fileRes.Candidates[1].InsertText != "daemon/ " {
		t.Errorf("expected directory InsertText %q, got %q", "daemon/ ", fileRes.Candidates[1].InsertText)
	}
	// Check prohibited candidate
	if !strings.Contains(fileRes.Candidates[2].Label, "[Prohibited]") {
		t.Errorf("expected [Prohibited] label, got %q", fileRes.Candidates[2].Label)
	}

	// Apply first candidate
	fileFinalText, _, err := fileRes.Apply(0)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if fileFinalText != "@cli/app.go " {
		t.Errorf("expected final file text %q, got %q", "@cli/app.go ", fileFinalText)
	}

	// 3. Editing an already-inserted @file:... token strips the "file:" prefix when querying
	editInput := "@file:internal/cli/ap"
	editRes, err := engine.Complete(context.Background(), editInput, len(editInput))
	if err != nil {
		t.Fatalf("Complete @file: error: %v", err)
	}
	if lastFileQuery != "internal/cli/ap" {
		t.Errorf("expected query %q, got %q", "internal/cli/ap", lastFileQuery)
	}
	editedText, _, err := editRes.Apply(0)
	if err != nil {
		t.Fatalf("Apply edited error: %v", err)
	}
	if editedText != "@cli/app.go " {
		t.Errorf("expected edited text %q, got %q", "@cli/app.go ", editedText)
	}

	// 4. Render CompletionViews["files"] via testutils.Render
	views := CompletionViews(api.ModeDefault)
	fileView, ok := views["files"]
	if !ok {
		t.Fatalf("expected files view in CompletionViews")
	}
	rendered := testutils.Render(t, fileView(fileRes, 0), 100, 20)
	for _, want := range []string{"app.go", "internal/cli · recent", "daemon/", "secret.key [Prohibited]"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected rendered file completion view to contain %q, got:\n%s", want, rendered)
		}
	}
}
