// Package xdg provides Neovim-inspired XDG Base Directory path resolution for TaskSmith.
//
// Modeled after Neovim's stdpath() and NVIM_APPNAME conventions, this package
// resolves user-level directories for configuration, persistent data, local state,
// cache, and ephemeral runtime files (such as Unix domain sockets and lockfiles).
//
// # Application Name (TASKSMITH_APPNAME)
//
// All application directories are scoped by the application name returned by [AppName],
// which defaults to "tasksmith". Users and test harnesses can override this name via the
// TASKSMITH_APPNAME environment variable (analogous to NVIM_APPNAME in Neovim) to run
// isolated profiles side by side. Path traversal components ("..") and absolute paths
// in TASKSMITH_APPNAME are rejected and fall back to "tasksmith".
//
// # Directory Types (VarType / stdpath equivalents)
//
//   - [VarTypeConfig] ("config"): User configuration files ($XDG_CONFIG_HOME/<appname>,
//     defaulting to ~/.config/<appname> on Unix or %LOCALAPPDATA%\<appname> on Windows).
//   - [VarTypeData] ("data"): Persistent application data and workspace repositories
//     ($XDG_DATA_HOME/<appname>, defaulting to ~/.local/share/<appname> on Unix or
//     %LOCALAPPDATA%\<appname> on Windows).
//   - [VarTypeState] ("state"): Persistent local state and logs ($XDG_STATE_HOME/<appname>,
//     defaulting to ~/.local/state/<appname> on Unix or %LOCALAPPDATA%\<appname> on Windows).
//   - [VarTypeCache] ("cache"): Non-essential cached data ($XDG_CACHE_HOME/<appname>,
//     defaulting to ~/.cache/<appname> on Unix or %TEMP%\<appname> on Windows).
//   - [VarTypeRuntime] ("run"): Ephemeral runtime files such as IPC sockets and lockfiles.
//     When $XDG_RUNTIME_DIR is set, [Home] resolves to $XDG_RUNTIME_DIR/<appname>.
//     When $XDG_RUNTIME_DIR is unset (common on macOS and headless environments),
//     [Home] falls back to <StateHome>/run (i.e. $XDG_STATE_HOME/<appname>/run).
//
// # Workspace, Log, and Socket Helpers
//
//   - [WorkspacesHome] and [WorkspaceDir] resolve deterministic per-workspace data
//     directories under <DataHome>/workspaces/<sha256-prefix>.
//   - [LogsDir] and [LogFilePath] resolve log files under <StateHome>/logs (following
//     Neovim's stdpath("log") convention under the state directory).
//   - [SocketPath] resolves IPC socket and lockfile paths inside the runtime directory.
package xdg
