package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestModeNativeAllowlistProxy is the end-to-end proof of the network
// allowlist posture: a kernel-sandboxed child reaches an explicitly allowed
// local server only through the daemon-side proxy, receives 403 for denied
// and unlisted hosts, and cannot bypass the proxy by disabling proxy
// handling in the client (curl --noproxy), because the kernel profile denies
// every outbound path except the proxy's loopback port.
func TestModeNativeAllowlistProxy(t *testing.T) {
	if err := checkNativeAvailable(); err != nil {
		t.Skipf("skipping live ModeNative test: %v", err)
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available for the allowlist integration test")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "allowlist-upstream-ok")
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sbx, err := New(Config{
		Mode:    ModeNative,
		WorkDir: t.TempDir(),
		Network: NetworkPolicy{
			Allowlist:     true,
			AllowedHosts:  []string{upHost},
			BlockedHosts:  []string{"blocked.test"},
			AllowLoopback: true,
		},
	})
	if err != nil {
		t.Fatalf("New allowlist sandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()

	if sbx.(*session).netProxyAddr() == "" {
		t.Fatal("allowlist ModeNative sandbox must start the network proxy")
	}

	runCurl := func(args ...string) (code string, stdout string, exit int, stderr string) {
		t.Helper()
		res, rerr := sbx.Proc().Exec(ctx, Command{
			Name: "curl",
			Args: append([]string{"-s", "-m", "20", "-w", "%{http_code}"}, args...),
		})
		if rerr != nil {
			t.Fatalf("curl exec: %v", rerr)
		}
		out := string(res.Stdout)
		if len(out) < 3 {
			return "", out, res.ExitCode, string(res.Stderr)
		}
		return out[len(out)-3:], out[:len(out)-3], res.ExitCode, string(res.Stderr)
	}

	// 1. Allowed local server: reaches the upstream through the proxy.
	code, body, exit, stderr := runCurl("http://" + upURL.Host + "/probe")
	if exit != 0 || code != "200" || body != "allowlist-upstream-ok" {
		t.Fatalf("allowed host: exit=%d code=%s body=%q stderr=%s", exit, code, body, stderr)
	}

	// 2. Denied host (mapped onto the local server via --resolve so no real
	// DNS is involved): the proxy itself answers 403.
	code, _, exit, stderr = runCurl("--resolve", "blocked.test:"+upPort+":127.0.0.1",
		"http://blocked.test:"+upPort+"/")
	if exit != 0 || code != "403" {
		t.Fatalf("denied host: exit=%d code=%s want 403, stderr=%s", exit, code, stderr)
	}

	// 3. Unlisted host without an approval callback: refused immediately
	// with 403.
	code, _, exit, stderr = runCurl("--resolve", "unlisted.test:"+upPort+":127.0.0.1",
		"http://unlisted.test:"+upPort+"/")
	if exit != 0 || code != "403" {
		t.Fatalf("unlisted host: exit=%d code=%s want 403, stderr=%s", exit, code, stderr)
	}

	// 4. No bypass: disabling proxy handling in the client must make the
	// same allowed request fail, because the kernel profile denies direct
	// outbound connections.
	_, _, exit, stderr = runCurl("--noproxy", "*", "http://"+upURL.Host+"/probe")
	if exit == 0 {
		t.Fatal("curl --noproxy bypass must be blocked by the kernel profile")
	}
	if !strings.Contains(strings.ToLower(stderr), "connect") &&
		!strings.Contains(strings.ToLower(stderr), "proxy") &&
		!strings.Contains(strings.ToLower(stderr), "timed out") &&
		!strings.Contains(strings.ToLower(stderr), "failed") {
		t.Logf("bypass attempt stderr (informational): %s", stderr)
	}
}

// TestModeNativeAllowlistInjectsProxyEnv verifies the child environment
// carries the proxy URL and an empty NO_PROXY so tools cannot silently opt
// out of proxying.
func TestModeNativeAllowlistInjectsProxyEnv(t *testing.T) {
	if err := checkNativeAvailable(); err != nil {
		t.Skipf("skipping live ModeNative test: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sbx, err := New(Config{
		Mode:    ModeNative,
		WorkDir: t.TempDir(),
		Network: NetworkPolicy{Allowlist: true, AllowedHosts: []string{"api.example.com"}},
	})
	if err != nil {
		t.Fatalf("New allowlist sandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()

	proxyAddr := sbx.(*session).netProxyAddr()
	if proxyAddr == "" {
		t.Fatal("expected an active network proxy")
	}

	res, err := sbx.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", `printf 'HTTP_PROXY=%s\nNO_PROXY=%s\n' "$HTTP_PROXY" "$NO_PROXY"`},
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("env probe failed: err=%v exit=%d stderr=%s", err, res.ExitCode, string(res.Stderr))
	}
	out := string(res.Stdout)
	if !strings.Contains(out, "HTTP_PROXY=http://"+proxyAddr) {
		t.Fatalf("HTTP_PROXY not injected (want http://%s):\n%s", proxyAddr, out)
	}
	if !strings.Contains(out, "NO_PROXY=\n") {
		t.Fatalf("NO_PROXY must be forced empty:\n%s", out)
	}
}
