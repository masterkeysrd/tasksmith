package mentions

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

var lineRangeRe = regexp.MustCompile(`#(?:L?(\d+))(?:-L?(\d+))?$`)

// ScannedToken is an inline token starting with '@' parsed from text.
type ScannedToken struct {
	FullToken string // Raw token including @ and line range, e.g. "@composer.go#L10-L20"
	BaseToken string // Token without line range, e.g. "@composer.go"
	StartLine int
	EndLine   int
}

// Resolve extracts structured attachments from user text in a single pass.
// It matches tracked bindings (supporting added or edited #L line ranges), resolves
// explicit @mcp:, @file:, @rule:, and @skill: prefixes, and—when lookupFile is non-nil—resolves
// any remaining unbound @<candidate> tokens against the workspace file index.
func Resolve(text string, tracked []api.Attachment, lookupFile func(candidate string) (target string, ok bool)) []api.Attachment {
	tokens := ScanTokens(text)
	if len(tokens) == 0 {
		return nil
	}

	byFullRef := make(map[string]api.Attachment, len(tracked))
	byBaseRef := make(map[string]api.Attachment, len(tracked))
	byMCPTarget := make(map[string]api.Attachment)
	for _, att := range tracked {
		if att.Ref != "" {
			byFullRef[att.Ref] = att
			byBaseRef[stripLineRangeSuffix(att.Ref)] = att
		}
		if att.Kind == api.AttachmentMCP && att.Target != "" {
			byMCPTarget[strings.ToLower(att.Target)] = att
		}
	}

	var attachments []api.Attachment
	seen := make(map[string]struct{})
	var lookupCache map[string]string

	add := func(att api.Attachment) {
		key := fmt.Sprintf("%s:%s:%d:%d", att.Kind, att.Target, att.StartLine, att.EndLine)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		attachments = append(attachments, att)
	}

	for _, tok := range tokens {
		if after, ok := strings.CutPrefix(tok.FullToken, "@mcp:"); ok {
			serverName, uri, found := strings.Cut(unquote(after), ":")
			serverName = strings.TrimSpace(serverName)
			uri = strings.TrimSpace(uri)
			if found && serverName != "" && uri != "" {
				target := serverName + ":" + uri
				var allowProhibited bool
				if trk, exists := byFullRef[tok.FullToken]; exists && trk.Kind == api.AttachmentMCP {
					target = trk.Target
					allowProhibited = trk.AllowProhibited
				} else if trk, exists := byMCPTarget[strings.ToLower(target)]; exists {
					target = trk.Target
					allowProhibited = trk.AllowProhibited
				}
				add(api.Attachment{
					Kind:            api.AttachmentMCP,
					Ref:             tok.FullToken,
					Target:          target,
					AllowProhibited: allowProhibited,
				})
			}
			continue
		}

		if trk, ok := byBaseRef[tok.BaseToken]; ok {
			add(api.Attachment{
				Kind:      trk.Kind,
				Ref:       tok.FullToken,
				Target:    trk.Target,
				StartLine: tok.StartLine,
				EndLine:   tok.EndLine,
			})
			continue
		}

		if after, ok := strings.CutPrefix(tok.BaseToken, "@file:"); ok {
			if rawPath := unquote(after); rawPath != "" {
				add(api.Attachment{
					Kind:      api.AttachmentFile,
					Ref:       tok.FullToken,
					Target:    cleanFileTarget(rawPath),
					StartLine: tok.StartLine,
					EndLine:   tok.EndLine,
				})
			}
			continue
		}

		if after, ok := strings.CutPrefix(tok.BaseToken, "@rule:"); ok {
			if ruleName := unquote(after); ruleName != "" {
				add(api.Attachment{
					Kind:   api.AttachmentRule,
					Ref:    tok.FullToken,
					Target: ruleName,
				})
			}
			continue
		}

		if after, ok := strings.CutPrefix(tok.BaseToken, "@skill:"); ok {
			if skillName := unquote(after); skillName != "" {
				add(api.Attachment{
					Kind:   api.AttachmentSkill,
					Ref:    tok.FullToken,
					Target: skillName,
				})
			}
			continue
		}

		if lookupFile == nil {
			continue
		}

		cand := unquote(strings.TrimPrefix(tok.BaseToken, "@"))
		if cand == "" {
			continue
		}
		if lookupCache == nil {
			lookupCache = make(map[string]string)
		}
		target, cached := lookupCache[cand]
		if !cached {
			if resolved, ok := lookupFile(cand); ok {
				target = resolved
			}
			lookupCache[cand] = target
		}
		if target != "" {
			add(api.Attachment{
				Kind:      api.AttachmentFile,
				Ref:       tok.FullToken,
				Target:    target,
				StartLine: tok.StartLine,
				EndLine:   tok.EndLine,
			})
		}
	}

	return attachments
}

