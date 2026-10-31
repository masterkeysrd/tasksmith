package tools_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

type sampleIn struct {
	Path  string `json:"path" jsonschema:"Target path."`
	Value int    `json:"value,omitempty" jsonschema:"Optional value."`
}

func (s sampleIn) Validate() error {
	if s.Value < 0 {
		return errors.New("value must be non-negative")
	}
	return nil
}

type ptrValidatorIn struct {
	Name string `json:"name"`
}

func (p *ptrValidatorIn) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name cannot be blank")
	}
	return nil
}

type textProviderOut struct {
	Msg string `json:"msg"`
}

func (o textProviderOut) TextContent() string {
	return "text:" + o.Msg
}

type contentProviderOut struct {
	Msg string `json:"msg"`
}

func (o contentProviderOut) ToolContent() message.Content {
	return message.Content{&message.TextBlock{Text: "block:" + o.Msg}}
}

func setupToolsEnv(t *testing.T) (string, sandbox.Sandbox, *workspace.Workspace) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	ws, err := workspace.Open(ctx, dir)
	if err != nil {
		t.Fatalf("workspace.Open failed: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	return dir, sb, ws
}

func TestConfigValidateAndNewContainer(t *testing.T) {
	_, sb, ws := setupToolsEnv(t)
	logger := log.Default()

	readTool, err := tools.New(tools.Spec[sampleIn, string]{
		Name:        "read_sample",
		Title:       "Read Sample",
		Description: "Reads a sample value",
		Annotation:  tool.Annotation{IsReadOnly: true},
		Execute: func(_ context.Context, in sampleIn) (string, error) {
			return fmt.Sprintf("read:%s:%d", in.Path, in.Value), nil
		},
	})
	if err != nil {
		t.Fatalf("tools.New failed: %v", err)
	}

	valid := tools.Config{
		Tools:     []tools.Tool{readTool},
		Sandbox:   sb,
		Workspace: ws,
		Logger:    logger,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*tools.Config)
	}{
		{"nil Sandbox", func(c *tools.Config) { c.Sandbox = nil }},
		{"nil Workspace", func(c *tools.Config) { c.Workspace = nil }},
		{"nil Logger", func(c *tools.Config) { c.Logger = nil }},
		{"nil Tool element", func(c *tools.Config) { c.Tools = []tools.Tool{nil} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			if err := cfg.Validate(); !errors.Is(err, tools.ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig for %s, got %v", tc.name, err)
			}
			if _, err := tools.NewContainer(cfg); !errors.Is(err, tools.ErrInvalidConfig) {
				t.Fatalf("expected NewContainer to return ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestSpecValidationAndDefaultPrepare(t *testing.T) {
	ctx := context.Background()

	// Empty name or nil Execute must fail with ErrInvalidConfig.
	if _, err := tools.New(tools.Spec[sampleIn, string]{
		Name:    "   ",
		Execute: func(context.Context, sampleIn) (string, error) { return "", nil },
	}); !errors.Is(err, tools.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for empty name, got %v", err)
	}
	if _, err := tools.New(tools.Spec[sampleIn, string]{
		Name: "missing_exec",
	}); !errors.Is(err, tools.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil Execute, got %v", err)
	}

	// Default Prepare behavior across annotation combinations + ContentProvider / TextContentProvider.
	roTool, err := tools.New(tools.Spec[sampleIn, textProviderOut]{
		Name:       "ro_tool",
		Annotation: tool.Annotation{IsReadOnly: true},
		Execute: func(_ context.Context, in sampleIn) (textProviderOut, error) {
			return textProviderOut{Msg: in.Path}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	roPlan, err := roTool.Prepare(ctx, map[string]any{"path": "a.txt", "value": 1})
	if err != nil {
		t.Fatalf("roTool.Prepare failed: %v", err)
	}
	if roPlan.Action != tools.ActionRead || roPlan.Safety != tools.SafetySafe || !roPlan.ReadOnly {
		t.Fatalf("unexpected default read-only plan: %+v", roPlan)
	}
	roMsg, err := roTool.Execute(ctx, &message.ToolCall{
		ID:   "call-ro",
		Name: "ro_tool",
		Args: map[string]any{"path": "a.txt", "value": 1},
	})
	if err != nil || roMsg.Content.Text() != "text:a.txt" {
		t.Fatalf("expected TextContentProvider output 'text:a.txt', got %q (err: %v)", roMsg.Content.Text(), err)
	}

	// Value validator rejection
	if _, err := roTool.Prepare(ctx, map[string]any{"path": "a.txt", "value": -5}); !errors.Is(err, tool.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput from Validator, got %v", err)
	}

	// Pointer validator + ContentProvider + OpenWorld/Dangerous default Prepare
	netDangTool, err := tools.New(tools.Spec[ptrValidatorIn, contentProviderOut]{
		Name:       "net_dang",
		Annotation: tool.Annotation{IsOpenWorld: true, IsDangerous: true},
		Execute: func(_ context.Context, in ptrValidatorIn) (contentProviderOut, error) {
			return contentProviderOut{Msg: in.Name}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := netDangTool.Prepare(ctx, map[string]any{"name": "   "}); !errors.Is(err, tool.ErrInvalidInput) {
		t.Fatalf("expected pointer Validator error wrapped in ErrInvalidInput, got %v", err)
	}
	ndPlan, err := netDangTool.Prepare(ctx, map[string]any{"name": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if ndPlan.Safety != tools.SafetyUnsafe || ndPlan.ReadOnly {
		t.Fatalf("expected SafetyUnsafe for dangerous tool, got %+v", ndPlan)
	}
	ndMsg, err := netDangTool.Execute(ctx, &message.ToolCall{
		ID:   "call-nd",
		Name: "net_dang",
		Args: map[string]any{"name": "ok"},
	})
	if err != nil || ndMsg.Content.Text() != "block:ok" {
		t.Fatalf("expected ContentProvider output 'block:ok', got %q (err: %v)", ndMsg.Content.Text(), err)
	}

	// OpenWorld non-dangerous non-readonly -> ActionNet + SafetyUnknown
	netTool, err := tools.New(tools.Spec[ptrValidatorIn, map[string]int]{
		Name:       "net_only",
		Annotation: tool.Annotation{IsOpenWorld: true},
		Execute: func(_ context.Context, _ ptrValidatorIn) (map[string]int, error) {
			return map[string]int{"ok": 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	netPlan, err := netTool.Prepare(ctx, map[string]any{"name": "host"})
	if err != nil {
		t.Fatal(err)
	}
	if netPlan.Action != tools.ActionNet || netPlan.Safety != tools.SafetyUnknown {
		t.Fatalf("expected ActionNet + SafetyUnknown, got %+v", netPlan)
	}
	netMsg, err := netTool.Execute(ctx, &message.ToolCall{
		ID:   "call-net",
		Name: "net_only",
		Args: map[string]any{"name": "host"},
	})
	if err != nil || netMsg.Content.Text() != "" || netMsg.StructuredContent == nil {
		t.Fatalf("expected empty Content and non-nil StructuredContent, got text %q, structured %+v (err: %v)", netMsg.Content.Text(), netMsg.StructuredContent, err)
	}
	if m, ok := netMsg.StructuredContent.(map[string]int); !ok || m["ok"] != 1 {
		t.Fatalf("unexpected structured output: %+v", netMsg.StructuredContent)
	}
}

func TestContainerExecuteLockingAndCustomWorkspaceTools(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	toolsDir := filepath.Join(dir, ".agents", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	echoManifest := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: custom_echo
  displayName: Custom Echo
  description: Echoes stdin JSON.
spec:
  command: ["sh", "-c", "cat"]
  env:
    ECHO_MODE: "1"
  inputSchema:
    type: object
    properties:
      msg:
        type: string
    required: ["msg"]
  annotations:
    isReadOnly: true
    isIdempotent: true
    userHint: "Echo payload: %s"
---

Use custom_echo to echo JSON input.
`
	failManifest := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: custom_fail
  description: Exits non-zero.
spec:
  command: ["sh", "-c", "echo 'boom' >&2; exit 3"]
  outputSchema:
    type: object
    properties:
      status:
        type: string
  annotations:
    isDangerous: true
    isOpenWorld: true
    userHint: "Run failing custom tool"
---
`
	if err := os.WriteFile(filepath.Join(toolsDir, "custom_echo.tool.md"), []byte(echoManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "custom_fail.tool.md"), []byte(failManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	var activeMu sync.Mutex
	active := 0
	maxActive := 0

	mutTool, err := tools.New(tools.Spec[sampleIn, string]{
		Name:       "mutate",
		Annotation: tool.Annotation{IsReadOnly: false},
		Execute: func(_ context.Context, in sampleIn) (string, error) {
			activeMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			activeMu.Unlock()

			defer func() {
				activeMu.Lock()
				active--
				activeMu.Unlock()
			}()

			if in.Value == 99 {
				return "", errors.New("simulated execution failure")
			}
			return "ok:" + in.Path, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	c, err := tools.NewContainer(tools.Config{
		Tools:     []tools.Tool{mutTool},
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(c.Definitions()) != 3 || len(c.List()) != 3 {
		t.Fatalf("expected 3 tools (1 Go + 2 custom), got %d", len(c.List()))
	}
	if _, ok := c.Get("custom_echo"); !ok {
		t.Fatalf("expected custom_echo to be registered")
	}

	// Concurrent executions targeting the same normalized path must be serialized by container.lockPath!
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			targetPath := "shared/file.txt"
			if idx%2 == 1 {
				targetPath = filepath.Join(dir, "shared", "file.txt")
			}
			res, execErr := c.Execute(ctx, &message.ToolCall{
				ID:   fmt.Sprintf("call-%d", idx),
				Name: "mutate",
				Args: map[string]any{"path": targetPath, "value": idx},
			})
			if execErr != nil || res.IsError {
				t.Errorf("concurrent Execute(%d) failed: err=%v, text=%s", idx, execErr, res.Content.Text())
			}
		}(i)
	}
	wg.Wait()
	if maxActive > 1 {
		t.Fatalf("expected per-path mutex to serialize concurrent mutating calls (maxActive=%d)", maxActive)
	}

	// Execution failure is wrapped into *message.Tool{IsError: true} with UUIDv7 ID.
	errRes, err := c.Execute(ctx, &message.ToolCall{
		ID:   "call-err",
		Name: "mutate",
		Args: map[string]any{"path": "shared/file.txt", "value": 99},
	})
	if err != nil || !errRes.IsError || !strings.Contains(errRes.Content.Text(), "simulated execution failure") {
		t.Fatalf("expected wrapped execution error, got err=%v, isError=%v, text=%q", err, errRes.IsError, errRes.Content.Text())
	}

	// Custom tool Prepare + Execute
	echoPlan, err := c.Prepare(ctx, &message.ToolCall{
		ID:   "echo-1",
		Name: "custom_echo",
		Args: map[string]any{"msg": "hello custom"},
	})
	if err != nil {
		t.Fatalf("Prepare(custom_echo) failed: %v", err)
	}
	if echoPlan.Action != tools.ActionRead || echoPlan.Safety != tools.SafetySafe || !echoPlan.ReadOnly {
		t.Fatalf("unexpected custom_echo plan: %+v", echoPlan)
	}
	echoRes, err := c.Execute(ctx, &message.ToolCall{
		ID:   "echo-1",
		Name: "custom_echo",
		Args: map[string]any{"msg": "hello custom"},
	})
	if err != nil || echoRes.IsError || !strings.Contains(echoRes.Content.Text(), "hello custom") {
		t.Fatalf("Execute(custom_echo) failed: err=%v, text=%q", err, echoRes.Content.Text())
	}

	// Custom tool non-zero exit code
	failPlan, err := c.Prepare(ctx, &message.ToolCall{
		ID:   "fail-1",
		Name: "custom_fail",
	})
	if err != nil || failPlan.Safety != tools.SafetyUnsafe {
		t.Fatalf("expected SafetyUnsafe for custom_fail, got %+v, err=%v", failPlan, err)
	}
	failRes, err := c.Execute(ctx, &message.ToolCall{
		ID:   "fail-1",
		Name: "custom_fail",
	})
	if err != nil || !failRes.IsError || !strings.Contains(failRes.Content.Text(), "boom") {
		t.Fatalf("expected non-zero custom tool exit to return IsError with stderr 'boom', got err=%v, isError=%v, text=%q", err, failRes.IsError, failRes.Content.Text())
	}

	// Unknown tool and nil tool call handling
	if _, err := c.Prepare(ctx, nil); !errors.Is(err, tools.ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound on nil tool call in Prepare, got %v", err)
	}
	if _, err := c.Prepare(ctx, &message.ToolCall{ID: "u", Name: "missing"}); !errors.Is(err, tools.ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound on unknown tool in Prepare, got %v", err)
	}
	nilExecRes, err := c.Execute(ctx, nil)
	if err != nil || !nilExecRes.IsError {
		t.Fatalf("expected IsError on nil tool call in Execute, got err=%v", err)
	}
	unkExecRes, err := c.Execute(ctx, &message.ToolCall{ID: "u", Name: "missing"})
	if err != nil || !unkExecRes.IsError || !slices.ContainsFunc([]string{unkExecRes.Content.Text()}, func(s string) bool {
		return strings.Contains(s, tools.ErrToolNotFound.Error())
	}) {
		t.Fatalf("expected ErrToolNotFound in unknown tool Execute result, got %q", unkExecRes.Content.Text())
	}
}

func TestCustomToolStreamingSpill(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	toolsDir := filepath.Join(dir, ".agents", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	spillManifest := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: custom_spill
  description: Generates high volume output.
spec:
  command: ["sh", "-c", "for i in $(seq 1 600); do echo \"custom line $i\"; done"]
  annotations:
    isReadOnly: true
---
`
	if err := os.WriteFile(filepath.Join(toolsDir, "custom_spill.tool.md"), []byte(spillManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()

	sp, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir: filepath.Join(dir, ".spill"),
	})
	if err != nil {
		t.Fatal(err)
	}

	c, err := tools.NewContainer(tools.Config{
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
		Spiller:   sp,
	})
	if err != nil {
		t.Fatal(err)
	}

	execCtx := scope.With(ctx, scope.Scope{
		SessionID:  "sess-custom-spill",
		ToolCallID: "call-custom-spill",
	})

	res, err := c.Execute(execCtx, &message.ToolCall{
		ID:   "call-custom-spill",
		Name: "custom_spill",
	})
	if err != nil || res.IsError {
		t.Fatalf("Execute(custom_spill) failed: err=%v, res=%+v", err, res)
	}

	outText := res.Content.Text()
	if !strings.Contains(outText, "truncated — full output saved to") {
		t.Fatalf("expected truncation notice in Content text, got:\n%s", outText)
	}
	structStr, ok := res.StructuredContent.(string)
	if !ok || !strings.Contains(structStr, "truncated — full output saved to") {
		t.Fatalf("expected truncation notice in StructuredContent, got:\n%v", res.StructuredContent)
	}

	// Extract spilled file path from notice
	startIdx := strings.Index(outText, "full output saved to ")
	if startIdx == -1 {
		t.Fatalf("did not find 'full output saved to ' in notice: %s", outText)
	}
	startPath := startIdx + len("full output saved to ")
	endIdx := strings.Index(outText[startPath:], ". Use view or grep")
	if endIdx == -1 {
		t.Fatalf("did not find end of path in notice: %s", outText)
	}
	spillPath := outText[startPath : startPath+endIdx]

	spillData, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", spillPath, err)
	}

	spillStr := string(spillData)
	if !strings.Contains(spillStr, "custom line 1\n") || !strings.Contains(spillStr, "custom line 600\n") {
		t.Fatalf("spilled content missing initial or final lines")
	}
}
