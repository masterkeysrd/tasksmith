package server

import (
	"context"
	"net/http"

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

	return mux
}
