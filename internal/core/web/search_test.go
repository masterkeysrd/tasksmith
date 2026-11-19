package web

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

const sampleResultsHTML = `
<!DOCTYPE html>
<html>
<body>
	<div class="result web-result">
		<h2 class="result__title">
			<a class="result__a" href="/l/?uddg=https%3A%2F%2Fgo.dev%2F&rut=abc">Go Programming Language</a>
		</h2>
		<a class="result__snippet" href="/l/?uddg=https%3A%2F%2Fgo.dev%2F">
			Go is an open source        programming language   that makes it easy to build
			simple, reliable, and efficient software.
		</a>
	</div>
	<div class="result">
		<h2 class="result__title">
			<a class="result__a" href="https://github.com/golang/go">GitHub - golang/go</a>
		</h2>
		<a class="result__snippet">The Go programming language repository.</a>
	</div>
	<div class="result">
		<h2 class="result__title">
			<a class="result__a" href="//docs.example.org/go">Go Documentation</a>
		</h2>
	</div>
	<div class="result">
		<h2 class="result__title"><a href="https://no-result-class.example.org/">Skipped</a></h2>
	</div>
</body>
</html>
`

func TestBuildSearchURL(t *testing.T) {
	endpoint, err := url.Parse("https://html.duckduckgo.com/html/?kl=us-en")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	got := BuildSearchURL(endpoint, "go language")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(got): %v", err)
	}
	if parsed.Query().Get("q") != "go language" {
		t.Errorf("q = %q, want %q", parsed.Query().Get("q"), "go language")
	}
	if parsed.Query().Get("kl") != "us-en" {
		t.Errorf("existing query param kl = %q, want %q", parsed.Query().Get("kl"), "us-en")
	}
	if endpoint.Query().Get("q") != "" {
		t.Errorf("BuildSearchURL mutated caller endpoint query: %q", endpoint.RawQuery)
	}
}

func TestParseSearchHTML(t *testing.T) {
	hits, err := ParseSearchHTML(strings.NewReader(sampleResultsHTML))
	if err != nil {
		t.Fatalf("ParseSearchHTML: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 parseable hits, got %d: %+v", len(hits), hits)
	}

	if hits[0].URL != "https://go.dev/" {
		t.Errorf("hits[0].URL = %q, want https://go.dev/", hits[0].URL)
	}
	if hits[0].Title != "Go Programming Language" {
		t.Errorf("hits[0].Title = %q", hits[0].Title)
	}
	wantSnippet := "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software."
	if hits[0].Snippet != wantSnippet {
		t.Errorf("hits[0].Snippet = %q, want %q", hits[0].Snippet, wantSnippet)
	}
	if hits[1].URL != "https://github.com/golang/go" {
		t.Errorf("hits[1].URL = %q", hits[1].URL)
	}
	if hits[2].URL != "https://docs.example.org/go" {
		t.Errorf("hits[2].URL = %q, want https://docs.example.org/go", hits[2].URL)
	}
	if hits[2].Snippet != "" {
		t.Errorf("hits[2].Snippet = %q, want empty", hits[2].Snippet)
	}
}

