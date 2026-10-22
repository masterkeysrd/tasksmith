package command

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

func TestRegistryLookupSuggestAndExecute(t *testing.T) {
	reg := NewRegistry()
	cmds := reg.List()
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty command registry")
	}

	// Lookup by primary name and alias
	if c, ok := reg.Lookup("help"); !ok || c.Name != "help" {
		t.Errorf("Lookup('help') failed: %+v", c)
	}
	if c, ok := reg.Lookup("h"); !ok || c.Name != "help" {
		t.Errorf("Lookup('h') alias failed: %+v", c)
	}
	if c, ok := reg.Lookup("/h"); !ok || c.Name != "help" {
		t.Errorf("Lookup('/h') with slash failed: %+v", c)
	}
	if _, ok := reg.Lookup("nonexistent"); ok {
		t.Errorf("Lookup('nonexistent') should return false")
	}

	// Suggest
	suggs := reg.Suggest("s")
	if len(suggs) < 1 {
		t.Errorf("Suggest('s') expected >=1, got %d", len(suggs))
	}
	if suggs[0].Name != "sessions" {
		t.Errorf("Suggest('s') expected sessions, got %s", suggs[0].Name)
	}
	suggsLs := reg.Suggest("/l")
	if len(suggsLs) == 0 {
		t.Errorf("Suggest('/l') expected alias match for ls/sessions")
	}

	// Custom registration override
	var customCalled bool
	reg.Register(Command{
		Name:        "custom",
		Description: "A custom test command",
		Handler: func(_ Env, _ []string) error {
			customCalled = true
			return nil
		},
	})
	err := reg.Execute(Env{}, "/custom")
	if err != nil || !customCalled {
		t.Errorf("Execute custom failed: called=%v, err=%v", customCalled, err)
	}

	// Empty line execution
	errEmpty := reg.Execute(Env{}, "   ")
	if errEmpty != nil {
		t.Errorf("Execute empty line error: err=%v", errEmpty)
	}

	// Unknown command error
	errUnknown := reg.Execute(Env{}, "/unknown_cmd")
	if errUnknown == nil || !strings.Contains(errUnknown.Error(), "unknown command") {
		t.Errorf("expected unknown command error, got %v", errUnknown)
	}
}

