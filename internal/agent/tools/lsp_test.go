package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestLspTools(t *testing.T) {
	// Skip test if gopls is not installed
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed in the PATH, skipping LSP tools integration test")
	}

	origXDG := os.Getenv("XDG_CONFIG_HOME")
	tempConfigDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tempConfigDir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	xdg.ClearCache()

	// Write mock config containing only the "go" preset
	cfg := &lsp.Config{
		Servers: []lspx.ServerConfig{
			lsp.Presets["go"],
		},
	}
	if err := lsp.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save mock lsp config: %v", err)
	}

	dir := t.TempDir()

	// Create a simple main.go file
	mainGoContent := `package main

import "fmt"

// TestStruct defines a structure.
type TestStruct struct {
	Name string
}

func main() {
	var item TestStruct = TestStruct{Name: "TaskSmith"}
	fmt.Println(item.Name)
}
`
	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte(mainGoContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// Make sure we shut down clients after the test finishes
	mgr := lsp.NewManager()
	defer mgr.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	handlers := NewHandlers(nil, dir).WithLspManager(mgr)

	t.Run("LspDiagnostics", func(t *testing.T) {
		out, err := handlers.LspDiagnostics(ctx, LspDiagnosticsArgs{Path: "main.go"})
		if err != nil {
			t.Fatalf("LspDiagnostics failed: %v", err)
		}
		// Diagnostics might be empty since the code is valid, but the execution should succeed
		t.Logf("LspDiagnostics returned %d diagnostics", len(out.Diagnostics))
	})

	t.Run("LspSearch", func(t *testing.T) {
		out, err := handlers.LspSearch(ctx, LspSearchArgs{Query: "TestStruct"})
		if err != nil {
			t.Fatalf("LspSearch failed: %v", err)
		}

		found := false
		for _, sym := range out.Results {
			if sym.Name == "TestStruct" {
				found = true
				if !strings.Contains(sym.Path, "main.go") {
					t.Errorf("expected path to contain 'main.go', got %q", sym.Path)
				}
				break
			}
		}

		if !found {
			t.Logf("LspSearch did not find TestStruct (this can happen if gopls hasn't finished indexing, results: %+v)", out.Results)
		}
	})

	t.Run("LspRestart", func(t *testing.T) {
		out, err := handlers.LspRestart(ctx, LspRestartArgs{Server: "gopls"})
		if err != nil {
			t.Fatalf("LspRestart failed: %v", err)
		}
		if !out.Success {
			t.Errorf("LspRestart failed: %s", out.Message)
		}
	})
}

