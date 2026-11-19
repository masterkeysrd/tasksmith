package web

import (
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Format
		wantErr bool
	}{
		{name: "empty defaults to markdown", raw: "", want: FormatMarkdown},
		{name: "whitespace defaults to markdown", raw: "   \t\n ", want: FormatMarkdown},
		{name: "markdown", raw: "markdown", want: FormatMarkdown},
		{name: "text", raw: "text", want: FormatText},
		{name: "html", raw: "html", want: FormatHTML},
		{name: "uppercase and spaces", raw: "  TeXt  ", want: FormatText},
		{name: "uppercase html", raw: "HTML", want: FormatHTML},
		{name: "invalid", raw: "json", wantErr: true},
		{name: "unsupported", raw: "xml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFormat(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFormat(%q) = %q, want error", tt.raw, got)
				}
				if !strings.Contains(err.Error(), "unsupported format") {
					t.Errorf("error = %q, want unsupported format message", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFormat(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestHTMLToMarkdownTitleAndNoCropping(t *testing.T) {
	t.Run("prepends title when body has no matching h1 and preserves layout regions", func(t *testing.T) {
		raw := `<!DOCTYPE html>
<html>
<head>
  <title>Docs Home</title>
  <style>.hidden { display: none; }</style>
  <script>console.log("ignore");</script>
</head>
<body>
  <header><a href="/home">Site Header</a></header>
  <nav><ul><li><a href="/guide">Guide</a></li><li><a href="/api">API</a></li></ul></nav>
  <main>
    <h2>Overview</h2>
    <p>Welcome to the docs.</p>
    <aside>Sidebar note</aside>
  </main>
  <footer>Copyright 2026</footer>
  <noscript>Enable JS</noscript>
  <svg><text>vector</text></svg>
  <canvas>fallback</canvas>
  <template><p>tpl</p></template>
</body>
</html>`

		got := HTMLToMarkdown(raw, "https://example.com/docs/intro")
		wantParts := []string{
			"# Docs Home",
			"[Site Header](https://example.com/home)",
			"- [Guide](https://example.com/guide)",
			"- [API](https://example.com/api)",
			"## Overview",
			"Welcome to the docs.",
			"Sidebar note",
			"Copyright 2026",
		}
		for _, part := range wantParts {
			if !strings.Contains(got, part) {
				t.Errorf("missing %q in markdown:\n%s", part, got)
			}
		}
		if !strings.HasPrefix(got, "# Docs Home\n\n") {
			t.Errorf("expected title prefix, got:\n%s", got)
		}
		for _, stripped := range []string{"display: none", "console.log", "Enable JS", "vector", "fallback", "tpl"} {
			if strings.Contains(got, stripped) {
				t.Errorf("expected %q to be stripped, got:\n%s", stripped, got)
			}
		}
	})

	t.Run("does not duplicate title when body starts with matching h1", func(t *testing.T) {
		raw := `<html><head><title>Getting Started</title></head><body><h1>Getting Started</h1><p>Step 1.</p></body></html>`
		got := HTMLToMarkdown(raw, "")
		if count := strings.Count(got, "# Getting Started"); count != 1 {
			t.Errorf("title count = %d, want 1; got:\n%s", count, got)
		}
	})
}

func TestHTMLToMarkdownElements(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		baseURL string
		want    string
	}{
		{
			name: "headings h1 to h6",
			html: `<h1>One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6>`,
			want: "# One\n\n## Two\n\n### Three\n\n#### Four\n\n##### Five\n\n###### Six",
		},
		{
			name: "paragraphs br hr and blank line collapsing",
			html: `<p>First   paragraph<br>second line</p><p></p><p></p><hr><p>After rule</p>`,
			want: "First paragraph\nsecond line\n\n---\n\nAfter rule",
		},
		{
			name: "inline formatting",
			html: `<p>Use <code>go test</code> with <strong>bold</strong>, <b>b</b>, <em>italic</em>, <i>i</i>, <del>old</del>, and <s>strike</s>.</p>`,
			want: "Use `go test` with **bold**, **b**, *italic*, *i*, ~~old~~, and ~~strike~~.",
		},
		{
			name:    "links and relative url resolution",
			html:    `<p><a href="sub/page.html">Relative</a>, <a href="/root">Root</a>, <a href="javascript:void(0)">JS</a>, <a href="">Empty</a>, <a>NoHref</a>.</p>`,
			baseURL: "https://example.com/docs/index.html",
			want:    "[Relative](https://example.com/docs/sub/page.html), [Root](https://example.com/root), JS, Empty, NoHref.",
		},
		{
			name:    "images",
			html:    `<p><img src="/img/logo.png" alt="Logo"><img src="" alt="Broken"></p>`,
			baseURL: "https://example.com/page",
			want:    "![Logo](https://example.com/img/logo.png)",
		},
		{
			name: "fenced code block with language- class and preserved indentation",
			html: `<pre><code class="language-go">func main() {
    if true {
        println("hi")
    }
}
</code></pre>`,
			want: "```go\nfunc main() {\n    if true {\n        println(\"hi\")\n    }\n}\n```",
		},
		{
			name: "fenced code block with highlight- class on pre and inner spans",
			html: `<pre class="highlight-python"><span>def</span> greet():
    return 1</pre>`,
			want: "```python\ndef greet():\n    return 1\n```",
		},
		{
			name: "nested ul and ol lists",
			html: `<ul>
  <li>Alpha
    <ul>
      <li>Beta</li>
      <li>Gamma</li>
    </ul>
  </li>
  <li>Delta
    <ol>
      <li>One</li>
      <li>Two</li>
    </ol>
  </li>
</ul>`,
			want: "- Alpha\n  - Beta\n  - Gamma\n- Delta\n  1. One\n  2. Two",
		},
		{
			name: "blockquote",
			html: `<blockquote><p>Line one.</p><p>Line two.</p></blockquote>`,
			want: "> Line one.\n>\n> Line two.",
		},
		{
			name: "gfm pipe table",
			html: `<table>
  <thead><tr><th>Name</th><th>Value | Alt</th></tr></thead>
  <tbody>
    <tr><td>alpha</td><td>1</td></tr>
    <tr><td>beta</td><td>2</td></tr>
  </tbody>
</table>`,
			want: "| Name | Value \\| Alt |\n| --- | --- |\n| alpha | 1 |\n| beta | 2 |",
		},
		{
			name: "definition list",
			html: `<dl><dt>Term</dt><dd>Definition text</dd></dl>`,
			want: "**Term**\n: Definition text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HTMLToMarkdown(tt.html, tt.baseURL)
			if got != tt.want {
				t.Errorf("HTMLToMarkdown() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestHTMLToText(t *testing.T) {
	raw := `<!DOCTYPE html>
<html>
<head>
  <title>Release Notes</title>
  <style>body { color: red; }</style>
  <script>alert(1)</script>
</head>
<body>
  <nav><a href="/home">Home</a></nav>
  <h2>Changes</h2>
  <p>Read the <a href="https://example.com/docs">full docs</a> with <strong>care</strong> and <code>flag</code>.</p>
  <ul>
    <li>Added feature
      <ul><li>Sub-item</li></ul>
    </li>
    <li>Fixed bug</li>
  </ul>
  <pre>  raw code
    indented</pre>
  <svg><text>ignored</text></svg>
</body>
</html>`

	got := HTMLToText(raw)
	want := strings.Join([]string{
		"Release Notes",
		"",
		"Home",
		"",
		"Changes",
		"",
		"Read the full docs with care and flag.",
		"",
		"- Added feature",
		"  - Sub-item",
		"- Fixed bug",
		"",
		"  raw code",
		"    indented",
	}, "\n")

	if got != want {
		t.Errorf("HTMLToText() =\n%s\n\nwant:\n%s", got, want)
	}
	for _, markup := range []string{"#", "**", "`", "[Home]", "(https://"} {
		if strings.Contains(got, markup) {
			t.Errorf("HTMLToText() leaked markdown markup %q:\n%s", markup, got)
		}
	}
}

func TestFormatContent(t *testing.T) {
	htmlBody := []byte(`<html><body><h1>Hello</h1><p>Visit <a href="/docs">docs</a>.</p></body></html>`)
	minifiedJSON := []byte(`{"z":1,"a":[true,1.23e4],"nested":{"b":"c"}}`)
	invalidJSON := []byte(`{"broken": `)
	minifiedXML := []byte(`<?xml version="1.0" encoding="UTF-8"?><root><item id="1">one</item><empty></empty></root>`)
	invalidXML := []byte(`<root><unclosed></root>`)

	tests := []struct {
		name        string
		rawURL      string
		contentType string
		body        []byte
		format      Format
		want        string
	}{
		{
			name:   "empty body",
			body:   nil,
			format: FormatMarkdown,
			want:   "",
		},
		{
			name:        "html as markdown",
			rawURL:      "https://example.com/home",
			contentType: "text/html; charset=utf-8",
			body:        htmlBody,
			format:      FormatMarkdown,
			want:        "# Hello\n\nVisit [docs](https://example.com/docs).",
		},
		{
			name:        "html defaults empty format to markdown",
			rawURL:      "https://example.com/home",
			contentType: "text/html",
			body:        htmlBody,
			format:      "",
			want:        "# Hello\n\nVisit [docs](https://example.com/docs).",
		},
		{
			name:        "html as text",
			rawURL:      "https://example.com/home",
			contentType: "text/html",
			body:        htmlBody,
			format:      FormatText,
			want:        "Hello\n\nVisit docs.",
		},
		{
			name:        "html raw mode returns unmodified",
			rawURL:      "https://example.com/home",
			contentType: "text/html",
			body:        htmlBody,
			format:      FormatHTML,
			want:        string(htmlBody),
		},
		{
			name:        "sniffed html when content-type is empty",
			rawURL:      "https://example.com/",
			contentType: "",
			body:        htmlBody,
			format:      FormatMarkdown,
			want:        "# Hello\n\nVisit [docs](https://example.com/docs).",
		},
		{
			name:        "sniffed html when content-type is octet-stream",
			rawURL:      "https://example.com/",
			contentType: "application/octet-stream",
			body:        htmlBody,
			format:      FormatText,
			want:        "Hello\n\nVisit docs.",
		},
		{
			name:        "json pretty-printed preserving key order and numbers",
			contentType: "application/json; charset=utf-8",
			body:        minifiedJSON,
			format:      FormatMarkdown,
			want:        "{\n  \"z\": 1,\n  \"a\": [\n    true,\n    1.23e4\n  ],\n  \"nested\": {\n    \"b\": \"c\"\n  }\n}",
		},
		{
			name:        "problem+json pretty-printed in text mode",
			contentType: "application/problem+json",
			body:        minifiedJSON,
			format:      FormatText,
			want:        "{\n  \"z\": 1,\n  \"a\": [\n    true,\n    1.23e4\n  ],\n  \"nested\": {\n    \"b\": \"c\"\n  }\n}",
		},
		{
			name:        "json raw mode returns unmodified",
			contentType: "application/json",
			body:        minifiedJSON,
			format:      FormatHTML,
			want:        string(minifiedJSON),
		},
		{
			name:        "invalid json falls back to raw",
			contentType: "application/json",
			body:        invalidJSON,
			format:      FormatMarkdown,
			want:        string(invalidJSON),
		},
		{
			name:        "xml pretty-printed",
			contentType: "application/xml",
			body:        minifiedXML,
			format:      FormatMarkdown,
			want:        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<root>\n  <item id=\"1\">one</item>\n  <empty></empty>\n</root>",
		},
		{
			name:        "rss+xml pretty-printed in text mode",
			contentType: "application/rss+xml",
			body:        []byte(`<rss><channel><title>Feed</title></channel></rss>`),
			format:      FormatText,
			want:        "<rss>\n  <channel>\n    <title>Feed</title>\n  </channel>\n</rss>",
		},
		{
			name:        "xml raw mode returns unmodified",
			contentType: "text/xml",
			body:        minifiedXML,
			format:      FormatHTML,
			want:        string(minifiedXML),
		},
		{
			name:        "invalid xml falls back to raw",
			contentType: "application/xml",
			body:        invalidXML,
			format:      FormatMarkdown,
			want:        string(invalidXML),
		},
		{
			name:        "plain text returned unmodified",
			contentType: "text/plain",
			body:        []byte("hello\nworld\n"),
			format:      FormatMarkdown,
			want:        "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatContent(tt.rawURL, tt.contentType, tt.body, tt.format)
			if got != tt.want {
				t.Errorf("FormatContent() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
