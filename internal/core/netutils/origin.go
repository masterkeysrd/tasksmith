package netutils

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func SameOrigin(first, second string) bool {
	a, errA := url.Parse(first)
	b, errB := url.Parse(second)
	return errA == nil && errB == nil && SameHTTPOrigin(a, b)
}

func SameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || a.Scheme != b.Scheme || a.Hostname() == "" || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	defaultPort := "443"
	switch a.Scheme {
	case "https":
	case "http":
		defaultPort = "80"
	default:
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		return defaultPort
	}
	return port(a) == port(b)
}

func CheckSameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if !SameHTTPOrigin(req.URL, via[0].URL) {
		return errors.New("refusing cross-origin redirect")
	}
	return nil
}
