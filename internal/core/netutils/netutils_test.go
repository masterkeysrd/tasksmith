package netutils

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"https://EXAMPLE.com:443/path", "https://example.com?q=v", true},
		{"http://[::1]", "http://[::1]:80", true},
		{"http://example.com", "https://example.com", false},
		{"https://example.com", "https://example.com:8443", false},
		{"https://example.com", "https://other.example.com", false},
		{"file://example.com", "file://example.com", false},
		{"relative", "relative", false},
		{"%zz", "%zz", false},
	} {
		if got := SameOrigin(tc.a, tc.b); got != tc.want {
			t.Errorf("SameOrigin(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckSameOriginRedirect(t *testing.T) {
	original := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = original
	}
	if err := CheckSameOriginRedirect(original, via[:9]); err != nil {
		t.Fatal(err)
	}
	if err := CheckSameOriginRedirect(original, via); err == nil {
		t.Fatal("allowed tenth redirect")
	}
	other := &http.Request{URL: &url.URL{Scheme: "https", Host: "other.example.com"}}
	if err := CheckSameOriginRedirect(other, via[:1]); err == nil {
		t.Fatal("allowed cross-origin redirect")
	}
}
