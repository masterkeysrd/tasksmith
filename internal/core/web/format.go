package web

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Format identifies the output representation used when formatting fetched web
// content.
type Format string

const (
	// FormatMarkdown converts HTML to GitHub-Flavored Markdown and pretty-prints
	// JSON/XML responses. It is the default format.
	FormatMarkdown Format = "markdown"

	// FormatText converts HTML to readable plain text and pretty-prints
	// JSON/XML responses.
	FormatText Format = "text"

	// FormatHTML returns the raw response body unmodified.
	FormatHTML Format = "html"
)

// ParseFormat normalizes and validates a requested fetch output format.
// An empty string defaults to FormatMarkdown.
func ParseFormat(raw string) (Format, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return FormatMarkdown, nil
	}
	switch Format(normalized) {
	case FormatMarkdown:
		return FormatMarkdown, nil
	case FormatText:
		return FormatText, nil
	case FormatHTML:
		return FormatHTML, nil
	default:
		return "", fmt.Errorf("unsupported format %q (want %q, %q, or %q)", raw, FormatMarkdown, FormatText, FormatHTML)
	}
}

// FormatContent converts a fetched response body into the requested Format
// based on its Content-Type (or sniffed MIME type when Content-Type is missing
// or application/octet-stream).
func FormatContent(rawURL, contentType string, body []byte, format Format) string {
	if len(body) == 0 {
		return ""
	}
	if format == "" {
		format = FormatMarkdown
	}
	if format == FormatHTML {
		return string(body)
	}

	mimeType := resolveMIMEType(contentType, body)
	switch {
	case isHTMLMIME(mimeType):
		if format == FormatText {
			return HTMLToText(string(body))
		}
		return HTMLToMarkdown(string(body), rawURL)
	case isJSONMIME(mimeType):
		var buf bytes.Buffer
		if err := json.Indent(&buf, bytes.TrimSpace(body), "", "  "); err == nil {
			return buf.String()
		}
		return string(body)
	case isXMLMIME(mimeType):
		return formatXML(body)
	default:
		return string(body)
	}
}

// HTMLToMarkdown converts a full HTML document into clean GitHub-Flavored
// Markdown without cropping layout containers such as nav, header, footer, or
// aside. Relative links and images are resolved against baseURL when valid.
func HTMLToMarkdown(rawHTML string, baseURL string) string {
	return convertHTML(rawHTML, baseURL, false)
}

// HTMLToText converts a full HTML document into readable plain text without
// Markdown markup tags, preserving paragraph breaks, headings, and list bullets.
func HTMLToText(rawHTML string) string {
	return convertHTML(rawHTML, "", true)
}

func resolveMIMEType(contentType string, body []byte) string {
	var mimeType string
	if contentType != "" {
		if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
			mimeType = strings.ToLower(parsed)
		}
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		detected := http.DetectContentType(body)
		if parsed, _, err := mime.ParseMediaType(detected); err == nil {
			mimeType = strings.ToLower(parsed)
		}
	}
	return mimeType
}

func isHTMLMIME(mimeType string) bool {
	return mimeType == "text/html" || mimeType == "application/xhtml+xml"
}

func isJSONMIME(mimeType string) bool {
	return mimeType == "application/json" || mimeType == "text/json" || strings.HasSuffix(mimeType, "+json")
}

func isXMLMIME(mimeType string) bool {
	if mimeType == "application/xhtml+xml" {
		return false
	}
	return mimeType == "application/xml" || mimeType == "text/xml" || strings.HasSuffix(mimeType, "+xml")
}

func formatXML(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	dec := xml.NewDecoder(bytes.NewReader(trimmed))
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")

	seenElement := false
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return string(body)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			seenElement = true
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if len(bytes.TrimSpace(t)) == 0 {
				continue
			}
		}
		if err := enc.EncodeToken(tok); err != nil {
			return string(body)
		}
		if _, isProcInst := tok.(xml.ProcInst); isProcInst && depth == 0 {
			if err := enc.Flush(); err != nil {
				return string(body)
			}
			buf.WriteByte('\n')
		}
	}
	if !seenElement {
		return string(body)
	}
	if err := enc.Flush(); err != nil {
		return string(body)
	}
	return strings.TrimSpace(buf.String())
}

