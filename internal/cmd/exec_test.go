package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/exec"
)

// execWorkspace writes the fixture workspace's WORKSPACE.md as a warp
// manifest declaring the fake provider's default model so sessions resolve a
// model (the fake gateway answers any model ID).
func execWorkspace(t *testing.T, wsDir string) {
	t.Helper()
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: exec-test\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write WORKSPACE.md: %v", err)
	}
}

// runExecCLI drives `tasksmith exec` through the root command against the
// in-process fixture daemon and returns (stdout, stderr, exit code). The
// fixture's fake gateway serves any model, so runs pin one explicitly.
func runExecCLI(t *testing.T, sock, ws, stdin string, args ...string) (string, string, int) {
	t.Helper()
	return runExecCLIReader(t, sock, ws, strings.NewReader(stdin), args...)
}

func runExecCLIReader(t *testing.T, sock, ws string, stdin io.Reader, args ...string) (string, string, int) {
	t.Helper()
	execWorkspace(t, ws)
	base := []string{"exec", "--socket", sock, "-w", ws, "--model", "fake/fake-default-model"}

	cmd := NewRootCmd()
	var outBuf, errBuf syncBuffer
	cmd.SetIn(stdin)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append(base, args...))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	return outBuf.String(), errBuf.String(), exec.ExitCode(err)
}

func decodeFrames[T any](t *testing.T, raw string) []T {
	t.Helper()
	var frames []T
	for line := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var f T
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("decode frame %q: %v", line, err)
		}
		frames = append(frames, f)
	}
	return frames
}

func TestExecCommandTextOutput(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "hello")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want %d", code, errOut, exec.ExitOK)
	}
	if got := strings.TrimSpace(out); got != "Hello from the Brain Daemon!" {
		t.Fatalf("stdout = %q, want the fake provider reply", got)
	}
}

func TestExecCommandPipedStdinBecomesContext(t *testing.T) {
	var seen []string
	prov := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
		var texts []string
		for _, m := range req.Messages {
			texts = append(texts, m.GetContent().Text())
		}
		seen = texts
		return func(yield func(message.AssistantChunk, error) bool) {
			yield(message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: uuid.NewString()},
				Model:     req.Model,
				Content:   []message.Block{&message.TextBlock{Text: "ok"}},
				Done:      true,
			}, nil)
		}, nil
	}}
	_, sockPath, wsDir := setupCLITestDaemon(t, prov)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "diff --git a/x b/x", "review this")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("stdout = %q", out)
	}
	var joined string
	for _, m := range seen {
		joined += m + "\n"
	}
	if !strings.Contains(joined, "diff --git a/x b/x") || !strings.Contains(joined, "review this") {
		t.Fatalf("prompt or piped context missing from model request:\n%s", joined)
	}
}

func TestExecCommandJSONEnvelope(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--output-format", "json", "hello")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}

	var res api.ExecResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, out)
	}
	if res.Status != api.ExecResultCompleted {
		t.Fatalf("status = %q, want completed", res.Status)
	}
	if res.Text != "Hello from the Brain Daemon!" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.SessionID == "" {
		t.Fatal("session_id must name the run's session")
	}
}

func TestExecCommandJSONLStream(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--output-format", "jsonl", "hello")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}

	type frame struct {
		Type      string          `json:"type"`
		SessionID string          `json:"session_id"`
		Status    string          `json:"status"`
		Text      string          `json:"text"`
		Payload   json.RawMessage `json:"payload"`
	}
	frames := decodeFrames[frame](t, out)
	if len(frames) < 3 {
		t.Fatalf("expected header + events + result, got %d frames:\n%s", len(frames), out)
	}
	if frames[0].Type != api.ExecHeaderType || frames[0].SessionID == "" {
		t.Fatalf("first frame must be the session header, got %+v", frames[0])
	}
	last := frames[len(frames)-1]
	if last.Type != api.ExecResultType || last.Status != api.ExecResultCompleted {
		t.Fatalf("last frame must be the completed result frame, got %+v", last)
	}
	if last.Text != "Hello from the Brain Daemon!" {
		t.Fatalf("result text = %q", last.Text)
	}

	var sawChunk, sawCompleted bool
	for _, f := range frames[1 : len(frames)-1] {
		switch f.Type {
		case string(api.EventChunk):
			sawChunk = true
		case string(api.EventCompleted):
			sawCompleted = true
		}
	}
	if !sawChunk || !sawCompleted {
		t.Fatalf("stream must carry verbatim chunk and completed events (chunk=%v completed=%v)", sawChunk, sawCompleted)
	}
}

