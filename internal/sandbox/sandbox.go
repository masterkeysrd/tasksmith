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
	"sync/atomic"
)

// session is the concrete implementation of the Sandbox interface.
type session struct {
	id          string
	mode        Mode
	workDir     string
	realWorkDir string
	cfg         Config
	root        *os.Root
	fsCap       *sandboxFS
	procCap     *sandboxProc
	netCap      *sandboxNet
	closed      atomic.Bool
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

	if mode == ModeNative {
		if err := checkNativeAvailable(); err != nil {
			return nil, err
		}
	}

	root, err := os.OpenRoot(cleanWorkDir)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to open root %q: %v", ErrInvalidConfig, cleanWorkDir, err)
	}

	// Clone environment map defensively so external mutations do not affect the sandbox.
	envCopy := make(map[string]string, len(cfg.Env))
	maps.Copy(envCopy, cfg.Env)
	cfg.Env = envCopy
	cfg.Mode = mode
	cfg.WorkDir = cleanWorkDir

	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		fp := cfg.Fingerprint()
		if len(fp) > 12 {
			id = "sbx-" + fp[:12]
		} else {
			id = "sbx-" + fp
		}
	}

	s := &session{
		id:          id,
		mode:        mode,
		workDir:     cleanWorkDir,
		realWorkDir: realWorkDir,
		cfg:         cfg,
		root:        root,
	}

	s.fsCap = newSandboxFS(s)
	s.procCap = newSandboxProc(s)
	s.netCap = newSandboxNet(s)

	return s, nil
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Mode() Mode {
	return s.mode
}

func (s *session) Platform() Platform {
	return Platform{
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		WorkDir: s.workDir,
	}
}

func (s *session) Env() map[string]string {
	out := make(map[string]string, len(s.cfg.Env))
	maps.Copy(out, s.cfg.Env)
	return out
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
	if s.cfg.ReadOnly {
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

func (s *session) uploadFile(ctx context.Context, hostFile, relDst string, perm fs.FileMode) error {
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
	defer srcFile.Close()

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

func (s *session) downloadFile(ctx context.Context, relSrc, hostDst string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(hostDst), 0o755); err != nil {
		return err
	}

	srcFile, err := s.fsCap.OpenFile(ctx, relSrc, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer srcFile.Close()

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

// Close terminates all active child processes and closes the os.Root handle.
// Calling Close multiple times is safe and idempotent.
func (s *session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	s.netCap.closeIdleConnections()
	procErr := s.procCap.closeAll()
	rootErr := s.root.Close()
	return errors.Join(procErr, rootErr)
}
