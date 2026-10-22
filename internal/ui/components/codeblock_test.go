package components

import (
	"testing"
)

func TestCodeBlock(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := CodeBlock(CodeBlockProps{
			Code: `package main
func main() {
	println("hello")
}`,
			Lang: "go",
		})
		if node == nil {
			t.Fatal("CodeBlock component returned nil for basic Go code")
		}
	})

	t.Run("LineNumbers", func(t *testing.T) {
		node := CodeBlock(CodeBlockProps{
			Code: `package main
func main() {
	println("hello")
}`,
			Lang:            "go",
			ShowLineNumbers: true,
			StartLine:       10,
		})
		if node == nil {
			t.Fatal("CodeBlock component returned nil with line numbers enabled")
		}
	})
}
