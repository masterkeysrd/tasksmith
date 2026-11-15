package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

type metricsRouter struct {
	b      MetricsBackend
	routes []Route
}

// NewMetricsRouter constructs the router exposing read-only usage and
// metrics query endpoints.
func NewMetricsRouter(b MetricsBackend) Router {
	r := &metricsRouter{b: b}
	r.routes = []Route{
		PostRoute("/v1/metrics/query", r.postQuery),
		GetRoute("/v1/sessions/{id}/usage", r.getSessionUsage),
		GetRoute("/v1/usage", r.getUsage),
	}
	return r
}

func (m *metricsRouter) Routes() []Route { return m.routes }

func (m *metricsRouter) postQuery(w http.ResponseWriter, r *http.Request) {
	req, err := DecodeJSON[api.MetricsQuery](r)
	if err != nil {
		WriteBadRequest(w, err.Error())
		return
	}
	res, err := m.b.MetricsQuery(r.Context(), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

func (m *metricsRouter) getSessionUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := m.b.SessionUsage(r.Context(), RequestWorkspace(r), r.PathValue("id"))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, usage)
}

func (m *metricsRouter) getUsage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := api.UsageRequest{
		Scope:   api.UsageScope(strings.TrimSpace(q.Get("scope"))),
		GroupBy: api.UsageGroupBy(strings.TrimSpace(q.Get("group_by"))),
	}
	var err error
	if req.Since, err = parseUsageTime(q.Get("since")); err != nil {
		WriteBadRequest(w, fmt.Sprintf("since: %v", err))
		return
	}
	if req.Until, err = parseUsageTime(q.Get("until")); err != nil {
		WriteBadRequest(w, fmt.Sprintf("until: %v", err))
		return
	}
	usage, err := m.b.Usage(r.Context(), RequestWorkspace(r), req)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, usage)
}

func parseUsageTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}