func TestExecCommandBidirectionalSendAndClose(t *testing.T) {
	var lastRequest string
	prov := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
		var texts []string
		for _, m := range req.Messages {
			texts = append(texts, m.GetContent().Text())
		}
		lastRequest = strings.Join(texts, "\n")
		return func(yield func(message.AssistantChunk, error) bool) {
			yield(message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: uuid.NewString()},
				Model:     req.Model,
				Content:   []message.Block{&message.TextBlock{Text: "ok"}},
				Done:      true,
			}, nil)
		}, nil
	}}
	_, sockPath, wsDir := setupCLITestDaemon(t, prov)

	frames := []api.ExecInputFrame{
		{Type: api.ExecInputSend, Content: "first"},
		{Type: api.ExecInputSend, Content: "second"},
		{Type: api.ExecInputClose},
	}
	var stdin strings.Builder
	for _, f := range frames {
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("marshal frame: %v", err)
		}
		stdin.Write(data)
		stdin.WriteByte('\n')
	}

	out, errOut, code := runExecCLI(t, sockPath, wsDir, stdin.String(), "--input-format", "jsonl", "--output-format", "jsonl")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}

	type frame struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	got := decodeFrames[frame](t, out)
	if len(got) == 0 || got[0].Type != api.ExecHeaderType {
		t.Fatalf("missing header frame in:\n%s", out)
	}
	last := got[len(got)-1]
	if last.Status != api.ExecResultCompleted {
		t.Fatalf("terminal status = %q, want completed", last.Status)
	}

	var completed int
	for _, f := range got {
		if f.Type == string(api.EventCompleted) {
			completed++
		}
	}
	if completed < 1 {
		t.Fatalf("stream must carry completed events:\n%s", out)
	}
	// The daemon batches queued sends; whichever turn shape resulted, the
	// final model request must contain both prompts.
	if !strings.Contains(lastRequest, "first") || !strings.Contains(lastRequest, "second") {
		t.Fatalf("both sends must reach the model, last request:\n%s", lastRequest)
	}
}

func TestExecCommandMaxTurnsStopsBidiRun(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	// The stdin pipe stays open (no close frame): only the turn cap can end
	// the bidirectional run, so a hang here means the cap is broken. An
	// io.Pipe write blocks until the run reads, so feed it from a goroutine.
	pipeR, pipeW := io.Pipe()
	t.Cleanup(func() { _ = pipeW.Close() })
	go func() {
		data, err := json.Marshal(api.ExecInputFrame{Type: api.ExecInputSend, Content: "bounded"})
		if err != nil {
			return
		}
		_, _ = pipeW.Write(append(data, '\n'))
	}()

	out, errOut, code := runExecCLIReader(t, sockPath, wsDir, pipeR,
		"--input-format", "jsonl", "--output-format", "jsonl", "--max-turns", "1")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	type frame struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	got := decodeFrames[frame](t, out)
	last := got[len(got)-1]
	if last.Status != api.ExecResultCompleted {
		t.Fatalf("terminal status = %q, want completed:\n%s", last.Status, out)
	}
}

func TestExecCommandLocalSlashModel(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--output-format", "jsonl", "/model")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if !strings.Contains(out, api.ExecCommandResultType) || !strings.Contains(out, "\"/model\"") {
		t.Fatalf("expected a command_result frame for /model in:\n%s", out)
	}
	// A local command must not burn a model turn.
	if strings.Contains(out, "\"type\":\""+string(api.EventCompleted)+"\"") {
		t.Fatalf("local /model must not start a daemon turn:\n%s", out)
	}
}