func convertHTML(rawHTML, baseURL string, textOnly bool) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return strings.TrimSpace(rawHTML)
	}

	title := extractTitle(doc)
	bodyNode := findFirstAtom(doc, atom.Body)
	if bodyNode == nil {
		bodyNode = doc
	}

	conv := &htmlConverter{
		baseURL:  strings.TrimSpace(baseURL),
		textOnly: textOnly,
	}
	conv.walkChildren(bodyNode)
	bodyOut := cleanOutput(conv.buf.String())

	if title != "" && !bodyStartsWithTitle(bodyOut, title, textOnly) {
		prefix := title
		if !textOnly {
			prefix = "# " + title
		}
		if bodyOut == "" {
			return prefix
		}
		return cleanOutput(prefix + "\n\n" + bodyOut)
	}
	return bodyOut
}

func bodyStartsWithTitle(bodyOut, title string, textOnly bool) bool {
	if bodyOut == "" {
		return false
	}
	firstLine, _, _ := strings.Cut(bodyOut, "\n")
	if !textOnly {
		if !strings.HasPrefix(firstLine, "# ") {
			return false
		}
		firstLine = strings.TrimPrefix(firstLine, "# ")
	}
	return strings.EqualFold(collapseWhitespace(firstLine), collapseWhitespace(title))
}

func extractTitle(doc *html.Node) string {
	titleNode := findFirstAtom(doc, atom.Title)
	if titleNode == nil {
		return ""
	}
	return collapseText(titleNode)
}

func findFirstAtom(n *html.Node, target atom.Atom) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.DataAtom == target {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirstAtom(child, target); found != nil {
			return found
		}
	}
	return nil
}

type htmlConverter struct {
	baseURL    string
	textOnly   bool
	listIndent string
	buf        strings.Builder
}

func (c *htmlConverter) subConverter() *htmlConverter {
	return &htmlConverter{
		baseURL:  c.baseURL,
		textOnly: c.textOnly,
	}
}

func (c *htmlConverter) walkChildren(n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.walk(child)
	}
}

func (c *htmlConverter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		c.writeCollapsedText(n.Data)
		return
	case html.ElementNode:
	default:
		return
	}

	if isIgnoredElement(n.DataAtom) {
		return
	}

	switch n.DataAtom {
	case atom.Head:
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		c.renderHeading(n)
	case atom.P:
		c.ensureBlankLine()
		c.walkChildren(n)
		c.ensureBlankLine()
	case atom.Br:
		c.trimTrailingSpacesOnLine()
		c.buf.WriteByte('\n')
	case atom.Hr:
		c.ensureBlankLine()
		if !c.textOnly {
			c.buf.WriteString("---")
			c.ensureBlankLine()
		}
	case atom.Pre:
		c.renderPre(n)
	case atom.Code, atom.Kbd, atom.Samp:
		c.renderInlineCode(n)
	case atom.Strong, atom.B:
		c.renderWrappedInline(n, "**", "**")
	case atom.Em, atom.I:
		c.renderWrappedInline(n, "*", "*")
	case atom.Del, atom.S:
		c.renderWrappedInline(n, "~~", "~~")
	case atom.A:
		c.renderLink(n)
	case atom.Img:
		c.renderImage(n)
	case atom.Ul:
		c.renderList(n, false)
	case atom.Ol:
		c.renderList(n, true)
	case atom.Li:
		// Bare <li> outside <ul>/<ol> falls back to a bullet item.
		c.renderListItem(n, "- ")
	case atom.Blockquote:
		c.renderBlockquote(n)
	case atom.Table:
		c.renderTable(n)
	case atom.Dl:
		c.ensureBlankLine()
		c.walkChildren(n)
		c.ensureBlankLine()
	case atom.Dt:
		c.renderDefinitionTerm(n)
	case atom.Dd:
		c.renderDefinitionDesc(n)
	case atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer,
		atom.Nav, atom.Aside, atom.Figure, atom.Figcaption, atom.Address,
		atom.Details, atom.Summary, atom.Fieldset, atom.Form:
		c.ensureNewline()
		c.walkChildren(n)
		c.ensureNewline()
	default:
		c.walkChildren(n)
	}
}

func isIgnoredElement(a atom.Atom) bool {
	switch a {
	case atom.Script, atom.Style, atom.Noscript, atom.Svg, atom.Canvas, atom.Template:
		return true
	default:
		return false
	}
}

func (c *htmlConverter) renderHeading(n *html.Node) {
	sub := c.subConverter()
	sub.walkChildren(n)
	text := collapseInlineLines(sub.buf.String())
	if text == "" {
		return
	}
	c.ensureBlankLine()
	if !c.textOnly {
		level := int(n.Data[1] - '0')
		c.buf.WriteString(strings.Repeat("#", level))
		c.buf.WriteByte(' ')
	}
	c.buf.WriteString(text)
	c.ensureBlankLine()
}

