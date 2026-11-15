package store

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/client"
)

func TestDaemonStatusDefaultsAndSetters(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if status := GetApp().DaemonStatus; status != DaemonConnected {
		t.Fatalf("expected default status %q, got %q", DaemonConnected, status)
	}

	SetDaemonStatus(DaemonDisconnected)
	if status := GetApp().DaemonStatus; status != DaemonDisconnected {
		t.Fatalf("expected status %q, got %q", DaemonDisconnected, status)
	}

	SetDaemonStatus(DaemonRestarting)
	if status := GetApp().DaemonStatus; status != DaemonRestarting {
		t.Fatalf("expected status %q, got %q", DaemonRestarting, status)
	}

	ResetApp()
	if status := GetApp().DaemonStatus; status != DaemonConnected {
		t.Fatalf("expected ResetApp to restore status %q, got %q", DaemonConnected, status)
	}
}

func TestFailFastWhenOffline(t *testing.T) {
	ResetApp()
	ResetSession()
	ClearTranscript()
	ResetStream()
	defer ResetApp()
	defer ResetSession()
	defer ClearTranscript()
	defer ResetStream()

	SetDaemonStatus(DaemonDisconnected)

	// Send should fail fast without sending to client
	Send("hello world")
	if globalConversation.status != StreamFailed {
		t.Fatalf("expected StreamFailed when disconnected, got %v", globalConversation.status)
	}
	if globalConversation.err == nil || !strings.Contains(globalConversation.err.Error(), "daemon is not running") {
		t.Fatalf("expected 'daemon is not running' error, got %v", globalConversation.err)
	}

	// SendCommand should fail fast without sending
	ResetStream()
	SendCommand("ls -la")
	if globalConversation.status != StreamFailed {
		t.Fatalf("expected StreamFailed when disconnected, got %v", globalConversation.status)
	}
	if globalConversation.err == nil || !strings.Contains(globalConversation.err.Error(), "daemon is not running") {
		t.Fatalf("expected 'daemon is not running' error, got %v", globalConversation.err)
	}

	// SendCompact should fail fast
	ClearTranscript()
	SendCompact("focus instructions")
	items := GetTranscript().Items
	if items == nil || len(*items) == 0 {
		t.Fatal("expected transcript error item after SendCompact when offline")
	}
	last := (*items)[len(*items)-1]
	if last.Kind != TranscriptKindError {
		t.Fatalf("expected TranscriptKindError, got %v", last.Kind)
	}
}

