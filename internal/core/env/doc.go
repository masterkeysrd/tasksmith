// Package env provides pure, daemon-safe .env file parsing, merging, and atomic
// file updating into isolated in-memory maps.
//
// # Daemon-Safe Isolation
//
// Unlike traditional dotenv libraries that mutate process-global state via
// os.Setenv or read ambient variables via os.Getenv, this package operates
// exclusively on isolated map[string]string values. In the TaskSmith Brain
// daemon, multiple workspaces, worktrees, and subagents execute concurrently
// within a single OS process. By forbidding global environment reads and
// mutations, credentials and configuration variables loaded for one workspace
// can never leak into or overwrite another workspace's environment.
//
// # Precedence and Merging
//
// Environment maps can be layered deterministically using [Merge], where later
// maps take precedence over earlier ones (for example, overlaying a workspace's
// local .tasksmith/.env on top of the global ~/.tasksmith/.env).
//
// # In-Place File Updates
//
// [SetInFile] updates or appends individual environment variables in a .env
// file while preserving existing comments, blank lines, key ordering, and file
// permissions, creating new files with restrictive 0600 permissions by default.
package env
