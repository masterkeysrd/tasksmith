package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func hookClient(t *testing.T, socket, cwd string, transport roundTripFunc) *client.Client {
	t.Helper()
	c, err := client.New(socket, client.WithCWD(cwd), client.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func response(r *http.Request, status int, body any) *http.Response {
	w := httptest.NewRecorder()
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
	res := w.Result()
	res.Request = r
	return res
}

func mountHooks[T any](t *testing.T, c *client.Client, render func() T, siblings ...kitex.Node) (*trail.Instance, func() *T, func()) {
	t.Helper()
	var latest atomic.Pointer[T]
	component := trail.FC("HookTest", func(_ struct{}) trail.Node {
		value := render()
		latest.Store(&value)
		return kitex.Text("hooks")
	})
	cache := wind.NewClient()
	cache.GcTime = time.Millisecond
	children := append([]kitex.Node{
		uiclient.Provider(uiclient.Props{Client: c}, component(struct{}{})),
	}, siblings...)
	inst, err := trail.New(
		wind.Provider(wind.ProviderProps{Client: cache}, children...),
		trail.WithBackend(mock.New(80, 24)),
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
				t.Error("timed out stopping hook component")
			}
		})
	}
	t.Cleanup(stop)
	return inst, latest.Load, stop
}

func awaitHooks[T any](t *testing.T, load func() *T, ready func(*T) bool) *T {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if value := load(); value != nil && ready(value) {
			return value
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for hook state; last state: %+v", load())
	return nil
}

type sessionHooks struct {
	sessions wind.Result[[]api.Session]
	rename   wind.MutationResult[RenameSessionInput]
	delete   wind.MutationResult[string]
}

func TestSessionMutationsInvalidateOnlyTheirScope(t *testing.T) {
	var mu sync.Mutex
	title := "Original"
	deleted := false
	listCalls := make(map[string]int)
	c := hookClient(t, "/daemon.sock", "/workspace", func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			cwd := r.Header.Get(api.HeaderCWD)
			listCalls[cwd]++
			sessions := []api.Session{}
			if cwd != "/workspace" || !deleted {
				sessions = append(sessions, api.Session{ID: "s1", Title: title})
			}
			return response(r, http.StatusOK, sessions), nil
		case http.MethodPatch:
			if r.URL.Path != "/v1/sessions/s1" {
				t.Errorf("rename path = %q", r.URL.Path)
			}
			var req api.UpdateSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode rename: %v", err)
			}
			title = req.Title
			return response(r, http.StatusOK, api.Session{ID: "s1", Title: title}), nil
		case http.MethodDelete:
			if r.URL.Path != "/v1/sessions/s1" {
				t.Errorf("delete path = %q", r.URL.Path)
			}
			deleted = true
			return response(r, http.StatusNoContent, nil), nil
		default:
			return response(r, http.StatusMethodNotAllowed, nil), nil
		}
	})
	otherWorkspace := c.WithCWD("/other")
	var otherDaemonCalls atomic.Int32
	otherDaemon := hookClient(t, "/other.sock", "/workspace", func(r *http.Request) (*http.Response, error) {
		otherDaemonCalls.Add(1)
		return response(r, http.StatusOK, []api.Session{{ID: "other-daemon"}}), nil
	})
	var renameCallbacks, deleteCallbacks atomic.Int32
	type snapshot struct {
		sessionHooks
		duplicate wind.Result[[]api.Session]
	}
	options := wind.Options{Enabled: true, StaleTime: time.Hour}
	var workspaceState, daemonState atomic.Pointer[wind.Result[[]api.Session]]
	observer := trail.FC("ScopedSessions", func(state *atomic.Pointer[wind.Result[[]api.Session]]) trail.Node {
		result := UseSessions(options)
		state.Store(&result)
		return kitex.Text("scope")
	})
	inst, load, _ := mountHooks(t, c, func() snapshot {
		return snapshot{
			sessionHooks: sessionHooks{
				sessions: UseSessions(options),
				rename: UseRenameSession(wind.MutationOptions[RenameSessionInput, *api.Session]{
					OnSuccess: func(s *api.Session, input RenameSessionInput, _ wind.MutationContext) {
						if s.ID != input.SessionID || s.Title != input.Title {
							t.Errorf("rename result = %+v, input = %+v", s, input)
						}
						renameCallbacks.Add(1)
					},
				}),
				delete: UseDeleteSession(wind.MutationOptions[string, struct{}]{
					OnSuccess: func(_ struct{}, id string, _ wind.MutationContext) {
						if id != "s1" {
							t.Errorf("deleted ID = %q", id)
						}
						deleteCallbacks.Add(1)
					},
				}),
			},
			duplicate: UseSessions(options),
		}
	},
		uiclient.Provider(uiclient.Props{Client: otherWorkspace}, observer(&workspaceState)),
		uiclient.Provider(uiclient.Props{Client: otherDaemon}, observer(&daemonState)),
	)
	awaitHooks(t, load, func(s *snapshot) bool {
		workspace, daemon := workspaceState.Load(), daemonState.Load()
		return len(s.sessions.Data) == 1 && len(s.duplicate.Data) == 1 &&
			workspace != nil && len(workspace.Data) == 1 && daemon != nil && len(daemon.Data) == 1
	})
	inst.Engine().PostMacro(func() {
		load().rename.Mutate(RenameSessionInput{SessionID: "s1", Title: "Renamed"})
	})
	awaitHooks(t, load, func(s *snapshot) bool {
		return len(s.sessions.Data) == 1 && s.sessions.Data[0].Title == "Renamed" &&
			len(s.duplicate.Data) == 1 && s.duplicate.Data[0].Title == "Renamed" && !s.rename.IsPending
	})
	inst.Engine().PostMacro(func() { load().delete.Mutate("s1") })
	awaitHooks(t, load, func(s *snapshot) bool {
		return !s.sessions.IsLoading && !s.sessions.IsFetching && len(s.sessions.Data) == 0 &&
			len(s.duplicate.Data) == 0 && !s.delete.IsPending && deleteCallbacks.Load() == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if listCalls["/workspace"] != 3 || listCalls["/other"] != 1 || otherDaemonCalls.Load() != 1 {
		t.Errorf("list requests = %v, other daemon = %d; want 3, 1, 1", listCalls, otherDaemonCalls.Load())
	}
	if renameCallbacks.Load() != 1 || deleteCallbacks.Load() != 1 {
		t.Errorf("success callbacks: rename=%d, delete=%d", renameCallbacks.Load(), deleteCallbacks.Load())
	}
}

func TestSessionHookErrors(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var lists, callbacks atomic.Int32
			c := hookClient(t, "/daemon.sock", "/workspace", func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					lists.Add(1)
				}
				if r.Method == method {
					return response(r, http.StatusForbidden, api.ErrorResponse{Message: "denied"}), nil
				}
				return response(r, http.StatusOK, []api.Session{{ID: "s1"}}), nil
			})
			inst, load, _ := mountHooks(t, c, func() sessionHooks {
				return sessionHooks{
					sessions: UseSessions(),
					rename: UseRenameSession(wind.MutationOptions[RenameSessionInput, *api.Session]{
						OnError: func(err error, input RenameSessionInput, _ wind.MutationContext) {
							if !strings.Contains(err.Error(), "denied") || input.SessionID != "s1" {
								t.Errorf("rename error = %v, input = %+v", err, input)
							}
							callbacks.Add(1)
						},
					}),
					delete: UseDeleteSession(wind.MutationOptions[string, struct{}]{
						OnError: func(err error, id string, _ wind.MutationContext) {
							if !strings.Contains(err.Error(), "denied") || id != "s1" {
								t.Errorf("delete error = %v, ID = %q", err, id)
							}
							callbacks.Add(1)
						},
					}),
				}
			})
			if method == http.MethodGet {
				s := awaitHooks(t, load, func(s *sessionHooks) bool { return s.sessions.IsError })
				if !strings.Contains(s.sessions.Error.Error(), "denied") {
					t.Errorf("query error = %v", s.sessions.Error)
				}
				return
			}
			awaitHooks(t, load, func(s *sessionHooks) bool { return len(s.sessions.Data) == 1 })
			inst.Engine().PostMacro(func() {
				if method == http.MethodPatch {
					load().rename.Mutate(RenameSessionInput{SessionID: "s1", Title: "Renamed"})
				} else {
					load().delete.Mutate("s1")
				}
			})
			s := awaitHooks(t, load, func(s *sessionHooks) bool { return s.rename.IsError || s.delete.IsError })
			if s.rename.IsPending || s.delete.IsPending || callbacks.Load() != 1 || lists.Load() != 1 {
				t.Errorf("failed mutation: rename=%+v, delete=%+v, callbacks=%d, lists=%d",
					s.rename, s.delete, callbacks.Load(), lists.Load())
			}
		})
	}
}

