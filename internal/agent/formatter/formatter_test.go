package formatter

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/lspx"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
)

func TestFormatFile(t *testing.T) {
	f := &resolver.ResolvedFile{
		FilePath:   "/workspace/bar.go",
		Content:    "package foo\nfunc Hello() {}",
		TotalLines: 2,
		MimeType:   "text/x-go",
		IsBinary:   false,
	}

	blocks := FormatFile(f)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	txtBlock, ok := blocks[0].(*message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock")
	}

	if !strings.Contains(txtBlock.Text, "bar.go") {
		t.Errorf("expected header to contain filename, got: %s", txtBlock.Text)
	}
	if !strings.Contains(txtBlock.Text, "1 | package foo") {
		t.Errorf("expected numbered lines, got: %s", txtBlock.Text)
	}
}

func TestFormatAttachmentsBlock(t *testing.T) {
	// Helper to create a resolver with a custom ShouldEmbed override.
	newResolver := func(_ bool) *resolver.Resolver {
		return &resolver.Resolver{} // ShouldEmbed uses default thresholds
	}

	tests := []struct {
		name         string
		resources    []resolver.ResolvedResource
		resolver     *resolver.Resolver
		wantContains []string
		wantMissing  []string
	}{
		{
			name:         "empty resources returns empty string",
			resources:    nil,
			resolver:     newResolver(false),
			wantContains: []string{},
			wantMissing:  []string{},
		},
		{
			name: "small file is embedded with content",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedFile{
					FilePath:   "internal/app/app.go",
					Content:    "package app\nfunc Main() {}",
					TotalLines: 2,
					StartLine:  1,
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<attachments>",
				"<file path=\"internal/app/app.go\" lines=\"2\">",
				"<content>",
				"1 | package app",
				"2 | func Main() {}",
				"</content>",
				"</file>",
				"</attachments>",
			},
			wantMissing: []string{"<diagnostics>", "reason="},
		},
		{
			name: "large file is referenced with self-closing tag",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedFile{
					FilePath:   "internal/gen/large.go",
					Content:    strings.Repeat("line\n", 5000),
					TotalLines: 8400,
					Truncated:  true,
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<attachments>",
				"<file path=\"internal/gen/large.go\" lines=\"8400\" reason=\"file too large, use view_file to read\" />",
				"</attachments>",
			},
			wantMissing: []string{"<content>", "<diagnostics>"},
		},
		{
			name: "binary file is referenced with mime type",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedFile{
					FilePath: "assets/image.png",
					IsBinary: true,
					MimeType: "image/png",
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<attachments>",
				`<file path="assets/image.png" mime="image/png" reason="binary file, use view_file" />`,
				"</attachments>",
			},
			wantMissing: []string{"<content>"},
		},
		{
			name: "symbol with content",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedSymbol{
					Name:      "NewResolver",
					Kind:      "function",
					FilePath:  "resolver.go",
					StartLine: 88,
					EndLine:   94,
					Snippet:   "func NewResolver(l *lsp.Manager, cwd string) *Resolver {\n\treturn &Resolver{}\n}",
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<attachments>",
				`<symbol name="NewResolver" kind="function" file="resolver.go" lines="88-94">`,
				"<content>",
				"func NewResolver",
				"</symbol>",
				"</attachments>",
			},
			wantMissing: []string{"reason="},
		},
		{
			name: "skill with content",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedSkill{
					Name:         "agent-tooling",
					Instructions: "## Agent Tooling Skill\n\nGuidelines for tools.",
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<attachments>",
				`<skill name="agent-tooling">`,
				"<content>",
				"Agent Tooling Skill",
				"</skill>",
				"</attachments>",
			},
			wantMissing: []string{"reason="},
		},
		{
			name: "mixed resources",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedFile{
					FilePath:   "internal/app/app.go",
					Content:    "package app",
					TotalLines: 1,
				},
				&resolver.ResolvedFile{
					FilePath:   "internal/gen/large.go",
					Content:    strings.Repeat("x\n", 5000),
					TotalLines: 8400,
					Truncated:  true,
				},
				&resolver.ResolvedSymbol{
					Name:      "Foo",
					Kind:      "function",
					FilePath:  "foo.go",
					StartLine: 1,
					EndLine:   5,
					Snippet:   "func Foo() {}",
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<attachments>",
				`<file path="internal/app/app.go" lines="1">`,
				`<file path="internal/gen/large.go" lines="8400" reason="file too large, use view_file to read" />`,
				`<symbol name="Foo" kind="function" file="foo.go" lines="1-5">`,
				"</attachments>",
			},
			wantMissing: []string{},
		},
		{
			name: "file with diagnostics",
			resources: []resolver.ResolvedResource{
				&resolver.ResolvedFile{
					FilePath:   "internal/app/app.go",
					Content:    "package app",
					TotalLines: 120,
					Diagnostics: []lsp.Diagnostic{
						{
							Diagnostic: lspx.Diagnostic{
								Message: lspx.DiagnosticMessage{String: new("undefined variable 'ctx'")},
								Range: lspx.Range{
									Start: lspx.Position{Line: 41},
								},
								Severity: new(lspx.DiagnosticSeverityError),
							},
						},
						{
							Diagnostic: lspx.Diagnostic{
								Message: lspx.DiagnosticMessage{String: new("unused parameter 'opts'")},
								Range: lspx.Range{
									Start: lspx.Position{Line: 77},
								},
								Severity: new(lspx.DiagnosticSeverityWarning),
							},
						},
					},
				},
			},
			resolver: newResolver(false),
			wantContains: []string{
				"<diagnostics>",
				`[error] line 42: undefined variable &apos;ctx&apos;`,
				`[warning] line 78: unused parameter &apos;opts&apos;`,
				"</diagnostics>",
			},
			wantMissing: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAttachmentsBlock(tt.resources, tt.resolver)

			if tt.name == "empty resources returns empty string" {
				if got != "" {
					t.Errorf("FormatAttachmentsBlock() = %q, want empty string", got)
				}
				return
			}

			if got == "" {
				t.Error("FormatAttachmentsBlock() returned empty string, expected content")
				return
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("FormatAttachmentsBlock() missing %q in:\n%s", want, got)
				}
			}

			for _, missing := range tt.wantMissing {
				if strings.Contains(got, missing) {
					t.Errorf("FormatAttachmentsBlock() unexpectedly contains %q in:\n%s", missing, got)
				}
			}
		})
	}
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no special chars", "hello", "hello"},
		{"ampersand", "a & b", "a &amp; b"},
		{"less than", "a < b", "a &lt; b"},
		{"greater than", "a > b", "a &gt; b"},
		{"double quote", "a \"b\"", "a &quot;b&quot;"},
		{"single quote", "a 'b'", "a &apos;b&apos;"},
		{"all special chars", "<tag attr='val'>&text</tag>", "&lt;tag attr=&apos;val&apos;&gt;&amp;text&lt;/tag&gt;"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeXML(tt.input)
			if got != tt.want {
				t.Errorf("escapeXML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatAttachmentsBlock_SymbolWithDiagnostics(t *testing.T) {
	res := &resolver.Resolver{}
	resources := []resolver.ResolvedResource{
		&resolver.ResolvedSymbol{
			Name:      "NewResolver",
			Kind:      "function",
			FilePath:  "resolver.go",
			StartLine: 88,
			EndLine:   94,
			Snippet:   "func NewResolver(l *lsp.Manager, cwd string) *Resolver {\n\treturn &Resolver{Lsp: l, Cwd: cwd}\n}",
			Diagnostics: []lsp.Diagnostic{
				{
					Diagnostic: lspx.Diagnostic{
						Message:  lspx.DiagnosticMessage{String: new("parameter 'lsp' is unused")},
						Range:    lspx.Range{Start: lspx.Position{Line: 89}},
						Severity: new(lspx.DiagnosticSeverityWarning),
					},
				},
			},
		},
	}

	got := FormatAttachmentsBlock(resources, res)

	if !strings.Contains(got, "<symbol name=\"NewResolver\" kind=\"function\" file=\"resolver.go\" lines=\"88-94\">") {
		t.Errorf("missing symbol tag: %s", got)
	}
	if !strings.Contains(got, "<diagnostics>") {
		t.Errorf("missing diagnostics tag: %s", got)
	}
	if !strings.Contains(got, "[warning] line 90: parameter &apos;lsp&apos; is unused") {
		t.Errorf("missing diagnostic message: %s", got)
	}
	if !strings.Contains(got, "</symbol>") {
		t.Errorf("missing closing symbol tag: %s", got)
	}
}

func TestFormatAttachmentsBlock_Skill(t *testing.T) {
	res := &resolver.Resolver{}
	resources := []resolver.ResolvedResource{
		&resolver.ResolvedSkill{
			Name:         "golang",
			Instructions: "# Go Skill\n\nUse camelCase for exported symbols.",
		},
	}

	got := FormatAttachmentsBlock(resources, res)

	if !strings.Contains(got, `<skill name="golang">`) {
		t.Errorf("missing skill tag: %s", got)
	}
	if !strings.Contains(got, "<content>") {
		t.Errorf("missing content tag: %s", got)
	}
	if !strings.Contains(got, "Use camelCase for exported symbols.") {
		t.Errorf("missing instructions: %s", got)
	}
	if !strings.Contains(got, "</skill>") {
		t.Errorf("missing closing skill tag: %s", got)
	}
}

func TestFormatAttachmentsBlock_BinaryFile(t *testing.T) {
	res := &resolver.Resolver{}
	resources := []resolver.ResolvedResource{
		&resolver.ResolvedFile{
			FilePath: "assets/logo.png",
			IsBinary: true,
			MimeType: "image/png",
		},
		&resolver.ResolvedFile{
			FilePath: "assets/data.bin",
			IsBinary: true,
			MimeType: "",
		},
	}

	got := FormatAttachmentsBlock(resources, res)

	if !strings.Contains(got, `path="assets/logo.png"`) {
		t.Errorf("missing logo path: %s", got)
	}
	if !strings.Contains(got, `mime="image/png"`) {
		t.Errorf("missing mime type: %s", got)
	}
	if !strings.Contains(got, `path="assets/data.bin"`) {
		t.Errorf("missing data.bin path: %s", got)
	}
	if !strings.Contains(got, `mime="application/octet-stream"`) {
		t.Errorf("missing default mime type: %s", got)
	}
}

func TestFormatAttachmentsBlock_ReferencedSymbol(t *testing.T) {
	res := &resolver.Resolver{}
	resources := []resolver.ResolvedResource{
		&resolver.ResolvedSymbol{
			Name:      "VeryLongSymbol",
			Kind:      "class",
			FilePath:  "big_file.go",
			StartLine: 1,
			EndLine:   500,
			Snippet:   strings.Repeat("a", resolver.EmbedThreshold+100),
		},
	}

	got := FormatAttachmentsBlock(resources, res)

	expected := `<symbol name="VeryLongSymbol" kind="class" file="big_file.go" lines="1-500">`
	if !strings.Contains(got, expected) {
		t.Errorf("expected embedded symbol string: %s, got: %s", expected, got)
	}
	if !strings.Contains(got, "<content>") {
		t.Errorf("expected content tag to be present in embedded symbol: %s", got)
	}
}

func TestFormatFile_Directory(t *testing.T) {
	f := &resolver.ResolvedFile{
		FilePath: "/workspace/some_dir",
		IsDir:    true,
	}

	blocks := FormatFile(f)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	txtBlock, ok := blocks[0].(*message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock")
	}

	if !strings.Contains(txtBlock.Text, "[Directory: some_dir]") {
		t.Errorf("expected directory description, got: %q", txtBlock.Text)
	}
}

func TestFormatAttachmentsBlock_Directory(t *testing.T) {
	res := &resolver.Resolver{}
	resources := []resolver.ResolvedResource{
		&resolver.ResolvedFile{
			FilePath: "/workspace/some_dir",
			IsDir:    true,
		},
	}

	got := FormatAttachmentsBlock(resources, res)
	expected := `<file path="/workspace/some_dir" is_dir="true" reason="directory, use ls to list contents" />`
	if !strings.Contains(got, expected) {
		t.Errorf("expected directory attachment tag: %s, got: %s", expected, got)
	}
}
