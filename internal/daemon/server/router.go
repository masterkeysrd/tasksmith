package server

import (
	"encoding/json"
	"net/http"
)

type Router struct {
	backend Backend
	routes  []Route
}

func NewRouter(b Backend) *Router {
	return &Router{
		backend: b,
		routes:  []Route{},
	}
}

type Route struct {
	Path    string
	Method  string
	Handler HandlerFunc
}

type HandlerFunc func(w http.ResponseWriter, r *http.Request)

func NewRoute(path, method string, handler HandlerFunc) Route {
	return Route{
		Path:    path,
		Method:  method,
		Handler: handler,
	}
}

func GetRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodGet, handler)
}

func PostRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodPost, handler)
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if jsonData, ok := data.([]byte); ok {
			w.Write(jsonData)
			return
		}

		b, err := json.Marshal(data)
		if err != nil {
			http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
			return
		}
		w.Write(b)
	}
}