func TestExecCommandPlainPromptStartingWithCommandWordReachesModel(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "help me explain this function")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if strings.TrimSpace(out) != "Hello from the Brain Daemon!" {
		t.Fatalf("plain prompt starting with 'help' hijacked locally: %q", out)
	}
}

func TestExecCommandFailedLocalSlashExitsNonZeroOnce(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--output-format", "jsonl", "/mode bogus")
	if code != exec.ExitError {
		t.Fatalf("exit code = %d (stderr=%q), want %d", code, errOut, exec.ExitError)
	}
	if count := strings.Count(out, `"type":"command_result"`); count != 1 {
		t.Fatalf("command_result count = %d, want 1:\n%s", count, out)
	}
	if !strings.Contains(out, `"type":"result"`) {
		t.Fatalf("expected terminal result frame on failure:\n%s", out)
	}
}

func TestExecCommandUnknownSlashExitsFailed(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "/definitely-not-a-command")
	if code == exec.ExitOK {
		t.Fatalf("unknown slash command must fail (stdout=%q)", out)
	}
	if code != exec.ExitError {
		t.Fatalf("exit code = %d, want %d (stderr=%q)", code, exec.ExitError, errOut)
	}
	if !strings.Contains(errOut, "unknown slash command") {
		t.Fatalf("stderr must name the failure: %q", errOut)
	}
}

func TestExecCommandNeedsInputGuard(t *testing.T) {
	prov := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
		return func(yield func(message.AssistantChunk, error) bool) {
			yield(message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: uuid.NewString()},
				Model:     req.Model,
				Content: []message.Block{
					&message.ToolCall{ID: "tc-guard", Name: "write", Args: map[string]any{
						"path":    "guarded.txt",
						"content": "nope\n",
					}},
				},
				Done: true,
			}, nil)
		}, nil
	}}
	_, sockPath, wsDir := setupCLITestDaemon(t, prov)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--mode", "ask", "write a file")
	if code != exec.ExitNeedsInput {
		t.Fatalf("exit code = %d (stderr=%q stdout=%q), want %d", code, errOut, out, exec.ExitNeedsInput)
	}
	if !strings.Contains(errOut, "cannot answer") {
		t.Fatalf("stderr must explain the guard: %q", errOut)
	}
}

func TestExecGrillMeNeedsInput(t *testing.T) {
	const topic = "Should task retries use limit=3?"
	prov := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
		found := false
		for _, m := range req.Messages {
			if text := m.GetContent().Text(); strings.Contains(text, topic) && strings.Contains(text, "Interview in dependency order") {
				found = true
				break
			}
		}
		if !found {
			t.Error("exec did not forward the interview topic to the built-in command")
		}
		return func(yield func(message.AssistantChunk, error) bool) {
			yield(message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: uuid.NewString()},
				Model:     req.Model,
				Content: []message.Block{&message.ToolCall{ID: "tc-interview", Name: "ask_question", Args: map[string]any{
					"questions": []map[string]any{{"type": "text", "question": "What should be retried?", "description": "Choose which failures can be retried safely."}},
				}}},
				Done: true,
			}, nil)
		}, nil
	}}
	_, sockPath, wsDir := setupCLITestDaemon(t, prov)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "/grill-me "+topic)
	if code != exec.ExitNeedsInput || !strings.Contains(errOut, "1 question(s)") || !strings.Contains(errOut, "--input-format jsonl") {
		t.Fatalf("exit code = %d (stderr=%q stdout=%q), want needs_input with question and JSONL guidance", code, errOut, out)
	}
}

