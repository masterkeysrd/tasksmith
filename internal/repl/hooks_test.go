package repl

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// replSnapshot is what the probe component saw on its latest render.
type replSnapshot struct {
	overlay  OverlayControl
	state    ui.State
	submit   func(string)
	session  uihooks.Session
	info     uihooks.ModelInfo
	entries  int
	sessions wind.Result[[]api.Session]
	// picker is the session browser's picker while it is open.
	picker *sessionPickerState
}

// mountREPL renders the real REPL providers and overlay with a probe next to
// them, so tests can observe the hooks and drive them from the UI thread.
func mountREPL(t *testing.T, props AppProps) (*mock.Backend, func(func(*replSnapshot)), func() *replSnapshot, func()) {
	t.Helper()
	var latest atomic.Pointer[replSnapshot]
	var picker atomic.Pointer[sessionPickerState]

	// pickerProbe reads the picker the way the session browser does, without
	// reproducing its effects.
	pickerProbe := trail.SimpleFC("PickerProbe", func() trail.Node {
		state := useSessionPicker()
		picker.Store(&state)
		return nil
	})
	probe := trail.SimpleFC("Probe", func() trail.Node {
		overlay := UseOverlay()
		sessions := uihooks.UseSessions(wind.Options{Enabled: false})
		latest.Store(&replSnapshot{
			overlay:  overlay,
			state:    UseReplState(),
			submit:   UseSubmit(),
			session:  uihooks.UseSession(),
			info:     uihooks.UseModelInfo(),
			entries:  len(UseTranscript().Entries),
			sessions: sessions,
		})
		if overlay.Kind != overlaySessions {
			picker.Store(nil)
			return Overlay()
		}
		return trail.Fragment(Overlay(), pickerProbe())
	})

	cache := wind.NewClient()
	cache.GcTime = time.Millisecond
	b := mock.New(100, 30)
	inst, err := trail.New(
		uiclient.Provider(uiclient.Props{Client: props.Client},
			wind.Provider(wind.ProviderProps{Client: cache},
				replProviders(props, trail.Fragment(TranscriptOutput(), probe())))),
		trail.WithBackend(b),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- inst.Run(ctx) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			defer cancel()
			inst.Exit(nil)
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("trail.Run: %v", err)
				}
			case <-time.After(3 * time.Second):
				cancel()
				<-done
				t.Error("timed out stopping the REPL")
			}
		})
	}
	t.Cleanup(stop)

	load := func() *replSnapshot {
		snapshot := latest.Load()
		if snapshot == nil {
			return nil
		}
		copied := *snapshot
		copied.picker = picker.Load()
		return &copied
	}
	act := func(fn func(*replSnapshot)) {
		inst.Engine().PostMacro(func() { fn(load()) })
	}
	return b, act, load, stop
}

func awaitREPL(t *testing.T, load func() *replSnapshot, ready func(*replSnapshot) bool) *replSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := load(); s != nil && ready(s) {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for the REPL; last state: %+v", load())
	return nil
}

func sessionOption(p ui.PickerProps, id string) components.Option {
	for _, option := range p.Options {
		if option.Value == id {
			return option
		}
	}
	return components.Option{}
}

func pickerOptions(s *replSnapshot) int {
	if s.picker == nil {
		return -1
	}
	return len(s.picker.Props.Options)
}

func browsing(s *replSnapshot) bool {
	return s.overlay.Kind == overlaySessions && s.picker != nil && !s.picker.Sessions.IsFetching
}

func TestSessionPickerTracksMutations(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	st.mu.Lock()
	active := st.sessions["s1"]
	st.sessions["s2"] = api.Session{ID: "s2", Title: "Second"}
	st.mu.Unlock()
	b, act, load, stop := mountREPL(t, AppProps{
		Client:            c,
		Workspace:         c.CWD(),
		InitialSess:       &active,
		InitialModelLabel: "Previous model",
		InitialThinking:   "high",
		Registry:          command.NewRegistry(),
	})
	act(func(s *replSnapshot) { s.submit("/sessions") })
	awaitREPL(t, load, func(s *replSnapshot) bool { return browsing(s) && pickerOptions(s) == 2 })

	act(func(s *replSnapshot) {
		p := s.picker.Props
		p.InputActions["f2"].OnSubmit(sessionOption(p, "s1"), "Renamed")
	})
	s := awaitREPL(t, load, func(s *replSnapshot) bool {
		return s.picker != nil && sessionOption(s.picker.Props, "s1").Label == "Renamed"
	})
	if !sessionOption(s.picker.Props, "s1").Current {
		t.Error("renamed session lost its active marker")
	}
	if s.session.Title != "Renamed" {
		t.Errorf("active session title = %q, want the renamed one", s.session.Title)
	}

	act(func(s *replSnapshot) {
		p := s.picker.Props
		p.ConfirmActions["f4"].OnConfirm(sessionOption(p, "s2"))
	})
	awaitREPL(t, load, func(s *replSnapshot) bool {
		return pickerOptions(s) == 1 && sessionOption(s.picker.Props, "s1").Current
	})

	// Deleting the active session starts a fresh draft with the default model.
	act(func(s *replSnapshot) {
		p := s.picker.Props
		p.ConfirmActions["f4"].OnConfirm(sessionOption(p, "s1"))
	})
	s = awaitREPL(t, load, func(s *replSnapshot) bool {
		return s.session.IsDraft() && !s.overlay.IsOpen && s.info.Ready && s.info.Label == "Claude 3.7 Sonnet"
	})
	if s.session.ID == "s1" {
		t.Error("the deleted session is still active")
	}
	stop()
	if above := insertedAboveText(b); strings.Count(above, "TaskSmith") < 2 {
		t.Errorf("expected a fresh header after the active session was deleted:\n%s", above)
	}
}

