package cmd

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/version"
)

func TestRelativeWorkspace(t *testing.T) {
	for _, path := range []string{".", "./child", "../repo"} {
		want, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := (&rootOptions{workspace: " " + path + " "}).resolveWorkspace()
		if err != nil || got != want {
			t.Fatalf("resolveWorkspace(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	out, _, err := execCLI(t, "", "--version")
	if err != nil {
		t.Fatalf("--version error: %v", err)
	}
	if !strings.Contains(out, version.Get()) {
		t.Errorf("--version output = %q, want it to contain %q", out, version.Get())
	}
}

func TestVersionCommandWithoutDaemon(t *testing.T) {
	shortTmp := t.TempDir()
	missingSock := shortTmp + "/none.sock"

	out, _, err := execCLI(t, "", "version", "--socket", missingSock)
	if err != nil {
		t.Fatalf("version error: %v", err)
	}
	if !strings.Contains(out, "TaskSmith "+version.Get()) {
		t.Errorf("version output = %q, want client line with %q", out, version.Get())
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("version output = %q, want daemon 'not running'", out)
	}
}

func TestVersionCommandWithRunningDaemon(t *testing.T) {
	_, sockPath, _ := setupCLITestDaemon(t, nil)

	out, _, err := execCLI(t, "", "version", "--socket", sockPath)
	if err != nil {
		t.Fatalf("version error: %v", err)
	}
	if !strings.Contains(out, "TaskSmith "+version.Get()) {
		t.Errorf("version output = %q, want client line with %q", out, version.Get())
	}
	if !strings.Contains(out, version.Get()+" (pid=") {
		t.Errorf("version output = %q, want daemon version %q with pid", out, version.Get())
	}
}

func TestDaemonStatusReportsVersion(t *testing.T) {
	_, sockPath, _ := setupCLITestDaemon(t, nil)

	out, _, err := execCLI(t, "", "daemon", "status", "--socket", sockPath)
	if err != nil {
		t.Fatalf("daemon status error: %v", err)
	}
	if !strings.Contains(out, "version="+version.Get()) {
		t.Errorf("daemon status output = %q, want 'version=%s'", out, version.Get())
	}
}

func TestDaemonVersionSkewed(t *testing.T) {
	current := version.Get()

	tests := []struct {
		name string
		vers string
		want bool
	}{
		{name: "matching version", vers: current, want: false},
		{name: "older daemon", vers: "v0.0.1", want: true},
		{name: "predates version reporting", vers: "", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &api.HealthResponse{Version: tt.vers}
			if got := daemonVersionSkewed(h); got != tt.want {
				t.Errorf("daemonVersionSkewed(%q) = %v, want %v", tt.vers, got, tt.want)
			}
		})
	}

	if daemonVersionSkewed(nil) {
		t.Error("daemonVersionSkewed(nil) = true, want false")
	}
}

func TestConnectOrStartSkew(t *testing.T) {
	const helperEnv = "TASKSMITH_TEST_REPLACEMENT_SOCKET"
	if socket := os.Getenv(helperEnv); socket != "" {
		ln, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{}
		srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/v1/shutdown" {
				go func() { _ = srv.Shutdown(context.Background()) }()
				return
			}
			_ = json.NewEncoder(w).Encode(api.HealthResponse{Version: version.Get(), PID: os.Getpid()})
		})
		timer := time.AfterFunc(10*time.Second, func() { _ = srv.Close() })
		defer timer.Stop()
		if err := srv.Serve(ln); err != http.ErrServerClosed {
			t.Fatal(err)
		}
		return
	}
	for _, tc := range []struct {
		name       string
		workspaces []string
		active     bool
	}{
		{name: "empty"}, {name: "idle", workspaces: []string{"/a", "/b"}}, {name: "active second workspace", workspaces: []string{"/a", "/b"}, active: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "ts-skew-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(dir) }()
			socket := filepath.Join(dir, "s")
			t.Setenv(helperEnv, socket)
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			seen := make(chan string, 4)
			srv := &http.Server{}
			srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/v1/health":
					_ = json.NewEncoder(w).Encode(api.HealthResponse{Version: "old", Workspaces: tc.workspaces})
				case "/v1/sessions":
					ws := req.Header.Get(api.HeaderCWD)
					seen <- ws
					status := api.StatusIdle
					if tc.active && ws == "/b" {
						status = api.StatusExecuting
					}
					_ = json.NewEncoder(w).Encode([]api.Session{{Status: status}})
				case "/v1/shutdown":
					go func() { _ = srv.Shutdown(context.Background()) }()
				default:
					http.NotFound(w, req)
				}
			})
			go func() { _ = srv.Serve(ln) }()
			defer func() { _ = srv.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			c, notices, err := ConnectOrStart(ctx, socket, "-test.run=^TestConnectOrStartSkew$")
			if err != nil {
				t.Fatal(err)
			}
			h, err := c.Health(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.active {
				if err := c.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if (h.Version == "old") != tc.active || len(notices) != 1 || strings.Contains(notices[0], "restarting") == tc.active {
				t.Fatalf("health=%+v notices=%v", h, notices)
			}
			close(seen)
			var checked []string
			for ws := range seen {
				checked = append(checked, ws)
			}
			if !slices.Equal(checked, tc.workspaces) {
				t.Fatalf("checked %v; want %v", checked, tc.workspaces)
			}
		})
	}
}

func TestHasActiveSessions(t *testing.T) {
	if hasActiveSessions(nil) {
		t.Error("hasActiveSessions(nil) = true, want false")
	}
	idle := []api.Session{
		{Status: api.StatusIdle},
		{Status: api.StatusIdle},
	}
	if hasActiveSessions(idle) {
		t.Error("hasActiveSessions(all idle) = true, want false")
	}
	mixed := []api.Session{
		{Status: api.StatusIdle},
		{Status: api.StatusExecuting},
	}
	if !hasActiveSessions(mixed) {
		t.Error("hasActiveSessions(with executing session) = false, want true")
	}
}

func TestRestartDaemonStopsHealthyDaemon(t *testing.T) {
	srv, sockPath, _ := setupCLITestDaemon(t, nil)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(context.Background()) }()

	c, err := client.New(sockPath)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	if err := restartDaemon(context.Background(), c); err != nil {
		t.Fatalf("restartDaemon: %v", err)
	}
	if isDaemonHealthy(context.Background(), c) {
		t.Error("daemon still healthy after restartDaemon")
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("Serve returned error after shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for Serve to return after shutdown")
	}
}

func TestDaemonCommandsRejectSandboxFlag(t *testing.T) {
	_, _, err := execCLI(t, "", "daemon", "serve", "--sandbox", "local")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("daemon serve --sandbox error = %v, want unknown flag rejection", err)
	}
}

func TestResolveSandboxModeValidation(t *testing.T) {
	for _, tc := range []struct {
		input   string
		want    api.SandboxMode
		wantErr bool
	}{
		{input: "", want: ""},
		{input: "native", want: api.SandboxNative},
		{input: "Local", want: api.SandboxLocal},
		{input: " native ", want: api.SandboxNative},
		{input: "docker", wantErr: true},
	} {
		got, err := (&replOptions{sandboxMode: tc.input}).resolveSandboxMode()
		if tc.wantErr {
			if err == nil {
				t.Errorf("resolveSandboxMode(%q) error = nil, want error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveSandboxMode(%q): %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveSandboxMode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
