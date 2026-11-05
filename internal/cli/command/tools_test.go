package command

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func TestToolsCommandArgsValidation(t *testing.T) {
	reg := NewRegistry()

	// 1. With arguments: must be rejected
	err := reg.Execute("/tools test")
	if err == nil {
		t.Fatal("expected error when running /tools with args, got nil")
	}
	if !strings.Contains(err.Error(), "/tools does not accept arguments") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. With multiple arguments: must be rejected
	err = reg.Execute("/tools inspect view")
	if err == nil {
		t.Fatal("expected error when running /tools with multiple args, got nil")
	}

	// 3. Without arguments: must succeed and enqueue slot
	store.ClearSlot()
	defer store.ClearSlot()
	err = reg.Execute("/tools")
	if err != nil {
		t.Fatalf("unexpected error running /tools: %v", err)
	}
	st := store.GetSlot()
	if st.Active == nil || st.Active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot to be enqueued, got %+v", st.Active)
	}
	if st.Active.Data == nil {
		t.Fatal("expected the tools browser to be enqueued as slot payload")
	}
	if kind := reflect.TypeOf(st.Active.Data).Kind(); kind != reflect.Func {
		t.Fatalf("expected slot payload to be a browser component, got %T", st.Active.Data)
	}
}

func TestRegisterToolCommandsMetadata(t *testing.T) {
	reg := NewRegistry()

	cmd, ok := reg.Lookup("tools")
	if !ok || cmd.Name != "tools" {
		t.Fatal("expected built-in /tools command to exist")
	}
	if cmd.Usage != "/tools" {
		t.Errorf("/tools usage = %q, want /tools", cmd.Usage)
	}
	if cmd.Description != "List, inspect, and reload workspace tools" {
		t.Errorf("/tools description = %q", cmd.Description)
	}
	if cmd.Complete != nil {
		t.Error("/tools should declare no argument completer")
	}

	// The slash completion source must offer /tools.
	candidates, err := reg.Sources()[0].Complete(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("commands completion: %v", err)
	}
	var found bool
	for _, c := range candidates {
		if c.Value == "tools" {
			found = true
			if c.Label != "/tools" {
				t.Errorf("/tools candidate label = %q, want /tools", c.Label)
			}
			break
		}
	}
	if !found {
		t.Error("expected /tools in slash completion candidates")
	}
}

