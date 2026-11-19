package std

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

const searchResultsHTML = `
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

func newSearchTestStd(t *testing.T, allowLoopback bool, endpoint string) *Std {
	t.Helper()

	sb, err := sandbox.New(sandbox.Config{
		Mode:           sandbox.ModeLocal,
		WorkDir:        t.TempDir(),
		Network:        sandbox.NetworkPolicy{AllowLoopback: allowLoopback},
		DefaultTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	s, err := New(Config{
		Sandbox:        sb,
		Tracker:        NewMemoryTracker(),
		Logger:         log.New(io.Discard, log.LevelDebug),
		SearchEndpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("std.New failed: %v", err)
	}
	return s
}

func TestSearchParsesAndFiltersResults(t *testing.T) {
	var gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/html/" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.Query().Get("q")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, searchResultsHTML)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL+"/html/")

	res, err := s.Search(context.Background(), SearchRequest{Query: "  go   language  "})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if gotQuery != "go language" {
		t.Errorf("endpoint received q=%q, want the normalized query", gotQuery)
	}
	if !strings.Contains(gotUA, "Mozilla/5.0") {
		t.Errorf("endpoint received User-Agent %q, want a browser UA", gotUA)
	}
	if res.Query != "go language" {
		t.Errorf("result query = %q, want whitespace-normalized %q", res.Query, "go language")
	}
	if len(res.Hits) != 3 {
		t.Fatalf("expected 3 parseable hits (class-less anchor skipped), got %d: %+v", len(res.Hits), res.Hits)
	}

	first := res.Hits[0]
	if first.URL != "https://go.dev/" {
		t.Errorf("redirect link = %q, want the uddg target https://go.dev/", first.URL)
	}
	if first.Title != "Go Programming Language" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Snippet != "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software." {
		t.Errorf("snippet = %q, want a single whitespace-normalized line", first.Snippet)
	}
	if res.Hits[1].URL != "https://github.com/golang/go" {
		t.Errorf("absolute link = %q", res.Hits[1].URL)
	}
	if res.Hits[2].URL != "https://docs.example.org/go" {
		t.Errorf("protocol-relative link = %q, want an https URL", res.Hits[2].URL)
	}
	if res.Hits[2].Snippet != "" {
		t.Errorf("result without a snippet node = %q, want empty", res.Hits[2].Snippet)
	}
	if res.Truncated {
		t.Error("Truncated = true for a page within the default limit, want false")
	}
}

func TestSearchLimitReportsTruncation(t *testing.T) {
	var blocks strings.Builder
	for i := range 25 {
		fmt.Fprintf(&blocks, `<div class="result"><a class="result__a" href="https://site%02d.example.org/">Result %d</a></div>`, i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "<html><body>%s</body></html>", blocks.String())
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)

	res, err := s.Search(context.Background(), SearchRequest{Query: "many", Limit: 5})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Hits) != 5 {
		t.Fatalf("limit=5 returned %d hits", len(res.Hits))
	}
	if !res.Truncated {
		t.Error("Truncated = false with 25 page results and limit=5, want true")
	}

	clamped, err := s.Search(context.Background(), SearchRequest{Query: "many", Limit: MaxSearchLimit + 50})
	if err != nil {
		t.Fatalf("Search with an over-max limit failed: %v", err)
	}
	if len(clamped.Hits) != MaxSearchLimit {
		t.Errorf("over-max limit returned %d hits, want the clamp at %d", len(clamped.Hits), MaxSearchLimit)
	}

	defaulted, err := s.Search(context.Background(), SearchRequest{Query: "many"})
	if err != nil {
		t.Fatalf("Search with the default limit failed: %v", err)
	}
	if len(defaulted.Hits) != DefaultSearchLimit {
		t.Errorf("default limit returned %d hits, want %d", len(defaulted.Hits), DefaultSearchLimit)
	}
}

func TestSearchDomainFilters(t *testing.T) {
	page := `<html><body>
		<div class="result"><a class="result__a" href="https://pkg.go.dev/context">pkg.go.dev</a></div>
		<div class="result"><a class="result__a" href="https://go.dev/doc/effective_go">go.dev</a></div>
		<div class="result"><a class="result__a" href="https://blog.example.org/post">blog.example.org</a></div>
		<div class="result"><a class="result__a" href="not a url">unparsable</a></div>
	</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)
	ctx := context.Background()

	t.Run("AllowedDomains", func(t *testing.T) {
		res, err := s.Search(ctx, SearchRequest{Query: "go", AllowedDomains: []string{"go.dev", "*.example.org"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		got := searchHitURLs(res.Hits)
		want := []string{"https://pkg.go.dev/context", "https://go.dev/doc/effective_go", "https://blog.example.org/post"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("allowed filter = %v, want %v (bare domain matches subdomains, hostless result dropped)", got, want)
		}
	})

	t.Run("BlockedDomains", func(t *testing.T) {
		res, err := s.Search(ctx, SearchRequest{Query: "go", BlockedDomains: []string{"*.GO.DEV"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		got := searchHitURLs(res.Hits)
		want := []string{"https://blog.example.org/post", "not a url"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("blocked filter = %v, want %v", got, want)
		}
	})

	t.Run("BlockedWinsOverAllowed", func(t *testing.T) {
		res, err := s.Search(ctx, SearchRequest{
			Query:          "go",
			AllowedDomains: []string{"*"},
			BlockedDomains: []string{"pkg.go.dev", "blog.example.org"},
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		got := searchHitURLs(res.Hits)
		want := []string{"https://go.dev/doc/effective_go"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("blocked precedence = %v, want %v (blocked hosts dropped, hostless result fails the allowlist)", got, want)
		}
	})
}

func searchHitURLs(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.URL)
	}
	return out
}

func TestSearchRejectsBotChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body><div class="anomaly-modal__modal"><h2>Confirm you are not a robot</h2></div></body></html>`)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)
	_, err := s.Search(context.Background(), SearchRequest{Query: "blocked"})
	if err == nil || !strings.Contains(err.Error(), "bot protection") {
		t.Fatalf("error = %v, want a bot-protection failure", err)
	}
}

func TestSearchSurfaceValidationAndStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)
	ctx := context.Background()

	if _, err := s.Search(ctx, SearchRequest{Query: "   "}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("blank query error = %v, want ErrInvalidArgument", err)
	}
	if _, err := s.Search(ctx, SearchRequest{}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty query error = %v, want ErrInvalidArgument", err)
	}

	// A request-level endpoint override is validated the same way.
	if _, err := s.Search(ctx, SearchRequest{Query: "x", Endpoint: "ftp://mirror.example.org/"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("non-http endpoint error = %v, want ErrInvalidArgument", err)
	}

	if _, err := s.Search(ctx, SearchRequest{Query: "rate limited"}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("non-OK status error = %v, want it to name the status code", err)
	}
}

// TestSearchRunsOnTheHostDespiteSandboxNetworkPolicy pins the deliberate
// boundary: web search executes from the daemon host, so a session whose
// network policy blocks loopback can still search, while child processes stay
// confined by that same policy.
func TestSearchRunsOnTheHostDespiteSandboxNetworkPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body><div class="result"><a class="result__a" href="https://go.dev/">Go</a></div></body></html>`)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)

	if _, err := s.Fetch(context.Background(), FetchRequest{URL: srv.URL}); !errors.Is(err, sandbox.ErrLoopbackBlocked) {
		t.Fatalf("Fetch error = %v, want the sandbox policy to still block loopback", err)
	}

	res, err := s.Search(context.Background(), SearchRequest{Query: "go"})
	if err != nil {
		t.Fatalf("Search should run host-side regardless of the child network policy, got: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %+v, want one result", res.Hits)
	}
}

func TestSearchDryRunValidatesWithoutRequest(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = fmt.Fprint(w, `<html><body></body></html>`)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)

	res, err := s.Search(context.Background(), SearchRequest{Query: "planned", DryRun: true})
	if err != nil {
		t.Fatalf("dry-run Search failed: %v", err)
	}
	if !res.DryRun || len(res.Hits) != 0 {
		t.Errorf("dry-run result = %+v, want DryRun with no hits", res)
	}
	if !strings.Contains(res.Endpoint, "q=planned") {
		t.Errorf("dry-run endpoint = %q, want the encoded query", res.Endpoint)
	}
	if hits != 0 {
		t.Errorf("dry run hit the search endpoint %d times, want 0", hits)
	}

	if _, err := s.Search(context.Background(), SearchRequest{DryRun: true}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("dry-run validation error = %v, want ErrInvalidArgument for a blank query", err)
	}
}

func TestSearchEndpointDefaultsAndOverrides(t *testing.T) {
	if got := resolveSearchEndpoint("  "); got != DefaultSearchEndpoint {
		t.Errorf("resolveSearchEndpoint(blank) = %q, want the built-in default", got)
	}
	if got := resolveSearchEndpoint(" https://mirror.example.org/search "); got != "https://mirror.example.org/search" {
		t.Errorf("resolveSearchEndpoint = %q, want the trimmed override", got)
	}

	var sawConfigured, sawOverride bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/configured":
			sawConfigured = true
		case "/override":
			sawOverride = true
		}
		_, _ = fmt.Fprint(w, `<html><body></body></html>`)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL+"/configured")
	if _, err := s.Search(context.Background(), SearchRequest{Query: "a"}); err != nil {
		t.Fatalf("Search via configured endpoint: %v", err)
	}
	if _, err := s.Search(context.Background(), SearchRequest{Query: "b", Endpoint: srv.URL + "/override"}); err != nil {
		t.Fatalf("Search via request override: %v", err)
	}
	if !sawConfigured {
		t.Error("configured default endpoint was never queried")
	}
	if !sawOverride {
		t.Error("request endpoint override was never queried")
	}
}

