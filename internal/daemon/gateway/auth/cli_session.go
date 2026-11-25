package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

const cliProfilePrefix = "account-"

// VendorCLI describes an isolated vendor CLI execution environment rooted in a
// TaskSmith-managed profile directory under the daemon credential store.
type VendorCLI struct {
	Executable string
	ProfileDir string
	WorkDir    string
	Env        []string
}

// NewVendorCLI resolves binary from PATH, prepares isolated work/home/tmp
// subdirectories under dir, and builds an allowlisted child environment bound
// to configKey=dir.
func NewVendorCLI(binary, configKey, dir string, extraEnv ...string) (VendorCLI, error) {
	executable, err := exec.LookPath(binary)
	if err != nil || !filepath.IsAbs(executable) {
		return VendorCLI{}, fmt.Errorf("install %s on the TaskSmith daemon host and add it to PATH", binary)
	}
	work := filepath.Join(dir, "work")
	home := filepath.Join(dir, "home")
	tmp := filepath.Join(dir, "tmp")
	for _, path := range []string{work, home, tmp} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return VendorCLI{}, fmt.Errorf("prepare isolated %s directories: %w", binary, err)
		}
	}
	var pathDirs []string
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(entry) {
			pathDirs = append(pathDirs, entry)
		}
	}
	env := []string{
		"PATH=" + strings.Join(pathDirs, string(os.PathListSeparator)),
		"HOME=" + home,
		"TMPDIR=" + tmp,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		configKey + "=" + dir,
	}
	for _, name := range []string{
		"LANG", "LC_ALL", "LC_CTYPE", "TERM",
		"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_CURRENT_DESKTOP",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return VendorCLI{
		Executable: executable,
		ProfileDir: dir,
		WorkDir:    work,
		Env:        append(env, extraEnv...),
	}, nil
}

// Command builds an exec.Cmd configured with the isolated working directory
// and environment.
func (c VendorCLI) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Executable, args...)
	cmd.Dir = c.WorkDir
	cmd.Env = c.Env
	cmd.WaitDelay = time.Second
	return cmd
}

// VendorAuthError returns ctx.Err() when canceled, or a redacted error that
// never leaks vendor CLI stderr/stdout.
func VendorAuthError(ctx context.Context, vendor, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s %s failed", vendor, action)
}

// CLISessionConfig wires provider-specific CLI operations into a generic
// CLISessionStore.
type CLISessionConfig[R io.Closer] struct {
	Provider string
	Label    string
	NewCLI   func(dir string) (VendorCLI, error)
	Login    func(context.Context, VendorCLI) error
	Account  func(context.Context, VendorCLI) (string, bool, error)
	Logout   func(context.Context, VendorCLI) (bool, error)
	Build    func(VendorCLI) (R, error)
}

// CLISessionStore manages isolated per-account profile directories, atomic
// active.json registration swaps, and one shared runtime instance per active
// registration for a vendor CLI provider.
type CLISessionStore[R io.Closer] struct {
	cfg CLISessionConfig[R]

	mu      sync.Mutex
	runtime *cliSessionRuntime[R]
}

// NewCLISessionStore constructs a CLISessionStore from cfg.
func NewCLISessionStore[R io.Closer](cfg CLISessionConfig[R]) *CLISessionStore[R] {
	return &CLISessionStore[R]{cfg: cfg}
}

type cliRegistration struct {
	Profile string `json:"profile"`
	Account string `json:"account,omitempty"`
}

type cliSessionRuntime[R io.Closer] struct {
	runtime R
	dir     string
}

func (r *cliSessionRuntime[R]) close() error {
	if r == nil {
		return nil
	}
	return r.runtime.Close()
}

// LoginLabel returns the UI label for browser sign-in.
func (s *CLISessionStore[R]) LoginLabel() string { return s.cfg.Label }

func (s *CLISessionStore[R]) registrationPath() (string, error) {
	return xdg.SubDataDir("credentials", s.cfg.Provider, "active.json")
}

func readCLIRegistration(path string) (cliRegistration, error) {
	var account cliRegistration
	data, err := os.ReadFile(path)
	if err != nil {
		return account, err
	}
	if err := json.Unmarshal(data, &account); err != nil {
		return account, fmt.Errorf("read provider registration: %w", err)
	}
	if !strings.HasPrefix(account.Profile, cliProfilePrefix) || filepath.Base(account.Profile) != account.Profile {
		return account, errors.New("invalid provider profile in registration")
	}
	return account, nil
}

// Account returns the active account identifier and whether a registration exists.
func (s *CLISessionStore[R]) Account() (string, bool) {
	path, err := s.registrationPath()
	if err != nil {
		return "", false
	}
	account, err := readCLIRegistration(path)
	return account.Account, err == nil
}

// CredentialRevision changes whenever the active profile directory changes.
func (s *CLISessionStore[R]) CredentialRevision() string {
	path, err := s.registrationPath()
	if err != nil {
		return ""
	}
	account, err := readCLIRegistration(path)
	if err != nil {
		return ""
	}
	return account.Profile
}

