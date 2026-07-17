package components

import (
	"image/color"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func TestMarkdown(t *testing.T) {
	t.Run("BasicParagraph", func(t *testing.T) {
		node := Markdown(MarkdownProps{
			Source: "Hello, world! This is markdown.",
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for basic text")
		}
	})

	t.Run("Headers", func(t *testing.T) {
		source := "# Header 1\n## Header 2\n### Header 3"
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for headers")
		}
	})

	t.Run("StyledText", func(t *testing.T) {
		source := "This is **bold** and *italic* text."
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for bold and italic text")
		}
	})

	t.Run("UnorderedList", func(t *testing.T) {
		source := "- Item 1\n- Item 2\n- Item 3"
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for unordered list")
		}
	})

	t.Run("OrderedList", func(t *testing.T) {
		source := "1. First\n2. Second\n3. Third"
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for ordered list")
		}
	})

	t.Run("CodeBlocks", func(t *testing.T) {
		source := "Inline `code` here.\n\n```go\nfmt.Println(\"Hello\")\n```"
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for code blocks")
		}
	})

	t.Run("BlockquoteAndBreak", func(t *testing.T) {
		source := "> To be or not to be\n\n---\nDone."
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for blockquote and break")
		}
	})

	t.Run("Table", func(t *testing.T) {
		source := "| Header 1 | Header 2 |\n|---|---|\n| Row 1 Col 1 | Row 1 Col 2 |\n| Row 2 Col 1 | Row 2 Col 2 |"
		node := Markdown(MarkdownProps{
			Source: source,
		})
		if node == nil {
			t.Fatal("Markdown component returned nil for table")
		}
	})
}

func TestMarkdownRender_ListPreservesInlineContent(t *testing.T) {
	env := renderMarkdownEnv(t, 80, 8, "- 📁 `internal/tui/components/markdown.go`\n- plain text")
	defer env.Close()

	lines := nonEmptyLines(env.DumpText())
	if len(lines) < 2 {
		t.Fatalf("expected at least two rendered lines, got %q", env.DumpText())
	}

	if !strings.Contains(lines[0], "•") ||
		!strings.Contains(lines[0], "📁") ||
		!strings.Contains(lines[0], "internal/tui/components/markdown.go") {
		t.Fatalf("first list item lost inline content: %q", lines[0])
	}
	if !strings.Contains(lines[1], "• plain text") {
		t.Fatalf("second list item rendered unexpectedly: %q", lines[1])
	}
}

func TestMarkdownRender_ListCodeSpanKeepsInlineStyling(t *testing.T) {
	const codeText = "tasksmith.lock"

	env := renderMarkdownEnv(t, 80, 6, "- prefix `tasksmith.lock` suffix")
	defer env.Close()

	lines := nonEmptyLines(env.DumpText())
	if len(lines) == 0 {
		t.Fatal("expected rendered markdown output")
	}

	idx := firstCellIndex(lines[0], codeText)
	if idx < 0 {
		t.Fatalf("expected inline code %q in rendered line %q", codeText, lines[0])
	}

	frame := env.Backend.LastFrame()
	if frame.Surface == nil {
		t.Fatal("expected a rendered framebuffer")
	}

	for offset := 0; offset < len(codeText); offset++ {
		cell := frame.Surface.CellAt(idx+offset, 0)
		if cell.Bg == nil {
			t.Fatalf("inline code cell %d missing background styling", offset)
		}
	}
}

func TestMarkdownRender_LongUnbreakableWordWraps(t *testing.T) {
	longWord := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	env := renderMarkdownEnv(t, 20, 8, longWord)
	defer env.Close()

	text := env.DumpText()
	lines := nonEmptyLines(text)
	if len(lines) < 2 {
		t.Fatalf("expected long word to wrap into multiple lines, but got %d lines: %q", len(lines), text)
	}
}

func renderMarkdownEnv(t *testing.T, width, height int, source string) *testenv.Environment {
	t.Helper()

	env := testenv.Default(width, height)
	container := element.NewBox(env.Document())
	env.Mount(container)

	scheme := &theme.Scheme{
		Color: theme.Color{
			Text: theme.TextColor{
				Primary:   color.RGBA{R: 255, G: 255, B: 255, A: 255},
				Secondary: color.RGBA{R: 210, G: 210, B: 210, A: 255},
			},
			Surface: theme.SurfaceColor{
				BaseFocus: color.RGBA{R: 60, G: 60, B: 60, A: 255},
				Primary:   color.RGBA{R: 120, G: 180, B: 255, A: 255},
			},
		},
	}

	kitex.Render(
		theme.Provider(theme.Props{Theme: scheme},
			Markdown(MarkdownProps{Source: source}),
		),
		container,
	)
	env.Flush()
	return env
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func firstCellIndex(line, needle string) int {
	byteIdx := strings.Index(line, needle)
	if byteIdx < 0 {
		return -1
	}
	return utf8.RuneCountInString(line[:byteIdx])
}
