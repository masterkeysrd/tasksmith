// Package workspace manages the discovery, loading, resolution, and rendering
// of declarative TaskSmith workspace configurations and resources.
//
// # Four-Tier Resource Resolution
//
// Resources are loaded and resolved across four hierarchical namespaces, listed
// from lowest to highest precedence:
//
//  1. [NamespaceSystem] ("system"): Embedded builtin agents shipped with the
//     binary alongside any additional [io/fs.FS] filesystems injected via
//     [WithBuiltinFS] (such as builtin tool manifests).
//  2. [NamespaceUser] ("user"): User-global resources discovered under the XDG
//     configuration directory ($XDG_CONFIG_HOME/tasksmith/agents).
//  3. [NamespaceWorkspace] ("workspace"): Workspace-wide resources located in
//     the root .agents/ directory of a multi-project workspace.
//  4. [NamespaceLocal] ("local"): Project-local resources located inside the
//     active project's .agents/ directory.
//
// When a workspace is opened at a directory belonging to a specific subproject,
// all resource lookups ([Workspace.Agents], [Workspace.Tools],
// [Workspace.ResolveModels], etc.) automatically scope resolution to that
// project so local overrides shadow workspace, user, and system definitions.
//
// # Isolated Environment Secret Layering
//
// Opening or reloading a workspace constructs an isolated in-memory environment
// map ([Workspace.Env]) without reading or mutating the host process environment
// ([os.Getenv] / [os.Setenv]). Environment files are layered in ascending order
// of precedence:
//
//  1. User-global secrets ($XDG_CONFIG_HOME/tasksmith/.env).
//  2. Ancestor .env files discovered while climbing toward WORKSPACE.md (such as
//     a shared .env in a bare-repository worktree hub).
//  3. Workspace root secrets (<RootDir>/.env).
//  4. Working directory secrets (<CWD>/.env) when operating inside a subproject.
//
// Calling [Workspace.SetEnv] updates the in-memory environment map and atomically
// persists the key-value pair to <RootDir>/.env.
package workspace
