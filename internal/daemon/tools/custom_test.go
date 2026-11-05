package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// probeScript reports how the process was invoked: its argv (excluding the
// interpreter) and the number of bytes available on standard input. It lets a
// single assertion distinguish "arguments were rendered onto the command line"
// from "arguments were piped as JSON on stdin".
const probeScript = `#!/bin/sh
echo "argc=$#"
for a in "$@"; do echo "arg=$a"; done
bytes=$(cat | wc -c | tr -d ' ')
echo "stdin_bytes=$bytes"
`

// customToolFixture writes the given manifests into a fresh workspace and
// returns a container wired with a sandbox, the workspace, and an invoking agent.
func customToolFixture(t *testing.T, manifests map[string]string, agent *warp.Agent) (string, tools.Container) {
	t.Helper()

	xdg.SetTestEnv(t.TempDir(), "tasksmith-custom-tool-test")
	t.Cleanup(xdg.ClearCache)

	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	toolsDir := filepath.Join(dir, ".agents", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range manifests {
		if err := os.WriteFile(filepath.Join(toolsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.sh"), []byte(probeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	var allowed []string
	for name := range manifests {
		allowed = append(allowed, strings.TrimSuffix(strings.TrimSuffix(name, ".tool.md"), ".tool.yaml"))
	}

	c, err := tools.NewContainer(tools.Config{
		Sandbox:      sb,
		Workspace:    ws,
		Agent:        agent,
		Logger:       log.Default(),
		AllowedTools: allowed,
	})
	if err != nil {
		t.Fatalf("tools.NewContainer: %v", err)
	}
	return dir, c
}

// probeResult parses the probe script output into argv and stdin byte count.
func probeResult(t *testing.T, out string) (args []string, stdinBytes int) {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		switch {
		case strings.HasPrefix(line, "argc="):
			// argc is asserted indirectly through the collected arg= lines.
		case strings.HasPrefix(line, "arg="):
			args = append(args, strings.TrimPrefix(line, "arg="))
		case strings.HasPrefix(line, "stdin_bytes="):
			n, err := strconv.Atoi(strings.TrimPrefix(line, "stdin_bytes="))
			if err != nil {
				t.Fatalf("probe stdin_bytes %q: %v", line, err)
			}
			stdinBytes = n
		default:
			t.Fatalf("unexpected probe output line %q in:\n%s", line, out)
		}
	}
	return args, stdinBytes
}

func mustExec(t *testing.T, c tools.Container, name string, args map[string]any) string {
	t.Helper()
	res, err := c.Execute(context.Background(), &message.ToolCall{ID: "call-" + name, Name: name, Args: args})
	if err != nil {
		t.Fatalf("Execute(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("Execute(%s) returned error: %s", name, res.Content.Text())
	}
	return res.Content.Text()
}

// TestCustomToolCommandTemplatingAndStdinFallback verifies the two mutually
// exclusive argument delivery modes: templated spec.command elements receive the
// call arguments on the command line (with no stdin payload), while non-templated
// commands keep receiving the complete JSON arguments on standard input.
func TestCustomToolCommandTemplatingAndStdinFallback(t *testing.T) {
	manifests := map[string]string{
		"probe_input.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: probe_input
  description: Reports argv and stdin size.
spec:
  command: ["sh", "probe.sh", "{{.Input.message}}"]
  annotations:
    isReadOnly: true
    isIdempotent: true
---
`,
		"probe_shorthand.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: probe_shorthand
  description: Reports argv and stdin size using the shorthand form.
spec:
  command: ["sh", "probe.sh", "{{.message}}"]
  annotations:
    isReadOnly: true
---
`,
		"probe_stdin.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: probe_stdin
  description: Reports argv and stdin size.
spec:
  command: ["sh", "probe.sh"]
  annotations:
    isReadOnly: true
---
`,
	}
	_, c := customToolFixture(t, manifests, nil)

	args := map[string]any{"message": "hello custom"}

	// 1. {{.Input.<field>}} renders onto the command line and suppresses stdin.
	inArgs, inStdin := probeResult(t, mustExec(t, c, "probe_input", args))
	if len(inArgs) != 1 || inArgs[0] != "hello custom" {
		t.Errorf("probe_input argv = %v, want [hello custom]", inArgs)
	}
	if inStdin != 0 {
		t.Errorf("probe_input stdin_bytes = %d, want 0 (arguments went to argv)", inStdin)
	}

	// 2. The top-level {{.<field>}} shorthand consumes arguments identically.
	shortArgs, shortStdin := probeResult(t, mustExec(t, c, "probe_shorthand", args))
	if len(shortArgs) != 1 || shortArgs[0] != "hello custom" {
		t.Errorf("probe_shorthand argv = %v, want [hello custom]", shortArgs)
	}
	if shortStdin != 0 {
		t.Errorf("probe_shorthand stdin_bytes = %d, want 0", shortStdin)
	}

	// 3. A non-templated command receives the complete JSON payload on stdin.
	stdinArgs, stdinBytes := probeResult(t, mustExec(t, c, "probe_stdin", args))
	if len(stdinArgs) != 0 {
		t.Errorf("probe_stdin argv = %v, want no arguments", stdinArgs)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if stdinBytes != len(raw) {
		t.Errorf("probe_stdin stdin_bytes = %d, want %d (JSON payload)", stdinBytes, len(raw))
	}
}

// TestCustomToolShellVariablesAreNotTemplates verifies that a manifest command
// using only shell syntax is never template-rendered, so classic CLI scripts
// still see their own shell variables and keep the JSON stdin contract.
func TestCustomToolShellVariablesAreNotTemplates(t *testing.T) {
	manifests := map[string]string{
		"shell_var.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: shell_var
  description: Echoes an unset positional parameter.
spec:
  command: ["sh", "-c", "printf '[%s]' \"$1\""]
  env:
    TOOL_ENV_MODE: "on"
  annotations:
    isReadOnly: true
---
`,
		"env_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: env_tool
  description: Echoes an injected environment variable.
spec:
  command: ["sh", "-c", "printf '[%s]' \"$TOOL_ENV_MODE\""]
  env:
    TOOL_ENV_MODE: "on"
  annotations:
    isReadOnly: true
---
`,
	}
	_, c := customToolFixture(t, manifests, nil)

	// An unset $1 renders as empty shells output, not as a template placeholder.
	if out := mustExec(t, c, "shell_var", map[string]any{"message": "hi"}); out != "[]" {
		t.Errorf("shell_var output = %q, want \"[]\" (shell variable must not be template-rendered)", out)
	}

	// spec.env values are injected into the process environment.
	if out := mustExec(t, c, "env_tool", nil); out != "[on]" {
		t.Errorf("env_tool output = %q, want \"[on]\"", out)
	}
}

// TestCustomToolPrepareUsesAnnotationsAndRenderedTarget verifies that the plan's
// action, safety, and read-only status come strictly from the manifest
// annotations, that Target/Summary expose the rendered command line, and that no
// file path is guessed from argument key names.
func TestCustomToolPrepareUsesAnnotationsAndRenderedTarget(t *testing.T) {
	manifests := map[string]string{
		"probe_input.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: probe_input
  description: Reports argv and stdin size.
spec:
  command: ["sh", "probe.sh", "{{.Input.message}}"]
  annotations:
    isReadOnly: true
    isIdempotent: true
    userHint: "Probe %s"
---
`,
		"dangerous_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: dangerous_tool
  description: Dangerous tool.
spec:
  command: ["sh", "probe.sh"]
  annotations:
    isDangerous: true
    isOpenWorld: true
    userHint: "Run dangerous tool"
---
`,
		"net_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: net_tool
  description: Read-only network tool.
spec:
  command: ["sh", "probe.sh"]
  annotations:
    isReadOnly: true
    isOpenWorld: true
---
`,
	}
	_, c := customToolFixture(t, manifests, nil)

	// A path-looking argument key must not leak into Target: the target is the
	// rendered command line, and the posture is the declared annotation.
	plan, err := c.Prepare(context.Background(), &message.ToolCall{
		ID:   "p1",
		Name: "probe_input",
		Args: map[string]any{"message": "hello", "path": "/etc/passwd"},
	})
	if err != nil {
		t.Fatalf("Prepare(probe_input): %v", err)
	}
	if plan.Action != api.ActionRead || plan.Safety != api.SafetySafe || !plan.ReadOnly {
		t.Errorf("probe_input plan posture = %+v, want read/safe/readonly", plan)
	}
	if plan.Target != "sh probe.sh hello" {
		t.Errorf("probe_input Target = %q, want \"sh probe.sh hello\"", plan.Target)
	}
	if strings.Contains(plan.Target, "/etc/passwd") {
		t.Errorf("probe_input Target leaked an argument path: %q", plan.Target)
	}
	if plan.Summary != "Probe sh probe.sh hello" {
		t.Errorf("probe_input Summary = %q, want userHint rendered with the command", plan.Summary)
	}

	dangerousPlan, err := c.Prepare(context.Background(), &message.ToolCall{ID: "p2", Name: "dangerous_tool"})
	if err != nil {
		t.Fatalf("Prepare(dangerous_tool): %v", err)
	}
	if dangerousPlan.Action != api.ActionExec || dangerousPlan.Safety != api.SafetyUnsafe || dangerousPlan.ReadOnly {
		t.Errorf("dangerous_tool plan = %+v, want exec/unsafe/not-readonly", dangerousPlan)
	}
	if dangerousPlan.Summary != "Run dangerous tool" {
		t.Errorf("dangerous_tool Summary = %q, want the literal userHint", dangerousPlan.Summary)
	}

	netPlan, err := c.Prepare(context.Background(), &message.ToolCall{ID: "p3", Name: "net_tool"})
	if err != nil {
		t.Fatalf("Prepare(net_tool): %v", err)
	}
	if netPlan.Action != api.ActionRead || netPlan.Safety == api.SafetySafe || !netPlan.ReadOnly {
		t.Errorf("net_tool plan = %+v, want read/unsafe-or-unknown/readonly", netPlan)
	}
}

// TestCustomToolInstructionTemplating verifies the tool description is rendered
// with the workspace and invoking agent context so manifests can describe
// per-workspace behavior.
func TestCustomToolInstructionTemplating(t *testing.T) {
	manifests := map[string]string{
		"context_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: context_tool
  description: Short description.
spec:
  command: ["sh", "probe.sh"]
  annotations:
    isReadOnly: true
---
Running for agent {{.Agent.Name}} in workspace {{.WorkspaceDir}}.
`,
		"opaque_body.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: opaque_body
  description: Short description.
spec:
  command: ["sh", "probe.sh"]
  annotations:
    isReadOnly: true
---
Plain instructions with no templates at all.
`,
		"unknown_var.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: unknown_var
  description: Short description.
spec:
  command: ["sh", "probe.sh"]
  annotations:
    isReadOnly: true
---
Needs {{.UndefinedThing}} to work.
`,
	}
	dir, c := customToolFixture(t, manifests, &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "linter-agent"},
		},
	})

	def, ok := c.Get("context_tool")
	if !ok {
		t.Fatal("context_tool not registered")
	}
	desc := def.Definition().Description
	if !strings.Contains(desc, "linter-agent") {
		t.Errorf("description = %q, want it to render {{.Agent.Name}}", desc)
	}
	if !strings.Contains(desc, filepath.Base(dir)) {
		t.Errorf("description = %q, want it to render {{.WorkspaceDir}} (%s)", desc, dir)
	}
	if strings.Contains(desc, "{{") {
		t.Errorf("description = %q, want all templates resolved", desc)
	}

	// A plain body is used verbatim as the description.
	opaque, ok := c.Get("opaque_body")
	if !ok {
		t.Fatal("opaque_body not registered")
	}
	if got := opaque.Definition().Description; got != "Plain instructions with no templates at all." {
		t.Errorf("opaque_body description = %q, want the raw body", got)
	}

	// An instruction referencing an unavailable variable keeps the raw body
	// rather than shipping a placeholder to the model.
	unknown, ok := c.Get("unknown_var")
	if !ok {
		t.Fatal("unknown_var not registered")
	}
	if got := unknown.Definition().Description; got != "Needs {{.UndefinedThing}} to work." {
		t.Errorf("unknown_var description = %q, want the raw body instead of an unresolved render", got)
	}
}

// TestCustomToolValidationAndExitCodeErrors verifies argument validation against
// the manifest inputSchema and that a non-zero exit surfaces stderr to the model.
func TestCustomToolValidationAndExitCodeErrors(t *testing.T) {
	manifests := map[string]string{
		"strict_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: strict_tool
  description: Requires a message.
spec:
  command: ["sh", "probe.sh", "{{.Input.message}}"]
  inputSchema:
    type: object
    properties:
      message:
        type: string
    required: ["message"]
  annotations:
    isReadOnly: true
---
`,
		"exit_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: exit_tool
  description: Fails with stderr output.
spec:
  command: ["sh", "-c", "echo 'kaboom' >&2; exit 4"]
  annotations:
    isDangerous: true
---
`,
	}
	_, c := customToolFixture(t, manifests, nil)

	// Missing a required inputSchema property fails validation in both phases.
	if _, err := c.Prepare(context.Background(), &message.ToolCall{ID: "v1", Name: "strict_tool", Args: map[string]any{}}); err == nil {
		t.Error("Prepare(strict_tool) with missing required argument = nil error, want validation error")
	}
	res, err := c.Execute(context.Background(), &message.ToolCall{ID: "v2", Name: "strict_tool", Args: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute(strict_tool): %v", err)
	}
	if !res.IsError {
		t.Errorf("Execute(strict_tool) with missing required argument: IsError = false, want true")
	}

	// A valid call renders and succeeds.
	if out := mustExec(t, c, "strict_tool", map[string]any{"message": "ok"}); !strings.Contains(out, "arg=ok") {
		t.Errorf("strict_tool output = %q, want the rendered argument", out)
	}

	// Non-zero exit surfaces stderr text.
	failRes, err := c.Execute(context.Background(), &message.ToolCall{ID: "v3", Name: "exit_tool"})
	if err != nil {
		t.Fatalf("Execute(exit_tool): %v", err)
	}
	if !failRes.IsError || !strings.Contains(failRes.Content.Text(), "kaboom") {
		t.Errorf("exit_tool result = %+v, want IsError with stderr 'kaboom'", failRes)
	}
}

// TestCustomToolMissingTemplateArgumentIsRejected verifies that a command
// template referencing an argument the model did not provide surfaces a
// validation error rather than executing with an unresolved placeholder argv.
func TestCustomToolMissingTemplateArgumentIsRejected(t *testing.T) {
	manifests := map[string]string{
		"optional_arg.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: optional_arg
  description: Templates an optional argument.
spec:
  command: ["sh", "probe.sh", "{{.Input.extra}}"]
  inputSchema:
    type: object
    properties:
      extra:
        type: string
  annotations:
    isReadOnly: true
---
`,
	}
	_, c := customToolFixture(t, manifests, nil)

	// The argument is optional per inputSchema, so validation passes but the
	// rendered command would contain an unresolved placeholder.
	res, err := c.Execute(context.Background(), &message.ToolCall{ID: "m1", Name: "optional_arg", Args: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute(optional_arg): %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute(optional_arg) = %q, want an error for the unresolved template argument", res.Content.Text())
	}
	if strings.Contains(res.Content.Text(), "<no value>") {
		t.Errorf("Execute(optional_arg) leaked the placeholder to the model: %q", res.Content.Text())
	}

	// Supplying the argument makes the same tool succeed.
	if out := mustExec(t, c, "optional_arg", map[string]any{"extra": "provided"}); !strings.Contains(out, "arg=provided") {
		t.Errorf("optional_arg output = %q, want the provided argument on the command line", out)
	}
}

// TestCustomToolRenderFailureIsReported verifies a broken command template is
// surfaced as an error rather than executing a partially rendered argv.
func TestCustomToolRenderFailureIsReported(t *testing.T) {
	manifests := map[string]string{
		"broken_tool.tool.md": `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: broken_tool
  description: Has an unclosed template action.
spec:
  command: ["sh", "probe.sh", "{{.Input.message"]
  annotations:
    isReadOnly: true
---
`,
	}
	_, c := customToolFixture(t, manifests, nil)

	_, err := c.Prepare(context.Background(), &message.ToolCall{ID: "b1", Name: "broken_tool", Args: map[string]any{"message": "x"}})
	if err == nil {
		t.Fatal("Prepare(broken_tool) = nil error, want a command render error")
	}
	if !strings.Contains(err.Error(), "broken_tool") {
		t.Errorf("Prepare(broken_tool) error = %v, want it to name the tool", err)
	}

	if _, ok := errors.AsType[*tool.ValidationError](err); ok {
		t.Errorf("Prepare(broken_tool) error = %v, want a render error rather than validation", err)
	}
}
