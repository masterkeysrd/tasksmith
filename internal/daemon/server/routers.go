package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

type systemRouter struct {
	b      SystemBackend
	routes []Route
}

// NewSystemRouter constructs the router exposing system lifecycle and health endpoints.
func NewSystemRouter(b SystemBackend) Router {
	r := &systemRouter{b: b}
	r.initRoutes()
	return r
}

func (s *systemRouter) Routes() []Route {
	return s.routes
}

func (s *systemRouter) initRoutes() {
	s.routes = []Route{
		GetRoute("/v1/health", s.getHealth),
		PostRoute("/v1/shutdown", s.postShutdown),
	}
}

func (s *systemRouter) getHealth(w http.ResponseWriter, r *http.Request) {
	resp, err := s.b.Health(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (s *systemRouter) postShutdown(w http.ResponseWriter, r *http.Request) {
	if err := s.b.Shutdown(r.Context()); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "shutting_down"})
}

type eventRouter struct {
	b      EventBackend
	routes []Route
}

// NewEventRouter constructs the router exposing real-time SSE event streaming
// and subscription management endpoints.
func NewEventRouter(b EventBackend) Router {
	r := &eventRouter{b: b}
	r.initRoutes()
	return r
}

func (e *eventRouter) Routes() []Route {
	return e.routes
}

func (e *eventRouter) initRoutes() {
	e.routes = []Route{
		GetRoute("/v1/events", e.getEvents),
		PostRoute("/v1/events/subscribe", e.postSubscribe),
		PostRoute("/v1/events/unsubscribe", e.postUnsubscribe),
	}
}

func (e *eventRouter) getEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, errors.New("streaming unsupported"))
		return
	}

	subID := strings.TrimSpace(r.Header.Get(api.HeaderSubscriberID))
	since := strings.TrimSpace(r.URL.Query().Get("since"))
	topics := r.URL.Query()["topic"]

	sub, subscribedTopics, err := e.b.AddSubscriber(r.Context(), subID, since, topics...)
	if err != nil {
		WriteError(w, err)
		return
	}
	defer e.b.RemoveSubscriber(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set(api.HeaderSubscriberID, sub.ID())
	w.WriteHeader(http.StatusOK)

	connEv := api.Event{
		Type: api.EventConnected,
		Payload: api.ConnectedEvent{
			SubscriberID: sub.ID(),
			Topics:       subscribedTopics,
		},
	}
	connData, err := json.Marshal(connEv)
	if err == nil {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", connEv.Type, connData)
		flusher.Flush()
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-e.b.Done():
			return
		case <-sub.Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case msg, open := <-sub.Ch():
			if !open {
				return
			}
			ev, err := e.b.FormatEvent(msg)
			if err != nil {
				continue
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if ev.ID != "" {
				fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, data)
			} else {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			}
			flusher.Flush()
		}
	}
}

func (e *eventRouter) postSubscribe(w http.ResponseWriter, r *http.Request) {
	subID := strings.TrimSpace(r.Header.Get(api.HeaderSubscriberID))
	req, err := DecodeJSON[api.SubscriptionRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	topics, err := e.b.Subscribe(r.Context(), subID, req.Since, req.Topics...)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, api.SubscriptionResponse{Topics: topics})
}

func (e *eventRouter) postUnsubscribe(w http.ResponseWriter, r *http.Request) {
	subID := strings.TrimSpace(r.Header.Get(api.HeaderSubscriberID))
	req, err := DecodeJSON[api.SubscriptionRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	topics, err := e.b.Unsubscribe(r.Context(), subID, req.Topics...)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, api.SubscriptionResponse{Topics: topics})
}

type workspaceRouter struct {
	b      WorkspaceBackend
	routes []Route
}

// NewWorkspaceRouter constructs the router exposing workspace metadata, agent
// discovery, and provider model queries.
func NewWorkspaceRouter(b WorkspaceBackend) Router {
	r := &workspaceRouter{b: b}
	r.initRoutes()
	return r
}

func (w *workspaceRouter) Routes() []Route {
	return w.routes
}

