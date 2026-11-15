package components

import (
	"cmp"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// attachmentRefKind is the kind string for an inline attachment reference token.
var attachmentRefKind = ast.NewNodeKind("AttachmentRef")

// AttachmentRefNode is a custom inline AST node representing a @file:, @sym:, @rule:, or structured @attachment token.
type AttachmentRefNode struct {
	ast.BaseInline
	RefType string // "file", "sym", "skill", or "rule"
	Label   string
	Target  string
	SymKind string
	IsDir   bool
}

func (n *AttachmentRefNode) Kind() ast.NodeKind { return attachmentRefKind }
func (n *AttachmentRefNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{
		"RefType": n.RefType,
		"Label":   n.Label,
		"Target":  n.Target,
		"SymKind": n.SymKind,
	}, nil)
}

var knownPrefixes = map[string]string{
	"@file:":  "file",
	"@sym:":   "sym",
	"@skill:": "skill",
	"@rule:":  "rule",
	"@mcp:":   "mcp",
}

type attachmentRefParser struct {
	attachments map[string]api.Attachment
}

func (p *attachmentRefParser) Trigger() []byte { return []byte{'@'} }

func (p *attachmentRefParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	end := scanInlineRefEnd(line)
	rawToken := string(line[:end])

	if node, advance := p.matchAttachment(rawToken); node != nil {
		block.Advance(advance)
		return node
	}

	for prefix, refType := range knownPrefixes {
		value, ok := strings.CutPrefix(rawToken, prefix)
		if !ok || value == "" {
			continue
		}
		label, symKind := labelFromValue(refType, value)
		block.Advance(end)
		return &AttachmentRefNode{
			RefType: refType,
			Label:   label,
			Target:  value,
			SymKind: symKind,
			IsDir:   refType == "file" && strings.HasSuffix(value, "/"),
		}
	}
	return nil
}

func (p *attachmentRefParser) matchAttachment(rawToken string) (*AttachmentRefNode, int) {
	if len(p.attachments) == 0 {
		return nil, 0
	}
	trimmed := strings.TrimRight(rawToken, ".,;:!?)]}")
	att, advance, ok := p.lookup(rawToken, trimmed)
	if !ok {
		return nil, 0
	}
	refType := cmp.Or(string(att.Kind), "file")
	tokenText := rawToken[:advance]
	var label string
	switch att.Kind {
	case api.AttachmentFile:
		label = strings.TrimPrefix(tokenText, "@file:")
		label = strings.Trim(strings.TrimPrefix(label, "@"), "\"'")
	case api.AttachmentRule, api.AttachmentSkill, api.AttachmentMCP:
		label = att.Target
	default:
		label = strings.TrimPrefix(strings.TrimPrefix(tokenText, "@"+refType+":"), "@")
	}
	return &AttachmentRefNode{
		RefType: refType,
		Label:   label,
		Target:  att.Target,
		IsDir:   att.Kind == api.AttachmentFile && (strings.HasSuffix(att.Target, "/") || att.Target == "."),
	}, advance
}

func (p *attachmentRefParser) lookup(rawToken, trimmed string) (api.Attachment, int, bool) {
	if att, ok := p.attachments[rawToken]; ok {
		return att, len(rawToken), true
	}
	if trimmed != rawToken {
		if att, ok := p.attachments[trimmed]; ok {
			return att, len(trimmed), true
		}
	}
	if base, _, hasHash := strings.Cut(trimmed, "#"); hasHash {
		if att, ok := p.attachments[base]; ok {
			return att, len(trimmed), true
		}
	}
	return api.Attachment{}, 0, false
}

func scanInlineRefEnd(line []byte) int {
	if len(line) > 1 && (line[1] == '"' || line[1] == '\'') {
		q := line[1]
		end := 2
		for end < len(line) && line[end] != q && line[end] != '\n' && line[end] != '\r' {
			if line[end] == '\\' && end+1 < len(line) {
				end += 2
				continue
			}
			end++
		}
		if end < len(line) && line[end] == q {
			end++
			for end < len(line) && !isInlineSpace(line[end]) {
				end++
			}
			return end
		}
	}
	end := 0
	for end < len(line) && !isInlineSpace(line[end]) {
		end++
	}
	return end
}

func isInlineSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func labelFromValue(refType, value string) (label, symKind string) {
	switch refType {
	case "file":
		if before, _, ok := strings.Cut(value, "#"); ok {
			value = before
		}
		return filepath.Base(value), ""
	case "sym":
		if idx := strings.LastIndex(value, ":"); idx != -1 {
			return value[:idx], value[idx+1:]
		}
		return value, ""
	default:
		return value, ""
	}
}
