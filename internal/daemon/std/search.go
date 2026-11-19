package std

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/web"
)

const (
	// DefaultSearchLimit is the default number of results returned by Search.
	DefaultSearchLimit = 10

	// MaxSearchLimit is the maximum number of results Search can return.
	MaxSearchLimit = 20

	// MaxSearchResponseBytes caps a search results page body (1 MiB); HTML
	// result pages are small, so anything larger is not a results page.
	MaxSearchResponseBytes int64 = 1024 * 1024

	// MaxSearchSnippetBytes is the maximum byte length of one result snippet.
	MaxSearchSnippetBytes = web.MaxSnippetBytes

	// DefaultSearchTimeout bounds one host-side web search request.
	DefaultSearchTimeout = 15 * time.Second
)

// ErrSearchResponseTooLarge is returned when a search endpoint's body exceeds
// MaxSearchResponseBytes.
var ErrSearchResponseTooLarge = errors.New("std: search response exceeds size limit")

// DefaultSearchEndpoint is the HTML search endpoint [Std.Search] queries when a
// request does not override it. DuckDuckGo's lightweight HTML surface needs no
// API key and returns parseable static markup.
const DefaultSearchEndpoint = "https://html.duckduckgo.com/html/"

// searchUserAgent mimics a desktop browser because search endpoints serve
// truncated or challenge pages to obviously scripted clients.
const searchUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// SearchRequest configures an outbound web search via [Std.Search].
type SearchRequest struct {
	// Query is the search string. It is required.
	Query string `json:"query"`

	// AllowedDomains, when non-empty, keeps only results whose host matches one
	// of these domains (a bare domain also matches its subdomains).
	AllowedDomains []string `json:"allowed_domains,omitempty"`

	// BlockedDomains drops results whose host matches one of these domains. It
	// takes precedence over AllowedDomains.
	BlockedDomains []string `json:"blocked_domains,omitempty"`

	// Limit caps the number of returned results. Zero uses DefaultSearchLimit;
	// values above MaxSearchLimit are clamped.
	Limit int `json:"limit,omitempty"`

	// Endpoint overrides [DefaultSearchEndpoint]. It exists for self-hosted
	// search mirrors and for tests; ordinary callers leave it empty.
	Endpoint string `json:"endpoint,omitempty"`

	// DryRun builds and validates the request without executing it.
	DryRun bool `json:"dry_run,omitempty"`
}

// SearchHit is one ranked web search result.
type SearchHit = web.Hit

// SearchResult is the outcome of a [Std.Search] invocation.
type SearchResult struct {
	Query     string        `json:"query"`
	Endpoint  string        `json:"endpoint"`
	Hits      []SearchHit   `json:"hits"`
	Truncated bool          `json:"truncated,omitempty"`
	Duration  time.Duration `json:"duration"`
	DryRun    bool          `json:"dry_run,omitempty"`
}

// Search executes a web search from the daemon host using a dedicated HTTP
// client with its own timeout budget. Unlike [Std.Fetch], it deliberately does
// not route through the sandbox network client: search is a daemon-side
// capability the harness mediates through the permission pipeline, while the
// session's [sandbox.NetworkPolicy] keeps confining child processes unchanged.
func (s *Std) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	query := strings.Join(strings.Fields(req.Query), " ")
	if query == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}

	endpoint := s.searchEndpoint
	if override := strings.TrimSpace(req.Endpoint); override != "" {
		endpoint = override
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || (parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https") || parsedEndpoint.Host == "" {
		return nil, fmt.Errorf("%w: search endpoint %q must be an absolute http(s) URL", ErrInvalidArgument, endpoint)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}

	searchURL := web.BuildSearchURL(parsedEndpoint, query)
	if req.DryRun {
		return &SearchResult{Query: query, Endpoint: searchURL, DryRun: true}, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, DefaultSearchTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	httpReq.Header.Set("User-Agent", searchUserAgent)
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	httpReq.Header.Set("Accept-Language", "en-US,en;q=0.5")
	httpReq.Header.Set("Upgrade-Insecure-Requests", "1")

	start := time.Now()
	resp, err := s.searchClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search request failed with status %d", resp.StatusCode)
	}

	// Read one byte past the cap so a body that exceeds it is rejected
	// instead of silently parsed as truncated HTML.
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxSearchResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > MaxSearchResponseBytes {
		return nil, fmt.Errorf("%w: %d byte limit", ErrSearchResponseTooLarge, MaxSearchResponseBytes)
	}

	hits, err := web.ParseSearchHTML(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	duration := time.Since(start)

	filtered, truncated := web.FilterHits(hits, req.AllowedDomains, req.BlockedDomains, limit)

	s.logger.Debug("searched the web",
		log.String("query", query),
		log.String("endpoint", searchURL),
		log.Int("results", len(filtered)),
	)

	return &SearchResult{
		Query:     query,
		Endpoint:  searchURL,
		Hits:      filtered,
		Truncated: truncated,
		Duration:  duration,
	}, nil
}

// resolveSearchEndpoint normalizes a configured default search endpoint,
// falling back to [DefaultSearchEndpoint] when unset.
func resolveSearchEndpoint(endpoint string) string {
	if trimmed := strings.TrimSpace(endpoint); trimmed != "" {
		return trimmed
	}
	return DefaultSearchEndpoint
}