func (w *workspaceRouter) initRoutes() {
	w.routes = []Route{
		GetRoute("/v1/workspace", w.getWorkspace),
		PostRoute("/v1/workspace/reload", w.postWorkspaceReload),
		GetRoute("/v1/agents", w.getAgentsList),
		GetRoute("/v1/agents/{name}", w.getAgent),
		GetRoute("/v1/commands", w.getCommandsList),
		GetRoute("/v1/commands/{name...}", w.getCommand),
		GetRoute("/v1/skills", w.getSkillsList),
		GetRoute("/v1/skills/{name...}", w.getSkill),
		GetRoute("/v1/models", w.getModelsList),
		GetRoute("/v1/models/{id...}", w.getModel),
		GetRoute("/v1/permissions", w.getPermissions),
		PostRoute("/v1/permissions", w.postPermissionRule),
		DeleteRoute("/v1/permissions", w.deletePermissionRule),
	}
}

func (w *workspaceRouter) getWorkspace(rw http.ResponseWriter, r *http.Request) {
	ws, err := w.b.GetWorkspace(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, ws)
}

func (w *workspaceRouter) postWorkspaceReload(rw http.ResponseWriter, r *http.Request) {
	if err := w.b.ReloadWorkspace(r.Context(), RequestWorkspace(r)); err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
}

func (w *workspaceRouter) getAgentsList(rw http.ResponseWriter, r *http.Request) {
	agents, err := w.b.ListAgents(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, agents)
}

func (w *workspaceRouter) getAgent(rw http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	agent, err := w.b.GetAgent(r.Context(), RequestWorkspace(r), name)
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, agent)
}

func (w *workspaceRouter) getCommandsList(rw http.ResponseWriter, r *http.Request) {
	commands, err := w.b.ListCommands(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, commands)
}

func (w *workspaceRouter) getCommand(rw http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cmd, err := w.b.GetCommand(r.Context(), RequestWorkspace(r), name)
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, cmd)
}

func (w *workspaceRouter) getSkillsList(rw http.ResponseWriter, r *http.Request) {
	skills, err := w.b.ListSkills(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, skills)
}

func (w *workspaceRouter) getSkill(rw http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sk, err := w.b.GetSkill(r.Context(), RequestWorkspace(r), name)
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, sk)
}

func (w *workspaceRouter) getModelsList(rw http.ResponseWriter, r *http.Request) {
	models, err := w.b.ListModels(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, models)
}

func (w *workspaceRouter) getModel(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	model, err := w.b.GetModel(r.Context(), RequestWorkspace(r), id)
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, model)
}

func (w *workspaceRouter) getPermissions(rw http.ResponseWriter, r *http.Request) {
	perms, err := w.b.GetPermissions(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, perms)
}

func (w *workspaceRouter) postPermissionRule(rw http.ResponseWriter, r *http.Request) {
	req, err := DecodeJSON[api.PermissionRuleRequest](r)
	if err != nil {
		WriteError(rw, err)
		return
	}
	perms, err := w.b.AddPermissionRule(r.Context(), RequestWorkspace(r), req)
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, perms)
}

func (w *workspaceRouter) deletePermissionRule(rw http.ResponseWriter, r *http.Request) {
	req, err := DecodeJSON[api.PermissionRuleRequest](r)
	if err != nil {
		WriteError(rw, err)
		return
	}
	perms, err := w.b.RemovePermissionRule(r.Context(), RequestWorkspace(r), req)
	if err != nil {
		WriteError(rw, err)
		return
	}
	WriteJSON(rw, http.StatusOK, perms)
}

type sessionRouter struct {
	b      SessionBackend
	routes []Route
}

// NewSessionRouter constructs the router exposing session lifecycle, chronological
// timeline events, turn execution, permissions, and file changes.
func NewSessionRouter(b SessionBackend) Router {
	r := &sessionRouter{b: b}
	r.initRoutes()
	return r
}

func (s *sessionRouter) Routes() []Route {
	return s.routes
}

