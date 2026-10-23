package server

import (
	"context"
	"net/http"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

type Server struct {
	log log.Interface
}

func NewServer(log log.Interface) *Server {
	return &Server{
		log: log,
	}
}

func (s *Server) CreateMux(ctx context.Context, routers ...*Router) http.Handler {
	s.log.Info("Registering routers")
	mux := http.NewServeMux()
	for _, router := range routers {
		for _, route := range router.routes {
			if ctx.Err() != nil {
				return mux
			}

			s.log.Info("Registering route", log.String("path", route.Path), log.String("method", route.Method))
			mux.HandleFunc(route.Path, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != route.Method {
					http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
					return
				}
				route.Handler(w, req)
			})
		}
	}

	// Setup a default handler for unmatched routes
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		WriteJSON(w, http.StatusNotFound, &api.APIError{
			Code:       "route_not_found",
			StatusCode: http.StatusNotFound,
			Message:    "Route not found",
		})
	})

	return mux
}