func TestStartStopRestartDaemonWithMockStarter(t *testing.T) {
	ResetApp()
	ResetSession()
	ClearTranscript()
	defer ResetApp()
	defer ResetSession()
	defer ClearTranscript()

	var healthy atomic.Bool
	healthy.Store(true)

	var spawnCalls atomic.Int32
	var shutdownCalls atomic.Int32

	rt := &mockSessionRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Path == "/v1/health":
				if !healthy.Load() {
					return nil, errors.New("connection refused")
				}
				resp := `{"pid":12345,"started_at":"2026-01-01T00:00:00Z","workspaces":["/workspace"]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(resp)),
					Header:     make(http.Header),
				}, nil
			case req.URL.Path == "/v1/shutdown":
				shutdownCalls.Add(1)
				healthy.Store(false)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			case req.URL.Path == "/v1/workspace":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"workspace":{"root":"/workspace"}}`)),
					Header:     make(http.Header),
				}, nil
			case req.URL.Path == "/v1/commands":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[]`)),
					Header:     make(http.Header),
				}, nil
			case req.URL.Path == "/v1/skills":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[]`)),
					Header:     make(http.Header),
				}, nil
			case req.URL.Path == "/v1/agents":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[]`)),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			}
		},
	}

	c, err := client.New("/tmp/test-daemon.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	starter := func(ctx context.Context, socketPath string) error {
		spawnCalls.Add(1)
		healthy.Store(true)
		return nil
	}

	InitSession(context.Background(), c, InitSessionOptions{
		StartDaemon: starter,
	})

	// 1. StartDaemon when already running
	h, err := StartDaemon(context.Background())
	if err != nil {
		t.Fatalf("StartDaemon when healthy: %v", err)
	}
	if h.PID != 12345 {
		t.Fatalf("expected PID 12345, got %d", h.PID)
	}
	if spawnCalls.Load() != 0 {
		t.Fatalf("expected no spawn calls when already healthy, got %d", spawnCalls.Load())
	}
	if GetApp().DaemonStatus != DaemonConnected {
		t.Fatalf("expected DaemonConnected, got %v", GetApp().DaemonStatus)
	}

	// 2. StopDaemon
	if err := StopDaemon(context.Background()); err != nil {
		t.Fatalf("StopDaemon: %v", err)
	}
	if shutdownCalls.Load() != 1 {
		t.Fatalf("expected 1 shutdown call, got %d", shutdownCalls.Load())
	}
	if GetApp().DaemonStatus != DaemonDisconnected {
		t.Fatalf("expected DaemonDisconnected after stop, got %v", GetApp().DaemonStatus)
	}

	// 3. StartDaemon when offline
	h, err = StartDaemon(context.Background())
	if err != nil {
		t.Fatalf("StartDaemon when offline: %v", err)
	}
	if h.PID != 12345 {
		t.Fatalf("expected PID 12345, got %d", h.PID)
	}
	if spawnCalls.Load() != 1 {
		t.Fatalf("expected 1 spawn call after start, got %d", spawnCalls.Load())
	}
	if GetApp().DaemonStatus != DaemonConnected {
		t.Fatalf("expected DaemonConnected after start, got %v", GetApp().DaemonStatus)
	}

	// 4. RestartDaemon
	h, err = RestartDaemon(context.Background())
	if err != nil {
		t.Fatalf("RestartDaemon: %v", err)
	}
	if h.PID != 12345 {
		t.Fatalf("expected PID 12345, got %d", h.PID)
	}
	if shutdownCalls.Load() != 2 {
		t.Fatalf("expected 2 shutdown calls after restart, got %d", shutdownCalls.Load())
	}
	if spawnCalls.Load() != 2 {
		t.Fatalf("expected 2 spawn calls after restart, got %d", spawnCalls.Load())
	}
	if GetApp().DaemonStatus != DaemonConnected {
		t.Fatalf("expected DaemonConnected after restart, got %v", GetApp().DaemonStatus)
	}
}

func TestDisconnectDetectionInSessionsWatcher(t *testing.T) {
	ResetApp()
	ResetSession()
	defer ResetApp()
	defer ResetSession()

	var healthy atomic.Bool
	healthy.Store(true)

	rt := &mockSessionRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Path == "/v1/events":
				if !healthy.Load() {
					return nil, errors.New("connection broken")
				}
				// Return empty SSE stream that immediately closes to simulate disconnect
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				}, nil
			case req.URL.Path == "/v1/health":
				if !healthy.Load() {
					return nil, errors.New("connection refused")
				}
				resp := `{"pid":54321,"started_at":"2026-01-01T00:00:00Z","workspaces":["/workspace"]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(resp)),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
				}, nil
			}
		},
	}

	c, err := client.New("/tmp/test-watcher.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	ctx := t.Context()

	InitSession(ctx, c, InitSessionOptions{})

	// Daemon is connected initially
	SetDaemonStatus(DaemonConnected)

	// Make daemon unhealthy
	healthy.Store(false)

	// Wait for watcher to detect disconnect
	deadline := time.Now().Add(2 * time.Second)
	disconnected := false
	for time.Now().Before(deadline) {
		if GetApp().DaemonStatus == DaemonDisconnected {
			disconnected = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !disconnected {
		t.Fatalf("expected watcher to detect disconnect and set DaemonDisconnected, got %v", GetApp().DaemonStatus)
	}

	// Now make daemon healthy again
	healthy.Store(true)

	// Wait for passive reconnection
	deadline = time.Now().Add(2 * time.Second)
	reconnected := false
	for time.Now().Before(deadline) {
		if GetApp().DaemonStatus == DaemonConnected {
			reconnected = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !reconnected {
		t.Fatalf("expected watcher to passively reconnect and set DaemonConnected, got %v", GetApp().DaemonStatus)
	}
}