func TestToolScopeMapping(t *testing.T) {
	tests := []struct {
		tool api.Tool
		want string
	}{
		{tool: api.Tool{Namespace: "workspace"}, want: "Workspace"},
		{tool: api.Tool{Namespace: "local"}, want: "Project"},
		{tool: api.Tool{Namespace: "project"}, want: "Project"},
		{tool: api.Tool{Namespace: "user"}, want: "User"},
		{tool: api.Tool{Namespace: "system"}, want: "Builtin"},
		{tool: api.Tool{Namespace: "builtin"}, want: "Builtin"},
		{tool: api.Tool{Namespace: ""}, want: "Builtin"},
		{tool: api.Tool{Scope: "Workspace", Namespace: "system"}, want: "Workspace"},
		{tool: api.Tool{Namespace: "my-plugin"}, want: "My-plugin"},
	}

	for _, tt := range tests {
		got := ToolScope(tt.tool)
		if got != tt.want {
			t.Errorf("ToolScope(%+v) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func TestToolStatusBadge(t *testing.T) {
	tests := []struct {
		name string
		tool api.Tool
		want string
	}{
		{name: "eager", tool: api.Tool{Status: api.ToolStatusEager}, want: "[Eager]"},
		{name: "deferred", tool: api.Tool{Status: api.ToolStatusDeferred}, want: "[Deferred]"},
		{name: "denied", tool: api.Tool{Status: api.ToolStatusDenied}, want: "[Denied]"},
		{name: "inactive", tool: api.Tool{Status: api.ToolStatusInactive}, want: "[Inactive]"},
		{name: "unclassified", tool: api.Tool{}, want: "[—]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolStatusBadge(tt.tool); got != tt.want {
				t.Errorf("toolStatusBadge(%+v) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestToolAnnotationBadges(t *testing.T) {
	tests := []struct {
		name string
		tool api.Tool
		want string
	}{
		{
			name: "read only",
			tool: api.Tool{Annotations: api.ToolAnnotation{IsReadOnly: true, IsIdempotent: true}},
			want: "[ReadOnly]",
		},
		{
			name: "dangerous and open world",
			tool: api.Tool{Annotations: api.ToolAnnotation{IsDangerous: true, IsOpenWorld: true}},
			want: "[Dangerous] [OpenWorld]",
		},
		{
			name: "all three",
			tool: api.Tool{Annotations: api.ToolAnnotation{IsReadOnly: true, IsDangerous: true, IsOpenWorld: true}},
			want: "[ReadOnly] [Dangerous] [OpenWorld]",
		},
		{
			name: "idempotent only is not shown",
			tool: api.Tool{Annotations: api.ToolAnnotation{IsIdempotent: true, UserHint: "Read %s"}},
			want: "",
		},
		{name: "none", tool: api.Tool{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolAnnotationBadges(tt.tool); got != tt.want {
				t.Errorf("toolAnnotationBadges(%+v) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestToolCommandLine(t *testing.T) {
	if got := toolCommandLine(api.Tool{Command: []string{"golangci-lint", "run", "{{.Input.path}}"}}); got != "golangci-lint run {{.Input.path}}" {
		t.Errorf("toolCommandLine = %q", got)
	}
	if got := toolCommandLine(api.Tool{}); got != "—" {
		t.Errorf("toolCommandLine for host-native tool = %q, want em dash", got)
	}
}

func TestToolSchemaJSON(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []any{"path"},
	}

	rendered := toolSchemaJSON(schema)
	if !strings.Contains(rendered, "\n  \"type\": \"object\"") {
		t.Fatalf("expected indented JSON, got:\n%s", rendered)
	}
	if !json.Valid([]byte(rendered)) {
		t.Errorf("rendered schema is not valid JSON: %s", rendered)
	}

	if got := toolSchemaJSON(nil); got != "" {
		t.Errorf("toolSchemaJSON(nil) = %q, want empty", got)
	}
	if got := toolSchemaJSON(map[string]any{}); got != "" {
		t.Errorf("toolSchemaJSON(empty) = %q, want empty", got)
	}
}

func TestToolColumnWidths(t *testing.T) {
	tools := []api.Tool{
		{Name: "bash"},
		{Name: "linter", Annotations: api.ToolAnnotation{IsReadOnly: true, IsDangerous: true, IsOpenWorld: true}},
	}

	// An unmeasured terminal leaves the description column unbounded.
	name, status, scope, annotations, desc := toolColumnWidths(tools, 0)
	if name < 16 || name > 30 {
		t.Errorf("name column width = %d, want clamped to [16,30]", name)
	}
	// Widths include the two-cell column padding, so each must fit its content.
	if status < len("[Deferred]") {
		t.Errorf("status column width = %d, want room for %q", status, "[Deferred]")
	}
	if scope < len("[Workspace]") {
		t.Errorf("scope column width = %d, want room for %q", scope, "[Workspace]")
	}
	if annotations < len("[ReadOnly] [Dangerous] [OpenWorld]") {
		t.Errorf("annotations column width = %d, want at least the widest badge set", annotations)
	}
	if desc != 0 {
		t.Errorf("description width = %d, want 0 when the terminal width is unknown", desc)
	}

	// A known terminal width gives the description whatever space remains.
	_, _, _, _, descWide := toolColumnWidths(tools, 160)
	if descWide <= 0 {
		t.Fatalf("description width = %d, want remaining terminal width", descWide)
	}
	if measured := name + status + scope + annotations + descWide; measured > 160 {
		t.Errorf("columns total %d cells for a %d cell terminal, want no overflow", measured, 160)
	}

	// Narrow terminals keep a readable description floor.
	_, _, _, _, descNarrow := toolColumnWidths(tools, 40)
	if descNarrow < 20 {
		t.Errorf("description width = %d on a narrow terminal, want at least 20", descNarrow)
	}
}

func TestToolsBrowserEmptyAndPopulatedRendering(t *testing.T) {
	store.ClearSlot()
	t.Cleanup(func() {
		store.ClearSlot()
		store.SetWorkspaceTools(nil, "")
	})

	// 1. Empty state
	store.SetWorkspaceTools([]api.Tool{}, "")
	out := renderTestNode(t, ToolsBrowser("slot-1"))
	if !strings.Contains(out, "Tools (0 available)") || !strings.Contains(out, "No tools available") {
		t.Fatalf("unexpected empty browser output:\n%s", out)
	}

	// 2. Populated state
	tools := []api.Tool{
		{
			Name:        "view",
			DisplayName: "View File",
			Description: "Read a file from the workspace",
			Namespace:   "system",
			Scope:       "Builtin",
			Annotations: api.ToolAnnotation{IsReadOnly: true, IsIdempotent: true},
			InputSchema: map[string]any{"type": "object", "required": []any{"path"}},
			Tokens:      180,
			Status:      api.ToolStatusEager,
		},
		{
			Name:        "linter",
			Description: "Lint the workspace sources",
			Namespace:   "workspace",
			Scope:       "Workspace",
			Command:     []string{"golangci-lint", "run", "{{.Input.path}}"},
			Annotations: api.ToolAnnotation{IsDangerous: true, UserHint: "Lint %s"},
			Tokens:      120,
			Status:      api.ToolStatusDeferred,
		},
		{
			Name:        "web_fetch",
			Description: "Fetch a URL",
			Namespace:   "system",
			Scope:       "Builtin",
			Annotations: api.ToolAnnotation{IsOpenWorld: true},
			Status:      api.ToolStatusDenied,
		},
	}
	store.SetWorkspaceTools(tools, "main")

	out = renderTestNode(t, ToolsBrowser("slot-2"))

	if !strings.Contains(out, "Tools (3 available)") {
		t.Fatalf("expected title to show 3 available, got:\n%s", out)
	}
	for _, want := range []string{"view", "linter", "web_fetch"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected tool name %q in output, got:\n%s", want, out)
		}
	}
	for _, want := range []string{"[Eager]", "[Deferred]", "[Denied]"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected status badge %s in output, got:\n%s", want, out)
		}
	}
	for _, want := range []string{"[Builtin]", "[Workspace]"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected scope badge %s in output, got:\n%s", want, out)
		}
	}
	for _, want := range []string{"[ReadOnly]", "[Dangerous]", "[OpenWorld]"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected annotation badge %s in output, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Lint the workspace sources") {
		t.Fatalf("expected description column in output, got:\n%s", out)
	}
}

func TestToolInspectorRendering(t *testing.T) {
	tool := api.Tool{
		Name:         "linter",
		DisplayName:  "Workspace Linter",
		Description:  "Lint the workspace sources",
		Namespace:    "workspace",
		Scope:        "Workspace",
		Command:      []string{"golangci-lint", "run", "{{.Input.path}}"},
		InputSchema:  map[string]any{"type": "object", "required": []any{"path"}},
		OutputSchema: map[string]any{"type": "string"},
		Annotations:  api.ToolAnnotation{IsDangerous: true, IsIdempotent: true, UserHint: "Lint %s"},
		Tokens:       120,
		Status:       api.ToolStatusDeferred,
	}

	out := renderTestNode(t, toolInspector(toolInspectorProps{
		slotID:  "slot-3",
		tool:    tool,
		onClose: func() {},
	}))

	if !strings.Contains(out, "Tool: linter") {
		t.Errorf("expected header to contain tool name, got:\n%s", out)
	}
	if !strings.Contains(out, "[Workspace]") || !strings.Contains(out, "[Deferred]") {
		t.Errorf("expected scope and status badges in header, got:\n%s", out)
	}
	if !strings.Contains(out, "Workspace Linter") {
		t.Errorf("expected display name, got:\n%s", out)
	}
	if !strings.Contains(out, "Lint the workspace sources") {
		t.Errorf("expected description, got:\n%s", out)
	}
	if !strings.Contains(out, "golangci-lint run {{.Input.path}}") {
		t.Errorf("expected rendered command line, got:\n%s", out)
	}
	if !strings.Contains(out, "[Dangerous]") || !strings.Contains(out, "[Idempotent]") {
		t.Errorf("expected annotation badges, got:\n%s", out)
	}
	if !strings.Contains(out, "Lint %s") {
		t.Errorf("expected annotation user hint, got:\n%s", out)
	}
	if !strings.Contains(out, "inputSchema") || !strings.Contains(out, "outputSchema") {
		t.Errorf("expected schema section labels, got:\n%s", out)
	}
	if !strings.Contains(out, "\"type\"") || !strings.Contains(out, "\"required\"") {
		t.Errorf("expected formatted input schema JSON, got:\n%s", out)
	}
}

func TestToolInspectorWithoutSchemaOrCommand(t *testing.T) {
	out := renderTestNode(t, toolInspector(toolInspectorProps{
		slotID: "slot-4",
		tool: api.Tool{
			Name:        "view",
			Description: "Read a file",
			Namespace:   "system",
			Scope:       "Builtin",
			Annotations: api.ToolAnnotation{IsReadOnly: true},
			Status:      api.ToolStatusEager,
		},
		onClose: func() {},
	}))

	if !strings.Contains(out, "—") {
		t.Errorf("expected em dash for the host-native command line, got:\n%s", out)
	}
	if !strings.Contains(out, "no schema declared") {
		t.Errorf("expected placeholder for missing schemas, got:\n%s", out)
	}
}

func TestToolsBrowserRowsDoNotWrap(t *testing.T) {
	store.ClearSlot()
	t.Cleanup(func() {
		store.ClearSlot()
		store.SetWorkspaceTools(nil, "")
	})

	longDesc := "Runs the project linter over the requested path and reports every diagnostic it finds, including style, correctness, and security findings."
	store.SetWorkspaceTools([]api.Tool{
		{Name: "view", Description: "Read a file from the workspace", Namespace: "system", Scope: "Builtin", Annotations: api.ToolAnnotation{IsReadOnly: true}, Status: api.ToolStatusEager},
		{Name: "linter", Description: longDesc, Namespace: "workspace", Scope: "Workspace", Annotations: api.ToolAnnotation{IsReadOnly: true, IsDangerous: true, IsOpenWorld: true}, Status: api.ToolStatusDeferred},
	}, "main")

	backend := mock.New(100, 24)
	inst, err := trail.New(ToolsBrowser("slot-wrap"), trail.WithBackend(backend))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()
	surf := surfaceText(backend)

	lines := strings.Split(surf, "\n")
	linterLineIdx := -1
	for idx, line := range lines {
		if strings.Contains(line, "linter") {
			linterLineIdx = idx
		}
	}
	if linterLineIdx < 0 {
		t.Fatalf("expected a row for the linter tool, got:\n%s", surf)
	}
	// The row must stay on one line: the following row (or hint) starts with the
	// cursor/indent prefix rather than continuing the linter's columns.
	if linterLineIdx+1 < len(lines) && strings.Contains(lines[linterLineIdx+1], "diagnostics") {
		t.Errorf("expected description to be truncated to a single line, got:\n%s", surf)
	}
	if !strings.Contains(surf, "…") {
		t.Errorf("expected the long description to be ellipsized, got:\n%s", surf)
	}
}
