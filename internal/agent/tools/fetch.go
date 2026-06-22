package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	htmlmarkdown "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/fs"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/html"
)

const (
	fetchDialTimeout         = 10 * time.Second
	fetchMaxIdleConns        = 100
	fetchMaxIdleConnsPerHost = 10
	fetchIdleConnTimeout     = 90 * time.Second
	fetchClientTimeout       = 30 * time.Second
	maxFetchTimeoutSeconds   = 120

	fetchUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// Fetch fetches a URL.
func (h *ToolHandlers) Fetch(ctx context.Context, in FetchArgs) (FetchOutput, error) {
	format := strings.ToLower(in.Format)
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "markdown" && format != "html" {
		return FetchOutput{}, fmt.Errorf("invalid format: must be one of text, markdown, html")
	}

	// 1. Determine timeout context
	timeoutSec := in.Timeout
	if timeoutSec <= 0 {
		timeoutSec = int(fetchClientTimeout / time.Second)
	} else if timeoutSec > maxFetchTimeoutSeconds {
		timeoutSec = maxFetchTimeoutSeconds
	}

	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Configure uTLS client (Negotiating ALPN fallback to match standard http.Transport expectations)
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: fetchDialTimeout,
			}
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}

			config := &utls.Config{
				ServerName:         host,
				InsecureSkipVerify: false,
			}
			uconn := utls.UClient(conn, config, utls.HelloCustom)

			spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
			if err != nil {
				conn.Close()
				return nil, err
			}

			// Force HTTP/1.1 ALPN
			for _, ext := range spec.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"}
				}
			}

			err = uconn.ApplyPreset(&spec)
			if err != nil {
				conn.Close()
				return nil, err
			}

			err = uconn.Handshake()
			if err != nil {
				conn.Close()
				return nil, err
			}
			return uconn, nil
		},
		TLSNextProto:        make(map[string]func(string, *tls.Conn) http.RoundTripper),
		MaxIdleConns:        fetchMaxIdleConns,
		MaxIdleConnsPerHost: fetchMaxIdleConnsPerHost,
		IdleConnTimeout:     fetchIdleConnTimeout,
	}

	client := &http.Client{
		Timeout:   0, // We control timeout via the context WithTimeout
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(requestCtx, "GET", in.Url, nil)
	if err != nil {
		return FetchOutput{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Browser spoof headers
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(req)
	if err != nil {
		return FetchOutput{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return FetchOutput{Status: resp.StatusCode}, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check UTF-8 validity
	if !utf8.Valid(bodyBytes) {
		return FetchOutput{}, fmt.Errorf("failed to fetch: response content is not valid UTF-8. Use the 'download' tool to save binary files")
	}

	contentType := resp.Header.Get("Content-Type")
	mimeType := "application/octet-stream"
	if contentType != "" {
		if m, _, err := mime.ParseMediaType(contentType); err == nil {
			mimeType = m
		}
	}
	if mimeType == "application/octet-stream" || mimeType == "" {
		detected := http.DetectContentType(bodyBytes)
		if detected != "" {
			if m, _, err := mime.ParseMediaType(detected); err == nil {
				mimeType = m
			}
		}
	}

	isBinary := fs.IsBinaryMIME(mimeType)
	if mimeType == "application/xml" || mimeType == "text/xml" || strings.HasSuffix(mimeType, "+xml") {
		isBinary = false
	}

	if isBinary {
		return FetchOutput{}, fmt.Errorf("failed to fetch: content is binary (%s). Use the 'download' tool to save binary files", mimeType)
	}

	rawContent := string(bodyBytes)
	content := rawContent

	// Process text types: JSON, XML, HTML, plain text
	if mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") {
		var jsonObj any
		if err := json.Unmarshal(bodyBytes, &jsonObj); err == nil {
			if formatted, err := json.MarshalIndent(jsonObj, "", "  "); err == nil {
				content = string(formatted)
			}
		}
	} else if mimeType == "application/xml" || mimeType == "text/xml" || strings.HasSuffix(mimeType, "+xml") {
		var indentBuf bytes.Buffer
		dec := xml.NewDecoder(bytes.NewReader(bodyBytes))
		enc := xml.NewEncoder(&indentBuf)
		enc.Indent("", "  ")
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			_ = enc.EncodeToken(tok)
		}
		_ = enc.Flush()
		if indentBuf.Len() > 0 {
			content = indentBuf.String()
		}
	} else if strings.Contains(mimeType, "text/html") {
		switch format {
		case "text":
			text, err := extractTextFromHTML(rawContent)
			if err != nil {
				return FetchOutput{}, fmt.Errorf("failed to extract text from HTML: %w", err)
			}
			content = text
		case "markdown":
			markdown, err := convertHTMLToMarkdown(rawContent)
			if err != nil {
				return FetchOutput{}, fmt.Errorf("failed to convert HTML to Markdown: %w", err)
			}
			content = markdown
		case "html":
			bodyHTML, err := extractBodyFromHTML(rawContent)
			if err != nil {
				return FetchOutput{}, fmt.Errorf("failed to extract body from HTML: %w", err)
			}
			content = bodyHTML
		}
	}

	var truncated bool
	var cachedPath string

	if len(content) > MaxTotalChars {
		truncated = true
		rawContentCopy := content // save full processed content
		content = content[:MaxTotalChars]

		if h.Storage != nil {
			filename := filepath.Base(in.Url)
			if filename == "" || filename == "." || filename == "/" {
				filename = "fetch_output.txt"
			}
			if idx := strings.Index(filename, "?"); idx != -1 {
				filename = filename[:idx]
			}
			switch format {
			case "markdown":
				if !strings.HasSuffix(filename, ".md") {
					filename += ".md"
				}
			case "html":
				if !strings.HasSuffix(filename, ".html") {
					filename += ".html"
				}
			default:
				if !strings.HasSuffix(filename, ".txt") && !strings.HasSuffix(filename, ".json") && !strings.HasSuffix(filename, ".xml") {
					filename += ".txt"
				}
			}
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = "unknown"
			}
			storagePath := fmt.Sprintf("%s_%s", toolCallID, filename)
			var errSave error
			cachedPath, errSave = h.Storage.Save(ctx, storagePath, strings.NewReader(rawContentCopy))
			if errSave != nil {
				return FetchOutput{}, fmt.Errorf("failed to cache truncated content: %w", errSave)
			}
		}
	}

	return FetchOutput{
		Status:     resp.StatusCode,
		Content:    content,
		Truncated:  truncated,
		CachedPath: cachedPath,
	}, nil
}

// ToolContent implements the loom tool.ContentProvider interface.
func (f FetchOutput) ToolContent() message.Content {
	var sb strings.Builder
	sb.WriteString(f.Content)

	if f.Truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: Content truncated due to size limits. Full content saved to cache. To read further, use the 'view' tool or 'grep' tool with source=%s]", f.CachedPath)
	}

	return message.Content{
		&message.TextBlock{
			Text: sb.String(),
		},
	}
}

