package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultSafePath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin"

// sandboxProc implements the Proc capability.
type sandboxProc struct {
	s      *session
	mu     sync.Mutex
	active map[int]*sandboxProcess
}

func newSandboxProc(s *session) *sandboxProc {
	return &sandboxProc{
		s:      s,
		active: make(map[int]*sandboxProcess),
	}
}

func (p *sandboxProc) resolveWorkDir(dir string) (string, error) {
	rel, err := p.s.fsCap.resolvePath(dir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(p.s.workDir, rel)
	info, err := p.s.root.Stat(rel)
	if err != nil {
		return "", wrapRootError(err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: working directory %q is not a directory", ErrInvalidConfig, dir)
	}
	return target, nil
}

// buildEnv constructs a deterministic environment slice strictly from
// Config.Env and Command.Env without ever inspecting os.Environ() or os.Getenv().
func (p *sandboxProc) buildEnv(cmdEnv map[string]string, workDir string) ([]string, string) {
	merged := make(map[string]string, len(p.s.cfg.Env)+len(cmdEnv)+8)
	maps.Copy(merged, p.s.cfg.Env)
	maps.Copy(merged, cmdEnv)
	if _, ok := merged["PATH"]; !ok {
		merged["PATH"] = defaultSafePath
	}
	if _, ok := merged["HOME"]; !ok {
		if p.s.mode == ModeNative && p.s.tempDir != "" {
			merged["HOME"] = filepath.Join(p.s.tempDir, "home")
		} else {
			home, err := os.UserHomeDir()
			if err != nil || strings.TrimSpace(home) == "" {
				home = os.TempDir()
			}
			merged["HOME"] = home
		}
	}
	if _, ok := merged["PWD"]; !ok {
		merged["PWD"] = workDir
	}

	if p.s.mode == ModeNative && p.s.tempDir != "" {
		if _, ok := merged["TMPDIR"]; !ok {
			merged["TMPDIR"] = filepath.Join(p.s.tempDir, "tmp")
		}
		if _, ok := merged["XDG_CACHE_HOME"]; !ok {
			merged["XDG_CACHE_HOME"] = filepath.Join(p.s.tempDir, "cache")
		}
		if _, ok := merged["GOCACHE"]; !ok {
			merged["GOCACHE"] = filepath.Join(p.s.tempDir, "gocache")
		}
	} else {
		if _, ok := merged["TMPDIR"]; !ok {
			merged["TMPDIR"] = os.TempDir()
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	envSlice := make([]string, 0, len(keys))
	for _, k := range keys {
		envSlice = append(envSlice, k+"="+merged[k])
	}
	return envSlice, merged["PATH"]
}

// resolveExecutable locates cmdName using the sandbox's explicit PATH variable
// rather than exec.LookPath (which reads the host daemon's os.Getenv("PATH")).
func resolveExecutable(cmdName, pathEnv, workDir string) (string, error) {
	name := strings.TrimSpace(cmdName)
	if name == "" {
		return "", fmt.Errorf("%w: command name is required", ErrInvalidConfig)
	}

	if strings.ContainsRune(name, filepath.Separator) {
		if filepath.IsAbs(name) {
			return filepath.Clean(name), nil
		}
		clean := filepath.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: relative command path %q escapes working directory", ErrPathEscape, cmdName)
		}
		return filepath.Join(workDir, clean), nil
	}

	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("sandbox: executable %q not found in sandbox PATH", name)
}

func (p *sandboxProc) prepareExecCmd(cmd Command) (*exec.Cmd, error) {
	if p.s.isClosed() {
		return nil, ErrSandboxClosed
	}

	targetDir, err := p.resolveWorkDir(cmd.Dir)
	if err != nil {
		return nil, err
	}

	envSlice, pathEnv := p.buildEnv(cmd.Env, targetDir)
	resolvedBin, err := resolveExecutable(cmd.Name, pathEnv, targetDir)
	if err != nil {
		return nil, err
	}

	bin := resolvedBin
	args := append([]string(nil), cmd.Args...)

	if p.s.mode == ModeNative {
		var wrapErr error
		bin, args, wrapErr = wrapNativeCommand(p.s, targetDir, resolvedBin, args)
		if wrapErr != nil {
			return nil, wrapErr
		}
	}

	execCmd := exec.Command(bin, args...)
	execCmd.Dir = targetDir
	execCmd.Env = envSlice
	execCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	return execCmd, nil
}

func (p *sandboxProc) Exec(ctx context.Context, cmd Command) (*ExecResult, error) {
	if p.s.isClosed() {
		return nil, ErrSandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	timeout := cmd.Timeout
	if timeout <= 0 {
		timeout = p.s.cfg.DefaultTimeout
	}
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	execCmd, err := p.prepareExecCmd(cmd)
	if err != nil {
		return nil, err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	if cmd.Stdout != nil {
		execCmd.Stdout = cmd.Stdout
	} else {
		execCmd.Stdout = &stdoutBuf
	}
	if cmd.Stderr != nil {
		execCmd.Stderr = cmd.Stderr
	} else {
		execCmd.Stderr = &stderrBuf
	}
	if cmd.Stdin != nil {
		execCmd.Stdin = cmd.Stdin
	}

	start := time.Now()
	if err := execCmd.Start(); err != nil {
		return nil, err
	}

	pid := execCmd.Process.Pid
	procHandle := &sandboxProcess{
		p:     p,
		cmd:   execCmd,
		pid:   pid,
		start: start,
		done:  make(chan struct{}),
	}
	p.register(procHandle)

	// Watch context cancellation while process runs.
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = procHandle.Kill()
		case <-watchDone:
		}
	}()

	waitErr := execCmd.Wait()
	close(watchDone)
	close(procHandle.done)
	p.unregister(pid)

	// Reap any leftover child processes in the process group.
	_ = syscall.Kill(-pid, syscall.SIGKILL)

	duration := time.Since(start)

	var resStdout, resStderr []byte
	if cmd.Stdout == nil {
		resStdout = stdoutBuf.Bytes()
	}
	if cmd.Stderr == nil {
		resStderr = stderrBuf.Bytes()
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return &ExecResult{
			Stdout:   resStdout,
			Stderr:   resStderr,
			ExitCode: exitCodeFromError(waitErr),
			Duration: duration,
		}, ctxErr
	}

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, waitErr
		}
	}

	return &ExecResult{
		Stdout:   resStdout,
		Stderr:   resStderr,
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

func (p *sandboxProc) Start(ctx context.Context, cmd Command) (Process, error) {
	if p.s.isClosed() {
		return nil, ErrSandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	execCmd, err := p.prepareExecCmd(cmd)
	if err != nil {
		return nil, err
	}

	stdinPipe, err := execCmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := execCmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, err
	}
	stderrPipe, err := execCmd.StderrPipe()
	if err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		return nil, err
	}

	start := time.Now()
	if err := execCmd.Start(); err != nil {
		_ = stdinPipe.Close()
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return nil, err
	}

	pid := execCmd.Process.Pid
	procHandle := &sandboxProcess{
		p:      p,
		cmd:    execCmd,
		pid:    pid,
		start:  start,
		stdin:  stdinPipe,
		stdout: stdoutPipe,
		stderr: stderrPipe,
		done:   make(chan struct{}),
	}
	p.register(procHandle)

	go func() {
		select {
		case <-ctx.Done():
			_ = procHandle.Kill()
		case <-procHandle.done:
		}
	}()

	return procHandle, nil
}

func (p *sandboxProc) register(proc *sandboxProcess) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.s.isClosed() {
		_ = syscall.Kill(-proc.pid, syscall.SIGKILL)
		return
	}
	p.active[proc.pid] = proc
}

func (p *sandboxProc) unregister(pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, pid)
}