func TestBuiltinCommandsExecution(t *testing.T) {
	reg := NewRegistry()

	sess := &api.Session{
		ID:    "s1",
		Title: "Session One",
		Agent: "build",
		Model: "gpt-5",
	}

	var appendedNodes []trail.Node
	var openedPicker *ui.PickerProps
	var setModeVal api.PermissionMode
	var newSessionModel string
	var switchSessionID string
	var renameSessionID, renameSessionTitle string
	var deleteSessionID string
	var listChangesCalled bool
	var diffPath, diffEntryID string
	var revertPath, revertEntryID string
	var clearScreenCalled bool
	var exitCalled bool

	reset := func() {
		appendedNodes = nil
		openedPicker = nil
		setModeVal = ""
		newSessionModel = ""
		switchSessionID = ""
		renameSessionID = ""
		renameSessionTitle = ""
		deleteSessionID = ""
		listChangesCalled = false
		diffPath = ""
		diffEntryID = ""
		revertPath = ""
		revertEntryID = ""
		clearScreenCalled = false
		exitCalled = false
	}

	env := Env{
		Session:      sess,
		Mode:         api.ModeDefault,
		DefaultAgent: "developer",
		DefaultModel: "claude-sonnet-4-6",
		AppendNode: func(n trail.Node) {
			appendedNodes = append(appendedNodes, n)
		},
		OpenPicker: func(p ui.PickerProps) {
			openedPicker = &p
		},
		SetMode: func(m api.PermissionMode) {
			setModeVal = m
		},
		NewSession: func(m string) {
			newSessionModel = m
		},
		SwitchSession: func(id string) {
			switchSessionID = id
		},
		RenameSession: func(id, title string) {
			renameSessionID = id
			renameSessionTitle = title
		},
		DeleteSession: func(id string) {
			deleteSessionID = id
		},
		ListChanges: func() {
			listChangesCalled = true
		},
		DiffChange: func(path, entryID string) {
			diffPath = path
			diffEntryID = entryID
		},
		RevertChanges: func(path, entryID string) {
			revertPath = path
			revertEntryID = entryID
		},
		ClearScreen: func() {
			clearScreenCalled = true
		},
		Exit: func() {
			exitCalled = true
		},
	}

	// 1. /help
	reset()
	if err := reg.Execute(env, "/help"); err != nil || openedPicker == nil {
		t.Fatalf("/help failed: opened=%v, err=%v", openedPicker != nil, err)
	}
	if len(openedPicker.Options) != len(reg.List()) {
		t.Errorf("expected %d help options, got %d", len(reg.List()), len(openedPicker.Options))
	}
	// Verify canceling /help emits Exited /help command
	openedPicker.OnCancel()
	if len(appendedNodes) != 1 {
		t.Fatalf("expected 1 appended node after cancel, got %d", len(appendedNodes))
	}

	// 2. /new
	reset()
	if err := reg.Execute(env, "/new my-model"); err != nil || newSessionModel != "my-model" {
		t.Errorf("/new failed: model=%q, err=%v", newSessionModel, err)
	}
	reset()
	if err := reg.Execute(env, "/new"); err != nil || newSessionModel != "claude-sonnet-4-6" {
		t.Errorf("/new default failed: model=%q, err=%v", newSessionModel, err)
	}

	// 3. /sessions
	// 3a. /sessions when ListSessions is nil
	reset()
	if err := reg.Execute(env, "/sessions"); err != nil || len(appendedNodes) != 1 {
		t.Errorf("/sessions (nil ListSessions) failed: appended=%d, err=%v", len(appendedNodes), err)
	}

	// 3b. /sessions when ListSessions returns empty
	envEmpty := env
	envEmpty.ListSessions = func() ([]api.Session, error) {
		return nil, nil
	}
	reset()
	if err := reg.Execute(envEmpty, "/sessions"); err != nil || len(appendedNodes) != 1 {
		t.Errorf("/sessions (empty) failed: appended=%d, err=%v", len(appendedNodes), err)
	}

	// 3c. /sessions when ListSessions returns sessions -> OpenPicker
	now := time.Now().UTC()
	envWithSessions := env
	envWithSessions.Session = &api.Session{ID: "s1"}
	envWithSessions.ListSessions = func() ([]api.Session, error) {
		return []api.Session{
			{
				ID:        "s1",
				Title:     "Session One",
				Model:     "claude-sonnet-4-6",
				UpdatedAt: now,
			},
			{
				ID:        "s2",
				Title:     "Session Two",
				Model:     "claude-opus-4-6",
				UpdatedAt: now,
			},
		}, nil
	}
	reset()
	if err := reg.Execute(envWithSessions, "/sessions"); err != nil || openedPicker == nil {
		t.Fatalf("/sessions failed: opened=%v, err=%v", openedPicker != nil, err)
	}
	if len(openedPicker.Options) != 2 {
		t.Fatalf("expected 2 picker options, got %d", len(openedPicker.Options))
	}
	if openedPicker.Options[0].Value != "s1" || !openedPicker.Options[0].Current {
		t.Errorf("expected option 0 to be current s1, got %+v", openedPicker.Options[0])
	}
	// Verify session id is not present in Cells (only title, model, relTime)
	cells := openedPicker.Options[0].Cells
	if len(cells) != 3 {
		t.Fatalf("expected 3 cells (title, model, relTime), got %d: %v", len(cells), cells)
	}
	if cells[0] != "Session One" || cells[1] != "claude-sonnet-4-6" {
		t.Errorf("unexpected cells: %v", cells)
	}
	for _, cell := range cells {
		if cell == "s1" {
			t.Errorf("session ID 's1' should not be present in cells: %v", cells)
		}
	}
	// Verify Columns configured on picker
	if len(openedPicker.Columns) != 3 || openedPicker.Columns[0].Width != 0 || openedPicker.Columns[1].Width != 24 || openedPicker.Columns[2].Width != 26 {
		t.Errorf("unexpected Columns: %+v", openedPicker.Columns)
	}
	// Verify canceling /sessions emits Exited /sessions command
	openedPicker.OnCancel()
	if len(appendedNodes) != 1 {
		t.Fatalf("expected 1 appended node after sessions cancel, got %d", len(appendedNodes))
	}
	reset()
	_ = reg.Execute(envWithSessions, "/sessions")
	openedPicker.OnSelect(openedPicker.Options[0])
	if switchSessionID != "s1" {
		t.Errorf("picker OnSelect() = %q, want 's1'", switchSessionID)
	}
	f4Action, ok := openedPicker.ConfirmActions["f4"]
	if !ok {
		t.Fatalf("expected 'f4' confirm action in picker")
	}
	f4Action.OnConfirm(openedPicker.Options[1])
	if deleteSessionID != "s2" {
		t.Errorf("f4Action.OnConfirm(s2) = %q, want 's2'", deleteSessionID)
	}

	f2Action, ok := openedPicker.InputActions["f2"]
	if !ok {
		t.Fatalf("expected 'f2' input action in picker")
	}
	f2Action.OnSubmit(openedPicker.Options[1], "New Title")
	if renameSessionID != "s2" || renameSessionTitle != "New Title" {
		t.Errorf("f2Action.OnSubmit(s2) = (%q, %q), want ('s2', 'New Title')", renameSessionID, renameSessionTitle)
	}

	// 3d. /sessions <id> direct switch
	reset()
	if err := reg.Execute(env, "/sessions s1"); err != nil || switchSessionID != "s1" {
		t.Errorf("/sessions s1 failed: switchID=%q, err=%v", switchSessionID, err)
	}
	reset()
	if err := reg.Execute(env, "/session s1"); err != nil || switchSessionID != "s1" {
		t.Errorf("/session s1 failed: switchID=%q, err=%v", switchSessionID, err)
	}
	reset()
	if err := reg.Execute(envWithSessions, "/ls"); err != nil || openedPicker == nil {
		t.Errorf("/ls failed: opened=%v, err=%v", openedPicker != nil, err)
	}
	if _, ok := reg.Lookup("switch"); ok {
		t.Errorf("/switch should not exist in registry")
	}
	if _, ok := reg.Lookup("history"); ok {
		t.Errorf("/history should not exist in registry")
	}

	// 4. /mode read & set
	reset()
	if err := reg.Execute(env, "/mode"); err != nil || openedPicker == nil || len(openedPicker.Options) != 5 {
		t.Fatalf("/mode read failed: opened=%v, err=%v", openedPicker != nil, err)
	}
	// Verify canceling /mode emits Exited /mode command
	openedPicker.OnCancel()
	if len(appendedNodes) != 1 {
		t.Fatalf("expected 1 appended node after mode cancel, got %d", len(appendedNodes))
	}
	reset()
	_ = reg.Execute(env, "/mode")
	openedPicker.OnSelect(openedPicker.Options[0])
	if setModeVal != api.ModeDefault {
		t.Errorf("picker OnSelect() = %q, want %q", setModeVal, api.ModeDefault)
	}

	reset()
	if err := reg.Execute(env, "/mode accept_edits"); err != nil || setModeVal != api.ModeAcceptEdits {
		t.Errorf("/mode set failed: mode=%q, err=%v", setModeVal, err)
	}
	if err := reg.Execute(env, "/mode invalid"); err == nil {
		t.Errorf("/mode invalid expected error")
	}

	// 5. /changes
	reset()
	if err := reg.Execute(env, "/changes"); err != nil || !listChangesCalled {
		t.Errorf("/changes failed: called=%v, err=%v", listChangesCalled, err)
	}

	// 6. /diff
	reset()
	if err := reg.Execute(env, "/diff main.go c1"); err != nil || diffPath != "main.go" || diffEntryID != "c1" {
		t.Errorf("/diff failed: path=%q, entryID=%q, err=%v", diffPath, diffEntryID, err)
	}
	if err := reg.Execute(env, "/diff main.go"); err == nil {
		t.Errorf("/diff missing entry_id expected error")
	}

	// 7. /revert
	reset()
	if err := reg.Execute(env, "/revert main.go c1"); err != nil || revertPath != "main.go" || revertEntryID != "c1" {
		t.Errorf("/revert failed: path=%q, entryID=%q, err=%v", revertPath, revertEntryID, err)
	}
	reset()
	if err := reg.Execute(env, "/revert main.go"); err != nil || revertPath != "main.go" || revertEntryID != "" {
		t.Errorf("/revert file failed: path=%q, err=%v", revertPath, err)
	}
	reset()
	if err := reg.Execute(env, "/revert"); err != nil || revertPath != "" || revertEntryID != "" {
		t.Errorf("/revert all failed: path=%q, err=%v", revertPath, err)
	}

	// 8. /clear
	reset()
	if err := reg.Execute(env, "/clear"); err != nil || !clearScreenCalled {
		t.Errorf("/clear failed: called=%v, err=%v", clearScreenCalled, err)
	}

	// 9. /exit
	reset()
	if err := reg.Execute(env, "/exit"); err != nil || !exitCalled {
		t.Errorf("/exit failed: called=%v, err=%v", exitCalled, err)
	}
}

