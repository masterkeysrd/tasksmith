package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const (
	// MaxSearchResults is the maximum number of search results returned.
	MaxSearchResults = 10
	// MaxSnippetLength is the maximum character length for result snippets.
	MaxSnippetLength = 300
)

// WebSearch searches the web using DuckDuckGo.
func (h *ToolHandlers) WebSearch(ctx context.Context, in WebSearchArgs) (WebSearchOutput, error) {
	if in.Query == "" {
		return WebSearchOutput{}, fmt.Errorf("query cannot be empty")
	}

	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(in.Query))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Mimic a modern web browser to avoid captcha / blocks
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return WebSearchOutput{}, fmt.Errorf("search request failed with status code %d", resp.StatusCode)
	}

	limit := in.MaxResults
	if limit <= 0 {
		limit = 10
	} else if limit > 20 {
		limit = 20
	}

	results, err := parseDuckDuckGoHTML(resp.Body, limit)
	if err != nil {
		return WebSearchOutput{}, fmt.Errorf("failed to parse search results: %w", err)
	}

	return WebSearchOutput{Results: results}, nil
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable list of search results instead of a raw JSON blob.
func (o WebSearchOutput) TextContent() string {
	if len(o.Results) == 0 {
		return "No search results found."
	}

	var sb strings.Builder
	for i, res := range o.Results {
		fmt.Fprintf(&sb, "%d. %s\n   URL: %s\n   %s\n\n", i+1, res.Title, res.Url, res.Snippet)
	}

	res := strings.TrimSuffix(sb.String(), "\n\n")
	return res
}

func parseDuckDuckGoHTML(r io.Reader, maxResults int) ([]WebSearchOutputResultsItem, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}

	// Check for CAPTCHA/anomaly modal
	if doc.Find(".anomaly-modal__modal, .anomaly-modal__title, .anomaly-modal").Length() > 0 {
		return nil, fmt.Errorf("DuckDuckGo bot protection triggered (CAPTCHA). Try searching again later or use a different search query")
	}

	var results []WebSearchOutputResultsItem

	doc.Find("div.result, div.web-result").Each(func(i int, s *goquery.Selection) {
		if len(results) >= maxResults {
			return
		}

		anchor := s.Find(".result__a")
		if anchor.Length() == 0 {
			return
		}

		title := strings.TrimSpace(anchor.Text())
		rawURL, ok := anchor.Attr("href")
		if !ok || rawURL == "" {
			return
		}

		cleanedURL := cleanURL(rawURL)

		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())
		if len(snippet) > MaxSnippetLength {
			snippet = snippet[:MaxSnippetLength] + "..."
		}

		if title != "" && cleanedURL != "" {
			results = append(results, WebSearchOutputResultsItem{
				Title:   title,
				Url:     cleanedURL,
				Snippet: snippet,
			})
		}
	})

	return results, nil
}

func cleanURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}
	var u *url.URL
	var err error
	if strings.HasPrefix(rawURL, "/") {
		u, err = url.Parse("https://duckduckgo.com" + rawURL)
	} else {
		u, err = url.Parse(rawURL)
	}
	if err == nil {
		if target := u.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return rawURL
}
