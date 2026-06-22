package tools

import (
	"strings"
	"testing"
)

func TestCleanURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "Absolute Redirect",
			raw:  "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F&rut=123",
			want: "https://golang.org/",
		},
		{
			name: "Relative Redirect",
			raw:  "/l/?uddg=https%3A%2F%2Fgolang.org%2F",
			want: "https://golang.org/",
		},
		{
			name: "Protocol Relative",
			raw:  "//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F",
			want: "https://golang.org/",
		},
		{
			name: "Normal Link",
			raw:  "https://golang.org/",
			want: "https://golang.org/",
		},
		{
			name: "Empty",
			raw:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanURL(tt.raw)
			if got != tt.want {
				t.Errorf("cleanURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseDuckDuckGoHTML(t *testing.T) {
	mockHTML := `
<!DOCTYPE html>
<html>
<body>
	<div class="result web-result">
		<h2 class="result__title">
			<a class="result__a" href="/l/?uddg=https%3A%2F%2Fgolang.org%2F">Go Programming Language</a>
		</h2>
		<a class="result__snippet" href="/l/?uddg=https%3A%2F%2Fgolang.org%2F">
			Go is an open source programming language that makes it easy to build simple, reliable, and efficient software.
		</a>
	</div>
	<div class="result web-result">
		<h2 class="result__title">
			<a class="result__a" href="https://github.com/golang/go">GitHub - golang/go</a>
		</h2>
		<a class="result__snippet" href="https://github.com/golang/go">
			The Go programming language repository.
		</a>
	</div>
</body>
</html>
`
	results, err := parseDuckDuckGoHTML(strings.NewReader(mockHTML), 10)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Title != "Go Programming Language" {
		t.Errorf("expected title 'Go Programming Language', got %q", results[0].Title)
	}
	if results[0].Url != "https://golang.org/" {
		t.Errorf("expected URL 'https://golang.org/', got %q", results[0].Url)
	}
	if !strings.Contains(results[0].Snippet, "open source") {
		t.Errorf("expected snippet to contain 'open source', got %q", results[0].Snippet)
	}

	if results[1].Title != "GitHub - golang/go" {
		t.Errorf("expected title 'GitHub - golang/go', got %q", results[1].Title)
	}
	if results[1].Url != "https://github.com/golang/go" {
		t.Errorf("expected URL 'https://github.com/golang/go', got %q", results[1].Url)
	}
}

func TestWebSearchOutput_TextContent(t *testing.T) {
	output := WebSearchOutput{
		Results: []WebSearchOutputResultsItem{
			{
				Title:   "Google",
				Url:     "https://google.com",
				Snippet: "Search the world's information.",
			},
		},
	}

	text := output.TextContent()
	expected := "1. Google\n   URL: https://google.com\n   Search the world's information."
	if text != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, text)
	}
}

func TestParseDuckDuckGoHTML_BotChallenge(t *testing.T) {
	mockCaptchaHTML := `
<!DOCTYPE html>
<html>
<body>
	<div class="anomaly-modal__modal">
		<div class="anomaly-modal__title">Unfortunately, bots use DuckDuckGo too.</div>
	</div>
</body>
</html>
`
	_, err := parseDuckDuckGoHTML(strings.NewReader(mockCaptchaHTML), 10)
	if err == nil {
		t.Fatal("expected error when parsing bot challenge HTML, got nil")
	}

	if !strings.Contains(err.Error(), "bot protection triggered") {
		t.Errorf("expected error message to mention 'bot protection triggered', got %v", err)
	}
}

func TestParseDuckDuckGoHTML_MaxResults(t *testing.T) {
	mockHTML := `
<!DOCTYPE html>
<html>
<body>
	<div class="result web-result">
		<a class="result__a" href="/l/?uddg=https%3A%2F%2Fgolang.org%2F">Go 1</a>
	</div>
	<div class="result web-result">
		<a class="result__a" href="https://github.com/golang/go">Go 2</a>
	</div>
</body>
</html>
`
	results, err := parseDuckDuckGoHTML(strings.NewReader(mockHTML), 1)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result due to maxResults limit, got %d", len(results))
	}
}