func (s *sessionRouter) initRoutes() {
	s.routes = []Route{
		PostRoute("/v1/sessions", s.postSessionCreate),
		PostRoute("/v1/sessions/preview", s.postSessionPreview),
		GetRoute("/v1/sessions", s.getSessionsList),
		GetRoute("/v1/sessions/{id}", s.getSession),
		PatchRoute("/v1/sessions/{id}", s.patchSession),
		DeleteRoute("/v1/sessions/{id}", s.deleteSession),
		GetRoute("/v1/sessions/{id}/timeline", s.getTimeline),
		PostRoute("/v1/sessions/{id}/send", s.postSend),
		PostRoute("/v1/sessions/{id}/cancel", s.postCancel),
		GetRoute("/v1/sessions/{id}/changes", s.getChanges),
		GetRoute("/v1/sessions/{id}/diff", s.getDiff),
		PostRoute("/v1/sessions/{id}/revert", s.postRevert),
		GetRoute("/v1/sessions/{id}/tasks", s.getTasks),
		PostRoute("/v1/sessions/{id}/tasks/{task_id}/kill", s.postKillTask),
		GetRoute("/v1/sessions/{id}/tasks/{task_id}/output", s.getTaskOutput),
	}
}

func (s *sessionRouter) postSessionCreate(w http.ResponseWriter, r *http.Request) {
	req, err := DecodeOptionalJSON[api.CreateSessionRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	sess, created, err := s.b.CreateSession(r.Context(), RequestWorkspace(r), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	WriteJSON(w, status, sess)
}

func (s *sessionRouter) postSessionPreview(w http.ResponseWriter, r *http.Request) {
	req, err := DecodeOptionalJSON[api.PreviewSessionRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	preview, err := s.b.PreviewSession(r.Context(), RequestWorkspace(r), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, preview)
}

func (s *sessionRouter) getSessionsList(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.b.ListSessions(r.Context(), RequestWorkspace(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sessions)
}

func (s *sessionRouter) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.b.GetSession(r.Context(), RequestWorkspace(r), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sess)
}

func (s *sessionRouter) patchSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := DecodeJSON[api.UpdateSessionRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	sess, err := s.b.UpdateSession(r.Context(), RequestWorkspace(r), id, req)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, sess)
}

func (s *sessionRouter) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.b.DeleteSession(r.Context(), RequestWorkspace(r), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *sessionRouter) getTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entries, err := s.b.Timeline(r.Context(), RequestWorkspace(r), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, entries)
}

func (s *sessionRouter) postSend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := DecodeJSON[api.SendRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	if err := s.b.Send(r.Context(), RequestWorkspace(r), id, req); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *sessionRouter) postCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.b.Cancel(r.Context(), RequestWorkspace(r), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *sessionRouter) getChanges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	changes, err := s.b.Changes(r.Context(), RequestWorkspace(r), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, changes)
}

func (s *sessionRouter) getDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := r.URL.Query().Get("path")
	entryID := r.URL.Query().Get("entry_id")
	diff, err := s.b.Diff(r.Context(), RequestWorkspace(r), id, path, entryID)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, diff)
}

func (s *sessionRouter) postRevert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := DecodeOptionalJSON[api.RevertRequest](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	if err := s.b.Revert(r.Context(), RequestWorkspace(r), id, req); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *sessionRouter) getTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	list, err := s.b.Tasks(r.Context(), RequestWorkspace(r), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if list == nil {
		list = []api.Task{}
	}
	WriteJSON(w, http.StatusOK, api.TasksResponse{Tasks: list})
}

func (s *sessionRouter) postKillTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	taskID := r.PathValue("task_id")
	if err := s.b.KillTask(r.Context(), RequestWorkspace(r), id, taskID); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "killed"})
}

func (s *sessionRouter) getTaskOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	taskID := r.PathValue("task_id")
	ch, initial, cancel, err := s.b.StreamTaskOutput(r.Context(), RequestWorkspace(r), id, taskID)
	if err != nil {
		WriteError(w, err)
		return
	}
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, fmt.Errorf("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if initial != "" {
		_, _ = w.Write([]byte(initial))
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, open := <-ch:
			if !open {
				return
			}
			if chunk != "" {
				_, _ = w.Write([]byte(chunk))
				flusher.Flush()
			}
		}
	}
}
