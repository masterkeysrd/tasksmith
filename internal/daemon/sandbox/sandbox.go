package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// session is the concrete implementation of the Sandbox interface.
type session struct {
	id               string
	mode             Mode
	workDir          string
	realWorkDir      string
	tempDir          string
	realTempDir      string
	sessionWriteDirs []string
	fsPolicy         atomic.Pointer[FilesystemPolicy]
	envMu            sync.RWMutex
	cfg              Config
	// netProxy serves allowlisted child traffic when the network posture is
	// "allowlist"; guarded by envMu together with cfg.
	netProxy       *Proxy
	onUnlistedHost HostApprovalFunc
	readOnly       atomic.Bool
	root           *os.Root
	fsCap          *sandboxFS
	procCap        *sandboxProc
	netCap         *sandboxNet
	closed         atomic.Bool
	watchersMu     sync.Mutex
	watchers       map[*sandboxWatcher]struct{}
}

// New validates cfg and initializes a new confined Sandbox instance.
func New(cfg Config) (Sandbox, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mode := cfg.Mode
	if mode == "" {
		mode = ModeLocal
	}

	cleanWorkDir := filepath.Clean(strings.TrimSpace(cfg.WorkDir))
	realWorkDir := cleanWorkDir
	if resolved, err := filepath.EvalSymlinks(cleanWorkDir); err == nil {
		realWorkDir = filepath.Clean(resolved)
	}

	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		fp := cfg.Fingerprint()
		if len(fp) > 12 {
			id = "sbx-" + fp[:12]
		} else {
			id = "sbx-" + fp
		}
	}

	var tempDir, realTempDir string
	if mode == ModeNative {
		if err := NativeAvailable(); err != nil {
			return nil, err
		}
		td, err := os.MkdirTemp("", "tasksmith-sbx-"+id+"-")
		if err != nil {
			return nil, fmt.Errorf("sandbox: failed to create temp dir: %w", err)
		}
		tempDir = filepath.Clean(td)
		realTempDir = tempDir
		if resolved, err := filepath.EvalSymlinks(tempDir); err == nil {
			realTempDir = filepath.Clean(resolved)
		}
		_ = os.MkdirAll(filepath.Join(tempDir, "tmp"), 0o755)
		_ = os.MkdirAll(filepath.Join(tempDir, "cache"), 0o755)
		_ = os.MkdirAll(filepath.Join(tempDir, "gocache"), 0o755)
		_ = os.MkdirAll(filepath.Join(tempDir, "home"), 0o755)
	}

	root, err := os.OpenRoot(cleanWorkDir)
	if err != nil {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
		return nil, fmt.Errorf("%w: failed to open root %q: %v", ErrInvalidConfig, cleanWorkDir, err)
	}

	// Clone environment map defensively so external mutations do not affect the sandbox.
	envCopy := make(map[string]string, len(cfg.Env))
	maps.Copy(envCopy, cfg.Env)
	cfg.Env = envCopy
	cfg.Network = cfg.Network.clone()
	cfg.Mode = mode
	cfg.WorkDir = cleanWorkDir

	var sessionWriteDirs []string
	for _, d := range cfg.SessionWriteDirs {
		if d = strings.TrimSpace(d); d != "" {
			sessionWriteDirs = append(sessionWriteDirs, filepath.Clean(d))
		}
	}

	s := &session{
		id:               id,
		mode:             mode,
		workDir:          cleanWorkDir,
		realWorkDir:      realWorkDir,
		tempDir:          tempDir,
		realTempDir:      realTempDir,
		sessionWriteDirs: sessionWriteDirs,
		cfg:              cfg,
		root:             root,
		watchers:         make(map[*sandboxWatcher]struct{}),
	}
	s.readOnly.Store(cfg.ReadOnly)
	s.fsPolicy.Store(normalizeFilesystemPolicy(cfg.Filesystem))
	s.onUnlistedHost = cfg.OnUnlistedHost

	s.fsCap = newSandboxFS(s)
	s.procCap = newSandboxProc(s)
	s.netCap = newSandboxNet(s)

	if mode == ModeNative && cfg.Network.Allowlist && !cfg.Network.Disabled {
		if err := s.startNetProxy(); err != nil {
			_ = root.Close()
			if tempDir != "" {
				_ = os.RemoveAll(tempDir)
			}
			return nil, err
		}
	}

	return s, nil
}

