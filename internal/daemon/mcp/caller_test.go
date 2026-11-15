package mcp

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// TestNewToolRuntimeNilManager verifies a workspace without an MCP manager yields no
// tool runtime at all, so the container leaves MCP tools unregistered instead of
// registering tools that would fail at call time.
func TestNewToolRuntimeNilManager(t *testing.T) {
	if got := NewToolRuntime(nil); got != nil {
		t.Fatalf("NewToolRuntime(nil) = %+v, want nil", got)
	}
	// The nil receiver must still answer safely rather than dereferencing.
	var runtime *ToolRuntime
	if runtime.HasAllowedMCPResources() {
		t.Error("a nil runtime must not report resources")
	}
	if _, err := runtime.CallMCPTool(context.Background(), "notes", "echo", nil); err == nil {
		t.Error("CallMCPTool on a nil runtime = nil error, want a rejection")
	}
	if _, err := runtime.ReadMCPResource(context.Background(), "notes", "file:///x", false); err == nil {
		t.Error("ReadMCPResource on a nil runtime = nil error, want a rejection")
	}
}

// TestToolRuntimeSatisfiesCaller pins the adapter to the contract the tool container
// depends on, so a signature drift fails to compile rather than failing in wiring.
func TestToolRuntimeSatisfiesCaller(t *testing.T) {
	var caller tools.MCPCaller = NewToolRuntime(&Manager{})
	if runtime, ok := caller.(*ToolRuntime); !ok || runtime.manager == nil {
		t.Fatalf("caller = %T, want a *ToolRuntime holding a manager", caller)
	}
}