// GetFileCacheMetadata satisfies the FileCacheProvider interface.
func (f FetchOutput) GetFileCacheMetadata() []FileCacheMetadata {
	if f.CachedPath != "" {
		return []FileCacheMetadata{
			{
				Source:     f.CachedPath,
				CachedPath: f.CachedPath,
			},
		}
	}
	return nil
}

// Helpers for HTML content formats

var voidTags = map[string]bool{
	"area":   true,
	"base":   true,
	"br":     true,
	"col":    true,
	"embed":  true,
	"hr":     true,
	"img":    true,
	"input":  true,
	"link":   true,
	"meta":   true,
	"param":  true,
	"source": true,
	"track":  true,
	"wbr":    true,
}

func isVoidTag(tag string) bool {
	return voidTags[strings.ToLower(tag)]
}

var blockTags = map[string]bool{
	"address":    true,
	"article":    true,
	"aside":      true,
	"blockquote": true,
	"canvas":     true,
	"dd":         true,
	"div":        true,
	"dl":         true,
	"dt":         true,
	"fieldset":   true,
	"figcaption": true,
	"figure":     true,
	"footer":     true,
	"form":       true,
	"h1":         true,
	"h2":         true,
	"h3":         true,
	"h4":         true,
	"h5":         true,
	"h6":         true,
	"header":     true,
	"hr":         true,
	"li":         true,
	"main":       true,
	"nav":        true,
	"noscript":   true,
	"ol":         true,
	"p":          true,
	"pre":        true,
	"section":    true,
	"table":      true,
	"tfoot":      true,
	"ul":         true,
	"video":      true,
	"br":         true,
}

