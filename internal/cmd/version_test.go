package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/version"
)

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