func TestSessionHooksCancelRequestsOnUnmount(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			started := make(chan struct{}, 1)
			canceled := make(chan struct{}, 1)
			var callbacks atomic.Int32
			c := hookClient(t, "/daemon.sock", "/workspace", func(r *http.Request) (*http.Response, error) {
				if r.Method == method {
					started <- struct{}{}
					<-r.Context().Done()
					canceled <- struct{}{}
					return nil, r.Context().Err()
				}
				return response(r, http.StatusOK, []api.Session{{ID: "s1"}}), nil
			})
			inst, load, stop := mountHooks(t, c, func() sessionHooks {
				return sessionHooks{
					sessions: UseSessions(),
					rename: UseRenameSession(wind.MutationOptions[RenameSessionInput, *api.Session]{
						OnSuccess: func(*api.Session, RenameSessionInput, wind.MutationContext) { callbacks.Add(1) },
						OnError:   func(error, RenameSessionInput, wind.MutationContext) { callbacks.Add(1) },
					}),
					delete: UseDeleteSession(wind.MutationOptions[string, struct{}]{
						OnSuccess: func(struct{}, string, wind.MutationContext) { callbacks.Add(1) },
						OnError:   func(error, string, wind.MutationContext) { callbacks.Add(1) },
					}),
				}
			})
			if method != http.MethodGet {
				awaitHooks(t, load, func(s *sessionHooks) bool { return len(s.sessions.Data) == 1 })
				inst.Engine().PostMacro(func() {
					if method == http.MethodPatch {
						load().rename.Mutate(RenameSessionInput{SessionID: "s1", Title: "Renamed"})
					} else {
						load().delete.Mutate("s1")
					}
				})
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not start")
			}
			stop()
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("unmount did not cancel request context")
			}
			if callbacks.Load() != 0 {
				t.Errorf("received %d mutation callbacks after unmount", callbacks.Load())
			}
		})
	}
}

func TestUseSessionsDisabled(t *testing.T) {
	var calls atomic.Int32
	c := hookClient(t, "/daemon.sock", "/workspace", func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(r, http.StatusOK, []api.Session{{ID: "s1"}}), nil
	})
	inst, load, _ := mountHooks(t, c, func() wind.Result[[]api.Session] {
		return UseSessions(wind.Options{Enabled: false})
	})
	done := make(chan struct{})
	inst.Engine().PostMacro(func() {
		if calls.Load() != 0 {
			t.Errorf("disabled query made %d requests", calls.Load())
		}
		load().Refetch()
		close(done)
	})
	<-done
	awaitHooks(t, load, func(s *wind.Result[[]api.Session]) bool { return len(s.Data) == 1 })
	if calls.Load() != 1 {
		t.Errorf("requests=%d, want 1", calls.Load())
	}
}
