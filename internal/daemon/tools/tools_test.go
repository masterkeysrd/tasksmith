package tools_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
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

func TestGatePermissionModesGrantsAndPolicies(t *testing.T) {
	_, sb, ws := setupToolsEnv(t)

	writeTool, err := tools.New(tools.Spec[sampleIn, string]{
		Name:       "write_file",
		Annotation: tool.Annotation{IsReadOnly: false},
		Prepare: func(_ context.Context, in sampleIn) (*tools.Plan, error) {
			return &tools.Plan{
				Action:   tools.ActionWrite,
				Safety:   tools.SafetySafe,
				Target:   in.Path,
				Summary:  "write " + in.Path,
				Diff:     "+hello",
				ReadOnly: false,
			}, nil
		},
		Execute: func(_ context.Context, in sampleIn) (string, error) {
			return "wrote " + in.Path, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	c, err := tools.NewContainer(tools.Config{
		Tools:     []tools.Tool{writeTool},
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	gate := c.Gate()
	if gate.Mode() != tools.ModeDefault {
		t.Fatalf("expected initial ModeDefault, got %s", gate.Mode())
	}

	safeReadPlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionRead,
		Safety:   tools.SafetySafe,
		Target:   "README.md",
		ReadOnly: true,
	}
	outsideReadPlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionRead,
		Safety:   tools.SafetyOutside,
		Target:   "/outside/somefile.txt",
		ReadOnly: true,
	}
	unsafeReadPlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionRead,
		Safety:   tools.SafetyUnsafe,
		Target:   "/etc/hosts",
		ReadOnly: true,
	}
	unknownReadPlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionNet,
		Safety:   tools.SafetyUnknown,
		Target:   "https://example.com/api",
		ReadOnly: true,
	}
	writePlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionWrite,
		Safety:   tools.SafetySafe,
		Target:   "internal/pkg/a.go",
		Diff:     "+package pkg",
		ReadOnly: false,
	}
	unsafePlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionDelete,
		Safety:   tools.SafetyUnsafe,
		Target:   ".git/config",
		ReadOnly: false,
	}

	// 1. ModeDefault: safe ReadOnly -> AccessAllow; SafetyOutside ReadOnly -> AccessAsk (can be granted);
	// SafetyUnsafe ReadOnly -> AccessAsk (never bypassed by session grant); write -> AccessAsk.
	if dec, req, err := c.Evaluate(safeReadPlan); err != nil || dec != tools.AccessAllow || req != nil {
		t.Fatalf("ModeDefault safeReadPlan: got %s, %+v, %v", dec, req, err)
	}
	outDec, outReq, err := c.Evaluate(outsideReadPlan)
	if err != nil || outDec != tools.AccessAsk || outReq == nil {
		t.Fatalf("ModeDefault outsideReadPlan: expected AccessAsk, got %s, %+v, %v", outDec, outReq, err)
	}
	if err := gate.ApplyDecision(outReq, tools.PermissionDecision{Kind: tools.DecisionAllowSession}); err != nil {
		t.Fatalf("ApplyDecision for outsideReadPlan failed: %v", err)
	}
	if dec, _, _ := c.Evaluate(outsideReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModeDefault outsideReadPlan with session grant: expected AccessAllow, got %s", dec)
	}
	c.AddGrant(tools.Grant{ToolName: "write_file", Action: tools.ActionRead, Pattern: "*"})
	if dec, req, err := c.Evaluate(unsafeReadPlan); err != nil || dec != tools.AccessAsk || req == nil {
		t.Fatalf("ModeDefault unsafeReadPlan with wildcard grant: expected AccessAsk, got %s, %+v, %v", dec, req, err)
	}
	c.ClearGrants()
	dec, req, err := c.Evaluate(writePlan)
	if err != nil || dec != tools.AccessAsk || req == nil {
		t.Fatalf("ModeDefault writePlan: expected AccessAsk, got %s, %+v, %v", dec, req, err)
	}
	if u, uErr := uuid.Parse(req.ID); uErr != nil || u.Version() != 7 {
		t.Fatalf("expected UUIDv7 on PermissionRequest.ID, got %q", req.ID)
	}
	if !slices.Contains(req.GrantPatterns, "internal/pkg/**") {
		t.Fatalf("expected derived directory grant pattern 'internal/pkg/**' in req.GrantPatterns, got %v", req.GrantPatterns)
	}

	// Verify deriveGrantPatterns for ActionExec, ActionNet, and empty Target
	if _, execReq, _ := c.Evaluate(&tools.Plan{ToolName: "write_file", Action: tools.ActionExec, Safety: tools.SafetyUnknown, Target: "go test ./..."}); execReq == nil || !slices.Contains(execReq.GrantPatterns, "go *") {
		t.Fatalf("expected 'go *' in ActionExec GrantPatterns, got %+v", execReq)
	}
	if _, netReq, _ := c.Evaluate(unknownReadPlan); netReq == nil || !slices.Contains(netReq.GrantPatterns, "example.com") {
		t.Fatalf("expected 'example.com' in ActionNet GrantPatterns, got %+v", netReq)
	}
	if _, emptyReq, _ := c.Evaluate(&tools.Plan{ToolName: "write_file", Action: tools.ActionExec, Safety: tools.SafetyUnknown}); emptyReq == nil || !slices.Equal(emptyReq.GrantPatterns, []string{"*"}) {
		t.Fatalf("expected ['*'] for empty target GrantPatterns, got %+v", emptyReq)
	}

	// 2. ModeAcceptEdits: safe write -> AccessAllow; unsafe -> AccessAsk.
	c.SetMode(tools.ModeAcceptEdits)
	if c.Mode() != tools.ModeAcceptEdits {
		t.Fatalf("expected ModeAcceptEdits, got %s", c.Mode())
	}
	if dec, _, _ := c.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAcceptEdits writePlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(unsafePlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAcceptEdits unsafePlan: expected AccessAsk, got %s", dec)
	}

	// 3. ModePlan: !ReadOnly -> AccessDeny; ReadOnly+SafetySafe -> AccessAllow; ReadOnly+SafetyUnknown -> AccessAsk.
	c.SetMode(tools.ModePlan)
	if dec, _, _ := c.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("ModePlan writePlan: expected AccessDeny, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModePlan safeReadPlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(unknownReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModePlan unknownReadPlan: expected AccessAsk, got %s", dec)
	}

	// 4. ModeAsk: even safe ReadOnly requires AccessAsk unless matched by an explicit session Grant!
	c.SetMode(tools.ModeAsk)
	if dec, _, _ := c.Evaluate(safeReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAsk safeReadPlan without grant: expected AccessAsk, got %s", dec)
	}
	c.AddGrant(tools.Grant{ToolName: "write_file", Action: tools.ActionRead, Pattern: "README.md"})
	if dec, _, _ := c.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAsk safeReadPlan with grant: expected AccessAllow, got %s", dec)
	}
	c.ClearGrants()

	// 5. ModeAuto: all policy-permitted operations -> AccessAllow.
	c.SetMode(tools.ModeAuto)
	if dec, _, _ := c.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto writePlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(unsafePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto unsafePlan: expected AccessAllow, got %s", dec)
	}

	// Reset to ModeDefault and test ApplyDecision + Grants matching.
	c.SetMode("")
	if c.Mode() != tools.ModeDefault {
		t.Fatalf("expected ModeDefault when set to empty, got %s", c.Mode())
	}

	if err := gate.ApplyDecision(req, tools.PermissionDecision{Kind: tools.DecisionAllowOnce}); err != nil {
		t.Fatalf("DecisionAllowOnce failed: %v", err)
	}
	if err := gate.ApplyDecision(req, tools.PermissionDecision{
		Kind:         tools.DecisionAllowSession,
		GrantPattern: "internal/**",
	}); err != nil {
		t.Fatalf("DecisionAllowSession failed: %v", err)
	}
	// Duplicate grant ignored
	c.AddGrant(tools.Grant{ToolName: "write_file", Action: tools.ActionWrite, Pattern: "internal/**"})
	if len(c.Grants()) != 1 {
		t.Fatalf("expected 1 deduplicated grant, got %d", len(c.Grants()))
	}

	// Directory prefix grant "internal/**" matches "internal/pkg/other.go"
	if dec, _, _ := c.Evaluate(&tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionWrite,
		Safety:   tools.SafetySafe,
		Target:   "internal/pkg/other.go",
	}); dec != tools.AccessAllow {
		t.Fatalf("expected internal/** grant to allow internal/pkg/other.go, got %s", dec)
	}

	// URL host grant, glob regex grant (** / ?), and command prefix grant
	c.AddGrant(tools.Grant{ToolName: "write_file", Action: tools.ActionNet, Pattern: "*.example.com"})
	if dec, _, _ := c.Evaluate(&tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionNet,
		Safety:   tools.SafetyUnknown,
		Target:   "https://api.example.com/v1",
	}); dec != tools.AccessAllow {
		t.Fatalf("expected host wildcard grant to allow https://api.example.com/v1, got %s", dec)
	}

	c.AddGrant(tools.Grant{ToolName: "write_file", Action: tools.ActionWrite, Pattern: "cmd/**/main_?.go"})
	if dec, _, _ := c.Evaluate(&tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionWrite,
		Safety:   tools.SafetySafe,
		Target:   "cmd/cli/main_1.go",
	}); dec != tools.AccessAllow {
		t.Fatalf("expected glob regex grant to allow cmd/cli/main_1.go, got %s", dec)
	}

	// ApplyDecision fallback pattern resolution (from GrantPatterns[0], Target, and "*")
	_ = gate.ApplyDecision(&tools.PermissionRequest{
		ToolName:      "write_file",
		Action:        tools.ActionWrite,
		GrantPatterns: []string{"pkg/**"},
	}, tools.PermissionDecision{Kind: tools.DecisionAllowSession})
	_ = gate.ApplyDecision(&tools.PermissionRequest{
		ToolName: "write_file",
		Action:   tools.ActionWrite,
		Target:   "single.txt",
	}, tools.PermissionDecision{Kind: tools.DecisionAllowSession})
	_ = gate.ApplyDecision(&tools.PermissionRequest{
		ToolName: "write_file",
		Action:   tools.ActionWrite,
	}, tools.PermissionDecision{Kind: tools.DecisionAllowSession})

	c.AddGrant(tools.Grant{ToolName: "write_file", Action: tools.ActionExec, Pattern: "go test*"})
	if dec, _, _ := c.Evaluate(&tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionExec,
		Safety:   tools.SafetyUnknown,
		Target:   "go test ./...",
	}); dec != tools.AccessAllow {
		t.Fatalf("expected prefix grant 'go test*' to allow 'go test ./...', got %s", dec)
	}

	// Reject decisions
	if err := gate.ApplyDecision(req, tools.PermissionDecision{
		Kind:     tools.DecisionReject,
		Feedback: "user denied",
	}); !errors.Is(err, tools.ErrPermissionDenied) || !strings.Contains(err.Error(), "user denied") {
		t.Fatalf("expected ErrPermissionDenied with feedback, got %v", err)
	}
	if err := gate.ApplyDecision(req, tools.PermissionDecision{Kind: tools.DecisionReject}); !errors.Is(err, tools.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied without feedback, got %v", err)
	}
	if err := gate.ApplyDecision(req, tools.PermissionDecision{Kind: "bad"}); !errors.Is(err, tools.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for unknown decision kind, got %v", err)
	}

	// Policies: Exclude, Include, AllowDangerous, AllowOpenWorld
	f := false
	gate.SetPolicies(&workspace.Policies{
		Tools: &workspace.ToolPolicies{
			Exclude:        []string{"excluded_*"},
			Include:        []string{"write_file"},
			AllowDangerous: &f,
			AllowOpenWorld: &f,
		},
	})
	if gate.Policies() == nil {
		t.Fatalf("expected non-nil gate policies")
	}
	if dec, _, _ := c.Evaluate(&tools.Plan{ToolName: "excluded_tool", Action: tools.ActionRead, Safety: tools.SafetySafe, ReadOnly: true}); dec != tools.AccessDeny {
		t.Fatalf("expected excluded tool to be denied, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(&tools.Plan{ToolName: "unlisted_tool", Action: tools.ActionRead, Safety: tools.SafetySafe, ReadOnly: true}); dec != tools.AccessDeny {
		t.Fatalf("expected non-included tool to be denied, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(unsafePlan); dec != tools.AccessDeny {
		t.Fatalf("expected unsafe plan to be denied when AllowDangerous=false, got %s", dec)
	}
	if dec, _, _ := c.Evaluate(unknownReadPlan); dec != tools.AccessDeny {
		t.Fatalf("expected ActionNet plan to be denied when AllowOpenWorld=false, got %s", dec)
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

	// Concurrent executions targeting the same normalized path must be serialized by Gate.lockPath!
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
