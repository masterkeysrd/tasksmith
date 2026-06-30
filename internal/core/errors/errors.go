package errors

import (
	"bytes"
	"fmt"
)

// Op describes an operation, usually as "package.Function" or "struct.Method".
type Op string

// Kind defines the category/class of the error.
type Kind int

const (
	Other         Kind = iota // Unclassified error
	Invalid                   // Invalid operation or input data
	Permission                // Permission denied / authorization failure
	IO                        // Input/Output or network failure
	Exist                     // Resource already exists
	NotExist                  // Resource does not exist
	Internal                  // Internal system failure
	Configuration             // Configuration or environment error
)

// Code represents a unique identifier for mapping specific errors.
type Code string

const (
	// System & Database
	ErrUnknown          Code = "ERR_UNKNOWN"
	ErrInternal         Code = "ERR_INTERNAL"
	ErrWorkspaceNotInit Code = "ERR_WORKSPACE_NOT_INIT"
	ErrDatabase         Code = "ERR_DATABASE"

	// Session Management
	ErrSessionNotFound     Code = "ERR_SESSION_NOT_FOUND"
	ErrSessionInvalidState Code = "ERR_SESSION_INVALID_STATE"
	ErrSessionExists       Code = "ERR_SESSION_EXISTS"

	// Language Server & Tooling
	ErrLspNotConfigured     Code = "ERR_LSP_NOT_CONFIGURED"
	ErrLspStartFailed       Code = "ERR_LSP_START_FAILED"
	ErrMcpRequestFailed     Code = "ERR_MCP_REQUEST_FAILED"
	ErrMcpServerStartFailed Code = "ERR_MCP_SERVER_START_FAILED"

	// Agent & Templates
	ErrTemplateParse  Code = "ERR_TEMPLATE_PARSE"
	ErrTemplateRender Code = "ERR_TEMPLATE_RENDER"
	ErrAuthPending    Code = "ERR_AUTH_PENDING"
)

// Title represents a user-friendly heading for the toast interface.
type Title string

// Msg represents a user-friendly explanation of the failure.
type Msg string

// T is a convenient, short constructor function to create a Title.
func T(title string) Title {
	return Title(title)
}

// M is a convenient, short constructor function to create a Msg.
func M(msg string) Msg {
	return Msg(msg)
}

// Error represents our custom structured application error.
type Error struct {
	Op          Op    // Operation trace
	Kind        Kind  // Category of error
	Code        Code  // Unique error code
	Title       Title // Toast heading
	Description Msg   // Toast body or user-friendly explanation
	Err         error // Underlying cause
}

// E is the variadic error builder. It collects fields of different types
// to build or wrap an Error.
func E(args ...any) error {
	if len(args) == 0 {
		return nil
	}
	e := &Error{}
	for _, arg := range args {
		switch arg := arg.(type) {
		case Op:
			e.Op = arg
		case Kind:
			e.Kind = arg
		case Code:
			e.Code = arg
		case Title:
			e.Title = arg
		case Msg:
			e.Description = arg
		case *Error:
			// Copy the inner error and wrap it
			copyErr := *arg
			e.Err = &copyErr
		case error:
			e.Err = arg
		}
	}
	return e
}

// Error formats the error chain as a string (e.g. "op1: op2: [code] description").
func (e *Error) Error() string {
	var buf bytes.Buffer

	if e.Op != "" {
		buf.WriteString(string(e.Op))
		buf.WriteString(": ")
	}

	if e.Err != nil {
		buf.WriteString(e.Err.Error())
	} else {
		if e.Code != "" {
			buf.WriteString(fmt.Sprintf("[%s] ", e.Code))
		}
		if e.Title != "" {
			buf.WriteString(string(e.Title))
			buf.WriteString(" - ")
		}
		if e.Description != "" {
			buf.WriteString(string(e.Description))
		} else {
			buf.WriteString("system error")
		}
	}

	return buf.String()
}

// Unwrap returns the underlying wrapped error.
func (e *Error) Unwrap() error {
	return e.Err
}

// GetTitle resolves the user-friendly title. It defaults to the Code's translation
// or "Error" if not specified.
func (e *Error) GetTitle() string {
	if e.Title != "" {
		return string(e.Title)
	}
	if e.Code != "" {
		return TranslateTitle(e.Code)
	}
	return "Error"
}

// GetDescription resolves the user-friendly description. It falls back to Code translation
// or the underlying error's details.
func (e *Error) GetDescription() string {
	if e.Description != "" {
		return string(e.Description)
	}
	if e.Code != "" {
		return TranslateDescription(e.Code)
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "An unexpected internal error occurred."
}

// TranslateTitle maps an Error Code to a user-facing short title.
func TranslateTitle(code Code) string {
	switch code {
	case ErrWorkspaceNotInit:
		return "Setup Required"
	case ErrSessionNotFound:
		return "Session Not Found"
	case ErrSessionInvalidState:
		return "Session Busy"
	case ErrLspNotConfigured:
		return "LSP Unconfigured"
	case ErrLspStartFailed:
		return "LSP Start Failure"
	case ErrMcpRequestFailed, ErrMcpServerStartFailed:
		return "MCP Error"
	case ErrTemplateParse, ErrTemplateRender:
		return "Template Error"
	case ErrAuthPending:
		return "Authorization Pending"
	case ErrDatabase:
		return "Database Failure"
	default:
		return "Error"
	}
}

// TranslateDescription maps an Error Code to a user-facing detailed explanation.
func TranslateDescription(code Code) string {
	switch code {
	case ErrWorkspaceNotInit:
		return "Workspace has not been initialized. Please run setup first."
	case ErrSessionNotFound:
		return "The requested chat session could not be found."
	case ErrSessionInvalidState:
		return "The session is busy or in an invalid state for this operation."
	case ErrLspNotConfigured:
		return "Language Server (LSP) is not configured for this workspace."
	case ErrLspStartFailed:
		return "Failed to start the Language Server. Check your LSP settings."
	case ErrMcpRequestFailed:
		return "An error occurred while communicating with the MCP server."
	case ErrMcpServerStartFailed:
		return "Failed to start the MCP server."
	case ErrTemplateParse, ErrTemplateRender:
		return "Failed to parse or render the agent prompt templates."
	case ErrAuthPending:
		return "Action blocked: A tool authorization request is pending approval."
	case ErrDatabase:
		return "A database read or write operation failed."
	default:
		return "An unexpected internal error occurred."
	}
}

// Match checks if two errors have the same Kind or Code.
func Match(err1, err2 error) bool {
	if err1 == nil || err2 == nil {
		return err1 == err2
	}
	e1, ok1 := err1.(*Error)
	e2, ok2 := err2.(*Error)
	if !ok1 || !ok2 {
		return false
	}
	if e1.Kind != 0 && e2.Kind != 0 && e1.Kind == e2.Kind {
		return true
	}
	if e1.Code != "" && e2.Code != "" && e1.Code == e2.Code {
		return true
	}
	return Match(e1.Err, e2.Err)
}