func TestExecCommandResumeSessionFlag(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	c, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	sess, err := c.CreateSession(context.Background(), api.CreateSessionRequest{Title: "resumed"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--session", sess.ID, "hello")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if strings.TrimSpace(out) != "Hello from the Brain Daemon!" {
		t.Fatalf("stdout = %q", out)
	}
	back, err := c.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if back.Status != api.StatusIdle {
		t.Fatalf("resumed session status = %q, want idle", back.Status)
	}
}

func TestExecCommandForkFlagBranchesSession(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	c, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	source, err := c.CreateSession(context.Background(), api.CreateSessionRequest{
		Title:  "source",
		Config: api.SessionConfig{Model: "fake/fake-default-model"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--session", source.ID, "--fork", "--output-format", "json", "hello")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	var res api.ExecResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.SessionID == source.ID {
		t.Fatalf("--fork must run on a branch, got source session %q", res.SessionID)
	}

	fork, err := c.GetSession(context.Background(), res.SessionID)
	if err != nil {
		t.Fatalf("GetSession(fork): %v", err)
	}
	if fork.ForkedFrom != source.ID {
		t.Fatalf("fork forked_from = %q, want source %q", fork.ForkedFrom, source.ID)
	}
	back, err := c.GetSession(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("GetSession(source): %v", err)
	}
	if back.Status != api.StatusIdle {
		t.Fatalf("source session status = %q after fork run, want idle", back.Status)
	}
}

func TestExecCommandForkFlagWithoutSessionIsUsageError(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	_, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--fork", "hello")
	if code != exec.ExitUsage {
		t.Fatalf("exit code = %d (stderr=%q), want usage error", code, errOut)
	}
	if !strings.Contains(errOut, "--fork") {
		t.Fatalf("stderr = %q, want it to name --fork", errOut)
	}
}

func TestExecCommandEphemeralDeletesSession(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--output-format", "json", "--ephemeral", "hello")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	var res api.ExecResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	c, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	if _, err := c.GetSession(context.Background(), res.SessionID); err == nil {
		t.Fatal("ephemeral session must be deleted after the run")
	}
}

func TestRootPrintShorthand(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)
	execWorkspace(t, wsDir)

	out, errOut, err := execCLI(t, "", "-p", "--socket", sockPath, "-w", wsDir, "--model", "fake/fake-default-model", "hello")
	code := exec.ExitCode(err)
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if strings.TrimSpace(out) != "Hello from the Brain Daemon!" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRootWithoutPrintStillRejectsUnknownCommand(t *testing.T) {
	_, _, err := execCLI(t, "", "bogus-subcommand")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("root with unknown args error = %v, want unknown command", err)
	}
}

func TestExecExitCodeErrorUnwrap(t *testing.T) {
	inner := errors.New("boom")
	err := &exec.ExitCodeError{Code: exec.ExitNeedsInput, Err: inner}
	if exec.ExitCode(err) != exec.ExitNeedsInput {
		t.Fatalf("ExitCode = %d, want %d", exec.ExitCode(err), exec.ExitNeedsInput)
	}
	if !errors.Is(err, inner) {
		t.Fatal("ExitCodeError must unwrap to its cause")
	}
	if exec.ExitCode(nil) != exec.ExitOK {
		t.Fatal("nil error must map to exit code 0")
	}
	if exec.ExitCode(errors.New("other")) != exec.ExitError {
		t.Fatal("generic errors must map to exit code 1")
	}
}

func TestExecCommandResolvesAtFileMentionsFromPromptOnly(t *testing.T) {
	var seenRequest string
	prov := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
		var parts []string
		for _, m := range req.Messages {
			parts = append(parts, m.GetContent().Text())
		}
		seenRequest = strings.Join(parts, "\n")
		return func(yield func(message.AssistantChunk, error) bool) {
			yield(message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: uuid.NewString()},
				Model:     req.Model,
				Content:   []message.Block{&message.TextBlock{Text: "reviewed"}},
				Done:      true,
			}, nil)
		}, nil
	}}
	_, sockPath, wsDir := setupCLITestDaemon(t, prov)

	if err := os.WriteFile(filepath.Join(wsDir, "notes.txt"), []byte("ATTACHED_PROMPT_FILE_CONTENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "ignored.txt"), []byte("PIPED_STDIN_MUST_NOT_ATTACH\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "patch mentions @ignored.txt", "Check @notes.txt")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if strings.TrimSpace(out) != "reviewed" {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(seenRequest, "ATTACHED_PROMPT_FILE_CONTENT") {
		t.Fatalf("expected @notes.txt from prompt argument to be attached, got:\n%s", seenRequest)
	}
	if strings.Contains(seenRequest, "PIPED_STDIN_MUST_NOT_ATTACH") {
		t.Fatalf("piped stdin @mention must never be expanded as an attachment:\n%s", seenRequest)
	}
}

func TestExecCommandBangShellPrintsOutput(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "!printf 'shell-ok'")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	if strings.TrimSpace(out) != "shell-ok" {
		t.Fatalf("text stdout = %q, want 'shell-ok'", out)
	}

	jsonOut, jsonErr, jsonCode := runExecCLI(t, sockPath, wsDir, "", "--output-format", "json", "!printf 'shell-json'")
	if jsonCode != exec.ExitOK {
		t.Fatalf("json exit code = %d (stderr=%q), want ok", jsonCode, jsonErr)
	}
	var res api.ExecResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOut)), &res); err != nil {
		t.Fatalf("decode json result: %v", err)
	}
	if res.Text != "shell-json" {
		t.Fatalf("json result text = %q, want 'shell-json'", res.Text)
	}
}