func TestSuggestArgs(t *testing.T) {
	reg := NewRegistry()
	env := Env{
		Mode: api.ModeDefault,
	}

	// 1. Without space -> false
	if _, _, ok := reg.SuggestArgs(env, "/mode"); ok {
		t.Errorf("SuggestArgs without space should return false")
	}

	// 2. Trailing space -> all 5 options
	cmd, opts, ok := reg.SuggestArgs(env, "/mode ")
	if !ok || cmd.Name != "mode" || len(opts) != 5 {
		t.Fatalf("SuggestArgs('/mode ') = (%+v, len=%d, ok=%v)", cmd, len(opts), ok)
	}
	if !opts[0].Current {
		t.Errorf("expected opts[0] (default) to be Current")
	}

	// 3. Filter by prefix
	_, filteredAS, ok := reg.SuggestArgs(env, "/mode as")
	if !ok || len(filteredAS) != 1 || filteredAS[0].Value != "ask" {
		t.Errorf("SuggestArgs('/mode as') expected 'ask', got %+v", filteredAS)
	}

	// 4. Filter exact match
	_, filteredPlan, ok := reg.SuggestArgs(env, "/mode plan")
	if !ok || len(filteredPlan) != 1 || filteredPlan[0].Value != "plan" {
		t.Errorf("SuggestArgs('/mode plan') expected 'plan', got %+v", filteredPlan)
	}

	// 5. Command without options -> false
	if _, _, ok := reg.SuggestArgs(env, "/help "); ok {
		t.Errorf("SuggestArgs('/help ') should return false")
	}
}
