package std

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// Sentinel errors returned by the std package.
var (
	ErrInvalidConfig   = errors.New("std: invalid configuration")
	ErrFileNotRead     = errors.New("std: file must be read before modification")
	ErrFileModified    = errors.New("std: file was modified since last read")
	ErrTargetNotFound  = errors.New("std: edit target not found in file")
	ErrAmbiguousEdit   = errors.New("std: edit target matches multiple locations")
	ErrBinaryFile      = errors.New("std: binary file not supported for text operation")
	ErrInvalidArgument = errors.New("std: invalid argument")
)

// ChangeSource identifies the origin of a recorded file mutation.
type ChangeSource string

const (
	// SourceAgent indicates a file change made directly through an agent tool
	// operation (Write, Edit, Remove, Download).
	SourceAgent ChangeSource = "agent"

	// SourceExternal indicates a file change made outside of direct agent file
	// tools (such as user IDE edits or shell commands run via Bash).
	SourceExternal ChangeSource = "external"
)

// ChangeKind classifies the type of mutation applied to a file.
type ChangeKind string

const (
	// KindCreated indicates a new file was created.
	KindCreated ChangeKind = "created"

	// KindModified indicates an existing file's content was changed.
	KindModified ChangeKind = "modified"

	// KindDeleted indicates an existing file was removed.
	KindDeleted ChangeKind = "deleted"
)

// FileChange captures metadata and content snapshots for a single file mutation.
type FileChange struct {
	ID            string       `json:"id"`
	Path          string       `json:"path"`
	Source        ChangeSource `json:"source"`
	Tool          string       `json:"tool,omitempty"`
	Kind          ChangeKind   `json:"kind"`
	BeforeHash    string       `json:"before_hash,omitempty"`
	AfterHash     string       `json:"after_hash,omitempty"`
	TurnID        string       `json:"turn_id,omitempty"`
	MessageID     string       `json:"message_id,omitempty"`
	ToolCallID    string       `json:"tool_call_id,omitempty"`
	BeforeContent []byte       `json:"-"`
	AfterContent  []byte       `json:"-"`
	Additions     int          `json:"additions,omitempty"`
	Deletions     int          `json:"deletions,omitempty"`
	IsBinary      bool         `json:"is_binary,omitempty"`
	Timestamp     time.Time    `json:"timestamp"`
}

// FileTracker tracks read state for Compare-And-Swap (CAS) conflict detection
// and records structured file changes across a session.
type FileTracker interface {
	LastHash(path string) (hash string, ok bool)
	LastContent(path string) (content []byte, ok bool)
	TrackedPaths() []string
	RecordRead(ctx context.Context, path, hash string) error
	RecordChange(ctx context.Context, change *FileChange) error
}

// ViewRequest configures a file read operation via [Std.View].
type ViewRequest struct {
	// Path is the file path relative to the sandbox root (or an absolute path
	// within the sandbox root).
	Path string `json:"path"`

	// Offset is the 1-based line number from which to start reading. Defaults to 1.
	Offset int `json:"offset,omitempty"`

	// Limit is the maximum number of lines to return. Defaults to DefaultViewLimit (2000).
	Limit int `json:"limit,omitempty"`
}

// ViewResult contains the sliced file content and metadata returned by [Std.View].
type ViewResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Numbered   string `json:"numbered"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated"`
	Hash       string `json:"hash"`
}

// ListRequest configures a directory listing operation via [Std.List].
type ListRequest struct {
	// Path is the directory path relative to the sandbox root. Defaults to ".".
	Path string `json:"path,omitempty"`

	// Recursive traverses subdirectories when true.
	Recursive bool `json:"recursive,omitempty"`

	// MaxDepth bounds recursion depth when Recursive is true (1 = direct children).
	// When zero or negative, recursion depth is unbounded.
	MaxDepth int `json:"max_depth,omitempty"`

	// IncludeHidden includes entries whose names begin with a dot (".").
	IncludeHidden bool `json:"include_hidden,omitempty"`

	// Ignore specifies glob patterns to exclude from the listing.
	Ignore []string `json:"ignore,omitempty"`

	// Limit caps the maximum number of entries returned.
	Limit int `json:"limit,omitempty"`
}

// ListEntry describes a single filesystem entry returned by [Std.List].
type ListEntry struct {
	Path    string      `json:"path"`
	Name    string      `json:"name"`
	IsDir   bool        `json:"is_dir"`
	Size    int64       `json:"size"`
	Mode    fs.FileMode `json:"mode"`
	ModTime time.Time   `json:"mod_time"`
}

