package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeProxyHost(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"Example.COM.", "example.com"},
		{"[::1]", "::1"},
		{"[2001:DB8::1]:8080", "[2001:db8::1]:8080"}, // only a bare bracket pair unwraps
		{"  127.0.0.1  ", "127.0.0.1"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeProxyHost(tc.raw); got != tc.want {
			t.Errorf("normalizeProxyHost(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSplitProxyAddr(t *testing.T) {
	cases := []struct {
		addr     string
		wantHost string
		wantPort string
		wantErr  bool
	}{
		{"example.com:443", "example.com", "443", false},
		{"[::1]:8443", "::1", "8443", false},
		{"[2001:db8::1]:80", "2001:db8::1", "80", false},
		{"127.0.0.1:9", "127.0.0.1", "9", false},
		{"nocolon", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range cases {
		host, port, err := splitProxyAddr(tc.addr)
		if tc.wantErr {
			if err == nil {
				t.Errorf("splitProxyAddr(%q) expected error", tc.addr)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitProxyAddr(%q) unexpected error: %v", tc.addr, err)
			continue
		}
		if host != tc.wantHost || port != tc.wantPort {
			t.Errorf("splitProxyAddr(%q) = (%q,%q), want (%q,%q)", tc.addr, host, port, tc.wantHost, tc.wantPort)
		}
	}
}

func TestIsBlockedProxyIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "127.5.5.5", "::1", "169.254.169.254", "fe80::1", "224.0.0.5", "0.0.0.0"}
	for _, raw := range blocked {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("bad test IP %q", raw)
		}
		if !isBlockedProxyIP(ip) {
			t.Errorf("isBlockedProxyIP(%s) = false, want true", raw)
		}
	}
	permitted := []string{"8.8.8.8", "2001:4860:4860::8888", "10.1.2.3", "192.168.64.5", "172.16.0.1"}
	for _, raw := range permitted {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("bad test IP %q", raw)
		}
		if isBlockedProxyIP(ip) {
			t.Errorf("isBlockedProxyIP(%s) = true, want false (RFC1918 space is intentionally allowed)", raw)
		}
	}
}

func TestClassifyProxyHost(t *testing.T) {
	policy := ProxyPolicy{
		AllowedDomains: []string{"api.example.com", "*.trusted.dev", "127.0.0.1"},
		DeniedDomains:  []string{"bad.example.com", "*.evil.test"},
	}
	cases := []struct {
		host string
		want proxyDecision
	}{
		{"api.example.com", proxyDecisionAllow},
		{"deep.sub.trusted.dev", proxyDecisionAllow},
		{"trusted.dev", proxyDecisionAsk}, // wildcard matches subdomains only
		{"bad.example.com", proxyDecisionDeny},
		{"a.b.evil.test", proxyDecisionDeny},
		{"unknown.org", proxyDecisionAsk},
		{"127.0.0.1", proxyDecisionAllow},
	}
	for _, tc := range cases {
		if got := classifyProxyHost(policy, tc.host); got != tc.want {
			t.Errorf("classifyProxyHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestClassifyProxyHostDeniedBeatsAllowed(t *testing.T) {
	policy := ProxyPolicy{
		AllowedDomains: []string{"*"},
		DeniedDomains:  []string{"metadata.example"},
	}
	if got := classifyProxyHost(policy, "metadata.example"); got != proxyDecisionDeny {
		t.Fatalf("denied pattern must win over a wildcard allow, got %v", got)
	}
}

func TestWaivesReservedIPGuard(t *testing.T) {
	exact := ProxyPolicy{AllowedDomains: []string{"127.0.0.1", "ok.test"}}
	if !reservedIPWaived(exact, "127.0.0.1") {
		t.Error("exact literal 127.0.0.1 in allowedDomains must waive the reserved-IP guard")
	}

	wildcard := ProxyPolicy{AllowedDomains: []string{"*.local", "ok.test"}}
	if reservedIPWaived(wildcard, "127.0.0.1") {
		t.Error("hostname patterns must never waive the reserved-IP guard for an IP literal")
	}
	if reservedIPWaived(ProxyPolicy{}, "127.0.0.1") {
		t.Error("empty policy must not waive the guard")
	}
	// Listing a hostname never waives the loopback address it may resolve
	// to: only the literal IP itself does (covered end-to-end by the
	// dialer rebinding tests).
	if reservedIPWaived(exact, "169.254.169.254") {
		t.Error("unlisted reserved address must not waive the guard")
	}
}

func TestProxyDialerBlocksLoopbackResolution(t *testing.T) {
	// An allowed hostname whose DNS answer points at loopback must still be
	// refused: this is the DNS-rebinding shape the daemon-side guard exists
	// for.
	st := &proxyState{policy: ProxyPolicy{AllowedDomains: []string{"evil.test"}}}
	d := newProxyDialer(st, func(_ context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	if _, err := d.dial(context.Background(), "tcp", "evil.test", "80"); !errors.Is(err, ErrLoopbackBlocked) {
		t.Fatalf("expected ErrLoopbackBlocked for loopback resolution, got %v", err)
	}
}

func TestProxyDialerBlocksMetadataEndpoint(t *testing.T) {
	st := &proxyState{policy: ProxyPolicy{AllowedDomains: []string{"metadata.example"}}}
	d := newProxyDialer(st, func(_ context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	})
	if _, err := d.dial(context.Background(), "tcp", "metadata.example", "80"); !errors.Is(err, ErrLoopbackBlocked) {
		t.Fatalf("expected ErrLoopbackBlocked for 169.254.169.254, got %v", err)
	}

	// Even the literal metadata IP stays blocked before any dial is
	// attempted when only a wildcard pattern (which does match the IP
	// string) allows it: wildcards never waive the reserved-IP guard.
	st2 := &proxyState{policy: ProxyPolicy{AllowedDomains: []string{"*.254.169.254"}}}
	d2 := newProxyDialer(st2, nil)
	if _, err := d2.dial(context.Background(), "tcp", "169.254.169.254", "80"); !errors.Is(err, ErrLoopbackBlocked) {
		t.Fatalf("expected ErrLoopbackBlocked for direct metadata IP under wildcard policy, got %v", err)
	}
}

func TestProxyDialerDenyAndUnknownWithoutApprover(t *testing.T) {
	st := &proxyState{policy: ProxyPolicy{DeniedDomains: []string{"blocked.test"}}}
	d := newProxyDialer(st, nil)
	if _, err := d.dial(context.Background(), "tcp", "blocked.test", "80"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("denied host: expected ErrHostBlocked, got %v", err)
	}
	if _, err := d.dial(context.Background(), "tcp", "unknown.test", "80"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("unlisted host without approver: expected ErrHostBlocked, got %v", err)
	}
}

func TestProxyDialerConsultsApprover(t *testing.T) {
	var gotHost string
	var gotPort int
	st := &proxyState{policy: ProxyPolicy{}}
	var approver HostApprover = HostApprovalFunc(func(_ context.Context, host string, port int) (bool, error) {
		gotHost, gotPort = host, port
		return false, nil
	})
	st.appver.Store(&approver)
	d := newProxyDialer(st, nil)
	if _, err := d.dial(context.Background(), "tcp", "ask.test", "8080"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("rejected approval: expected ErrHostBlocked, got %v", err)
	}
	if gotHost != "ask.test" || gotPort != 8080 {
		t.Errorf("approver saw (%q,%d), want (ask.test,8080)", gotHost, gotPort)
	}
}

func TestProxyDialerApprovedHostCannotOpenReservedResolution(t *testing.T) {
	// An interactive approval only puts a hostname on the same footing as an
	// allowedDomains entry: resolving to loopback/link-local/metadata space
	// still requires the literal IP in AllowedDomains, otherwise a single
	// Allow-once click on an attacker-chosen domain could SSRF the host.
	approver := HostApprover(HostApprovalFunc(func(context.Context, string, int) (bool, error) { return true, nil }))
	st := &proxyState{policy: ProxyPolicy{}}
	st.appver.Store(&approver)
	d := newProxyDialer(st, func(_ context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	if _, err := d.dial(context.Background(), "tcp", "evil.example", "80"); !errors.Is(err, ErrLoopbackBlocked) {
		t.Fatalf("approved host resolving to loopback must stay blocked, got %v", err)
	}

	// Listing the literal re-opens exactly that address for hostnames
	// resolving to it (and for the literal dialed directly).
	st.setPolicy(ProxyPolicy{AllowedDomains: []string{"evil.example", "127.0.0.1"}})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listen unavailable: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	conn, err := d.dial(context.Background(), "tcp", "evil.example", port)
	if err != nil {
		t.Fatalf("host resolving to a literally allowed address must connect: %v", err)
	}
	_ = conn.Close()
}

func TestProxyHTTPDeniedAndAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "upstream-ok")
	}))
	defer upstream.Close()
	upURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	upHost, upPort, err := net.SplitHostPort(upURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	p, err := NewProxy(ProxyConfig{Policy: ProxyPolicy{
		AllowedDomains: []string{upHost},
		DeniedDomains:  []string{"nope.test"},
	}})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer func() { _ = p.Close() }()

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) {
				return &url.URL{Scheme: "http", Host: p.Addr()}, nil
			},
		},
		Timeout: 10 * time.Second,
	}

	// Allowed loopback literal (exact pattern waives the reserved-IP guard).
	resp, err := client.Get(upstream.URL + "/probe")
	if err != nil {
		t.Fatalf("allowed request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "upstream-ok" {
		t.Fatalf("allowed request: got %d %q", resp.StatusCode, body)
	}

	// Denied host: 403 from the proxy itself.
	denied := fmt.Sprintf("http://nope.test:%s/", upPort)
	resp, err = client.Get(denied)
	if err != nil {
		t.Fatalf("denied request transport error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied host: want 403, got %d", resp.StatusCode)
	}

	// Unlisted host without an approver: refused immediately with 403.
	unlisted := fmt.Sprintf("http://unlisted.test:%s/", upPort)
	resp, err = client.Get(unlisted)
	if err != nil {
		t.Fatalf("unlisted request transport error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unlisted host: want 403, got %d", resp.StatusCode)
	}
}

func TestProxyConnectTunnel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listen unavailable: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn) // echo
			}()
		}
	}()
	_, echoPort, _ := net.SplitHostPort(ln.Addr().String())

	p, err := NewProxy(ProxyConfig{Policy: ProxyPolicy{AllowedDomains: []string{"127.0.0.1"}}})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer func() { _ = p.Close() }()

	sendConnect := func(target string) (string, error) {
		conn, derr := net.DialTimeout("tcp", p.Addr(), 5*time.Second)
		if derr != nil {
			return "", derr
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, werr := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); werr != nil {
			return "", werr
		}
		status, rerr := bufio.NewReader(conn).ReadString('\n')
		return status, rerr
	}

	status, err := sendConnect("127.0.0.1:" + echoPort)
	if err != nil {
		t.Fatalf("CONNECT allowed target: %v", err)
	}
	if want := "HTTP/1.1 200"; len(status) < len(want) || status[:len(want)] != want {
		t.Fatalf("CONNECT allowed target: want %q, got %q", want, status)
	}

	status, err = sendConnect("169.254.169.254:80")
	if err != nil {
		t.Fatalf("CONNECT metadata target: %v", err)
	}
	if want := "HTTP/1.1 403"; len(status) < len(want) || status[:len(want)] != want {
		t.Fatalf("CONNECT metadata target: want 403, got %q", status)
	}
}

