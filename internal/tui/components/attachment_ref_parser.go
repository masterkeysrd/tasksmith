package components

import (
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// attachmentRefKind is the kind string for an inline attachment reference token.
var attachmentRefKind = ast.NewNodeKind("AttachmentRef")

// AttachmentRefNode is a custom inline AST node representing a @file:, @sym:, or @skill: token.
type AttachmentRefNode struct {
	ast.BaseInline
	// RefType is "file", "sym", or "skill".
	RefType string
	// Label is the display label derived from the value:
	//   @file:internal/foo/bar.go → "bar.go"
	//   @sym:MyFunc               → "MyFunc"
	//   @skill:agent-tooling      → "agent-tooling"
	Label string
	// Kind is the LSP symbol kind, populated only for @sym: references.
	// Extracted from the value when the format is "name:kind" (e.g. "MyFunc:function").
	SymKind string
}

func (n *AttachmentRefNode) Kind() ast.NodeKind { return attachmentRefKind }
func (n *AttachmentRefNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{
		"RefType": n.RefType,
		"Label":   n.Label,
		"SymKind": n.SymKind,
	}, nil)
}

// knownPrefixes maps the trigger prefix to the RefType.
var knownPrefixes = map[string]string{
	"@file:":  "file",
	"@sym:":   "sym",
	"@skill:": "skill",
}

// attachmentRefParser is a goldmark inline parser that emits AttachmentRefNode
// for @file:, @sym:, and @skill: tokens.
type attachmentRefParser struct{}

func (p *attachmentRefParser) Trigger() []byte { return []byte{'@'} }

func (p *attachmentRefParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, segment := block.PeekLine()
	src := block.Value(segment)

	// Find where the token ends (next whitespace or end of line).
	end := 0
	for end < len(line) && line[end] != ' ' && line[end] != '\t' && line[end] != '\n' && line[end] != '\r' {
		end++
	}
	token := string(line[:end])

	for prefix, refType := range knownPrefixes {
		if !strings.HasPrefix(token, prefix) {
			continue
		}
		value := token[len(prefix):]
		if value == "" {
			continue
		}

		label, symKind := labelFromValue(refType, value, src)

		node := &AttachmentRefNode{
			RefType: refType,
			Label:   label,
			SymKind: symKind,
		}
		block.Advance(end)
		return node
	}

	return nil
}

// labelFromValue derives the display label and optional LSP kind from a raw value.
func labelFromValue(refType, value string, _ []byte) (label, symKind string) {
	switch refType {
	case "file":
		// Strip optional line range anchor (e.g. "foo.go#L10-L20" → "foo.go")
		if idx := strings.Index(value, "#"); idx != -1 {
			value = value[:idx]
		}
		label = filepath.Base(value)
	case "sym":
		// Optional "Name:kind" encoding (e.g. "MyFunc:function")
		if idx := strings.LastIndex(value, ":"); idx != -1 {
			label = value[:idx]
			symKind = value[idx+1:]
		} else {
			label = value
		}
	case "skill":
		label = value
	default:
		label = value
	}
	return label, symKind
}