// ListResult contains the directory entries returned by [Std.List].
type ListResult struct {
	Path      string      `json:"path"`
	Entries   []ListEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// GlobRequest configures a wildcard path search via [Std.Glob].
type GlobRequest struct {
	// Pattern is the glob expression to match (supports *, ?, [...], and **).
	Pattern string `json:"pattern"`

	// Path is the root directory from which to walk. Defaults to ".".
	Path string `json:"path,omitempty"`

	// IncludeHidden includes dot-prefixed files and directories (except .git and .bare).
	IncludeHidden bool `json:"include_hidden,omitempty"`

	// Ignore specifies additional glob patterns to skip.
	Ignore []string `json:"ignore,omitempty"`

	// Limit caps the maximum number of matched paths returned.
	Limit int `json:"limit,omitempty"`
}

// GlobResult contains the matched file paths returned by [Std.Glob].
type GlobResult struct {
	Pattern   string   `json:"pattern"`
	Path      string   `json:"path"`
	Matches   []string `json:"matches"`
	Truncated bool     `json:"truncated,omitempty"`
}

// GrepRequest configures a regular expression search across text files via [Std.Grep].
type GrepRequest struct {
	// Pattern is the regular expression to search for.
	Pattern string `json:"pattern"`

	// Path is the file or directory path under which to search. Defaults to ".".
	Path string `json:"path,omitempty"`

	// Include is an optional glob pattern filtering which file paths are searched.
	Include string `json:"include,omitempty"`

	// CaseInsensitive enables case-insensitive regex matching.
	CaseInsensitive bool `json:"case_insensitive,omitempty"`

	// ContextLines specifies the number of surrounding lines to capture before
	// and after each matching line.
	ContextLines int `json:"context_lines,omitempty"`

	// IncludeHidden searches dot-prefixed files and directories (except .git and .bare).
	IncludeHidden bool `json:"include_hidden,omitempty"`

	// Limit caps the maximum number of matches returned.
	Limit int `json:"limit,omitempty"`
}

// GrepMatch represents a single regular expression match within a text file.
type GrepMatch struct {
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// GrepResult contains the matches returned by [Std.Grep].
type GrepResult struct {
	Pattern   string      `json:"pattern"`
	Matches   []GrepMatch `json:"matches"`
	Truncated bool        `json:"truncated,omitempty"`
}

// WriteRequest configures a file creation or overwrite operation via [Std.Write].
type WriteRequest struct {
	// Path is the target file path within the sandbox.
	Path string `json:"path"`

	// Content is the full text content to write to the file.
	Content string `json:"content"`

	// Mode sets the file permission bits. Defaults to the existing file's mode
	// or 0644 for newly created files.
	Mode fs.FileMode `json:"mode,omitempty"`

	// ExpectedHash optionally specifies the expected on-disk SHA-256 hex digest
	// for Compare-And-Swap verification. When empty, the FileTracker's last
	// recorded hash is used.
	ExpectedHash string `json:"expected_hash,omitempty"`

	// Force bypasses Read-Before-Overwrite and Compare-And-Swap checks when
	// overwriting an existing file.
	Force bool `json:"force,omitempty"`

	// DryRun computes the unified diff and line statistics without writing to
	// disk or updating the FileTracker.
	DryRun bool `json:"dry_run,omitempty"`
}

// WriteResult contains the diff, statistics, and hashes produced by [Std.Write].
type WriteResult struct {
	Path         string `json:"path"`
	Created      bool   `json:"created"`
	BytesWritten int    `json:"bytes_written"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Diff         string `json:"diff,omitempty"`
	BeforeHash   string `json:"before_hash,omitempty"`
	AfterHash    string `json:"after_hash"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

// EditRequest configures a targeted string replacement within a file via [Std.Edit].
type EditRequest struct {
	// Path is the target file path within the sandbox.
	Path string `json:"path"`

	// OldText is the exact literal substring to search for and replace.
	OldText string `json:"old_text"`

	// NewText is the replacement string.
	NewText string `json:"new_text"`

	// ReplaceAll replaces every occurrence of OldText when true. When false,
	// Edit requires OldText to match exactly once.
	ReplaceAll bool `json:"replace_all,omitempty"`

	// ExpectedHash optionally specifies the expected on-disk SHA-256 hex digest
	// for Compare-And-Swap verification. When empty, the FileTracker's last
	// recorded hash is used.
	ExpectedHash string `json:"expected_hash,omitempty"`

	// Force bypasses Read-Before-Overwrite and Compare-And-Swap checks.
	Force bool `json:"force,omitempty"`

	// DryRun computes the unified diff and line statistics without writing to
	// disk or updating the FileTracker.
	DryRun bool `json:"dry_run,omitempty"`
}

// EditResult contains the diff, replacement count, and hashes produced by [Std.Edit].
type EditResult struct {
	Path         string `json:"path"`
	Replacements int    `json:"replacements"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Diff         string `json:"diff,omitempty"`
	BeforeHash   string `json:"before_hash"`
	AfterHash    string `json:"after_hash"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

// RemoveRequest configures a file deletion operation via [Std.Remove].
type RemoveRequest struct {
	// Path is the target file path within the sandbox.
	Path string `json:"path"`

	// ExpectedHash optionally specifies the expected on-disk SHA-256 hex digest
	// for Compare-And-Swap verification. When empty, the FileTracker's last
	// recorded hash is used.
	ExpectedHash string `json:"expected_hash,omitempty"`

	// Force bypasses Read-Before-Overwrite and Compare-And-Swap checks.
	Force bool `json:"force,omitempty"`

	// DryRun computes the deletion diff and line statistics without removing the
	// file or updating the FileTracker.
	DryRun bool `json:"dry_run,omitempty"`
}

// RemoveResult contains the deletion diff and metadata produced by [Std.Remove].
type RemoveResult struct {
	Path       string `json:"path"`
	Deletions  int    `json:"deletions"`
	Diff       string `json:"diff,omitempty"`
	BeforeHash string `json:"before_hash"`
	DryRun     bool   `json:"dry_run,omitempty"`
}

// BashRequest configures a shell command execution via [Std.Bash].
type BashRequest struct {
	// Command is the shell command string passed to "sh -c".
	Command string `json:"command"`

	// Dir is the optional working directory relative to the sandbox root.
	Dir string `json:"dir,omitempty"`

	// Env specifies optional per-command environment variable overrides.
	Env map[string]string `json:"env,omitempty"`

	// Stdin provides optional standard input to the command.
	Stdin string `json:"stdin,omitempty"`

	// StdinReader provides optional streaming standard input to the command.
	// When non-nil, StdinReader takes precedence over Stdin.
	StdinReader io.Reader `json:"-"`

	// Output optionally receives combined stdout and stderr from the command.
	Output io.Writer `json:"-"`

	// Timeout overrides the sandbox's default execution timeout.
	Timeout time.Duration `json:"timeout,omitempty"`
}

// BashResult contains the process output, exit status, and any external file
// changes detected after [Std.Bash] finishes.
type BashResult struct {
	Stdout          string        `json:"stdout"`
	Stderr          string        `json:"stderr"`
	ExitCode        int           `json:"exit_code"`
	Duration        time.Duration `json:"duration"`
	ExternalChanges []FileChange  `json:"external_changes,omitempty"`
}

// FetchRequest configures an outbound HTTP request via [Std.Fetch].
type FetchRequest struct {
	// URL is the target HTTP or HTTPS URL.
	URL string `json:"url"`

	// Method is the HTTP verb (defaults to "GET").
	Method string `json:"method,omitempty"`

	// Headers contains optional HTTP request headers.
	Headers map[string]string `json:"headers,omitempty"`

	// Body is the optional raw request payload.
	Body []byte `json:"body,omitempty"`

	// Timeout specifies an optional per-request timeout.
	Timeout time.Duration `json:"timeout,omitempty"`

	// MaxBytes caps the maximum number of response body bytes read.
	// Defaults to DefaultMaxFetchBytes (10 MiB).
	MaxBytes int64 `json:"max_bytes,omitempty"`

	// DryRun validates URL and network policy without executing the HTTP request.
	DryRun bool `json:"dry_run,omitempty"`
}

// FetchResult contains the HTTP response status, headers, and body from [Std.Fetch].
type FetchResult struct {
	URL         string            `json:"url"`
	StatusCode  int               `json:"status_code"`
	Status      string            `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Body        []byte            `json:"body,omitempty"`
	Text        string            `json:"text,omitempty"`
	Truncated   bool              `json:"truncated,omitempty"`
	Duration    time.Duration     `json:"duration"`
	DryRun      bool              `json:"dry_run,omitempty"`
}

// DownloadRequest configures an HTTP file download into the sandbox via [Std.Download].
type DownloadRequest struct {
	// URL is the source HTTP or HTTPS URL.
	URL string `json:"url"`

	// Path is the destination file path within the sandbox.
	Path string `json:"path"`

	// Headers contains optional HTTP request headers.
	Headers map[string]string `json:"headers,omitempty"`

	// Mode sets the destination file permission bits. Defaults to 0644.
	Mode fs.FileMode `json:"mode,omitempty"`

	// ExpectedHash optionally specifies the expected on-disk SHA-256 hex digest
	// for Compare-And-Swap verification when overwriting an existing file.
	ExpectedHash string `json:"expected_hash,omitempty"`

	// Force bypasses Read-Before-Overwrite and Compare-And-Swap checks when the
	// destination file already exists.
	Force bool `json:"force,omitempty"`

	// Timeout specifies an optional timeout for the download request.
	Timeout time.Duration `json:"timeout,omitempty"`

	// MaxBytes caps the maximum allowed download size in bytes.
	// Defaults to DefaultMaxDownloadBytes (100 MiB).
	MaxBytes int64 `json:"max_bytes,omitempty"`

	// DryRun validates URL and destination path and enforces Read-Before-Overwrite
	// and Compare-And-Swap checks without performing the network request or
	// writing to disk.
	DryRun bool `json:"dry_run,omitempty"`
}

// DownloadResult contains metadata for a completed [Std.Download] operation.
type DownloadResult struct {
	URL          string        `json:"url"`
	Path         string        `json:"path"`
	StatusCode   int           `json:"status_code"`
	ContentType  string        `json:"content_type,omitempty"`
	BytesWritten int           `json:"bytes_written"`
	Created      bool          `json:"created"`
	BeforeHash   string        `json:"before_hash,omitempty"`
	AfterHash    string        `json:"after_hash"`
	IsBinary     bool          `json:"is_binary,omitempty"`
	Duration     time.Duration `json:"duration"`
	DryRun       bool          `json:"dry_run,omitempty"`
}
