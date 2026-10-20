package sandbox

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"
)

// Sentinel errors returned by the sandbox package.
var (
	ErrSandboxClosed     = errors.New("sandbox: sandbox is closed")
	ErrInvalidConfig     = errors.New("sandbox: invalid configuration")
	ErrReadOnly          = errors.New("sandbox: filesystem is read-only")
	ErrPathEscape        = errors.New("sandbox: path escapes sandbox root")
	ErrNetworkDisabled   = errors.New("sandbox: network access is disabled")
	ErrHostBlocked       = errors.New("sandbox: target host is blocked by policy")
	ErrLoopbackBlocked   = errors.New("sandbox: loopback address access is blocked by policy")
	ErrNativeUnavailable = errors.New("sandbox: native OS sandbox backend is unavailable")
)

// Mode defines the isolation backend used by a Sandbox.
type Mode string

const (
	// ModeLocal confines operations to the host working directory using os.Root,
	// process group lifecycle management, and in-process network policy dialers.
	ModeLocal Mode = "local"

	// ModeNative wraps child process execution in OS-level kernel sandboxes
	// (macOS Seatbelt sandbox-exec or Linux Bubblewrap bwrap) in addition to
	// ModeLocal's os.Root and Net enforcement.
	ModeNative Mode = "native"
)

// Platform describes the target operating system, architecture, and root path
// of a sandbox instance.
type Platform struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	WorkDir string `json:"work_dir"`
}

// NetworkPolicy defines outbound network access controls for the sandbox.
type NetworkPolicy struct {
	// Disabled completely blocks all outbound network connections when true.
	Disabled bool `json:"disabled,omitempty"`

	// AllowedHosts restricts outbound connections to matching hostnames or
	// wildcard domain patterns (e.g., "api.example.com", "*.example.com").
	// When empty, all non-blocked hosts are permitted.
	AllowedHosts []string `json:"allowed_hosts,omitempty"`

	// BlockedHosts denies outbound connections to matching hostnames or
	// wildcard domain patterns. BlockedHosts takes precedence over AllowedHosts.
	BlockedHosts []string `json:"blocked_hosts,omitempty"`

	// AllowLoopback permits connections to loopback IP addresses (127.0.0.0/8, ::1).
	// When false (the default), dialing any host that resolves to or specifies a
	// loopback address is rejected with ErrLoopbackBlocked.
	AllowLoopback bool `json:"allow_loopback,omitempty"`
}

func (p *NetworkPolicy) IsZero() bool {
	return !p.Disabled &&
		len(p.AllowedHosts) == 0 &&
		len(p.BlockedHosts) == 0 &&
		!p.AllowLoopback
}

// Sandbox represents an isolated OS execution environment exposing confined
// filesystem, process, and network capabilities.
type Sandbox interface {
	// ID returns the unique identifier of the sandbox instance.
	ID() string

	// Mode returns the active isolation mode (ModeLocal or ModeNative).
	Mode() Mode

	// Platform returns the OS, architecture, and canonical root directory.
	Platform() Platform

	// Env returns a defensive copy of the sandbox's base environment variables.
	Env() map[string]string

	// FS returns the confined filesystem capability rooted at WorkDir.
	FS() FS

	// Proc returns the confined process execution capability.
	Proc() Proc

	// Net returns the policy-enforced network capability.
	Net() Net

	// Upload copies a file or directory tree from hostSrc on the host filesystem
	// into sandboxDst within the sandbox. Returns ErrReadOnly if the sandbox is
	// configured as read-only, or ErrPathEscape if sandboxDst escapes WorkDir.
	Upload(ctx context.Context, hostSrc, sandboxDst string) error

	// Download copies a file or directory tree from sandboxSrc within the sandbox
	// to hostDst on the host filesystem. Returns ErrPathEscape if sandboxSrc
	// escapes WorkDir.
	Download(ctx context.Context, sandboxSrc, hostDst string) error

	// Close terminates all active processes spawned inside the sandbox and
	// releases underlying OS resources (such as the os.Root handle).
	Close() error
}

