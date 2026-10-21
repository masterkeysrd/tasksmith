// Package cli implements TaskSmith's command-line interface and interactive
// terminal REPL built on [github.com/spf13/cobra].
//
// All commands communicate with the background TaskSmith Brain Daemon over its
// Unix domain socket using [github.com/masterkeysrd/tasksmith/client] and
// [github.com/masterkeysrd/tasksmith/api], automatically starting the daemon
// on demand when not already running.
package cli