func (c *htmlConverter) renderPre(n *html.Node) {
	lang := detectCodeLanguage(n)
	code := extractPreText(n)
	code = strings.TrimRight(code, "\n")
	if code == "" {
		return
	}
	c.ensureBlankLine()
	if c.textOnly {
		c.buf.WriteString(code)
		c.ensureBlankLine()
		return
	}
	fence := codeFenceFor(code)
	c.buf.WriteString(fence)
	if lang != "" {
		c.buf.WriteString(lang)
	}
	c.buf.WriteByte('\n')
	c.buf.WriteString(code)
	c.buf.WriteByte('\n')
	c.buf.WriteString(fence)
	c.ensureBlankLine()
}

func detectCodeLanguage(pre *html.Node) string {
	// Check an inner <code> element first, since highlighters and Markdown
	// renderers commonly attach language-* classes there.
	for child := pre.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.DataAtom == atom.Code {
			if lang := languageFromClass(attrValue(child, "class")); lang != "" {
				return lang
			}
		}
	}
	return languageFromClass(attrValue(pre, "class"))
}

func languageFromClass(classAttr string) string {
	for cls := range strings.FieldsSeq(classAttr) {
		if after, ok := strings.CutPrefix(cls, "language-"); ok && after != "" {
			return after
		}
		if after, ok := strings.CutPrefix(cls, "highlight-"); ok && after != "" {
			return after
		}
	}
	return ""
}

func extractPreText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		switch node.Type {
		case html.TextNode:
			sb.WriteString(strings.ReplaceAll(node.Data, "\r\n", "\n"))
		case html.ElementNode:
			if isIgnoredElement(node.DataAtom) {
				return
			}
			if node.DataAtom == atom.Br {
				sb.WriteByte('\n')
				return
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
	}
	walk(n)
	return sb.String()
}

func codeFenceFor(code string) string {
	maxRun := 0
	curRun := 0
	for i := 0; i < len(code); i++ {
		if code[i] == '`' {
			curRun++
			if curRun > maxRun {
				maxRun = curRun
			}
		} else {
			curRun = 0
		}
	}
	if maxRun < 3 {
		return "```"
	}
	return strings.Repeat("`", maxRun+1)
}

func (c *htmlConverter) renderInlineCode(n *html.Node) {
	sub := c.subConverter()
	sub.walkChildren(n)
	text := strings.TrimSpace(collapseInlineLines(sub.buf.String()))
	if text == "" {
		return
	}
	if c.textOnly {
		c.buf.WriteString(text)
		return
	}
	delim := "`"
	if strings.Contains(text, "`") {
		delim = "``"
		if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
			text = " " + text + " "
		}
	}
	c.buf.WriteString(delim)
	c.buf.WriteString(text)
	c.buf.WriteString(delim)
}

func (c *htmlConverter) renderWrappedInline(n *html.Node, prefix, suffix string) {
	sub := c.subConverter()
	sub.walkChildren(n)
	raw := sub.buf.String()
	if strings.TrimSpace(raw) == "" {
		return
	}
	if c.textOnly {
		c.buf.WriteString(raw)
		return
	}
	hasLeadingSpace := len(raw) > 0 && raw[0] == ' '
	hasTrailingSpace := len(raw) > 0 && raw[len(raw)-1] == ' '
	inner := strings.TrimSpace(raw)
	if inner == "" {
		return
	}
	if hasLeadingSpace && !c.atLineStart() {
		c.buf.WriteByte(' ')
	}
	c.buf.WriteString(prefix)
	c.buf.WriteString(inner)
	c.buf.WriteString(suffix)
	if hasTrailingSpace {
		c.buf.WriteByte(' ')
	}
}

func (c *htmlConverter) renderLink(n *html.Node) {
	sub := c.subConverter()
	sub.walkChildren(n)
	text := collapseInlineLines(sub.buf.String())
	if text == "" {
		return
	}
	if c.textOnly {
		c.buf.WriteString(text)
		return
	}
	href := resolveURL(c.baseURL, attrValue(n, "href"))
	if href == "" {
		c.buf.WriteString(text)
		return
	}
	c.buf.WriteByte('[')
	c.buf.WriteString(text)
	c.buf.WriteString("](")
	c.buf.WriteString(href)
	c.buf.WriteByte(')')
}

func (c *htmlConverter) renderImage(n *html.Node) {
	alt := collapseWhitespace(attrValue(n, "alt"))
	if c.textOnly {
		if alt != "" {
			c.buf.WriteString(alt)
		}
		return
	}
	src := resolveURL(c.baseURL, attrValue(n, "src"))
	if src == "" {
		return
	}
	c.buf.WriteString("![")
	c.buf.WriteString(alt)
	c.buf.WriteString("](")
	c.buf.WriteString(src)
	c.buf.WriteByte(')')
}

