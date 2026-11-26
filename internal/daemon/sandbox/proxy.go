package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrProxyClosed is returned when the proxy is asked to serve after Close.
var ErrProxyClosed = errors.New("sandbox: network proxy is closed")

// ProxyPolicy carries the domain rules the network proxy enforces on child
// process traffic. It mirrors the merged sandbox.json network posture.
type ProxyPolicy struct {
	// AllowedDomains lists exact hostnames and "*.domain" wildcard patterns
	// routed without prompting. A literal-IP pattern (e.g. "127.0.0.1") also
	// waives the reserved-IP guard for that address.
	AllowedDomains []string

	// DeniedDomains lists patterns that are always refused, taking precedence
	// over AllowedDomains and any host approval.
	DeniedDomains []string
}

// Clone returns a deep copy so stored policies cannot be mutated in place.
func (p ProxyPolicy) Clone() ProxyPolicy {
	return ProxyPolicy{
		AllowedDomains: append([]string(nil), p.AllowedDomains...),
		DeniedDomains:  append([]string(nil), p.DeniedDomains...),
	}
}

// HostApprover is consulted when child traffic targets a host that matches no
// policy pattern. Returning true admits the connection for this request only;
// persistent grants live in the caller's permission rules. A non-nil error is
// surfaced to the client alongside the refusal. The session package supplies
// an implementation wired to the permission engine ([HostApprovalFunc] adapts
// a plain function).
type HostApprover interface {
	ApproveHost(ctx context.Context, host string, port int) (bool, error)
}

// HostApprovalFunc adapts a plain function to [HostApprover].
type HostApprovalFunc func(ctx context.Context, host string, port int) (bool, error)

// ApproveHost implements [HostApprover].
func (f HostApprovalFunc) ApproveHost(ctx context.Context, host string, port int) (bool, error) {
	return f(ctx, host, port)
}

// LookupIP resolves a hostname; tests inject deterministic tables to exercise
// the DNS-rebinding guard without live DNS.
type LookupIP func(ctx context.Context, host string) ([]net.IPAddr, error)

// proxyDecision is the outcome of evaluating a host against the policy.
type proxyDecision int

const (
	proxyDecisionAllow proxyDecision = iota
	proxyDecisionDeny
	proxyDecisionAsk
)

// normalizeProxyHost lowercases a hostname, strips a trailing root dot, and
// unwraps bracketed IPv6 literals ("[::1]" -> "::1").
func normalizeProxyHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimSuffix(h, ".")
	if len(h) >= 2 && strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	return h
}

// splitProxyAddr splits a "host:port" authority while keeping bracketed IPv6
// literals intact.
func splitProxyAddr(addr string) (host, port string, err error) {
	h, p, splitErr := net.SplitHostPort(strings.TrimSpace(addr))
	if splitErr != nil {
		return "", "", fmt.Errorf("sandbox: invalid proxy target %q: %w", addr, splitErr)
	}
	return normalizeProxyHost(h), p, nil
}

// isBlockedProxyIP reports whether an address belongs to a range reserved for
// the host or the local network: loopback, the entire 0/8 "this network"
// block (0.0.0.1-0.255.255.255 route to loopback on Linux and often on
// macOS), unspecified, link-local (which includes the 169.254.169.254 cloud
// metadata endpoint), the AWS IPv6 metadata address (fd00:ec2::254), and
// multicast. Site-local (RFC1918) space is intentionally not blocked:
// private networks legitimately host allowed domains.
func isBlockedProxyIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 0 {
		return true
	}
	if ip.String() == awsIPv6Metadata {
		return true
	}
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

// awsIPv6Metadata is the AWS instance metadata service IPv6 address, which
// sits outside the link-local range Seatbelt and the kernel would otherwise
// catch.
const awsIPv6Metadata = "fd00:ec2::254"

// maxUnixSocketPathLen is the portable sockaddr_un sun_path budget
// (104 bytes on macOS/BSD, 108 on Linux); the smaller value keeps socket
// binds portable across hosts.
const maxUnixSocketPathLen = 104