func isBlockTag(tag string) bool {
	return blockTags[strings.ToLower(tag)]
}

func prettyPrintHTML(n *html.Node, depth int) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	indent := strings.Repeat("  ", depth)

	switch n.Type {
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			sb.WriteString(prettyPrintHTML(c, depth))
		}
	case html.ElementNode:
		tagName := strings.ToLower(n.Data)
		var attrs []string
		for _, a := range n.Attr {
			attrs = append(attrs, fmt.Sprintf("%s=%q", a.Key, a.Val))
		}
		attrStr := ""
		if len(attrs) > 0 {
			attrStr = " " + strings.Join(attrs, " ")
		}

		if isVoidTag(tagName) {
			sb.WriteString(fmt.Sprintf("%s<%s%s />\n", indent, tagName, attrStr))
		} else {
			sb.WriteString(fmt.Sprintf("%s<%s%s>", indent, tagName, attrStr))
			hasSingleTextChild := n.FirstChild != nil && n.FirstChild.NextSibling == nil && n.FirstChild.Type == html.TextNode
			if hasSingleTextChild {
				sb.WriteString(strings.TrimSpace(n.FirstChild.Data))
				sb.WriteString(fmt.Sprintf("</%s>\n", tagName))
			} else {
				sb.WriteString("\n")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" {
						continue
					}
					sb.WriteString(prettyPrintHTML(c, depth+1))
				}
				sb.WriteString(fmt.Sprintf("%s</%s>\n", indent, tagName))
			}
		}
	case html.TextNode:
		txt := strings.TrimSpace(n.Data)
		if txt != "" {
			sb.WriteString(fmt.Sprintf("%s%s\n", indent, txt))
		}
	case html.CommentNode:
		sb.WriteString(fmt.Sprintf("%s<!-- %s -->\n", indent, strings.TrimSpace(n.Data)))
	case html.DoctypeNode:
		sb.WriteString(fmt.Sprintf("<!DOCTYPE %s>\n", n.Data))
	}
	return sb.String()
}

func extractTextFromHTML(htmlContent string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.TextNode {
			txt := strings.TrimSpace(n.Data)
			if txt != "" {
				fields := strings.Fields(txt)
				sb.WriteString(strings.Join(fields, " "))
				sb.WriteString(" ")
			}
			return
		}

		tagName := strings.ToLower(n.Data)
		isBlock := isBlockTag(tagName)

		if isBlock && sb.Len() > 0 {
			curr := sb.String()
			if !strings.HasSuffix(curr, "\n") && !strings.HasSuffix(curr, "\n\n") {
				sb.WriteString("\n")
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				cTagName := strings.ToLower(c.Data)
				if cTagName == "script" || cTagName == "style" {
					continue
				}
			}
			walk(c)
		}

		if isBlock && sb.Len() > 0 {
			curr := sb.String()
			if !strings.HasSuffix(curr, "\n") && !strings.HasSuffix(curr, "\n\n") {
				sb.WriteString("\n")
			}
		}
	}

	bodySelection := doc.Find("body")
	if bodySelection.Length() > 0 {
		walk(bodySelection.Get(0))
	} else if len(doc.Nodes) > 0 {
		walk(doc.Nodes[0])
	}

	lines := strings.Split(sb.String(), "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		} else if len(cleaned) > 0 && cleaned[len(cleaned)-1] != "" {
			cleaned = append(cleaned, "")
		}
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n")), nil
}

func convertHTMLToMarkdown(htmlContent string) (string, error) {
	converter := htmlmarkdown.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(htmlContent)
	if err != nil {
		return "", err
	}
	return markdown, nil
}

func extractBodyFromHTML(htmlContent string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}
	bodySelection := doc.Find("body")
	var node *html.Node
	if bodySelection.Length() > 0 {
		node = bodySelection.Get(0)
	} else if len(doc.Nodes) > 0 {
		node = doc.Nodes[0]
	}
	if node == nil {
		return "<html>\n<body>\n</body>\n</html>", nil
	}
	formatted := prettyPrintHTML(node, 0)
	return strings.TrimSpace(formatted), nil
}