func (c *htmlConverter) renderList(n *html.Node, ordered bool) {
	isTopLevel := c.listIndent == ""
	if isTopLevel {
		c.ensureBlankLine()
	} else {
		c.ensureNewline()
	}

	itemIdx := 0
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.DataAtom != atom.Li {
			continue
		}
		itemIdx++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", itemIdx)
		}
		c.renderListItem(child, marker)
	}

	if isTopLevel {
		c.ensureBlankLine()
	} else {
		c.ensureNewline()
	}
}

func (c *htmlConverter) renderListItem(li *html.Node, marker string) {
	contIndent := c.listIndent + strings.Repeat(" ", len(marker))

	c.ensureNewline()
	c.buf.WriteString(c.listIndent)
	c.buf.WriteString(marker)

	sub := c.subConverter()
	hasContent := false

	flushSub := func() {
		if sub.buf.Len() == 0 {
			return
		}
		part := cleanOutput(sub.buf.String())
		sub.buf.Reset()
		if part == "" {
			return
		}
		indented := strings.ReplaceAll(part, "\n", "\n"+contIndent)
		if hasContent {
			c.ensureNewline()
			c.buf.WriteString(contIndent)
		}
		c.buf.WriteString(indented)
		hasContent = true
	}

	for child := li.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.DataAtom == atom.Ul || child.DataAtom == atom.Ol) {
			flushSub()
			nested := c.subConverter()
			nested.listIndent = contIndent
			nested.walk(child)
			nestedOut := strings.Trim(nested.buf.String(), "\n")
			if nestedOut != "" {
				c.ensureNewline()
				c.buf.WriteString(nestedOut)
				hasContent = true
			}
			continue
		}
		sub.walk(child)
	}
	flushSub()
	c.ensureNewline()
}

func (c *htmlConverter) renderBlockquote(n *html.Node) {
	sub := c.subConverter()
	sub.walkChildren(n)
	inner := cleanOutput(sub.buf.String())
	if inner == "" {
		return
	}
	c.ensureBlankLine()
	if c.textOnly {
		c.buf.WriteString(inner)
		c.ensureBlankLine()
		return
	}
	lines := strings.Split(inner, "\n")
	for i, line := range lines {
		if i > 0 {
			c.buf.WriteByte('\n')
		}
		if line == "" {
			c.buf.WriteByte('>')
		} else {
			c.buf.WriteString("> ")
			c.buf.WriteString(line)
		}
	}
	c.ensureBlankLine()
}

func (c *htmlConverter) renderTable(table *html.Node) {
	var rows [][]*html.Node
	var collectRows func(*html.Node)
	collectRows = func(n *html.Node) {
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode {
				continue
			}
			switch child.DataAtom {
			case atom.Table:
				// Do not absorb rows from nested tables.
				continue
			case atom.Tr:
				var cells []*html.Node
				for cell := child.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type == html.ElementNode && (cell.DataAtom == atom.Th || cell.DataAtom == atom.Td) {
						cells = append(cells, cell)
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			case atom.Thead, atom.Tbody, atom.Tfoot:
				collectRows(child)
			}
		}
	}
	collectRows(table)

	if len(rows) == 0 {
		return
	}

	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return
	}

	renderedRows := make([][]string, len(rows))
	for i, r := range rows {
		rowText := make([]string, maxCols)
		for j := 0; j < maxCols; j++ {
			if j >= len(r) {
				continue
			}
			sub := c.subConverter()
			sub.walkChildren(r[j])
			cell := collapseInlineLines(sub.buf.String())
			if !c.textOnly {
				cell = strings.ReplaceAll(cell, "|", `\|`)
			}
			rowText[j] = cell
		}
		renderedRows[i] = rowText
	}

	c.ensureBlankLine()
	if c.textOnly {
		for i, row := range renderedRows {
			if i > 0 {
				c.buf.WriteByte('\n')
			}
			c.buf.WriteString(strings.Join(row, "\t"))
		}
		c.ensureBlankLine()
		return
	}

	writePipeRow := func(cells []string) {
		c.buf.WriteString("| ")
		c.buf.WriteString(strings.Join(cells, " | "))
		c.buf.WriteString(" |")
	}

	writePipeRow(renderedRows[0])
	c.buf.WriteByte('\n')
	sep := make([]string, maxCols)
	for i := range sep {
		sep[i] = "---"
	}
	writePipeRow(sep)
	for _, row := range renderedRows[1:] {
		c.buf.WriteByte('\n')
		writePipeRow(row)
	}
	c.ensureBlankLine()
}