// classifyProxyHost evaluates a normalized hostname against the policy. It is
// DNS-free and side-effect-free so the full rule matrix is unit-testable.
// Denied wins over Allowed.
func classifyProxyHost(policy ProxyPolicy, normalizedHost string) proxyDecision {
	for _, pattern := range policy.DeniedDomains {
		if matchHostPattern(normalizedHost, pattern) {
			return proxyDecisionDeny
		}
	}
	for _, pattern := range policy.AllowedDomains {
		if matchHostPattern(normalizedHost, pattern) {
			return proxyDecisionAllow
		}
	}
	return proxyDecisionAsk
}

// reservedIPWaived reports whether the exact literal IP address is explicitly
// listed in AllowedDomains, which is the only policy way to open a reserved
// (loopback/link-local/metadata) address: wildcards never waive the guard,
// and an allowed hostname alone never re-opens the addresses it happens to
// resolve to (DNS rebinding defense). Both sides are canonicalized through
// net.IP so equivalent IPv6 / IPv4-mapped spellings match consistently.
func reservedIPWaived(policy ProxyPolicy, literalIP string) bool {
	want := canonicalProxyIP(literalIP)
	if want == "" {
		return false
	}
	for _, pattern := range policy.AllowedDomains {
		if canonicalProxyIP(pattern) == want {
			return true
		}
	}
	return false
}

// canonicalProxyIP normalizes an IP literal (brackets allowed) to its
// canonical string form, returning "" when the value is not an IP.
func canonicalProxyIP(raw string) string {
	h := normalizeProxyHost(raw)
	if h == "" {
		return ""
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return ""
	}
	return ip.String()
}

type proxyState struct {
	// policyMu guards policy; approvals and dials read it per request.
	policyMu sync.RWMutex
	policy   ProxyPolicy
	appver   atomic.Pointer[HostApprover]
}

func (st *proxyState) currentPolicy() ProxyPolicy {
	st.policyMu.RLock()
	defer st.policyMu.RUnlock()
	return st.policy
}

func (st *proxyState) setPolicy(p ProxyPolicy) {
	st.policyMu.Lock()
	st.policy = p
	st.policyMu.Unlock()
}

// proxyDialer enforces the policy for one connection flow: it evaluates the
// hostname, requests approval for unlisted hosts, resolves DNS on the daemon
// side, refuses targets resolving into reserved ranges, and dials the
// resolved IP directly so a second lookup cannot rebind the target between
// check and connect (DNS rebinding).
type proxyDialer struct {
	st      *proxyState
	dialer  *net.Dialer
	resolve LookupIP
}

func newProxyDialer(st *proxyState, resolve LookupIP) *proxyDialer {
	if resolve == nil {
		resolve = net.DefaultResolver.LookupIPAddr
	}
	return &proxyDialer{
		st: st,
		dialer: &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		resolve: resolve,
	}
}

// dial connects to host:port under the current policy. host must be
// normalized; port is the decimal service string.
func (d *proxyDialer) dial(ctx context.Context, network, host, port string) (net.Conn, error) {
	policy := d.st.currentPolicy()
	switch classifyProxyHost(policy, host) {
	case proxyDecisionDeny:
		return nil, fmt.Errorf("%w: %q is blocked by sandbox network policy", ErrHostBlocked, host)
	case proxyDecisionAsk:
		approver := d.st.appver.Load()
		if approver == nil || *approver == nil {
			return nil, fmt.Errorf("%w: %q is not in the sandbox network allowlist", ErrHostBlocked, host)
		}
		portNum, err := parseProxyPort(port)
		if err != nil {
			return nil, err
		}
		ok, err := (*approver).ApproveHost(ctx, host, portNum)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: host approval denied for %q", ErrHostBlocked, host)
		}
	}

	if ip := net.ParseIP(host); ip != nil {
		// A reserved literal stays gated behind an exact allowedDomains
		// entry even with an interactive approval: prompts are about
		// hostnames, and approving a metadata/loopback literal by accident
		// must not open it.
		if isBlockedProxyIP(ip) && !reservedIPWaived(policy, host) {
			return nil, fmt.Errorf("%w: %s", ErrLoopbackBlocked, ip.String())
		}
		return d.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}

	addrs, err := d.resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("sandbox: no addresses resolved for %q", host)
	}
	// A hostname may not re-open a reserved address by resolving to one:
	// reaching loopback/link-local/metadata space always requires the literal
	// IP in AllowedDomains, whether the host was allowlisted or just approved
	// interactively. An approval only puts a hostname on the same footing as
	// an allowedDomains entry, never above it. The checked IPs are dialed
	// directly, so a later DNS answer cannot rebind the target.
	for _, a := range addrs {
		if isBlockedProxyIP(a.IP) && !reservedIPWaived(policy, a.IP.String()) {
			return nil, fmt.Errorf("%w: host %q resolves to reserved address %s",
				ErrLoopbackBlocked, host, a.IP.String())
		}
	}

	var lastErr error
	for _, a := range addrs {
		conn, dialErr := d.dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("sandbox: dial %q: %w", host, lastErr)
}

