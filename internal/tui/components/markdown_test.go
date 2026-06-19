package components

import (
	"testing"
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