// Authorize signs in inside a freshly created isolated profile directory and
// returns a commit function that atomically activates it and retires any
// previous profile.
func (s *CLISessionStore[R]) Authorize(ctx context.Context) (func() (string, error), error) {
	path, err := s.registrationPath()
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(parent, cliProfilePrefix)
	if err != nil {
		return nil, err
	}
	cli, err := s.cfg.NewCLI(dir)
	if err != nil {
		discardCLIProfile(dir)
		return nil, err
	}
	if err := s.cfg.Login(ctx, cli); err != nil {
		discardCLIProfile(dir)
		return nil, err
	}
	account, ok, err := s.cfg.Account(ctx, cli)
	if err != nil || !ok {
		s.retire(nil, dir)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s did not report a signed-in subscription account", s.cfg.Provider)
	}
	data, err := json.Marshal(cliRegistration{Profile: filepath.Base(dir), Account: account})
	if err != nil {
		s.retire(nil, dir)
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { s.retire(nil, dir) })
	return func() (string, error) {
		if !stop() {
			return "", context.Canceled
		}
		previous, retired, err := s.activate(ctx, path, data)
		if err != nil {
			s.retire(nil, dir)
			return "", err
		}
		s.retire(retired, previous)
		return account, nil
	}, nil
}

func (s *CLISessionStore[R]) activate(ctx context.Context, path string, data []byte) (string, *cliSessionRuntime[R], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	previous, err := readCLIRegistration(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, err
	}
	if err := fsutils.WriteFileAtomic(path, data, 0o600); err != nil {
		return "", nil, err
	}
	var dir string
	if previous.Profile != "" {
		dir = filepath.Join(filepath.Dir(path), previous.Profile)
	}
	return dir, s.detachRuntimeLocked(), nil
}

// Logout removes the active registration, closes any cached runtime, and
// performs best-effort vendor sign-out and profile cleanup.
func (s *CLISessionStore[R]) Logout() (bool, error) {
	dir, retired, deleted, err := s.clearRegistration()
	if err != nil {
		return false, err
	}
	s.retire(retired, dir)
	return deleted, nil
}

func (s *CLISessionStore[R]) clearRegistration() (string, *cliSessionRuntime[R], bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.registrationPath()
	if err != nil {
		return "", nil, false, err
	}
	account, readErr := readCLIRegistration(path)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", s.detachRuntimeLocked(), false, nil
		}
		return "", nil, false, err
	}
	var dir string
	if readErr == nil {
		dir = filepath.Join(filepath.Dir(path), account.Profile)
	} else {
		slog.Warn("removed unreadable provider registration", "provider", s.cfg.Provider, "error", readErr)
		if s.runtime != nil {
			dir = s.runtime.dir
		}
	}
	return dir, s.detachRuntimeLocked(), true, nil
}

func (s *CLISessionStore[R]) retire(runtime *cliSessionRuntime[R], dir string) {
	if err := runtime.close(); err != nil {
		slog.Warn("close subscription runtime", "provider", s.cfg.Provider, "error", err)
	}
	if dir == "" {
		return
	}
	defer discardCLIProfile(dir)
	cli, err := s.cfg.NewCLI(dir)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_, err = s.cfg.Logout(ctx, cli)
	}
	if err != nil {
		slog.Warn("vendor sign-out did not complete", "provider", s.cfg.Provider, "error", err)
	}
}

func discardCLIProfile(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("remove retired provider profile", "error", err)
	}
}

func (s *CLISessionStore[R]) detachRuntimeLocked() *cliSessionRuntime[R] {
	runtime := s.runtime
	s.runtime = nil
	return runtime
}

// Close closes the cached shared runtime without modifying saved registrations.
func (s *CLISessionStore[R]) Close() error {
	s.mu.Lock()
	runtime := s.detachRuntimeLocked()
	s.mu.Unlock()
	return runtime.close()
}

// SharedRuntime returns the shared runtime instance for the active registration,
// building it lazily or replacing it when the active profile on disk changed.
func (s *CLISessionStore[R]) SharedRuntime() (R, error) {
	for {
		s.mu.Lock()
		runtime, stale, err := s.sharedRuntimeLocked()
		s.mu.Unlock()
		if stale == nil {
			return runtime, err
		}
		s.retire(stale, "")
	}
}

func (s *CLISessionStore[R]) sharedRuntimeLocked() (R, *cliSessionRuntime[R], error) {
	var zero R
	path, err := s.registrationPath()
	if err != nil {
		return zero, nil, err
	}
	account, err := readCLIRegistration(path)
	if errors.Is(err, os.ErrNotExist) {
		return zero, nil, fmt.Errorf("%w: sign in with `tasksmith auth login %s`", ErrMissingCredential, s.cfg.Provider)
	}
	if err != nil {
		return zero, nil, err
	}
	dir := filepath.Join(filepath.Dir(path), account.Profile)
	if s.runtime != nil {
		if s.runtime.dir != dir {
			return zero, s.detachRuntimeLocked(), nil
		}
		return s.runtime.runtime, nil, nil
	}
	cli, err := s.cfg.NewCLI(dir)
	if err != nil {
		return zero, nil, err
	}
	runtime, err := s.cfg.Build(cli)
	if err != nil {
		return zero, nil, fmt.Errorf("initialize %s: %w", s.cfg.Provider, err)
	}
	s.runtime = &cliSessionRuntime[R]{runtime: runtime, dir: dir}
	return runtime, nil, nil
}
