package server

import "net/http"

func (r *Router) buildRoutes() {
}

func (r *Router) handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range r.routes {
		mux.HandleFunc(route.Path, func(w http.ResponseWriter, req *http.Request) {
			if req.Method != route.Method {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}
			route.Handler(w, req)
		})
	}

	return mux
}