// dialContext adapts the policy-checked dialer to http.Transport, which
// supplies the original "host:port" authority of each request.
func (d *proxyDialer) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := splitProxyAddr(addr)
	if err != nil {
		return nil, err
	}
	if host == "" {
		return nil, fmt.Errorf("sandbox: empty host in address %q", addr)
	}
	return d.dial(ctx, network, host, port)
}

func parseProxyPort(port string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("sandbox: invalid port %q", port)
	}
	return n, nil
}

// Proxy is a daemon-side HTTP and CONNECT proxy enforcing the sandbox.json
// network allowlist for sandboxed child processes. Child processes reach it
// through the proxy environment variables injected into their environment;
// the kernel profile denies them every other outbound path.
type Proxy struct {
	st        *proxyState
	address   string
	listener  net.Listener
	unixPath  string
	unix      net.Listener
	server    *http.Server
	dialer    *proxyDialer
	transport *http.Transport
	closed    atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
	closeMu   sync.Mutex
}

// ProxyConfig configures a [Proxy].
type ProxyConfig struct {
	// Policy is the initial domain policy; it can be swapped at runtime via
	// [Proxy.SetPolicy].
	Policy ProxyPolicy

	// Approver is consulted for hosts matching no policy pattern. It may be
	// replaced later via [Proxy.SetApprover].
	Approver HostApprover

	// Resolve optionally overrides DNS resolution (tests).
	Resolve LookupIP

	// UnixSocket optionally binds an additional listener at the given path so
	// network-namespaced sandboxes can reach the proxy over a filesystem
	// socket. A bind failure is returned to the caller.
	UnixSocket string
}

// NewProxy binds the proxy to an ephemeral loopback TCP port and starts
// serving.
func NewProxy(cfg ProxyConfig) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("sandbox: proxy listen: %w", err)
	}

	st := &proxyState{policy: cfg.Policy.Clone()}
	if cfg.Approver != nil {
		appver := cfg.Approver
		st.appver.Store(&appver)
	}

	p := &Proxy{
		st:       st,
		address:  ln.Addr().String(),
		listener: ln,
		dialer:   newProxyDialer(st, cfg.Resolve),
	}
	// One shared transport for all forwarded HTTP requests: per-request
	// transports leak their keep-alive connections. The policy-checked
	// dialer runs on every new upstream connection.
	p.transport = &http.Transport{
		DialContext:         p.dialer.dialContext,
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     60 * time.Second,
	}
	p.server = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 20 * time.Second,
	}

	if cfg.UnixSocket != "" {
		path := filepath.Clean(cfg.UnixSocket)
		// The kernel sun_path limit (~104 bytes on macOS/BSD, 108 on
		// Linux) rejects longer binds with a confusing errno; report it
		// explicitly instead.
		if len(path) > maxUnixSocketPathLen {
			_ = ln.Close()
			return nil, fmt.Errorf("sandbox: proxy unix socket path %q is %d bytes, over the %d-byte socket path limit", path, len(path), maxUnixSocketPathLen)
		}
		// A leftover socket file (e.g. from a crashed previous run sharing the
		// temp dir) would make bind() fail with EADDRINUSE.
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(path)
		}
		ul, err := net.Listen("unix", path)
		if err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("sandbox: proxy unix listen: %w", err)
		}
		p.unixPath = path
		p.unix = ul
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.server.BaseContext = func(net.Listener) context.Context { return ctx }

	go func() { _ = p.server.Serve(ln) }()
	if p.unix != nil {
		go func() { _ = p.server.Serve(p.unix) }()
	}
	return p, nil
}

