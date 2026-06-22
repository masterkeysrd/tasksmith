package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/message"
)

func TestFetch_Text_Success(t *testing.T) {
	expectedJSON := `{"status":"ok","msg":"hello from mock api"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(expectedJSON))
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")
	out, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, out.Status)
	}

	var gotVal, expVal any
	if err := json.Unmarshal([]byte(out.Content), &gotVal); err != nil {
		t.Fatalf("failed to unmarshal got content: %v", err)
	}
	if err := json.Unmarshal([]byte(expectedJSON), &expVal); err != nil {
		t.Fatalf("failed to unmarshal expected content: %v", err)
	}
	if !strings.Contains(out.Content, "\n") {
		t.Errorf("expected pretty-printed JSON, got %q", out.Content)
	}
}

func TestFetch_HTTP_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("custom 404 page body"))
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")
	out, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, out.Status)
	}
	if out.Content != "custom 404 page body" {
		t.Errorf("expected content %q, got %q", "custom 404 page body", out.Content)
	}
}

func TestFetch_Binary_Mime_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("valid utf-8 payload but image mime type"))
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")
	_, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL})
	if err == nil {
		t.Fatalf("expected binary fetch to fail, but it succeeded")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "content is binary") {
		t.Errorf("expected error to mention 'content is binary', got %q", errMsg)
	}
	if !strings.Contains(errMsg, "image/png") {
		t.Errorf("expected error to include mime-type 'image/png', got %q", errMsg)
	}
	if !strings.Contains(errMsg, "download") {
		t.Errorf("expected error to suggest using 'download' tool, got %q", errMsg)
	}
}

func TestFetch_NonUTF8_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // Non-UTF8 PNG signature
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")
	_, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL})
	if err == nil {
		t.Fatalf("expected non-UTF8 fetch to fail, but it succeeded")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "not valid UTF-8") {
		t.Errorf("expected error to mention 'not valid UTF-8', got %q", errMsg)
	}
	if !strings.Contains(errMsg, "download") {
		t.Errorf("expected error to suggest using 'download' tool, got %q", errMsg)
	}
}

func TestFetch_HTML_Formats(t *testing.T) {
	htmlContent := `<!DOCTYPE html><html><head><title>My Title</title></head><body><h1>Heading</h1><p>Paragraph text <strong>strong</strong>.</p></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")

	// 1. Text format
	outText, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL, Format: "text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedText := "Heading\nParagraph text strong ."
	if outText.Content != expectedText {
		t.Errorf("expected parsed body text %q, got %q", expectedText, outText.Content)
	}

	// 2. Markdown format
	outMD, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL, Format: "markdown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outMD.Content, "# Heading") || !strings.Contains(outMD.Content, "Paragraph text **strong**.") {
		t.Errorf("expected Markdown content, got %q", outMD.Content)
	}

	// 3. HTML body format
	outHTML, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL, Format: "html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outHTML.Content, "<body>") || !strings.Contains(outHTML.Content, "Heading") {
		t.Errorf("expected HTML body, got %q", outHTML.Content)
	}
	if !strings.Contains(outHTML.Content, "  <h1>Heading</h1>") {
		t.Errorf("expected pretty-printed HTML node indentation, got %q", outHTML.Content)
	}
	if strings.Contains(outHTML.Content, "<head>") {
		t.Errorf("expected HTML content to omit <head>, got %q", outHTML.Content)
	}
}

func TestFetch_XML_PrettyPrint(t *testing.T) {
	rawXML := `<root><child>value</child></root>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(rawXML))
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")
	out, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedXML := "<root>\n  <child>value</child>\n</root>"
	if strings.TrimSpace(out.Content) != expectedXML {
		t.Errorf("expected pretty-printed XML %q, got %q", expectedXML, out.Content)
	}
}

func TestFetch_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	handlers := NewHandlers(nil, "")
	// Timeout set to 1 second
	_, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL, Timeout: 1})
	if err == nil {
		t.Fatalf("expected request to timeout, but succeeded")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got %q", err.Error())
	}
}

func TestFetch_Truncation(t *testing.T) {
	// Generate large content (> 16000 chars)
	largeContent := strings.Repeat("A", MaxTotalChars+1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(largeContent))
	}))
	defer server.Close()

	storage := &testMockStorage{}
	handlers := NewHandlers(storage, "")
	out, err := handlers.Fetch(context.Background(), FetchArgs{Url: server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Truncated {
		t.Errorf("expected out.Truncated to be true")
	}
	if len(out.Content) != MaxTotalChars {
		t.Errorf("expected content length %d, got %d", MaxTotalChars, len(out.Content))
	}
	if out.CachedPath == "" {
		t.Errorf("expected cached path to be set")
	}
	if storage.savedPath != out.CachedPath {
		t.Errorf("expected storage saved path %q, got %q", storage.savedPath, out.CachedPath)
	}
	if string(storage.savedData) != largeContent {
		t.Errorf("expected saved data to contain original large content")
	}

	// Verify ToolContent displays truncation warning
	toolContent := out.ToolContent()
	if len(toolContent) != 1 {
		t.Fatalf("expected 1 tool content block")
	}
	textBlock, ok := toolContent[0].(*message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock")
	}
	if !strings.Contains(textBlock.Text, "Content truncated due to size limits") {
		t.Errorf("expected text to mention truncation, got %q", textBlock.Text)
	}

	// Verify FileCacheMetadata
	cacheMetadata := out.GetFileCacheMetadata()
	if len(cacheMetadata) != 1 || cacheMetadata[0].CachedPath != out.CachedPath {
		t.Errorf("expected file cache metadata pointing to cached path")
	}
}