func (c *htmlConverter) renderDefinitionTerm(n *html.Node) {
	sub := c.subConverter()
	sub.walkChildren(n)
	text := collapseInlineLines(sub.buf.String())
	if text == "" {
		return
	}
	c.ensureNewline()
	if c.textOnly {
		c.buf.WriteString(text)
	} else {
		c.buf.WriteString("**")
		c.buf.WriteString(text)
		c.buf.WriteString("**")
	}
	c.buf.WriteByte('\n')
}

func (c *htmlConverter) renderDefinitionDesc(n *html.Node) {
	sub := c.subConverter()
	sub.walkChildren(n)
	text := collapseInlineLines(sub.buf.String())
	if text == "" {
		return
	}
	c.ensureNewline()
	if c.textOnly {
		c.buf.WriteString("  ")
		c.buf.WriteString(text)
	} else {
		c.buf.WriteString(": ")
		c.buf.WriteString(text)
	}
	c.buf.WriteByte('\n')
}

func (c *htmlConverter) writeCollapsedText(raw string) {
	if raw == "" {
		return
	}
	collapsed := collapsePreservingEdges(raw)
	if collapsed == "" {
		return
	}
	if collapsed == " " {
		if c.atLineStart() || c.endsWithSpace() {
			return
		}
		c.buf.WriteByte(' ')
		return
	}
	if strings.HasPrefix(collapsed, " ") && (c.atLineStart() || c.endsWithSpace()) {
		collapsed = collapsed[1:]
	}
	c.buf.WriteString(collapsed)
}

func (c *htmlConverter) atLineStart() bool {
	s := c.buf.String()
	return s == "" || strings.HasSuffix(s, "\n")
}

func (c *htmlConverter) endsWithSpace() bool {
	s := c.buf.String()
	return strings.HasSuffix(s, " ")
}

func (c *htmlConverter) trimTrailingSpacesOnLine() {
	s := c.buf.String()
	trimmed := strings.TrimRight(s, " \t")
	if len(trimmed) != len(s) {
		c.buf.Reset()
		c.buf.WriteString(trimmed)
	}
}

func (c *htmlConverter) ensureNewline() {
	c.trimTrailingSpacesOnLine()
	s := c.buf.String()
	if s == "" || strings.HasSuffix(s, "\n") {
		return
	}
	c.buf.WriteByte('\n')
}

func (c *htmlConverter) ensureBlankLine() {
	c.trimTrailingSpacesOnLine()
	s := c.buf.String()
	if s == "" || strings.HasSuffix(s, "\n\n") {
		return
	}
	if strings.HasSuffix(s, "\n") {
		c.buf.WriteByte('\n')
		return
	}
	c.buf.WriteString("\n\n")
}

func resolveURL(baseURL, rawRef string) string {
	refStr := strings.TrimSpace(rawRef)
	if refStr == "" {
		return ""
	}
	ref, err := url.Parse(refStr)
	if err != nil {
		return ""
	}
	switch ref.Scheme {
	case "", "http", "https", "mailto":
	default:
		return ""
	}
	if baseURL == "" {
		return refStr
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" {
		return refStr
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return refStr
	}
	return base.ResolveReference(ref).String()
}

func collapsePreservingEdges(s string) string {
	var sb strings.Builder
	inSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace {
				sb.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		sb.WriteRune(r)
		inSpace = false
	}
	return sb.String()
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func collapseInlineLines(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func cleanOutput(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var out []string
	blankRun := 0
	var activeFence string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if activeFence != "" {
			out = append(out, line)
			if strings.HasPrefix(trimmed, activeFence) && strings.Trim(trimmed, "`") == "" {
				activeFence = ""
			}
			continue
		}
		if fence := leadingFence(trimmed); fence != "" {
			blankRun = 0
			activeFence = fence
			out = append(out, strings.TrimRight(line, " \t"))
			continue
		}
		trimmedRight := strings.TrimRight(line, " \t")
		if trimmedRight == "" {
			blankRun++
			if blankRun <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blankRun = 0
		out = append(out, trimmedRight)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func leadingFence(trimmedLine string) string {
	if !strings.HasPrefix(trimmedLine, "```") {
		return ""
	}
	count := 0
	for count < len(trimmedLine) && trimmedLine[count] == '`' {
		count++
	}
	return trimmedLine[:count]
}