func (p *sandboxProc) closeAll() error {
	p.mu.Lock()
	procs := make([]*sandboxProcess, 0, len(p.active))
	for _, proc := range p.active {
		procs = append(procs, proc)
	}
	p.mu.Unlock()

	var errs []error
	for _, proc := range procs {
		if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sandboxProcess implements the Process interface.
type sandboxProcess struct {
	p        *sandboxProc
	cmd      *exec.Cmd
	pid      int
	start    time.Time
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	done     chan struct{}
	waitOnce sync.Once
	res      *ExecResult
	waitErr  error
}

func (sp *sandboxProcess) PID() int {
	return sp.pid
}

func (sp *sandboxProcess) Stdin() io.WriteCloser {
	return sp.stdin
}

func (sp *sandboxProcess) Stdout() io.ReadCloser {
	return sp.stdout
}

func (sp *sandboxProcess) Stderr() io.ReadCloser {
	return sp.stderr
}

func (sp *sandboxProcess) Wait() (*ExecResult, error) {
	sp.waitOnce.Do(func() {
		waitErr := sp.cmd.Wait()
		close(sp.done)
		sp.p.unregister(sp.pid)
		_ = syscall.Kill(-sp.pid, syscall.SIGKILL)

		duration := time.Since(sp.start)
		exitCode := 0
		if waitErr != nil {
			if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
				exitCode = exitErr.ExitCode()
			} else {
				sp.waitErr = waitErr
				return
			}
		}
		sp.res = &ExecResult{
			ExitCode: exitCode,
			Duration: duration,
		}
	})
	return sp.res, sp.waitErr
}

func (sp *sandboxProcess) Signal(sig os.Signal) error {
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return sp.cmd.Process.Signal(sig)
	}
	err := syscall.Kill(-sp.pid, sysSig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func (sp *sandboxProcess) Kill() error {
	err := syscall.Kill(-sp.pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return -1
}
