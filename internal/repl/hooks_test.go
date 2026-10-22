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

type controllerSnapshot struct {
	controller REPLController
	state      ui.State
	picker     ui.PickerProps
	sessions   wind.Result[[]api.Session]
	model      string
	itemCount  int
}

func mountController(t *testing.T, props AppProps) (*mock.Backend, func(func(REPLController)), func() *controllerSnapshot, func()) {
	t.Helper()
	var latest atomic.Pointer[controllerSnapshot]
	component := trail.FC("ControllerTest", func(props AppProps) trail.Node {
		ctrl := useREPLController(props, trail.UseApp())
		sessions := uihooks.UseSessions(wind.Options{Enabled: false})
		latest.Store(&controllerSnapshot{
			controller: ctrl,
			state:      ctrl.State(),
			picker:     ctrl.ActivePicker(),
			sessions:   sessions,
			model:      ctrl.ModelLabel(),
			itemCount:  len(ctrl.Items()),
		})
		return ui.Transcript(ui.TranscriptProps{Items: ctrl.Items()})
	})
	cache := wind.NewClient()
	cache.GcTime = time.Millisecond
	b := mock.New(100, 30)
	inst, err := trail.New(
		uiclient.Provider(uiclient.Props{Client: props.Client},
			wind.Provider(wind.ProviderProps{Client: cache}, component(props))),
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
				t.Error("timed out stopping controller")
			}
		})
	}
	t.Cleanup(stop)
	act := func(fn func(REPLController)) {
		inst.Engine().PostMacro(func() { fn(latest.Load().controller) })
	}
	return b, act, latest.Load, stop
}

func awaitController(t *testing.T, load func() *controllerSnapshot, ready func(*controllerSnapshot) bool) *controllerSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := load(); s != nil && ready(s) {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for controller; last state: %+v", load())
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

func TestSessionPickerTracksMutations(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	st.mu.Lock()
	active := st.sessions["s1"]
	st.sessions["s2"] = api.Session{ID: "s2", Title: "Second"}
	st.mu.Unlock()
	b, act, load, stop := mountController(t, AppProps{
		Client:            c,
		Workspace:         c.CWD(),
		InitialSess:       &active,
		InitialModelLabel: "Previous model",
		InitialThinking:   "high",
		Registry:          command.NewRegistry(),
	})
	act(func(ctrl REPLController) { ctrl.Submit("/sessions") })
	awaitController(t, load, func(s *controllerSnapshot) bool {
		return s.state == ui.StatePicker && len(s.picker.Options) == 2 && !s.sessions.IsFetching
	})
	act(func(ctrl REPLController) {
		p := ctrl.ActivePicker()
		p.InputActions["f2"].OnSubmit(sessionOption(p, "s1"), "Renamed")
	})
	s := awaitController(t, load, func(s *controllerSnapshot) bool {
		return sessionOption(s.picker, "s1").Label == "Renamed"
	})
	if !sessionOption(s.picker, "s1").Current {
		t.Error("renamed session lost its active marker")
	}
	act(func(ctrl REPLController) {
		p := ctrl.ActivePicker()
		p.ConfirmActions["f4"].OnConfirm(sessionOption(p, "s2"))
	})
	awaitController(t, load, func(s *controllerSnapshot) bool {
		return len(s.picker.Options) == 1 && sessionOption(s.picker, "s1").Current
	})
	act(func(ctrl REPLController) {
		p := ctrl.ActivePicker()
		p.ConfirmActions["f4"].OnConfirm(sessionOption(p, "s1"))
	})
	awaitController(t, load, func(s *controllerSnapshot) bool {
		return s.state == ui.StateIdle && len(s.sessions.Data) == 0 && s.model == "Claude 3.7 Sonnet"
	})
	stop()
	if above := insertedAboveText(b); !strings.Contains(above, "No sessions found.") {
		t.Errorf("missing empty session-list notice:\n%s", above)
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
			_, act, load, _ := mountController(t, AppProps{Client: c, Workspace: c.CWD(), Registry: command.NewRegistry()})
			act(func(ctrl REPLController) { ctrl.Submit("/sessions") })
			if method != http.MethodGet {
				awaitController(t, load, func(s *controllerSnapshot) bool {
					return s.state == ui.StatePicker && len(s.picker.Options) == 2
				})
				act(func(ctrl REPLController) {
					p := ctrl.ActivePicker()
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
			act(func(ctrl REPLController) { ctrl.ActivePicker().OnCancel() })
			awaitController(t, load, func(s *controllerSnapshot) bool { return s.state == ui.StateIdle })
			close(release)
			s := awaitController(t, load, func(s *controllerSnapshot) bool {
				if s.sessions.IsLoading || s.sessions.IsFetching {
					return false
				}
				switch method {
				case http.MethodGet:
					return len(s.sessions.Data) == 2
				case http.MethodPatch:
					for _, session := range s.sessions.Data {
						if session.ID == "s2" && session.Title == "Renamed" {
							return true
						}
					}
					return false
				default:
					return len(s.sessions.Data) == 1
				}
			})
			if s.state != ui.StateIdle {
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
	b, act, load, stop := mountController(t, AppProps{Client: c, Workspace: c.CWD(), Registry: command.NewRegistry()})
	act(func(ctrl REPLController) { ctrl.Submit("/sessions") })
	awaitController(t, load, func(s *controllerSnapshot) bool {
		return s.state == ui.StateIdle && s.sessions.IsError && s.itemCount > 1
	})
	act(func(ctrl REPLController) { ctrl.Submit("/sessions") })
	awaitController(t, load, func(s *controllerSnapshot) bool {
		return s.state == ui.StatePicker && len(s.picker.Options) == 1 && !s.sessions.IsFetching && !s.sessions.IsError
	})
	stop()
	if above := insertedAboveText(b); !strings.Contains(above, "403") {
		t.Errorf("missing request error in transcript:\n%s", above)
	}
}
