package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
)

// HandlerFunc defines the signature of a route handler.
type HandlerFunc func(w http.ResponseWriter, r *http.Request)

// Route maps a URL pattern and HTTP method to a [HandlerFunc].
type Route struct {
	Path    string
	Method  string
	Handler HandlerFunc
}

// Router provides route definitions to [Server.CreateMux].
type Router interface {
	Routes() []Route
}

// NewRoute constructs a Route with the given path, method, and handler.
func NewRoute(path, method string, handler HandlerFunc) Route {
	return Route{
		Path:    path,
		Method:  method,
		Handler: handler,
	}
}

// GetRoute creates a GET route helper.
func GetRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodGet, handler)
}

// PostRoute creates a POST route helper.
func PostRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodPost, handler)
}

// PutRoute creates a PUT route helper.
func PutRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodPut, handler)
}

// PatchRoute creates a PATCH route helper.
func PatchRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodPatch, handler)
}

// DeleteRoute creates a DELETE route helper.
func DeleteRoute(path string, handler HandlerFunc) Route {
	return NewRoute(path, http.MethodDelete, handler)
}

// WriteJSON sets the Content-Type header to application/json, writes the given
// HTTP status code, and serializes data as JSON to the response writer.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if b, ok := data.([]byte); ok {
			_, _ = w.Write(b)
			return
		}
		_ = json.NewEncoder(w).Encode(data)
	}
}

// WriteBadRequest writes a 400 Bad Request error response with a standardized
// api.ErrorResponse envelope.
func WriteBadRequest(w http.ResponseWriter, msg string) {
	WriteJSON(w, http.StatusBadRequest, api.ErrorResponse{
		Code:    "bad_request",
		Message: msg,
	})
}

// WriteError inspects err, unwraps any *api.APIError or known sentinel error,
// and serializes a structured api.ErrorResponse JSON body with the appropriate
// HTTP status code.
func WriteError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		code := apiErr.Code
		if code == "" {
			code = "error"
		}
		WriteJSON(w, apiErr.StatusCode, api.ErrorResponse{
			Code:    code,
			Message: apiErr.Message,
		})
		return
	}

	status, code := classifyError(err)
	WriteJSON(w, status, api.ErrorResponse{
		Code:    code,
		Message: err.Error(),
	})
}

func classifyError(err error) (int, string) {
	switch {
	case errors.Is(err, broker.ErrNeedSync):
		return http.StatusConflict, api.ErrCodeNeedSync

	case errors.Is(err, session.ErrEmptySessionID),
		errors.Is(err, session.ErrInvalidDecision),
		errors.Is(err, session.ErrInvalidConfig),
		errors.Is(err, session.ErrInvalidSessionID),
		errors.Is(err, session.ErrNoPendingPermissions),
		errors.Is(err, session.ErrNilMessage),
		errors.Is(err, permissions.ErrInvalidRule),
		errors.Is(err, permissions.ErrInvalidScope),
		errors.Is(err, broker.ErrInvalidTopic),
		errors.Is(err, broker.ErrTooManyTopics),
		errors.Is(err, broker.ErrNotRegistered),
		errors.Is(err, broker.ErrInvalidCursor),
		strings.Contains(err.Error(), "workspace parameter is required"),
		strings.Contains(err.Error(), "message content must not be empty"):
		return http.StatusBadRequest, "bad_request"

	case errors.Is(err, session.ErrSessionNotFound),
		errors.Is(err, session.ErrPermissionNotFound),
		errors.Is(err, session.ErrJournalNotFound),
		errors.Is(err, session.ErrEntryNotFound),
		errors.Is(err, session.ErrBlobNotFound),
		strings.Contains(err.Error(), "subscriber not found"):
		return http.StatusNotFound, "not_found"

	case errors.Is(err, session.ErrSessionConflict),
		errors.Is(err, session.ErrRevertConflict),
		errors.Is(err, broker.ErrExists),
		strings.Contains(err.Error(), "session already has an active turn"):
		return http.StatusConflict, "conflict"

	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

// DecodeJSON reads up to 4MB from the request body and decodes it as JSON into T.
func DecodeJSON[T any](r *http.Request) (T, error) {
	var v T
	if r.Body == nil {
		return v, errors.New("empty request body")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

// DecodeOptionalJSON reads up to 4MB from the request body and decodes it into T
// if present, returning the zero value and nil error if empty or EOF.
func DecodeOptionalJSON[T any](r *http.Request) (T, error) {
	var v T
	if r.Body == nil {
		return v, nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	if err := dec.Decode(&v); err != nil && !errors.Is(err, io.EOF) {
		return v, err
	}
	return v, nil
}

// RequestWorkspace extracts the workspace directory from either the X-TaskSmith-CWD
// header or the "workspace" query parameter.
func RequestWorkspace(r *http.Request) string {
	if cwd := strings.TrimSpace(r.Header.Get(api.HeaderCWD)); cwd != "" {
		return cwd
	}
	return strings.TrimSpace(r.URL.Query().Get("workspace"))
}
