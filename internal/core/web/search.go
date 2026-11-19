package web

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

// MaxSnippetBytes is the maximum byte length of one result snippet.
const MaxSnippetBytes = 300

// ErrBotChallenge is returned when a search endpoint serves a CAPTCHA or
// anomaly interstitial instead of search results.
var ErrBotChallenge = errors.New("search engine bot protection triggered; try again later or rephrase the query")

// Hit is one ranked web search result.
type Hit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// BuildSearchURL appends the encoded query to the search endpoint, preserving
// any query parameters the endpoint itself carries.
func BuildSearchURL(endpoint *url.URL, query string) string {
	u := *endpoint
	params := u.Query()
	params.Set("q", query)
	u.RawQuery = params.Encode()
	return u.String()
}

// ParseSearchHTML extracts the ranked results from a search engine results
// page.
func ParseSearchHTML(r io.Reader) ([]Hit, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse search results: %w", err)
	}

	if isBotChallenge(doc) {
		return nil, ErrBotChallenge
	}

	var hits []Hit
	for _, block := range resultBlocks(doc) {
		anchor := firstWithClass(block, "result__a")
		if anchor == nil {
			continue
		}
		href := CleanResultURL(attrValue(anchor, "href"))
		title := collapseText(anchor)
		if title == "" || href == "" {
			continue
		}
		snippet := ""
		if snippetNode := firstWithClass(block, "result__snippet"); snippetNode != nil {
			snippet = collapseText(snippetNode)
		}
		snippet, _ = strutils.TruncateLine(snippet, MaxSnippetBytes, "…")
		hits = append(hits, Hit{Title: title, URL: href, Snippet: snippet})
	}

	return hits, nil
}

// FilterHits applies the optional domain allow/block filters and the final
// result cap. Blocked domains win over allowed domains. When limit > 0, it
// caps the returned hits at limit and reports whether additional matching hits
// were dropped beyond the cap.
func FilterHits(hits []Hit, allowed, blocked []string, limit int) ([]Hit, bool) {
	out := make([]Hit, 0, len(hits))
	for _, hit := range hits {
		host := ResultHost(hit.URL)
		if MatchesAnyDomain(host, blocked) {
			continue
		}
		if len(allowed) > 0 && !MatchesAnyDomain(host, allowed) {
			continue
		}
		if limit > 0 && len(out) >= limit {
			return out, true
		}
		out = append(out, hit)
	}
	return out, false
}

// ResultHost returns the lowercase hostname of a result URL, or "" when the
// URL is unparsable or host-less.
func ResultHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
}

// MatchesAnyDomain reports whether host equals or is a subdomain of any of the
// given domain patterns, or any pattern is the "*" wildcard. Leading "*." and
// "." decorations are tolerated.
func MatchesAnyDomain(host string, domains []string) bool {
	if host == "" || len(domains) == 0 {
		return false
	}
	for _, raw := range domains {
		domain := strings.ToLower(strings.TrimSpace(raw))
		if domain == "*" {
			return true
		}
		domain = strings.TrimPrefix(domain, "*.")
		domain = strings.TrimPrefix(domain, ".")
		domain = strings.TrimSuffix(domain, ".")
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// CleanResultURL resolves a search engine result link to its destination.
// Result pages wrap the real URL in a redirect (/l/?uddg=<encoded>) so absolute,
// relative, and protocol-relative redirect forms all resolve to the target.
func CleanResultURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}

	candidate := value
	if strings.HasPrefix(candidate, "/") {
		candidate = "https://duckduckgo.com" + candidate
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return value
	}
	if target := parsed.Query().Get("uddg"); target != "" {
		return target
	}
	return value
}

// isBotChallenge reports whether a results page is a search engine's CAPTCHA or
// anomaly interstitial rather than actual results.
func isBotChallenge(doc *html.Node) bool {
	var found bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode {
			for class := range strings.FieldsSeq(attrValue(n, "class")) {
				if strings.HasPrefix(class, "anomaly-modal") {
					found = true
					return
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

// resultBlocks returns the result container elements of a results page.
func resultBlocks(doc *html.Node) []*html.Node {
	var blocks []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (hasClass(n, "result") || hasClass(n, "web-result")) {
			blocks = append(blocks, n)
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return blocks
}

// firstWithClass returns the first element at or beneath n carrying class.
func firstWithClass(n *html.Node, class string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && hasClass(n, class) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := firstWithClass(child, class); found != nil {
			return found
		}
	}
	return nil
}

// hasClass reports whether an element's class attribute contains the token.
func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attrValue(n, "class")), class)
}

// attrValue returns the value of the named attribute on an element node.
func attrValue(n *html.Node, name string) string {
	if n == nil {
		return ""
	}
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

// collapseText concatenates the text beneath n into a single whitespace-
// normalized line.
func collapseText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}