// Addr returns the "127.0.0.1:<port>" address child processes proxy through.
func (p *Proxy) Addr() string { return p.address }

// Port returns the TCP port the proxy listens on.
func (p *Proxy) Port() int {
	_, port, err := net.SplitHostPort(p.address)
	if err != nil {
		return 0
	}
	n, err := parseProxyPort(port)
	if err != nil {
		return 0
	}
	return n
}

// UnixSocket returns the optional unix socket path the proxy also serves.
func (p *Proxy) UnixSocket() string { return p.unixPath }

// SetPolicy atomically swaps the domain policy for subsequent connections.
func (p *Proxy) SetPolicy(policy ProxyPolicy) { p.st.setPolicy(policy) }

// Policy returns a deep copy of the current policy.
func (p *Proxy) Policy() ProxyPolicy { return p.st.currentPolicy() }

// SetApprover swaps the unlisted-host approver; nil refuses unlisted hosts.
func (p *Proxy) SetApprover(a HostApprover) {
	if a == nil {
		p.st.appver.Store(nil)
		return
	}
	p.st.appver.Store(&a)
}

// DialContext exposes the policy-checked dialer for in-process consumers.
func (p *Proxy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return p.dialer.dialContext(ctx, network, addr)
}

// Close stops the proxy and removes its unix socket file.
func (p *Proxy) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	p.cancel()
	err := p.server.Close()
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
	if p.unix != nil {
		_ = p.unix.Close()
		_ = os.Remove(p.unixPath)
	}
	return err
}

// ServeHTTP implements both plain HTTP forwarding and CONNECT tunneling.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.closed.Load() {
		http.Error(w, "proxy closed", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host, port, err := splitProxyAddr(r.Host)
	if err != nil || host == "" {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}

	conn, err := p.dialer.dial(r.Context(), "tcp", host, port)
	if err != nil {
		http.Error(w, proxyDenialMessage(err), http.StatusForbidden)
		return
	}
	defer func() { _ = conn.Close() }()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy cannot hijack", http.StatusInternalServerError)
		return
	}
	client, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()

	closeBoth := func() {
		_ = client.Close()
		_ = conn.Close()
	}
	stop := context.AfterFunc(p.ctx, closeBoth)
	defer stop()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	var clientReader io.Reader = client
	if n := brw.Reader.Buffered(); n > 0 {
		clientReader = io.MultiReader(io.LimitReader(brw.Reader, int64(n)), client)
	}

	forward := func(dst net.Conn, src io.Reader) {
		if _, err := io.Copy(dst, src); err == nil {
			if half, ok := dst.(interface{ CloseWrite() error }); ok && half.CloseWrite() == nil {
				return
			}
		}
		closeBoth()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		forward(conn, clientReader)
	}()
	forward(client, conn)
	<-done
}

var proxyHopHeaders = []string{
	"Proxy-Connection",
	"Proxy-Authorization",
	"Proxy-Authenticate",
	"Keep-Alive",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" {
		http.Error(w, "unsupported proxy scheme", http.StatusBadGateway)
		return
	}
	if normalizeProxyHost(r.URL.Hostname()) == "" {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}
	if r.URL.Port() == "" {
		r.URL.Host = r.URL.Host + ":80"
	}

	// The absolute-URI authority is the routing decision the policy was
	// evaluated against; the Host header must follow it. Otherwise a client
	// could dial an allowed domain while smuggling a denied virtual host
	// through Host (domain fronting).
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	outReq.Host = outReq.URL.Host
	for _, h := range proxyHopHeaders {
		outReq.Header.Del(h)
	}

	resp, err := p.transport.RoundTrip(outReq)
	if err != nil {
		if errors.Is(err, ErrHostBlocked) || errors.Is(err, ErrLoopbackBlocked) {
			http.Error(w, proxyDenialMessage(err), http.StatusForbidden)
		} else {
			http.Error(w, fmt.Sprintf("proxy upstream: %v", err), http.StatusBadGateway)
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func proxyDenialMessage(err error) string {
	return strings.TrimPrefix(err.Error(), "sandbox: ")
}
