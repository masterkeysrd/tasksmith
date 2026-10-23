// Package api defines the public, wire-level JSON data transfer objects,
// enumerations, error types, and Server-Sent Event (SSE) envelopes exchanged
// between the TaskSmith Brain Daemon and stateless clients (TUI, CLI, and
// external integrations). It also includes the generated built-in tool input
// and output types (tools.go).
//
// This package is a pure protocol contract and depends exclusively on the Go
// standard library with zero internal or third-party dependencies.
package api