// ScanTokens parses all valid @tokens outside fenced code blocks from text.
func ScanTokens(text string) []ScannedToken {
	var tokens []ScannedToken
	inFence := false
	fenceMarker := ""

	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = true
				fenceMarker = trimmed[:3]
				continue
			}
		} else {
			if strings.HasPrefix(trimmed, fenceMarker) {
				inFence = false
				fenceMarker = ""
			}
			continue
		}

		for i := 0; i < len(line); {
			if line[i] != '@' || (i > 0 && !isPrecedingDelimiter(line[i-1])) {
				i++
				continue
			}
			tok, next := parseAtToken(line, i)
			if tok.FullToken != "" {
				tokens = append(tokens, tok)
			}
			i = next
		}
	}

	return tokens
}

func parseAtToken(line string, start int) (ScannedToken, int) {
	n := len(line)
	curr := start + 1
	if curr >= n {
		return ScannedToken{}, curr
	}

	prefix := ""
	for _, p := range []string{"file:", "rule:", "skill:", "mcp:"} {
		if strings.HasPrefix(line[curr:], p) {
			prefix = p
			curr += len(p)
			break
		}
	}
	if curr >= n {
		return ScannedToken{}, curr
	}

	var body string
	quoted := line[curr] == '"' || line[curr] == '\''
	if quoted {
		q := line[curr]
		j := curr + 1
		for j < n && line[j] != q {
			if line[j] == '\\' && j+1 < n {
				j += 2
				continue
			}
			j++
		}
		if j >= n {
			return ScannedToken{}, curr + 1
		}
		body = line[curr : j+1]
		curr = j + 1
	} else {
		tokenStart := curr
		for curr < n && !isUnquotedStop(line[curr]) && line[curr] != '#' {
			curr++
		}
		body = line[tokenStart:curr]
	}

	var lineSuffix string
	var startLine, endLine int
	if curr < n && line[curr] == '#' {
		hashStart := curr
		for curr < n && !isUnquotedStop(line[curr]) {
			curr++
		}
		rawHash := line[hashStart:curr]
		trimmedHash := strings.TrimRight(rawHash, ".,;:!?)]}")
		if m := lineRangeRe.FindStringSubmatch(trimmedHash); len(m) > 1 {
			curr = hashStart + len(trimmedHash)
			lineSuffix = trimmedHash
			startLine, _ = strconv.Atoi(m[1])
			if len(m) > 2 && m[2] != "" {
				endLine, _ = strconv.Atoi(m[2])
			}
			if startLine > 0 && endLine > 0 && endLine < startLine {
				startLine, endLine = endLine, startLine
			}
		} else {
			body += rawHash
		}
	}

	if !quoted && lineSuffix == "" && !strings.HasSuffix(body, "/") {
		body = strings.TrimRight(body, ".,;:!?)]}")
	}
	if body == "" {
		return ScannedToken{}, curr
	}

	return ScannedToken{
		FullToken: "@" + prefix + body + lineSuffix,
		BaseToken: "@" + prefix + body,
		StartLine: startLine,
		EndLine:   endLine,
	}, curr
}

func cleanFileTarget(rawPath string) string {
	isDir := strings.HasSuffix(rawPath, "/") || rawPath == "."
	clean := filepath.ToSlash(filepath.Clean(rawPath))
	if isDir && !strings.HasSuffix(clean, "/") && clean != "." {
		clean += "/"
	}
	return clean
}

func stripLineRangeSuffix(ref string) string {
	if loc := lineRangeRe.FindStringIndex(ref); loc != nil {
		return ref[:loc[0]]
	}
	return ref
}

func isPrecedingDelimiter(b byte) bool {
	switch b {
	case ' ', '\t', '(', '[', '{', '"', '\'':
		return true
	default:
		return false
	}
}

func isUnquotedStop(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '`', '<', '>', '"', '\'':
		return true
	default:
		return false
	}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		inner := strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
		return strings.ReplaceAll(inner, `\'`, `'`)
	}
	return s
}
