package command

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/api"
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
	if len(suggs) < 2 { // sessions, switch
		t.Errorf("Suggest('s') expected >=2, got %d", len(suggs))
	}
	suggsLs := reg.Suggest("/l")
	if len(suggsLs) == 0 {
		t.Errorf("Suggest('/l') expected alias match for ls/sessions")
	}

	// Custom registration override
	reg.Register(Command{
		Name:        "custom",
		Description: "A custom test command",
		Handler: func(_ Env, _ []string) (tea.Msg, error) {
			return OutputMsg{Text: "custom ok"}, nil
		},
	})
	res, err := reg.Execute(Env{}, "/custom")
	if err != nil || res != (OutputMsg{Text: "custom ok"}) {
		t.Errorf("Execute custom failed: res=%+v, err=%v", res, err)
	}

	// Empty line execution
	resEmpty, errEmpty := reg.Execute(Env{}, "   ")
	if errEmpty != nil || resEmpty != nil {
		t.Errorf("Execute empty line error: res=%+v, err=%v", resEmpty, errEmpty)
	}

	// Unknown command error
	_, errUnknown := reg.Execute(Env{}, "/unknown_cmd")
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

	env := Env{
		Session:      sess,
		Mode:         api.ModeDefault,
		DefaultAgent: "developer",
		DefaultModel: "claude-sonnet-4-6",
	}

	// 1. /help
	resHelp, err := reg.Execute(env, "/help")
	if err != nil {
		t.Fatalf("/help failed: %v", err)
	}
	outHelp, ok := resHelp.(OutputMsg)
	if !ok || !strings.Contains(outHelp.Text, "Available commands:") {
		t.Errorf("/help returned invalid message: %+v", resHelp)
	}

	// 2. /new
	resNew, err := reg.Execute(env, "/new my-model")
	if err != nil || resNew != (NewSessionMsg{Model: "my-model"}) {
		t.Errorf("/new failed: res=%+v, err=%v", resNew, err)
	}
	resNewDefault, err := reg.Execute(env, "/new")
	if err != nil || resNewDefault != (NewSessionMsg{Model: "claude-sonnet-4-6"}) {
		t.Errorf("/new default failed: res=%+v, err=%v", resNewDefault, err)
	}

	// 3. /sessions
	resSessions, err := reg.Execute(env, "/sessions")
	if err != nil || resSessions != (ListSessionsMsg{}) {
		t.Errorf("/sessions failed: res=%+v, err=%v", resSessions, err)
	}

	// 4. /switch
	resSwitch, err := reg.Execute(env, "/switch s1")
	if err != nil || resSwitch != (SwitchSessionMsg{SessionID: "s1"}) {
		t.Errorf("/switch failed: res=%+v, err=%v", resSwitch, err)
	}
	if _, err := reg.Execute(env, "/switch"); err == nil {
		t.Errorf("/switch without args expected error")
	}

	// 5. /history
	resHist, err := reg.Execute(env, "/history")
	if err != nil || resHist != (ShowHistoryMsg{}) {
		t.Errorf("/history failed: res=%+v, err=%v", resHist, err)
	}

	// 6. /mode read & set
	resModeRead, err := reg.Execute(env, "/mode")
	if err != nil {
		t.Fatalf("/mode read failed: %v", err)
	}
	openPicker, ok := resModeRead.(OpenPickerMsg)
	if !ok || len(openPicker.Picker.Options) != 5 {
		t.Errorf("/mode read expected OpenPickerMsg with 5 options, got %+v", resModeRead)
	}
	selMsg := openPicker.Picker.Select()
	if selMsg != (ChangeModeMsg{Mode: api.ModeDefault}) {
		t.Errorf("picker Select() = %+v, want ChangeModeMsg(default)", selMsg)
	}

	resModeSet, err := reg.Execute(env, "/mode accept_edits")
	if err != nil || resModeSet != (ChangeModeMsg{Mode: api.ModeAcceptEdits}) {
		t.Errorf("/mode set failed: res=%+v, err=%v", resModeSet, err)
	}
	if _, err := reg.Execute(env, "/mode invalid"); err == nil {
		t.Errorf("/mode invalid expected error")
	}

	// 7. /changes
	resChanges, err := reg.Execute(env, "/changes")
	if err != nil || resChanges != (ListChangesMsg{}) {
		t.Errorf("/changes failed: res=%+v, err=%v", resChanges, err)
	}

	// 8. /diff
	resDiff, err := reg.Execute(env, "/diff main.go c1")
	if err != nil || resDiff != (ShowDiffMsg{Path: "main.go", EntryID: "c1"}) {
		t.Errorf("/diff failed: res=%+v, err=%v", resDiff, err)
	}
	if _, err := reg.Execute(env, "/diff main.go"); err == nil {
		t.Errorf("/diff missing entry_id expected error")
	}

	// 9. /revert
	resRevert, err := reg.Execute(env, "/revert main.go")
	if err != nil || resRevert != (RevertMsg{Path: "main.go"}) {
		t.Errorf("/revert failed: res=%+v, err=%v", resRevert, err)
	}
	resRevertAll, err := reg.Execute(env, "/revert")
	if err != nil || resRevertAll != (RevertMsg{}) {
		t.Errorf("/revert all failed: res=%+v, err=%v", resRevertAll, err)
	}

	// 10. /clear
	resClear, err := reg.Execute(env, "/clear")
	if err != nil || resClear != (ClearScreenMsg{}) {
		t.Errorf("/clear failed: res=%+v, err=%v", resClear, err)
	}

	// 11. /exit
	resExit, err := reg.Execute(env, "/exit")
	if err != nil || resExit != (QuitMsg{}) {
		t.Errorf("/exit failed: res=%+v, err=%v", resExit, err)
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
	if cmd.OnSelect != nil {
		msg := cmd.OnSelect(opts[1])
		if msg != (ChangeModeMsg{Mode: api.ModeAcceptEdits}) {
			t.Errorf("OnSelect returned %+v", msg)
		}
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