func TestExecCommandStructuredDataOnLocalCommands(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	out, errOut, code := runExecCLI(t, sockPath, wsDir, "", "--output-format", "jsonl", "/usage")
	if code != exec.ExitOK {
		t.Fatalf("exit code = %d (stderr=%q), want ok", code, errOut)
	}
	var sawUsageData bool
	for _, f := range decodeFrames[map[string]any](t, out) {
		if f["type"] == api.ExecCommandResultType && f["command"] == "/usage" {
			if _, ok := f["data"].(map[string]any); ok {
				sawUsageData = true
			}
		}
	}
	if !sawUsageData {
		t.Fatalf("expected structured data on /usage command_result:\n%s", out)
	}

	outEffort, errEffort, codeEffort := runExecCLI(t, sockPath, wsDir, "", "--output-format", "jsonl", "/effort")
	if codeEffort != exec.ExitOK || !strings.Contains(outEffort, `"command":"/effort"`) {
		t.Fatalf("/effort failed: code=%d stderr=%q stdout=%s", codeEffort, errEffort, outEffort)
	}
}

func TestExecCommandSubagentPermissionBubblesAndApprovesByRequestID(t *testing.T) {
	calls := 0
	prov := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
		calls++
		callNum := calls
		return func(yield func(message.AssistantChunk, error) bool) {
			if callNum == 1 {
				// First call runs inside the forked /deploy subagent and triggers a gated write.
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.ToolCall{ID: "tc-sub-write", Name: "write", Args: map[string]any{
							"path":    "sub-deployed.txt",
							"content": "approved-from-bidi\n",
						}},
					},
					Done: true,
				}, nil)
				return
			}
			yield(message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: uuid.NewString()},
				Model:     req.Model,
				Content:   []message.Block{&message.TextBlock{Text: "done-" + string(rune('0'+callNum))}},
				Done:      true,
			}, nil)
		}, nil
	}}
	_, sockPath, wsDir := setupCLITestDaemon(t, prov)
	execWorkspace(t, wsDir)

	cmdDir := filepath.Join(wsDir, ".agents", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdManifest := "---\napiVersion: warp/v1alpha1\nkind: Command\nmetadata:\n  name: deploy\nspec:\n  description: Deploy via subagent\n  context: clean\n---\nWrite sub-deployed.txt now.\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "deploy.command.md"), []byte(cmdManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outW.Close()
	})

	var createdSubagent *api.Session
	var sawSubPerm bool
	driverDone := make(chan struct{})
	go func() {
		defer close(driverDone)
		defer func() { _ = inW.Close() }()

		reader := api.NewExecFrameReader(outR)
		var rootID string
		for {
			var raw map[string]json.RawMessage
			if err := reader.Next(&raw); err != nil {
				return
			}
			var typ string
			_ = json.Unmarshal(raw["type"], &typ)
			switch typ {
			case api.ExecHeaderType:
				_ = json.Unmarshal(raw["session_id"], &rootID)
				_ = api.WriteNDJSON(inW, api.ExecInputFrame{Type: api.ExecInputSend, Content: "/deploy"})
			case string(api.EventSessionCreated):
				var payload api.SessionCreatedEvent
				if json.Unmarshal(raw["payload"], &payload) == nil && payload.Session.ParentID == rootID {
					createdSubagent = &payload.Session
				}
			case string(api.EventPermissionRequested):
				var payload api.PermissionRequestedEvent
				if json.Unmarshal(raw["payload"], &payload) == nil && len(payload.Permissions) > 0 {
					if payload.SessionID != "" && payload.SessionID != rootID {
						sawSubPerm = true
					}
					req := payload.Permissions[0]
					// Omit session_id on the reply to verify automatic routing by request_id.
					_ = api.WriteNDJSON(inW, api.ExecInputFrame{
						Type: api.ExecInputPermission,
						Permission: &api.PermissionDecision{
							RequestID:  req.ID,
							ToolCallID: req.ToolCallID,
							Kind:       api.DecisionAllowOnce,
						},
					})
				}
			case string(api.EventCompleted):
				var payload api.CompletedEvent
				if json.Unmarshal(raw["payload"], &payload) == nil && payload.SessionID == rootID {
					_ = api.WriteNDJSON(inW, api.ExecInputFrame{Type: api.ExecInputClose})
				}
			case api.ExecResultType:
				return
			}
		}
	}()

	cmd := NewRootCmd()
	var errBuf syncBuffer
	cmd.SetIn(inR)
	cmd.SetOut(outW)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"exec", "--socket", sockPath, "-w", wsDir,
		"--model", "fake/fake-default-model",
		"--mode", "ask",
		"--input-format", "jsonl", "--output-format", "jsonl",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runErr := cmd.ExecuteContext(ctx)
	_ = outW.Close()
	<-driverDone

	if code := exec.ExitCode(runErr); code != exec.ExitOK {
		t.Fatalf("exit code = %d (err=%v stderr=%q), want ok", code, runErr, errBuf.String())
	}
	if createdSubagent == nil {
		t.Fatal("expected session_created frame for child subagent")
	}
	const wantModel = "fake/fake-default-model"
	if createdSubagent.Config.Model != wantModel || createdSubagent.Model == nil || createdSubagent.Model.ID != wantModel {
		t.Fatalf("child frame config model = %q, resolved model = %+v; want %q", createdSubagent.Config.Model, createdSubagent.Model, wantModel)
	}
	if !sawSubPerm {
		t.Fatal("expected permission_requested frame keyed by child subagent session_id")
	}
	gotFile, err := os.ReadFile(filepath.Join(wsDir, "sub-deployed.txt"))
	if err != nil || string(gotFile) != "approved-from-bidi\n" {
		t.Fatalf("sub-deployed.txt = %q (err=%v), want approved write", string(gotFile), err)
	}
}

