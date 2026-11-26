package server

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

// Server coordinates HTTP multiplexing and route registration.
type Server struct {
	log log.Interface
}

// NewServer constructs a new [Server].
func NewServer(logger log.Interface) *Server {
	return &Server{
		log: logger,
	}
}

// CreateMux registers all routes across the provided routers on a Go 1.22+
// http.ServeMux and attaches a fallback 404 handler for unmatched paths.
func (s *Server) CreateMux(ctx context.Context, routers ...Router) http.Handler {
	mux := http.NewServeMux()
	s.log.Info("Registering routers")
	for _, router := range routers {
		for _, route := range router.Routes() {
			s.log.Info("Registering route", log.String("path", route.Path), log.String("method", route.Method))
			mux.HandleFunc(route.Method+" "+route.Path, route.Handler)
		}
	}

	// Setup a default handler for unmatched routes
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		WriteJSON(w, http.StatusNotFound, api.ErrorResponse{
			Code:    "route_not_found",
			Message: "Route not found",
		})
	})

	return validateWorkspace(mux)
}

func cleanWorkspacePath(raw string) (string, bool) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", true
	}
	if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
		return "", false
	}
	return filepath.Clean(path), true
}

func validateWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cwd, ok := cleanWorkspacePath(RequestWorkspace(r)); !ok {
			WriteBadRequest(w, "workspace must be an absolute path without NUL bytes")
			return
		} else if cwd != "" {
			r.Header.Set(api.HeaderCWD, cwd)
		}
		next.ServeHTTP(w, r)
	})
}
