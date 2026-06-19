package components

import (
	"testing"
)

func TestCodeBlock(t *testing.T) {
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
}