// startNetProxy binds the domain-allowlist proxy for this sandbox. The unix
// socket lives inside the sandbox temp dir so network-namespaced children can
// reach the proxy over the bind-mounted filesystem.
func (s *session) startNetProxy() error {
	cfg := s.netProxyConfig(s.cfg.Network)
	p, err := NewProxy(cfg)
	if err != nil {
		return err
	}
	s.netProxy = p
	return nil
}

func (s *session) netProxyConfig(network NetworkPolicy) ProxyConfig {
	cfg := ProxyConfig{Policy: network.ProxyPolicy()}
	if s.onUnlistedHost != nil {
		cfg.Approver = HostApprovalFunc(s.onUnlistedHost)
	}
	// On Linux the child runs in a closed network namespace and can only reach
	// the proxy through a Unix socket on the bind-mounted temp dir; macOS
	// children connect straight to the loopback TCP listener.
	if sock := s.netProxySocketPath(); sock != "" {
		cfg.UnixSocket = sock
	}
	return cfg
}

// netProxySocketPath returns the Unix socket path the allowlist proxy binds
// for network-namespaced children, or "" when children reach the proxy over
// loopback TCP instead.
func (s *session) netProxySocketPath() string {
	if runtime.GOOS == "linux" && s.tempDir != "" {
		return filepath.Join(s.tempDir, "net.sock")
	}
	return ""
}

// netProxyAddr returns the proxy's loopback address, or "" when no proxy is
// active.
func (s *session) netProxyAddr() string {
	s.envMu.RLock()
	defer s.envMu.RUnlock()
	if s.netProxy == nil {
		return ""
	}
	return s.netProxy.Addr()
}

// networkPosture snapshots the live network policy and the allowlist proxy
// port (0 when no proxy is active) under the environment lock, for the
// platform command wrappers.
func (s *session) networkPosture() (NetworkPolicy, int) {
	s.envMu.RLock()
	defer s.envMu.RUnlock()
	port := 0
	if s.netProxy != nil {
		port = s.netProxy.Port()
	}
	return s.cfg.Network, port
}