func TestSearchTruncatesLongSnippets(t *testing.T) {
	long := strings.Repeat("word ", 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `<html><body><div class="result"><a class="result__a" href="https://go.dev/">T</a><div class="result__snippet">%s</div></div></body></html>`, long)
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)
	res, err := s.Search(context.Background(), SearchRequest{Query: "long"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	snippet := res.Hits[0].Snippet
	if len(snippet) > MaxSearchSnippetBytes+len("…") {
		t.Errorf("snippet length = %d, want it capped near %d", len(snippet), MaxSearchSnippetBytes)
	}
	if !strings.HasSuffix(snippet, "…") {
		t.Errorf("snippet = %q..., want an ellipsis marking truncation", snippet[:min(len(snippet), 40)])
	}
}

func TestSearchRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div class="result"><a class="result__a" href="https://go.dev/">Go</a></div>`))
		pad := strings.Repeat("p", 1024)
		for int64(len(pad)) <= MaxSearchResponseBytes {
			if _, err := w.Write([]byte(pad)); err != nil {
				return
			}
			pad += pad
		}
		_, _ = w.Write([]byte(`</body></html>`))
	}))
	defer srv.Close()

	s := newSearchTestStd(t, false, srv.URL)
	if _, err := s.Search(context.Background(), SearchRequest{Query: "huge"}); !errors.Is(err, ErrSearchResponseTooLarge) {
		t.Fatalf("Search error = %v, want ErrSearchResponseTooLarge", err)
	}
}
