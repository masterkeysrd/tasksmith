// Package client provides a stateless HTTP and Server-Sent Events (SSE) client
// for communicating with the TaskSmith Brain Daemon over a Unix domain socket.
//
// This package depends exclusively on [github.com/masterkeysrd/tasksmith/api]
// and the Go standard library, maintaining strict separation from internal
// daemon and engine packages.
package client
