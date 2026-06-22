package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	htmlmarkdown "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/fs"
	utls "github.com/refraction-networking/utls"
)

// WebFetch fetches web page content.
func (h *ToolHandlers) WebFetch(ctx context.Context, in WebFetchArgs) (WebFetchOutput, error) {
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: 10 * time.Second,
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

			// Force HTTP/1.1 ALPN to match standard http.Transport expectations
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
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", in.Url, nil)
	if err != nil {
		return WebFetchOutput{}, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Ch-Ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Sec-Fetch-Dest", "document")

	resp, err := client.Do(req)
	if err != nil {
		return WebFetchOutput{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return WebFetchOutput{}, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return WebFetchOutput{}, fmt.Errorf("failed to read response body: %w", err)
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

	// Decide if binary
	isBinary := fs.IsBinaryMIME(mimeType)
	if mimeType == "application/xml" || mimeType == "text/xml" || strings.HasSuffix(mimeType, "+xml") {
		isBinary = false
	}

	// Caching logic for binary files
	var cachedPath string
	if isBinary {
		if h.Storage != nil {
			filename := filepath.Base(in.Url)
			if filename == "" || filename == "." || filename == "/" {
				filename = "download"
			}
			if idx := strings.Index(filename, "?"); idx != -1 {
				filename = filename[:idx]
			}
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = "unknown"
			}
			storagePath := fmt.Sprintf("%s_%s", toolCallID, filename)
			var errSave error
			cachedPath, errSave = h.Storage.Save(ctx, storagePath, bytes.NewReader(bodyBytes))
			if errSave != nil {
				return WebFetchOutput{}, fmt.Errorf("failed to cache binary file: %w", errSave)
			}
		}

		return WebFetchOutput{
			Url:        in.Url,
			MimeType:   mimeType,
			IsBinary:   true,
			CachedPath: cachedPath,
		}, nil
	}

	// Process text types: JSON, XML, HTML, plain text
	var title string
	if mimeType == "text/html" {
		htmlStr := string(bodyBytes)
		title = extractTitle(htmlStr)

		converter := htmlmarkdown.NewConverter("", true, nil)
		markdown, err := converter.ConvertString(htmlStr)
		if err == nil {
			bodyBytes = []byte(markdown)
		}
	} else if mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") {
		var jsonObj any
		if err := json.Unmarshal(bodyBytes, &jsonObj); err == nil {
			if formatted, err := json.MarshalIndent(jsonObj, "", "  "); err == nil {
				bodyBytes = formatted
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
			bodyBytes = indentBuf.Bytes()
		}
	}

	text := string(bodyBytes)
	content := text
	truncated := false

	// Handle truncation (> 16,000 chars)
	if len(text) > MaxTotalChars {
		truncated = true
		content = text[:MaxTotalChars]

		if h.Storage != nil {
			filename := filepath.Base(in.Url)
			if filename == "" || filename == "." || filename == "/" {
				filename = "download.txt"
			}
			if idx := strings.Index(filename, "?"); idx != -1 {
				filename = filename[:idx]
			}
			if !strings.HasSuffix(filename, ".txt") && !strings.HasSuffix(filename, ".md") {
				filename += ".txt"
			}
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = "unknown"
			}
			storagePath := fmt.Sprintf("%s_%s", toolCallID, filename)
			var errSave error
			cachedPath, errSave = h.Storage.Save(ctx, storagePath, strings.NewReader(text))
			if errSave != nil {
				return WebFetchOutput{}, fmt.Errorf("failed to cache truncated content: %w", errSave)
			}
		}
	}

	return WebFetchOutput{
		Url:        in.Url,
		MimeType:   mimeType,
		IsBinary:   false,
		Title:      title,
		Content:    content,
		Truncated:  truncated,
		CachedPath: cachedPath,
	}, nil
}

func extractTitle(htmlContent string) string {
	re := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	matches := re.FindStringSubmatch(htmlContent)
	if len(matches) > 1 {
		return strings.TrimSpace(html.UnescapeString(matches[1]))
	}
	return ""
}

// ToolContent implements the loom tool.ContentProvider interface.
func (v WebFetchOutput) ToolContent() message.Content {
	if v.IsBinary {
		if strings.HasPrefix(v.MimeType, "image/") {
			return message.Content{
				&message.ImageBlock{
					MIMEType: v.MimeType,
					URL:      v.CachedPath,
				},
			}
		}

		if v.MimeType == "application/pdf" {
			return message.Content{
				&message.DocumentBlock{
					MIMEType: v.MimeType,
					URL:      v.CachedPath,
				},
			}
		}

		// Fallback for other documents or unsupported binaries
		return message.Content{
			&message.TextBlock{
				Text: fmt.Sprintf("[Binary download: %s (%s)]", filepath.Base(v.Url), v.MimeType),
			},
		}
	}

	var sb strings.Builder
	if v.Title != "" {
		fmt.Fprintf(&sb, "Title: %s\nURL: %s\n\n", v.Title, v.Url)
	} else {
		fmt.Fprintf(&sb, "URL: %s\n\n", v.Url)
	}
	sb.WriteString(v.Content)

	if v.Truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: Content truncated due to size limits. Full content saved to cache. To read further, use the view tool with path=%s]", v.CachedPath)
	}

	return message.Content{
		&message.TextBlock{
			Text: sb.String(),
		},
	}
}

// GetFileCacheMetadata implements the FileCacheProvider interface.
func (v WebFetchOutput) GetFileCacheMetadata() []FileCacheMetadata {
	return []FileCacheMetadata{
		{
			Source:     v.Url,
			CachedPath: v.CachedPath,
			MimeType:   v.MimeType,
			IsBinary:   v.IsBinary,
		},
	}
}
