package std

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

const editTabWidth = 4

type editFile struct {
	raw, text string
	crlf, lf  []int
}

func normalizeEditFile(raw string) editFile {
	f := editFile{raw: raw, text: raw}
	if !strings.Contains(raw, "\r\n") {
		return f
	}
	var b strings.Builder
	b.Grow(len(raw))
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\n' {
			continue
		}
		if i > 0 && raw[i-1] == '\r' {
			b.WriteString(raw[start : i-1])
			f.crlf = append(f.crlf, b.Len())
			b.WriteByte('\n')
		} else {
			b.WriteString(raw[start:i])
			f.lf = append(f.lf, b.Len())
			b.WriteByte('\n')
		}
		start = i + 1
	}
	b.WriteString(raw[start:])
	f.text = b.String()
	return f
}

func (f editFile) rawOffset(offset int) int {
	return offset + sort.SearchInts(f.crlf, offset)
}

func (f editFile) replacement(start, end int, text string) string {
	if len(f.crlf) == 0 || !strings.Contains(text, "\n") {
		return text
	}
	if len(f.lf) == 0 {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	c, l := sort.SearchInts(f.crlf, start), sort.SearchInts(f.lf, start)
	crlf := c > 0 && (l == 0 || f.crlf[c-1] > f.lf[l-1])
	if c < len(f.crlf) || l < len(f.lf) {
		crlf = c < len(f.crlf) && (l == len(f.lf) || f.crlf[c] < f.lf[l])
	}
	var b strings.Builder
	for {
		line, rest, hasNewline := strings.Cut(text, "\n")
		b.WriteString(line)
		if !hasNewline {
			return b.String()
		}
		if c < len(f.crlf) && f.crlf[c] < end && (l == len(f.lf) || f.crlf[c] < f.lf[l]) {
			crlf = true
			c++
		} else if l < len(f.lf) && f.lf[l] < end {
			crlf = false
			l++
		}
		if crlf {
			b.WriteByte('\r')
		}
		b.WriteByte('\n')
		text = rest
	}
}

type editSpan struct {
	start, end int
	text       string
}

func (f editFile) splice(spans []editSpan) string {
	size := len(f.raw)
	for i := range spans {
		s := &spans[i]
		s.text = f.replacement(s.start, s.end, s.text)
		s.start, s.end = f.rawOffset(s.start), f.rawOffset(s.end)
		size += len(s.text) - (s.end - s.start)
	}
	out := make([]byte, size)
	end := len(f.raw)
	for _, s := range slices.Backward(spans) {
		size -= end - s.end
		copy(out[size:], f.raw[s.end:end])
		size -= len(s.text)
		copy(out[size:], s.text)
		end = s.start
	}
	copy(out, f.raw[:end])
	return string(out)
}

func smartReplace(raw, oldText, newText string, replaceAll bool) (string, int, error) {
	f := normalizeEditFile(raw)
	oldText = strings.ReplaceAll(oldText, "\r\n", "\n")
	newText = strings.ReplaceAll(newText, "\r\n", "\n")
	count := strings.Count(f.text, oldText)
	if count > 0 {
		if count > 1 && !replaceAll {
			return raw, count, ErrAmbiguousEdit
		}
		if len(f.crlf) == 0 {
			return strings.Replace(f.text, oldText, newText, count), count, nil
		}
		spans := make([]editSpan, 0, count)
		for start := 0; len(spans) < count; {
			start += strings.Index(f.text[start:], oldText)
			spans = append(spans, editSpan{start, start + len(oldText), newText})
			start += len(oldText)
		}
		return f.splice(spans), count, nil
	}

	target := splitEditLines(oldText)
	first := slices.IndexFunc(target, func(l editLine) bool { return l.body != "" })
	if first < 0 {
		return raw, 0, ErrTargetNotFound
	}
	lines := splitEditLines(f.text)
	if len(lines) < len(target) {
		return raw, 0, ErrTargetNotFound
	}
	_, targetTabs := blockIndent(target)
	trailingLF := strings.HasSuffix(oldText, "\n")
	for _, stage := range []editStage{editTrailing, editIndent, editFuzzy} {
		if stage == editFuzzy && (len(target) < 3 || len(target) > 256 || strings.TrimSpace(newText) == "") {
			break
		}
		var spans []editSpan
		var invalidIndent bool
		for i := 0; i <= len(lines)-len(target); i++ {
			last := i + len(target) - 1
			start, end := lines[i].start, lines[last].end
			if trailingLF {
				if end == len(f.text) {
					continue
				}
				end++
			}
			if replaceAll && len(spans) != 0 && start < spans[len(spans)-1].end {
				continue
			}
			window := lines[i : last+1]
			shift := window[first].width - target[first].width
			if !matchEditLines(stage, target, window, shift) {
				continue
			}
			replacement := newText
			if stage != editTrailing {
				base, tabs := blockIndent(window)
				if shift != 0 || tabs != targetTabs {
					var ok bool
					replacement, ok = rebaseEditIndent(newText, shift, base, tabs)
					invalidIndent = invalidIndent || !ok
				}
			}
			spans = append(spans, editSpan{start, end, replacement})
		}
		if len(spans) > 1 && !replaceAll {
			return raw, len(spans), ErrAmbiguousEdit
		}
		if invalidIndent {
			return raw, 0, fmt.Errorf("%w: replacement indentation cannot be shifted to the matched block", ErrTargetNotFound)
		}
		if len(spans) > 0 {
			return f.splice(spans), len(spans), nil
		}
	}
	return raw, 0, ErrTargetNotFound
}

type editStage uint8

const (
	editTrailing editStage = iota
	editIndent
	editFuzzy
)

type editLine struct {
	start, end, width   int
	trimmed, lead, body string
}

func splitEditLines(text string) []editLine {
	lines := make([]editLine, 0, strings.Count(text, "\n")+1)
	start := 0
	for line := range strings.SplitSeq(text, "\n") {
		if start == len(text) {
			break
		}
		trimmed := strings.TrimRight(line, " \t")
		body := strings.TrimLeft(trimmed, " \t")
		lead := trimmed[:len(trimmed)-len(body)]
		end := start + len(line)
		lines = append(lines, editLine{start, end, indentWidth(lead), trimmed, lead, body})
		start = end + 1
	}
	return lines
}

func indentWidth(lead string) int {
	return len(lead) + (editTabWidth-1)*strings.Count(lead, "\t")
}

func blockIndent(lines []editLine) (base string, tabs bool) {
	width := -1
	for _, l := range lines {
		if l.body == "" {
			continue
		}
		if width < 0 || l.width < width {
			base, width = l.lead, l.width
		}
		tabs = tabs || strings.Contains(l.lead, "\t")
	}
	return base, tabs
}

func rebaseEditIndent(text string, shift int, base string, tabs bool) (string, bool) {
	lines := strings.Split(text, "\n")
	baseWidth := indentWidth(base)
	for i, line := range lines {
		body := strings.TrimLeft(line, " \t")
		if body == "" {
			continue
		}
		width := indentWidth(line[:len(line)-len(body)]) + shift
		if width < 0 {
			return "", false
		}
		prefix := ""
		if width >= baseWidth {
			prefix = base
			width -= baseWidth
		}
		if tabs {
			prefix += strings.Repeat("\t", width/editTabWidth)
			width %= editTabWidth
		}
		lines[i] = prefix + strings.Repeat(" ", width) + body
	}
	return strings.Join(lines, "\n"), true
}

func matchEditLines(stage editStage, target, window []editLine, shift int) bool {
	if stage == editFuzzy {
		first, last := target[0].body, target[len(target)-1].body
		if first == "" || last == "" || first != window[0].body || last != window[len(window)-1].body ||
			(strings.IndexFunc(first, isEditAnchor) < 0 && strings.IndexFunc(last, isEditAnchor) < 0) {
			return false
		}
	}
	changed := 0
	for i, want := range target {
		got := window[i]
		if stage == editTrailing {
			if got.trimmed != want.trimmed {
				return false
			}
			continue
		}
		if want.body == "" && got.body == "" {
			continue
		}
		if got.width-want.width != shift {
			return false
		}
		if got.body != want.body {
			changed++
			if stage != editFuzzy || changed > max(1, len(target)/10) || !similarEditLine(want.body, got.body) {
				return false
			}
		}
	}
	return stage != editFuzzy || (changed != 0 && alignedEditLines(target, window))
}

func isEditAnchor(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func similarEditLine(want, got string) bool {
	if len(want) > 512 || len(got) > 512 {
		return false
	}
	a, b := []rune(want), []rune(got)
	limit := min(2, max(len(a), len(b))/10)
	if limit == 0 || max(len(a), len(b))-min(len(a), len(b)) > limit {
		return false
	}
	for len(a) != 0 && len(b) != 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) != 0 && len(b) != 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	for _, text := range [][]rune{a, b} {
		for _, r := range text {
			if !unicode.IsLetter(r) {
				return false
			}
		}
	}
	return strutils.Levenshtein(string(a), string(b)) <= limit
}

func alignedEditLines(target, window []editLine) bool {
	a, b := make([]string, len(target)), make([]string, len(window))
	for i := range target {
		a[i], b[i] = target[i].body, window[i].body
	}
	i, j := 0, 0
	for _, op := range diff.MyersDiff(a, b) {
		switch op.Op {
		case diff.OpEqual:
			if i != j {
				return false
			}
			i++
			j++
		case diff.OpDelete:
			i++
		case diff.OpInsert:
			j++
		}
	}
	return i == len(a) && j == len(b)
}
