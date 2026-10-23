// Package sandbox provides an OS-level virtualized execution environment with
// strict confinement across three foundational capabilities: Filesystem ([FS]),
// Process execution ([Proc]), and Network access ([Net]).
//
// # Pure Virtualized OS Layer
//
// The sandbox package is a pure operating-system virtualization boundary. It has
// zero awareness of AI models, Loom, agents, tools, or Git worktrees, and does
// not provide context-value injection helpers. Higher-level subsystems (such as
// workspace managers or tool executors) consume a [Sandbox] explicitly via its
// capability interfaces. Host-to-sandbox file transfers ([Sandbox.Upload] and
// [Sandbox.Download]) live exclusively on the top-level [Sandbox] controller and
// are intentionally omitted from [FS] so untrusted callers holding only [FS]
// cannot reference host paths.
//
// # Execution Modes
//
// A [Sandbox] operates in one of two modes configured via [Mode]:
//
//   - [ModeLocal]: Confines filesystem operations to the sandbox working directory
//     using Go's kernel-backed [os.Root] capability handle, manages child processes
//     via isolated Unix process groups, and enforces network policies in-process
//     through a policy-enforcing dialer ([Net]).
//
//   - [ModeNative]: Layers OS-native kernel sandboxing over [ModeLocal] for spawned
//     processes. On macOS (darwin), child processes are wrapped with Seatbelt
//     (/usr/bin/sandbox-exec) using dynamically generated mandatory access control
//     profiles. On Linux, child processes are isolated inside unprivileged
//     namespaces via Bubblewrap (bwrap) with read-only root bind mounts, isolated
//     PID namespaces, and optional network namespace unsharing.
//
// # Strict Environment & Security Invariants
//
//   - Zero Implicit Environment Leakage: The sandbox never reads [os.Environ] or
//     [os.Getenv] when constructing child process environments. Child processes
//     receive strictly the variables declared in [Config.Env] merged with
//     per-command overrides in [Command.Env] (plus a minimal safe PATH default if
//     none is provided).
//   - Traversal-Safe Filesystem: All filesystem operations are rooted in
//     [Config.WorkDir] via [os.Root], preventing symlink escapes and "../" path
//     traversal attacks with [ErrPathEscape].
//   - Read-Only Enforcement: When [Config.ReadOnly] is enabled, mutating filesystem
//     calls return [ErrReadOnly] immediately, and native OS profiles disallow writes
//     to the working directory.
//   - Process Group Lifecycle: Every spawned process runs in its own process group.
//     Killing a process or closing the sandbox terminates the entire process tree,
//     preventing orphan background processes.
package sandbox