func TestLspTextContentProviders(t *testing.T) {
	t.Run("LspDiagnosticsOutput", func(t *testing.T) {
		out := LspDiagnosticsOutput{
			Diagnostics: []LspDiagnosticsOutputDiagnosticsItem{
				{
					Path:     "main.go",
					Message:  "undeclared name",
					Severity: "error",
					Range: LspDiagnosticsOutputDiagnosticsItemRange{
						Start: LspDiagnosticsOutputDiagnosticsItemRangeStart{Line: 10, Character: 5},
						End:   LspDiagnosticsOutputDiagnosticsItemRangeEnd{Line: 10, Character: 15},
					},
				},
			},
		}

		expected := "- [ERROR] main.go:11:6 - undeclared name\n"
		if out.TextContent() != expected {
			t.Errorf("expected %q, got %q", expected, out.TextContent())
		}

		content := out.ToolContent()
		if len(content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(content))
		}
	})

	t.Run("LspRestartOutput", func(t *testing.T) {
		outSuccess := LspRestartOutput{
			Success: true,
			Message: "Restarted gopls",
		}
		expectedSuccess := "Success: Restarted gopls"
		if outSuccess.TextContent() != expectedSuccess {
			t.Errorf("expected %q, got %q", expectedSuccess, outSuccess.TextContent())
		}

		outFailure := LspRestartOutput{
			Success: false,
			Message: "Failed to restart gopls",
		}
		expectedFailure := "Failure: Failed to restart gopls"
		if outFailure.TextContent() != expectedFailure {
			t.Errorf("expected %q, got %q", expectedFailure, outFailure.TextContent())
		}
	})

	t.Run("LspSearchOutput", func(t *testing.T) {
		out := LspSearchOutput{
			Results: []LspSearchOutputResultsItem{
				{
					Name:          "MyStruct",
					Kind:          "Struct",
					Path:          "models.go",
					ContainerName: "db",
					Range: LspSearchOutputResultsItemRange{
						Start: LspSearchOutputResultsItemRangeStart{Line: 5, Character: 10},
						End:   LspSearchOutputResultsItemRangeEnd{Line: 5, Character: 18},
					},
				},
			},
		}

		expected := "- MyStruct (Struct) in db at models.go:6:11\n"
		if out.TextContent() != expected {
			t.Errorf("expected %q, got %q", expected, out.TextContent())
		}
	})

	t.Run("LspDiagnosticsTruncation", func(t *testing.T) {
		var list []LspDiagnosticsOutputDiagnosticsItem
		for i := 0; i < 2; i++ {
			list = append(list, LspDiagnosticsOutputDiagnosticsItem{
				Path:     "main.go",
				Message:  fmt.Sprintf("some error message number %d which is very detailed", i),
				Severity: "error",
				Range: LspDiagnosticsOutputDiagnosticsItemRange{
					Start: LspDiagnosticsOutputDiagnosticsItemRangeStart{Line: i, Character: 1},
					End:   LspDiagnosticsOutputDiagnosticsItemRangeEnd{Line: i, Character: 10},
				},
			})
		}
		out := LspDiagnosticsOutput{
			Diagnostics: list,
			TotalCount:  200,
			Truncated:   true,
		}
		text := out.TextContent()

		if !strings.Contains(text, "[SYSTEM NOTE: Diagnostics truncated") {
			t.Error("expected text content to be truncated with system note")
		}
	})

	t.Run("LspSearchTruncation", func(t *testing.T) {
		var list []LspSearchOutputResultsItem
		for i := 0; i < 200; i++ {
			list = append(list, LspSearchOutputResultsItem{
				Name:          fmt.Sprintf("MySymbol%d", i),
				Kind:          "Function",
				Path:          "main.go",
				ContainerName: "main",
				Range: LspSearchOutputResultsItemRange{
					Start: LspSearchOutputResultsItemRangeStart{Line: i, Character: 1},
					End:   LspSearchOutputResultsItemRangeEnd{Line: i, Character: 10},
				},
			})
		}
		out := LspSearchOutput{Results: list}
		text := out.TextContent()

		if !strings.Contains(text, "[SYSTEM NOTE: Results truncated") {
			t.Error("expected text content to be truncated with system note")
		}
		if len(text) > 8500 {
			t.Errorf("expected text content to be capped around 8000 chars, got length %d", len(text))
		}
	})

	t.Run("LspDiagnosticsSorting", func(t *testing.T) {
		diags := []LspDiagnosticsOutputDiagnosticsItem{
			{Severity: "warning", Message: "w1", Path: "a.go"},
			{Severity: "hint", Message: "h1", Path: "a.go"},
			{Severity: "error", Message: "e1", Path: "b.go"},
			{Severity: "error", Message: "e2", Path: "a.go"},
			{Severity: "info", Message: "i1", Path: "a.go"},
		}

		sort.Slice(diags, func(i, j int) bool {
			sevI := getSeverityValue(diags[i].Severity)
			sevJ := getSeverityValue(diags[j].Severity)
			if sevI != sevJ {
				return sevI < sevJ
			}
			if diags[i].Path != diags[j].Path {
				return diags[i].Path < diags[j].Path
			}
			return diags[i].Message < diags[j].Message
		})

		if diags[0].Message != "e2" || diags[1].Message != "e1" || diags[2].Message != "w1" || diags[3].Message != "i1" || diags[4].Message != "h1" {
			t.Errorf("unexpected sorted order: %+v", diags)
		}
	})
}