type replRoundTripFunc func(*http.Request) (*http.Response, error)

func (f replRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func interceptDaemon(t *testing.T, c *client.Client, intercept func(*http.Request, http.RoundTripper) (*http.Response, error)) *client.Client {
	t.Helper()
	var dialer net.Dialer
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", c.SocketPath())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	wrapped, err := client.New(c.SocketPath(), client.WithCWD(c.CWD()), client.WithHTTPClient(&http.Client{
		Transport: replRoundTripFunc(func(r *http.Request) (*http.Response, error) { return intercept(r, transport) }),
	}))
	if err != nil {
		t.Fatal(err)
	}
	return wrapped
}

func TestSessionPickerStaysClosedAfterPendingRequests(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			c, st := startTestUnixDaemon(t)
			st.mu.Lock()
			st.sessions["s2"] = api.Session{ID: "s2", Title: "Second"}
			st.mu.Unlock()
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			var requests atomic.Int32
			c = interceptDaemon(t, c, func(r *http.Request, base http.RoundTripper) (*http.Response, error) {
				if r.Method == method && (r.URL.Path == "/v1/sessions" || r.URL.Path == "/v1/sessions/s2") {
					requests.Add(1)
					started <- struct{}{}
					select {
					case <-release:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				return base.RoundTrip(r)
			})
			_, act, load, _ := mountREPL(t, AppProps{Client: c, Workspace: c.CWD(), Registry: command.NewRegistry()})
			act(func(s *replSnapshot) { s.submit("/sessions") })
			if method != http.MethodGet {
				awaitREPL(t, load, func(s *replSnapshot) bool { return s.overlay.Kind == overlaySessions && pickerOptions(s) == 2 })
				act(func(s *replSnapshot) {
					p := s.picker.Props
					for range 2 {
						if method == http.MethodPatch {
							p.InputActions["f2"].OnSubmit(sessionOption(p, "s2"), "Renamed")
						} else {
							p.ConfirmActions["f4"].OnConfirm(sessionOption(p, "s2"))
						}
					}
				})
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not start")
			}
			act(func(s *replSnapshot) { s.picker.Props.OnCancel() })
			awaitREPL(t, load, func(s *replSnapshot) bool { return !s.overlay.IsOpen && s.state == ui.StateIdle })
			close(release)

			if method != http.MethodGet {
				// A rename or delete that finishes after the picker closed still
				// refreshes the session list.
				s := awaitREPL(t, load, func(s *replSnapshot) bool {
					if s.sessions.IsLoading || s.sessions.IsFetching {
						return false
					}
					if method == http.MethodPatch {
						for _, session := range s.sessions.Data {
							if session.ID == "s2" && session.Title == "Renamed" {
								return true
							}
						}
						return false
					}
					return len(s.sessions.Data) == 1
				})
				if s.overlay.IsOpen {
					t.Errorf("request reopened the dismissed picker: state=%v", s.state)
				}
			}
			time.Sleep(50 * time.Millisecond)
			if s := load(); s.overlay.IsOpen {
				t.Errorf("request reopened the dismissed picker: state=%v", s.state)
			}
			if requests.Load() != 1 {
				t.Errorf("made %d concurrent %s requests, want 1", requests.Load(), method)
			}
		})
	}
}

func TestSessionPickerReportsErrorsAndRetries(t *testing.T) {
	c, _ := startTestUnixDaemon(t)
	var lists atomic.Int32
	c = interceptDaemon(t, c, func(r *http.Request, base http.RoundTripper) (*http.Response, error) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/sessions" && lists.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    r,
			}, nil
		}
		return base.RoundTrip(r)
	})
	b, act, load, stop := mountREPL(t, AppProps{Client: c, Workspace: c.CWD(), Registry: command.NewRegistry()})
	act(func(s *replSnapshot) { s.submit("/sessions") })
	awaitREPL(t, load, func(s *replSnapshot) bool {
		return s.state == ui.StateIdle && s.sessions.IsError && s.entries > 1
	})
	act(func(s *replSnapshot) { s.submit("/sessions") })
	awaitREPL(t, load, func(s *replSnapshot) bool {
		return s.overlay.Kind == overlaySessions && pickerOptions(s) == 1 && !s.picker.Sessions.IsFetching && !s.picker.Sessions.IsError
	})
	stop()
	if above := insertedAboveText(b); !strings.Contains(above, "403") {
		t.Errorf("missing request error in transcript:\n%s", above)
	}
}
