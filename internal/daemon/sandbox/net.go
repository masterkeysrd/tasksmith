package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sandboxNet implements the Net capability.
type sandboxNet struct {
	s         *session
	dialer    *net.Dialer
	transport *http.Transport
	client    *http.Client
}

func newSandboxNet(s *session) *sandboxNet {
	n := &sandboxNet{
		s: s,
		dialer: &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
	n.transport = &http.Transport{
		DialContext:           n.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	n.client = &http.Client{
		Transport: n.transport,
	}
	return n
}

// checkHostPolicy enforces Disabled, BlockedHosts, AllowedHosts, and literal IP
// or localhost loopback restrictions for a normalized hostname.
func checkHostPolicy(policy NetworkPolicy, rawHost, normalizedHost string) error {
	if policy.Disabled {
		return ErrNetworkDisabled
	}

	// 1. Check BlockedHosts first (denylist takes precedence).
	for _, pattern := range policy.BlockedHosts {
		if matchHostPattern(normalizedHost, pattern) {
			return fmt.Errorf("%w: %q matches blocked pattern %q", ErrHostBlocked, rawHost, pattern)
		}
	}

	// 2. Check AllowedHosts if non-empty.
	if len(policy.AllowedHosts) > 0 {
		allowed := false
		for _, pattern := range policy.AllowedHosts {
			if matchHostPattern(normalizedHost, pattern) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q is not in allowed_hosts", ErrHostBlocked, rawHost)
		}
	}

	// 3. Check literal IP or localhost loopback rules.
	if ip := net.ParseIP(normalizedHost); ip != nil {
		if !policy.AllowLoopback && (ip.IsLoopback() || ip.IsUnspecified()) {
			return fmt.Errorf("%w: %s", ErrLoopbackBlocked, ip.String())
		}
		return nil
	}

	if !policy.AllowLoopback && (normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost")) {
		return fmt.Errorf("%w: %s", ErrLoopbackBlocked, rawHost)
	}

	return nil
}

func (n *sandboxNet) ValidateURL(rawURL string) (*url.URL, error) {
	if n.s.isClosed() {
		return nil, ErrSandboxClosed
	}

	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: url is required", ErrInvalidURL)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w: unsupported scheme %q (must be http or https)", ErrInvalidURL, u.Scheme)
	}

	rawHost := u.Hostname()
	normalizedHost := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawHost), "."))
	if normalizedHost == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidURL)
	}

	if err := checkHostPolicy(n.s.cfg.Network, rawHost, normalizedHost); err != nil {
		return nil, err
	}

	return u, nil
}

func (n *sandboxNet) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if n.s.isClosed() {
		return nil, ErrSandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	policy := n.s.cfg.Network
	if policy.Disabled {
		return nil, ErrNetworkDisabled
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("sandbox: invalid dial address %q: %w", addr, err)
	}

	normalizedHost := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if normalizedHost == "" {
		return nil, fmt.Errorf("sandbox: empty host in address %q", addr)
	}

	if err := checkHostPolicy(policy, host, normalizedHost); err != nil {
		return nil, err
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		return n.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}

	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, normalizedHost)
	if err != nil {
		return nil, err
	}
	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("sandbox: no IP addresses resolved for %q", host)
	}

	for _, ipAddr := range ipAddrs {
		if !policy.AllowLoopback && (ipAddr.IP.IsLoopback() || ipAddr.IP.IsUnspecified()) {
			return nil, fmt.Errorf("%w: host %q resolved to loopback/unspecified IP %s", ErrLoopbackBlocked, host, ipAddr.IP.String())
		}
	}

	// Dial the first reachable resolved IP directly to prevent TOCTOU DNS rebinding.
	var lastErr error
	for _, ipAddr := range ipAddrs {
		target := net.JoinHostPort(ipAddr.IP.String(), port)
		conn, dialErr := n.dialer.DialContext(ctx, network, target)
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func (n *sandboxNet) HTTPClient() *http.Client {
	return n.client
}

func (n *sandboxNet) closeIdleConnections() {
	if n.transport != nil {
		n.transport.CloseIdleConnections()
	}
}

// matchHostPattern checks if a lowercase normalized hostname matches an exact
// host or a "*.domain" wildcard pattern.
func matchHostPattern(host, rawPattern string) bool {
	pattern := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rawPattern), "."))
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if host == pattern {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