func TestParseSearchHTMLTruncatesLongSnippets(t *testing.T) {
	long := strings.Repeat("word ", 200)
	doc := fmt.Sprintf(`<html><body><div class="result"><a class="result__a" href="https://go.dev/">T</a><div class="result__snippet">%s</div></div></body></html>`, long)

	hits, err := ParseSearchHTML(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ParseSearchHTML: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	snippet := hits[0].Snippet
	if len(snippet) > MaxSnippetBytes+len("…") {
		t.Errorf("snippet length = %d, want it capped near %d", len(snippet), MaxSnippetBytes)
	}
	if !strings.HasSuffix(snippet, "…") {
		t.Errorf("snippet = %q..., want trailing ellipsis", snippet[:min(len(snippet), 40)])
	}
}

func TestParseSearchHTMLRejectsBotChallenge(t *testing.T) {
	doc := `<html><body><div class="anomaly-modal__modal"><h2>Confirm you are not a robot</h2></div></body></html>`
	_, err := ParseSearchHTML(strings.NewReader(doc))
	if !errors.Is(err, ErrBotChallenge) {
		t.Fatalf("error = %v, want ErrBotChallenge", err)
	}
}

func TestCleanResultURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "absolute redirect", raw: "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F&rut=123", want: "https://golang.org/"},
		{name: "relative redirect", raw: "/l/?uddg=https%3A%2F%2Fgolang.org%2F", want: "https://golang.org/"},
		{name: "protocol-relative redirect", raw: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F", want: "https://golang.org/"},
		{name: "direct link", raw: "https://golang.org/", want: "https://golang.org/"},
		{name: "redirect without target", raw: "/l/?rut=123", want: "/l/?rut=123"},
		{name: "empty", raw: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanResultURL(tt.raw); got != tt.want {
				t.Errorf("CleanResultURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFilterHits(t *testing.T) {
	hits := []Hit{
		{Title: "pkg.go.dev", URL: "https://pkg.go.dev/context"},
		{Title: "go.dev", URL: "https://go.dev/doc/effective_go"},
		{Title: "blog.example.org", URL: "https://blog.example.org/post"},
		{Title: "unparsable", URL: "not a url"},
	}

	hitURLs := func(in []Hit) []string {
		out := make([]string, 0, len(in))
		for _, h := range in {
			out = append(out, h.URL)
		}
		return out
	}

	t.Run("AllowedDomains", func(t *testing.T) {
		got, truncated := FilterHits(hits, []string{"go.dev", "*.example.org"}, nil, 10)
		if truncated {
			t.Errorf("truncated = true, want false")
		}
		want := []string{"https://pkg.go.dev/context", "https://go.dev/doc/effective_go", "https://blog.example.org/post"}
		if strings.Join(hitURLs(got), ",") != strings.Join(want, ",") {
			t.Errorf("got = %v, want %v", hitURLs(got), want)
		}
	})

	t.Run("BlockedDomains", func(t *testing.T) {
		got, truncated := FilterHits(hits, nil, []string{"*.GO.DEV"}, 10)
		if truncated {
			t.Errorf("truncated = true, want false")
		}
		want := []string{"https://blog.example.org/post", "not a url"}
		if strings.Join(hitURLs(got), ",") != strings.Join(want, ",") {
			t.Errorf("got = %v, want %v", hitURLs(got), want)
		}
	})

	t.Run("BlockedWinsOverAllowed", func(t *testing.T) {
		got, _ := FilterHits(hits, []string{"*"}, []string{"pkg.go.dev", "blog.example.org"}, 10)
		want := []string{"https://go.dev/doc/effective_go"}
		if strings.Join(hitURLs(got), ",") != strings.Join(want, ",") {
			t.Errorf("got = %v, want %v", hitURLs(got), want)
		}
	})

	t.Run("LimitReportsTruncation", func(t *testing.T) {
		got, truncated := FilterHits(hits, nil, nil, 2)
		if !truncated || len(got) != 2 {
			t.Errorf("got len=%d truncated=%t, want len=2 truncated=true", len(got), truncated)
		}
	})

	t.Run("ZeroLimitReturnsAll", func(t *testing.T) {
		got, truncated := FilterHits(hits, nil, nil, 0)
		if truncated || len(got) != len(hits) {
			t.Errorf("got len=%d truncated=%t, want len=%d truncated=false", len(got), truncated, len(hits))
		}
	})
}

func TestResultHostAndMatchesAnyDomain(t *testing.T) {
	if got := ResultHost(" HTTPS://PKG.GO.DEV./context "); got != "pkg.go.dev" {
		t.Errorf("ResultHost = %q, want pkg.go.dev", got)
	}
	if got := ResultHost("not a url"); got != "" {
		t.Errorf("ResultHost(invalid) = %q, want empty", got)
	}

	if !MatchesAnyDomain("pkg.go.dev", []string{".go.dev."}) {
		t.Error("expected subdomain match with leading/trailing dot")
	}
	if MatchesAnyDomain("notgo.dev", []string{"go.dev"}) {
		t.Error("did not expect suffix match across domain label boundary")
	}
	if MatchesAnyDomain("", []string{"*"}) {
		t.Error("did not expect empty host to match wildcard")
	}
}