func TestExecAutoModePermissionReviewJSONLApproveAndDefer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		choice      string
		probability float64
		wantExit    int
		wantDeleted bool
	}{
		{
			name:        "approve_once_executes_and_emits_receipt",
			choice:      "approve_once",
			probability: 0.96,
			wantExit:    exec.ExitOK,
			wantDeleted: true,
		},
		{
			name:        "defer_to_user_triggers_needs_input",
			choice:      "defer_to_user",
			probability: 0.20,
			wantExit:    exec.ExitNeedsInput,
			wantDeleted: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &fakeProvider{
				chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
					hasToolResult := false
					for _, m := range req.Messages {
						if _, ok := m.(*message.Tool); ok {
							hasToolResult = true
							break
						}
					}
					return func(yield func(message.AssistantChunk, error) bool) {
						if !hasToolResult {
							yield(message.AssistantChunk{
								BaseChunk: message.BaseChunk{ID: uuid.NewString()},
								Model:     req.Model,
								Content: []message.Block{
									&message.ToolCall{
										ID:   "call-rm-1",
										Name: "remove",
										Args: map[string]any{"path": "obsolete.txt", "force": true},
									},
								},
								Done: true,
							}, nil)
							return
						}
						yield(message.AssistantChunk{
							BaseChunk: message.BaseChunk{ID: uuid.NewString()},
							Model:     req.Model,
							Content:   []message.Block{&message.TextBlock{Text: "removed"}},
							Done:      true,
						}, nil)
					}, nil
				},
				decisionHandler: func(_ context.Context, req *decision.Request) (*decision.Response, error) {
					return &decision.Response{
						Model: req.Model,
						Answers: map[string]decision.Answer{
							"verdict": {
								Name:   "verdict",
								Type:   decision.QuestionChoice,
								Choice: tc.choice,
								ChoiceProbabilities: []decision.OptionProbability{
									{Value: "approve_once", Probability: tc.probability},
									{Value: "defer_to_user", Probability: 1 - tc.probability},
								},
								Confidence: 0.85,
							},
						},
					}, nil
				},
			}
			_, sockPath, wsDir := setupCLITestDaemon(t, prov)
			targetFile := filepath.Join(wsDir, "obsolete.txt")
			if err := os.WriteFile(targetFile, []byte("delete me"), 0o644); err != nil {
				t.Fatalf("write target file: %v", err)
			}
			provDir := filepath.Join(wsDir, ".agents", "providers")
			if err := os.MkdirAll(provDir, 0o755); err != nil {
				t.Fatalf("mkdir providers: %v", err)
			}
			provManifest := "apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: fake\nspec:\n  type: ollama\n  models:\n    - id: fake-default-model\n      capabilities:\n        completion: true\n    - id: fake-decision-model\n      capabilities:\n        decision: true\n        structuredOutput: true\n---\n"
			if err := os.WriteFile(filepath.Join(provDir, "fake.provider.yaml"), []byte(provManifest), 0o644); err != nil {
				t.Fatalf("write fake.provider.yaml: %v", err)
			}
			manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: exec-auto-test\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n    decision: fake-decision-model\n---\n"
			if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
				t.Fatalf("write WORKSPACE.md: %v", err)
			}

			cmd := NewRootCmd()
			var outBuf, errBuf syncBuffer
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(&outBuf)
			cmd.SetErr(&errBuf)
			cmd.SetArgs([]string{
				"exec", "--socket", sockPath, "-w", wsDir,
				"--model", "fake/fake-default-model",
				"--mode", "auto",
				"--output-format", "jsonl",
				"delete obsolete.txt",
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := cmd.ExecuteContext(ctx)
			if code := exec.ExitCode(err); code != tc.wantExit {
				t.Fatalf("exit code = %d (err=%v stderr=%q), want %d\nout=%s", code, err, errBuf.String(), tc.wantExit, outBuf.String())
			}

			_, statErr := os.Stat(targetFile)
			if tc.wantDeleted && !os.IsNotExist(statErr) {
				t.Fatalf("expected obsolete.txt deleted after approve_once, stat err = %v", statErr)
			}
			if !tc.wantDeleted && statErr != nil {
				t.Fatalf("expected obsolete.txt preserved on deferral, stat err = %v", statErr)
			}

			frames := decodeFrames[map[string]json.RawMessage](t, outBuf.String())
			var sawPermReviewRecord, sawToolReviewReceipt bool
			for _, f := range frames {
				var typ string
				_ = json.Unmarshal(f["type"], &typ)
				if typ != string(api.EventEntryAdded) {
					continue
				}
				var payload api.EntryAddedEvent
				if json.Unmarshal(f["payload"], &payload) != nil {
					continue
				}
				if payload.Entry.Kind == api.TimelineKindPermission && payload.Entry.Permission != nil && payload.Entry.Permission.Review != nil {
					sawPermReviewRecord = true
				}
				if payload.Entry.Kind == api.TimelineKindMessage && payload.Entry.Message != nil && payload.Entry.Message.Role == api.RoleTool && payload.Entry.Message.PermissionReview != nil {
					sawToolReviewReceipt = true
				}
			}
			if tc.wantDeleted && (!sawPermReviewRecord || !sawToolReviewReceipt) {
				t.Fatalf("missing wire permission review receipts (perm=%v tool=%v) in:\n%s", sawPermReviewRecord, sawToolReviewReceipt, outBuf.String())
			}
		})
	}
}