// File represents an open file handle within the sandbox filesystem.
type File interface {
	fs.File
	io.Writer
	io.ReaderAt
	io.Seeker
	Sync() error
}

// FS provides traversal-safe filesystem operations confined to the sandbox's
// working directory. It implements [io/fs.FS] for read-only standard library
// compatibility alongside context-aware read and write operations.
type FS interface {
	fs.FS // Open(name string) (fs.File, error)

	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) error
	OpenFile(ctx context.Context, path string, flag int, perm fs.FileMode) (File, error)

	Stat(ctx context.Context, path string) (fs.FileInfo, error)
	Lstat(ctx context.Context, path string) (fs.FileInfo, error)
	ReadDir(ctx context.Context, path string) ([]fs.DirEntry, error)

	MkdirAll(ctx context.Context, path string, perm fs.FileMode) error
	Remove(ctx context.Context, path string) error
	RemoveAll(ctx context.Context, path string) error
	Rename(ctx context.Context, oldpath, newpath string) error
	Chmod(ctx context.Context, path string, perm fs.FileMode) error
}

// Command specifies the parameters for executing a child process inside the sandbox.
type Command struct {
	// Name is the executable name or path to invoke.
	Name string

	// Args holds command-line arguments passed to the executable (excluding argv[0]).
	Args []string

	// Dir specifies the working directory relative to the sandbox root (or an
	// absolute path within the sandbox root). If empty, defaults to WorkDir.
	Dir string

	// Env contains per-command environment variables merged on top of the
	// sandbox's base Config.Env.
	Env map[string]string

	// Stdin supplies standard input when calling Proc.Exec.
	Stdin io.Reader

	// Timeout overrides the sandbox's DefaultTimeout for Proc.Exec.
	Timeout time.Duration
}

// ExecResult contains the captured output and exit metadata of a completed process.
type ExecResult struct {
	Stdout   []byte        `json:"stdout,omitempty"`
	Stderr   []byte        `json:"stderr,omitempty"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
}

// Process represents a running child process started via [Proc.Start].
type Process interface {
	// PID returns the operating system process ID.
	PID() int

	// Stdin returns the write-closer pipe connected to the process's standard input.
	Stdin() io.WriteCloser

	// Stdout returns the read-closer pipe connected to the process's standard output.
	Stdout() io.ReadCloser

	// Stderr returns the read-closer pipe connected to the process's standard error.
	Stderr() io.ReadCloser

	// Wait waits for the process to exit and returns its execution result.
	// When pipes are consumed directly via Stdout()/Stderr(), ExecResult contains
	// any remaining unread bytes alongside the exit code and duration.
	Wait() (*ExecResult, error)

	// Signal sends an OS signal to the process group.
	Signal(sig os.Signal) error

	// Kill forcefully terminates the entire process group with SIGKILL.
	Kill() error
}

// Proc provides process execution confined to the sandbox.
type Proc interface {
	// Exec runs a command to completion, capturing its stdout, stderr, exit code,
	// and execution duration. Non-zero exit codes are returned in ExecResult.ExitCode
	// without returning a Go error unless the context expires or startup fails.
	Exec(ctx context.Context, cmd Command) (*ExecResult, error)

	// Start launches a command asynchronously with streaming stdio pipes.
	Start(ctx context.Context, cmd Command) (Process, error)
}

// Net provides network dialing and HTTP client generation subject to the
// sandbox's NetworkPolicy.
type Net interface {
	// DialContext connects to the address on the named network while enforcing
	// the sandbox's NetworkPolicy (disabled check, host allow/block lists, and
	// loopback IP prevention).
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)

	// HTTPClient returns a new *http.Client whose transport is wired to use
	// the sandbox's policy-enforced DialContext.
	HTTPClient() *http.Client
}