func TestProxyPolicyAndApproverSwaps(t *testing.T) {
	p, err := NewProxy(ProxyConfig{Policy: ProxyPolicy{DeniedDomains: []string{"a.test"}}})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer func() { _ = p.Close() }()

	if got := p.Policy().DeniedDomains; len(got) != 1 || got[0] != "a.test" {
		t.Fatalf("initial policy not stored: %+v", p.Policy())
	}
	p.SetPolicy(ProxyPolicy{AllowedDomains: []string{"b.test"}})
	if got := p.Policy(); len(got.DeniedDomains) != 0 || len(got.AllowedDomains) != 1 {
		t.Fatalf("SetPolicy must replace wholesale: %+v", got)
	}

	approver := HostApprovalFunc(func(context.Context, string, int) (bool, error) { return true, nil })
	p.SetApprover(approver)
	p.SetApprover(nil) // back to refuse-immediately for unlisted hosts

	if p.Port() <= 0 || p.Addr() == "" {
		t.Fatalf("proxy must expose address and port, got %q/%d", p.Addr(), p.Port())
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close must be idempotent: %v", err)
	}
}

func TestProxyUnixSocket(t *testing.T) {
	// Keep the socket path short: <TMPDIR>/<TestName>/<n>/net.sock can
	// exceed the 104-byte sun_path limit under long test names on macOS.
	sockDir, err := os.MkdirTemp("", "tspx")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s.sock")

	p, err := NewProxy(ProxyConfig{
		Policy:     ProxyPolicy{AllowedDomains: []string{"127.0.0.1"}},
		UnixSocket: sock,
	})
	if err != nil {
		t.Skipf("unix socket bind unavailable in this environment: %v", err)
	}
	defer func() { _ = p.Close() }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "via-unix")
	}))
	defer upstream.Close()

	// The child-side shape over a unix socket: an absolute-URI request to
	// the proxy (Transport.Proxy set), with DialContext pointed at the
	// socket instead of the proxy's TCP address.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
			Proxy: func(*http.Request) (*url.URL, error) {
				return &url.URL{Scheme: "http", Host: "unix:"}, nil
			},
		},
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("request over unix socket: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "via-unix" {
		t.Fatalf("request over unix socket: got %d %q", resp.StatusCode, body)
	}

	_ = p.Close()
	if _, err := os.Stat(sock); err == nil {
		t.Error("Close must remove the unix socket file")
	}
}
