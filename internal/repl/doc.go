// Package repl implements the TaskSmith interactive terminal user interface
// using Charm v2 (Bubble Tea v2, Lip Gloss v2, and Bubbles v2) along with a
// deterministic line-driven fallback for non-TTY inputs.
//
// The package communicates exclusively with the TaskSmith Brain Daemon over
// the typed HTTP+SSE client and never imports internal runtime or persistence
// packages directly.
package repl