// SetNetworkPolicy atomically replaces the live network posture for future
// child processes and in-process dials. The allowlist proxy starts or stops
// with the Allowlist flag; already-running children keep the profile they
// started with. A proxy that cannot start keeps the fail-closed allowlist
// posture (confined children get no network) and reports the error.
func (s *session) SetNetworkPolicy(policy NetworkPolicy) error {
	s.envMu.Lock()
	defer s.envMu.Unlock()
	if s.closed.Load() {
		return ErrSandboxClosed
	}
	s.cfg.Network = policy.clone()
	switch {
	case s.netProxy != nil && (policy.Allowlist && !policy.Disabled):
		s.netProxy.SetPolicy(policy.ProxyPolicy())
		return nil
	case s.netProxy != nil:
		_ = s.netProxy.Close()
		s.netProxy = nil
		return nil
	case policy.Allowlist && !policy.Disabled && s.mode == ModeNative:
		return s.startNetProxy()
	}
	return nil
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Mode() Mode {
	return s.mode
}

// ReadOnly reports whether the sandbox currently enforces read-only
// filesystem semantics.
func (s *session) ReadOnly() bool {
	return s.readOnly.Load()
}

// SetReadOnly toggles read-only enforcement. Subsequent confined filesystem
// mutations fail with ErrReadOnly, and subsequent ModeNative child processes
// receive kernel profiles that deny writes inside WorkDir. Already-running
// child processes keep the profile they started with.
func (s *session) SetReadOnly(readOnly bool) {
	s.readOnly.Store(readOnly)
}

// nativeAvailable caches the kernel sandbox probe for the lifetime of the
// process: whether host policy permits sandboxing does not change while the
// daemon runs, and the probe spawns a child process.
var nativeAvailable = sync.OnceValue(checkNativeAvailable)

// NativeAvailable reports whether the OS-native kernel sandbox backend
// (Seatbelt on macOS, Bubblewrap on Linux) is usable on this host. The check
// actually probes sandbox creation (cached), not just binary presence, so
// restricted containers and nested-sandbox hosts degrade to local isolation.
func NativeAvailable() error {
	return nativeAvailable()
}

// DefaultMode returns ModeNative when the OS kernel sandbox backend is usable
// on this host, falling back to ModeLocal otherwise.
func DefaultMode() Mode {
	if NativeAvailable() != nil {
		return ModeLocal
	}
	return ModeNative
}

// ResolveMode maps a requested sandbox backend onto the mode this host can
// enforce: explicit ModeLocal selects ModeLocal, while empty or ModeNative
// prefers ModeNative and degrades to ModeLocal when the kernel backend is
// unavailable.
func ResolveMode(requested Mode) Mode {
	if Mode(strings.ToLower(strings.TrimSpace(string(requested)))) == ModeLocal {
		return ModeLocal
	}
	return DefaultMode()
}

func (s *session) Platform() Platform {
	return Platform{
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		WorkDir: s.workDir,
	}
}

func (s *session) Env() map[string]string {
	s.envMu.RLock()
	defer s.envMu.RUnlock()
	out := make(map[string]string, len(s.cfg.Env))
	maps.Copy(out, s.cfg.Env)
	return out
}

func (s *session) SetEnv(env map[string]string) {
	cloned := make(map[string]string, len(env))
	maps.Copy(cloned, env)
	s.envMu.Lock()
	s.cfg.Env = cloned
	s.envMu.Unlock()
}

// normalizeFilesystemPolicy defensively clones policy, dropping blank entries
// and cleaning every path. Logical paths are preserved deliberately: resolving
// a leaf symlink here (e.g. a repository-planted .git/hooks -> ../.githooks)
// would anchor rules only on the target and leave the symlink entry itself
// writable, so a sandboxed process could unlink and replace it. Profile
// builders emit rules for both the logical entry and its deepest-existing
// resolved variant via withCanonicalVariants, so confinement still anchors on
// the kernel's canonical vnode path.
func normalizeFilesystemPolicy(policy FilesystemPolicy) *FilesystemPolicy {
	normalizeList := func(paths []string) []string {
		if len(paths) == 0 {
			return nil
		}
		out := make([]string, 0, len(paths))
		for _, p := range paths {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			out = append(out, filepath.Clean(p))
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return &FilesystemPolicy{
		DeniedReadPaths:  normalizeList(policy.DeniedReadPaths),
		AllowReadPaths:   normalizeList(policy.AllowReadPaths),
		ExtraWritePaths:  normalizeList(policy.ExtraWritePaths),
		DeniedWritePaths: normalizeList(policy.DeniedWritePaths),
	}
}

// Filesystem returns a defensive copy of the sandbox's current native
// filesystem path policy.
func (s *session) Filesystem() FilesystemPolicy {
	p := s.fsPolicy.Load()
	if p == nil {
		return FilesystemPolicy{}
	}
	cloneList := func(paths []string) []string {
		if len(paths) == 0 {
			return nil
		}
		return append([]string(nil), paths...)
	}
	return FilesystemPolicy{
		DeniedReadPaths:  cloneList(p.DeniedReadPaths),
		AllowReadPaths:   cloneList(p.AllowReadPaths),
		ExtraWritePaths:  cloneList(p.ExtraWritePaths),
		DeniedWritePaths: cloneList(p.DeniedWritePaths),
	}
}

// SetFilesystemPolicy atomically replaces the native filesystem path policy
// for future child processes. Already-running children keep the profile they
// started with; subsequent executions observe the new rules immediately.
func (s *session) SetFilesystemPolicy(policy FilesystemPolicy) {
	s.fsPolicy.Store(normalizeFilesystemPolicy(policy))
}

func (s *session) FS() FS {
	return s.fsCap
}

func (s *session) Proc() Proc {
	return s.procCap
}

func (s *session) Net() Net {
	return s.netCap
}

func (s *session) isClosed() bool {
	return s.closed.Load()
}

// Upload copies a file or directory tree from hostSrc on the host filesystem
// into sandboxDst inside the sandbox using the confined FS capability.
func (s *session) Upload(ctx context.Context, hostSrc, sandboxDst string) error {
	if s.isClosed() {
		return ErrSandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.readOnly.Load() {
		return ErrReadOnly
	}

	relDst, err := s.fsCap.resolvePath(sandboxDst)
	if err != nil {
		return err
	}

	srcInfo, err := os.Stat(hostSrc)
	if err != nil {
		return fmt.Errorf("sandbox: upload stat host source %q: %w", hostSrc, err)
	}

	if !srcInfo.IsDir() {
		return s.uploadFile(ctx, hostSrc, relDst, srcInfo.Mode().Perm())
	}

	if relDst != "." {
		perm := srcInfo.Mode().Perm()
		if perm == 0 {
			perm = 0o755
		}
		if err := s.fsCap.MkdirAll(ctx, relDst, perm); err != nil {
			return err
		}
	}

	return filepath.WalkDir(hostSrc, func(currPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(hostSrc, currPath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		targetRel := rel
		if relDst != "." {
			targetRel = filepath.Join(relDst, rel)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			perm := info.Mode().Perm()
			if perm == 0 {
				perm = 0o755
			}
			return s.fsCap.MkdirAll(ctx, targetRel, perm)
		}

		if info.Mode().IsRegular() {
			return s.uploadFile(ctx, currPath, targetRel, info.Mode().Perm())
		}
		return nil
	})
}

func (s *session) uploadFile(ctx context.Context, hostFile, relDst string, perm fs.FileMode) (retErr error) {
	if relDst == "." {
		return fmt.Errorf("%w: upload file destination cannot be root directory", ErrInvalidConfig)
	}
	parent := filepath.Dir(relDst)
	if parent != "." && parent != "" {
		if err := s.fsCap.MkdirAll(ctx, parent, 0o755); err != nil {
			return err
		}
	}

	srcFile, err := os.Open(hostFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); retErr == nil {
			retErr = err
		}
	}()

	if perm == 0 {
		perm = 0o644
	}
	dstFile, err := s.fsCap.OpenFile(ctx, relDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// Download copies a file or directory tree from sandboxSrc inside the sandbox
// to hostDst on the host filesystem.
func (s *session) Download(ctx context.Context, sandboxSrc, hostDst string) error {
	if s.isClosed() {
		return ErrSandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	relSrc, err := s.fsCap.resolvePath(sandboxSrc)
	if err != nil {
		return err
	}

	srcInfo, err := s.fsCap.Stat(ctx, relSrc)
	if err != nil {
		return err
	}

	if !srcInfo.IsDir() {
		return s.downloadFile(ctx, relSrc, hostDst, srcInfo.Mode().Perm())
	}

	return s.downloadDir(ctx, relSrc, hostDst, srcInfo.Mode().Perm())
}

func (s *session) downloadDir(ctx context.Context, relSrc, hostDst string, perm fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if perm == 0 {
		perm = 0o755
	}
	if err := os.MkdirAll(hostDst, perm); err != nil {
		return err
	}

	entries, err := s.fsCap.ReadDir(ctx, relSrc)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		childRel := entry.Name()
		if relSrc != "." {
			childRel = filepath.Join(relSrc, entry.Name())
		}
		childHost := filepath.Join(hostDst, entry.Name())

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if err := s.downloadDir(ctx, childRel, childHost, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}

		if info.Mode().IsRegular() {
			if err := s.downloadFile(ctx, childRel, childHost, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *session) downloadFile(ctx context.Context, relSrc, hostDst string, perm fs.FileMode) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(hostDst), 0o755); err != nil {
		return err
	}

	srcFile, err := s.fsCap.OpenFile(ctx, relSrc, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err := srcFile.Close(); retErr == nil {
			retErr = err
		}
	}()

	if perm == 0 {
		perm = 0o644
	}
	dstFile, err := os.OpenFile(hostDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// Close terminates all active child processes, stops all active watchers,
// and closes the os.Root handle.
// Calling Close multiple times is safe and idempotent.
func (s *session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	s.closeWatchers()
	s.netCap.closeIdleConnections()
	s.envMu.Lock()
	if s.netProxy != nil {
		_ = s.netProxy.Close()
		s.netProxy = nil
	}
	s.envMu.Unlock()
	procErr := s.procCap.closeAll()
	rootErr := s.root.Close()
	var tempErr error
	if s.tempDir != "" {
		_ = filepath.Walk(s.tempDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				_ = os.Chmod(path, 0o777)
			}
			return nil
		})
		tempErr = os.RemoveAll(s.tempDir)
	}
	return errors.Join(procErr, rootErr, tempErr)
}

func (s *session) registerWatcher(w *sandboxWatcher) bool {
	if s.closed.Load() {
		return false
	}
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	if s.closed.Load() || s.watchers == nil {
		return false
	}
	s.watchers[w] = struct{}{}
	return true
}

func (s *session) unregisterWatcher(w *sandboxWatcher) {
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	if s.watchers != nil {
		delete(s.watchers, w)
	}
}

func (s *session) closeWatchers() {
	s.watchersMu.Lock()
	active := make([]*sandboxWatcher, 0, len(s.watchers))
	for w := range s.watchers {
		active = append(active, w)
	}
	s.watchers = nil
	s.watchersMu.Unlock()

	for _, w := range active {
		_ = w.Close()
	}
}
