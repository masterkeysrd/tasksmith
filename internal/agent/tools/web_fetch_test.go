package tools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
)

type testMockStorage struct {
	savedPath string
	savedData []byte
}

func (m *testMockStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	m.savedData = data
	m.savedPath = "/mock/dest/" + relativePath
	return m.savedPath, nil
}

func (m *testMockStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.savedData)), nil
}

func TestWebFetchHTML(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Test Page Title</title>
</head>
<body>
    <h1>Welcome to the Test Page</h1>
    <p>This is a <strong>paragraph</strong>.</p>
</body>
</html>`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	storage := &testMockStorage{}
	handlers := NewHandlers(storage, "")

	out, err := handlers.WebFetch(ctx, WebFetchArgs{Url: server.URL + "/page"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Title != "Test Page Title" {
		t.Errorf("expected title to be 'Test Page Title', got %q", out.Title)
	}
	if out.MimeType != "text/html" {
		t.Errorf("expected mime_type 'text/html', got %q", out.MimeType)
	}
	if out.IsBinary {
		t.Errorf("expected is_binary to be false, got true")
	}

	// Verify it was converted to Markdown
	if !strings.Contains(out.Content, "# Welcome to the Test Page") {
		t.Errorf("expected converted markdown to contain h1, got %q", out.Content)
	}
	if !strings.Contains(out.Content, "This is a **paragraph**.") {
		t.Errorf("expected converted markdown to contain strong, got %q", out.Content)
	}

	// Verify ToolContent formatting
	toolContent := out.ToolContent()
	if len(toolContent) != 1 {
		t.Fatalf("expected 1 tool content block")
	}
	textBlock, ok := toolContent[0].(*message.TextBlock)
	if !ok {
		t.Fatalf("expected *message.TextBlock, got %T", toolContent[0])
	}
	if !strings.Contains(textBlock.Text, "Title: Test Page Title") {
		t.Errorf("expected ToolContent text to contain title, got: %q", textBlock.Text)
	}
}

func TestWebFetchJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test","active":true,"tags":["a","b"]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	handlers := NewHandlers(nil, "")

	out, err := handlers.WebFetch(ctx, WebFetchArgs{Url: server.URL + "/json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.MimeType != "application/json" {
		t.Errorf("expected mime_type 'application/json', got %q", out.MimeType)
	}

	// Verify pretty printed content
	expected := `{
  "active": true,
  "name": "test",
  "tags": [
    "a",
    "b"
  ]
}`
	if strings.TrimSpace(out.Content) != expected {
		t.Errorf("expected pretty printed JSON, got %q", out.Content)
	}
}

func TestWebFetchXML(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<note><to>Tove</to><from>Jani</from><body>Don't forget me this weekend!</body></note>`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	handlers := NewHandlers(nil, "")

	out, err := handlers.WebFetch(ctx, WebFetchArgs{Url: server.URL + "/xml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.MimeType != "application/xml" {
		t.Errorf("expected mime_type 'application/xml', got %q", out.MimeType)
	}

	// Verify indented XML structure
	if !strings.Contains(out.Content, "  <to>Tove</to>") {
		t.Errorf("expected indented XML, got %q", out.Content)
	}
}

func TestWebFetchBinary(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/image.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png-bytes-content"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.WithValue(context.Background(), "tool_call_id", "call-web-1")
	storage := &testMockStorage{}
	handlers := NewHandlers(storage, "")

	out, err := handlers.WebFetch(ctx, WebFetchArgs{Url: server.URL + "/image.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.IsBinary {
		t.Errorf("expected is_binary to be true")
	}
	if out.MimeType != "image/png" {
		t.Errorf("expected mime_type 'image/png', got %q", out.MimeType)
	}

	expectedSavedPath := "/mock/dest/call-web-1_image.png"
	if out.CachedPath != expectedSavedPath {
		t.Errorf("expected cached path %q, got %q", expectedSavedPath, out.CachedPath)
	}

	if !bytes.Equal(storage.savedData, []byte("fake-png-bytes-content")) {
		t.Errorf("expected saved storage content to match HTTP response")
	}

	// Verify ToolContent contains image block
	toolContent := out.ToolContent()
	if len(toolContent) != 1 {
		t.Fatalf("expected 1 tool content block")
	}
	imgBlock, ok := toolContent[0].(*message.ImageBlock)
	if !ok {
		t.Fatalf("expected *message.ImageBlock, got %T", toolContent[0])
	}
	if imgBlock.MIMEType != "image/png" {
		t.Errorf("expected image block MIMEType 'image/png', got %q", imgBlock.MIMEType)
	}
	if imgBlock.URL != expectedSavedPath {
		t.Errorf("expected image block URL %q, got %q", expectedSavedPath, imgBlock.URL)
	}

	// Verify GetFileCacheMetadata
	metaList := out.GetFileCacheMetadata()
	if len(metaList) != 1 {
		t.Fatalf("expected 1 metadata element")
	}
	meta := metaList[0]
	if meta.Source != server.URL+"/image.png" {
		t.Errorf("expected metadata Source to be the URL, got %q", meta.Source)
	}
	if meta.CachedPath != expectedSavedPath {
		t.Errorf("expected metadata CachedPath to be %q, got %q", expectedSavedPath, meta.CachedPath)
	}
	if meta.IsBinary != true {
		t.Errorf("expected metadata IsBinary to be true")
	}
}

func TestWebFetchTruncation(t *testing.T) {
	mux := http.NewServeMux()
	largeText := strings.Repeat("A", 17000)
	mux.HandleFunc("/large.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(largeText))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.WithValue(context.Background(), "tool_call_id", "call-large-1")
	storage := &testMockStorage{}
	handlers := NewHandlers(storage, "")

	out, err := handlers.WebFetch(ctx, WebFetchArgs{Url: server.URL + "/large.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Truncated {
		t.Errorf("expected truncated to be true")
	}
	if len(out.Content) != MaxTotalChars {
		t.Errorf("expected content length %d, got %d", MaxTotalChars, len(out.Content))
	}

	expectedSavedPath := "/mock/dest/call-large-1_large.txt"
	if out.CachedPath != expectedSavedPath {
		t.Errorf("expected cached path %q, got %q", expectedSavedPath, out.CachedPath)
	}

	if len(storage.savedData) != len(largeText) {
		t.Errorf("expected full data saved to storage, saved length %d vs original %d", len(storage.savedData), len(largeText))
	}

	// Verify ToolContent system note is appended
	toolContent := out.ToolContent()
	textBlock := toolContent[0].(*message.TextBlock)
	if !strings.Contains(textBlock.Text, "SYSTEM NOTE: Content truncated due to size limits.") {
		t.Errorf("expected text block to contain system note, got: %q", textBlock.Text)
	}
	if !strings.Contains(textBlock.Text, "use the view tool with path="+expectedSavedPath) {
		t.Errorf("expected note to contain view command instruction, got: %q", textBlock.Text)
	}
}
