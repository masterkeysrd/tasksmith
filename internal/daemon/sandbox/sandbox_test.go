package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigValidateAndFingerprint(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(tmpFile, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	var nilCfg *Config
	if err := nilCfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil config, got %v", err)
	}

	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing work_dir",
			cfg:  Config{Mode: ModeLocal},
		},
		{
			name: "relative work_dir",
			cfg:  Config{Mode: ModeLocal, WorkDir: "relative/path"},
		},
		{
			name: "non-existent work_dir",
			cfg:  Config{Mode: ModeLocal, WorkDir: filepath.Join(tmpDir, "does-not-exist")},
		},
		{
			name: "work_dir is a file",
			cfg:  Config{Mode: ModeLocal, WorkDir: tmpFile},
		},
		{
			name: "unsupported mode",
			cfg:  Config{Mode: Mode("docker"), WorkDir: tmpDir},
		},
		{
			name: "negative timeout",
			cfg:  Config{Mode: ModeLocal, WorkDir: tmpDir, DefaultTimeout: -1 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}

	cfgA := Config{
		Mode:    ModeLocal,
		WorkDir: tmpDir,
		Env:     map[string]string{"B": "2", "A": "1"},
		Network: NetworkPolicy{
			AllowedHosts: []string{"*.example.com", "api.openai.com"},
			BlockedHosts: []string{"evil.com", "bad.org"},
		},
	}
	cfgB := Config{
		WorkDir: tmpDir, // Mode defaults to ModeLocal
		Env:     map[string]string{"A": "1", "B": "2"},
		Network: NetworkPolicy{
			AllowedHosts: []string{"api.openai.com", "*.example.com"},
			BlockedHosts: []string{"bad.org", "evil.com"},
		},
	}

	if err := cfgA.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
	if cfgA.Fingerprint() != cfgB.Fingerprint() {
		t.Fatalf("expected identical fingerprints, got %s vs %s", cfgA.Fingerprint(), cfgB.Fingerprint())
	}
}

func TestFSCapabilityAndConfinement(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()

	sbx, err := New(Config{
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("New sandbox: %v", err)
	}
	defer sbx.Close()

	fsys := sbx.FS()

	// WriteFile & ReadFile
	if err := fsys.MkdirAll(ctx, "sub/nested", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := fsys.WriteFile(ctx, "sub/nested/hello.txt", []byte("world"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	data, err := fsys.ReadFile(ctx, "sub/nested/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "world" {
		t.Fatalf("expected %q, got %q", "world", string(data))
	}

	// Standard io/fs.FS compatibility (fs.ReadFile)
	stdData, err := fs.ReadFile(fsys, "sub/nested/hello.txt")
	if err != nil {
		t.Fatalf("fs.ReadFile: %v", err)
	}
	if string(stdData) != "world" {
		t.Fatalf("expected %q, got %q", "world", string(stdData))
	}

	// ReadDir on ".", "", and subdirectory
	for _, rootPath := range []string{".", ""} {
		entries, err := fsys.ReadDir(ctx, rootPath)
		if err != nil {
			t.Fatalf("ReadDir(%q): %v", rootPath, err)
		}
		if len(entries) != 1 || entries[0].Name() != "sub" {
			t.Fatalf("unexpected ReadDir(%q) entries: %+v", rootPath, entries)
		}
	}

	// Stat, Lstat, Chmod, Rename, Remove, RemoveAll
	info, err := fsys.Stat(ctx, "sub/nested/hello.txt")
	if err != nil || info.Size() != 5 {
		t.Fatalf("Stat failed: info=%+v, err=%v", info, err)
	}
	if _, err := fsys.Lstat(ctx, "sub/nested/hello.txt"); err != nil {
		t.Fatalf("Lstat failed: %v", err)
	}
	if err := fsys.Chmod(ctx, "sub/nested/hello.txt", 0o600); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	if err := fsys.Rename(ctx, "sub/nested/hello.txt", "sub/nested/renamed.txt"); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if err := fsys.Remove(ctx, "sub/nested/renamed.txt"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if err := fsys.RemoveAll(ctx, "sub"); err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}

	// Path escape checks: relative traversal, absolute outside path, and symlink escape
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	escapePaths := []string{
		"../secret.txt",
		"sub/../../secret.txt",
		outsideFile,
		"/etc/passwd",
	}
	for _, p := range escapePaths {
		if _, err := fsys.ResolvePath(p); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("ResolvePath(%q): expected ErrPathEscape, got %v", p, err)
		}
	}

	// Outside files can be accessed directly via fsys when outside workDir
	got, err := fsys.ReadFile(ctx, outsideFile)
	if err != nil {
		t.Fatalf("ReadFile(outsideFile): expected success, got %v", err)
	}
	if string(got) != "secret" {
		t.Fatalf("ReadFile(outsideFile): expected 'secret', got %q", string(got))
	}
	if err := fsys.WriteFile(ctx, outsideFile, []byte("updated"), 0o644); err != nil {
		t.Fatalf("WriteFile(outsideFile): expected success, got %v", err)
	}
	if gotUpdated, err := fsys.ReadFile(ctx, outsideFile); err != nil || string(gotUpdated) != "updated" {
		t.Fatalf("ReadFile(outsideFile) after write: got %q (err=%v)", string(gotUpdated), err)
	}

	// Symlink escape inside workDir pointing outside workDir
	symlinkPath := filepath.Join(workDir, "evil_link")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := fsys.ReadFile(ctx, "evil_link"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape on symlink escape, got %v", err)
	}

	// ResolvePath tests: relative, empty/dot, absolute inside WorkDir & realWorkDir, and escaping paths
	for _, rootIn := range []string{"", "."} {
		got, err := fsys.ResolvePath(rootIn)
		if err != nil || got != "." {
			t.Fatalf("ResolvePath(%q): expected \".\", got %q (err=%v)", rootIn, got, err)
		}
	}

	relInputs := []string{
		"./pkg/a.go",
		"pkg/sub/../a.go",
		filepath.Join(workDir, "pkg", "a.go"),
	}
	if realWorkDir, err := filepath.EvalSymlinks(workDir); err == nil && realWorkDir != workDir {
		relInputs = append(relInputs, filepath.Join(realWorkDir, "pkg", "a.go"))
	}
	for _, in := range relInputs {
		got, err := fsys.ResolvePath(in)
		if err != nil || got != "pkg/a.go" {
			t.Fatalf("ResolvePath(%q): expected \"pkg/a.go\", got %q (err=%v)", in, got, err)
		}
	}

	for _, esc := range []string{"../outside", "pkg/../../outside", "/etc/passwd", outsideFile} {
		if _, err := fsys.ResolvePath(esc); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("ResolvePath(%q): expected ErrPathEscape, got %v", esc, err)
		}
	}
}

func TestFSReadOnlyEnforcement(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "existing.txt"), []byte("readonly-data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	sbx, err := New(Config{
		WorkDir:  workDir,
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("New read-only sandbox: %v", err)
	}
	defer sbx.Close()

	fsys := sbx.FS()

	// Reads succeed
	data, err := fsys.ReadFile(ctx, "existing.txt")
	if err != nil || string(data) != "readonly-data" {
		t.Fatalf("ReadFile failed on read-only sandbox: %v", err)
	}

	// All mutating operations fail with ErrReadOnly
	if err := fsys.WriteFile(ctx, "new.txt", []byte("x"), 0o644); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on WriteFile, got %v", err)
	}
	if _, err := fsys.OpenFile(ctx, "existing.txt", os.O_WRONLY, 0o644); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on OpenFile(O_WRONLY), got %v", err)
	}
	if err := fsys.MkdirAll(ctx, "dir", 0o755); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on MkdirAll, got %v", err)
	}
	if err := fsys.Remove(ctx, "existing.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on Remove, got %v", err)
	}
	if err := fsys.RemoveAll(ctx, "existing.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on RemoveAll, got %v", err)
	}
	if err := fsys.Rename(ctx, "existing.txt", "renamed.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on Rename, got %v", err)
	}
	if err := fsys.Chmod(ctx, "existing.txt", 0o600); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on Chmod, got %v", err)
	}

	// Opening read-only succeeds, but calling Write on the returned File returns ErrReadOnly
	f, err := fsys.OpenFile(ctx, "existing.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile(O_RDONLY) failed: %v", err)
	}
	defer f.Close()
	if _, err := f.Write([]byte("mutate")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly when writing to read-only File handle, got %v", err)
	}
}

func TestUploadAndDownload(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	hostSrcDir := t.TempDir()
	hostDstDir := t.TempDir()

	// Populate hostSrcDir with a file and a nested directory tree
	singleHostFile := filepath.Join(hostSrcDir, "single.txt")
	if err := os.WriteFile(singleHostFile, []byte("single-file-payload"), 0o640); err != nil {
		t.Fatalf("write singleHostFile: %v", err)
	}
	treeRoot := filepath.Join(hostSrcDir, "bundle")
	if err := os.MkdirAll(filepath.Join(treeRoot, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(treeRoot, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(treeRoot, "nested", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	sbx, err := New(Config{WorkDir: workDir})
	if err != nil {
		t.Fatalf("New sandbox: %v", err)
	}
	defer sbx.Close()

	// 1. Upload single file into sandbox
	if err := sbx.Upload(ctx, singleHostFile, "uploaded/single.txt"); err != nil {
		t.Fatalf("Upload single file: %v", err)
	}
	gotSingle, err := sbx.FS().ReadFile(ctx, "uploaded/single.txt")
	if err != nil || string(gotSingle) != "single-file-payload" {
		t.Fatalf("ReadFile uploaded single: got %q, err %v", string(gotSingle), err)
	}

	// 2. Upload directory tree into sandbox
	if err := sbx.Upload(ctx, treeRoot, "imported_bundle"); err != nil {
		t.Fatalf("Upload directory tree: %v", err)
	}
	gotBeta, err := sbx.FS().ReadFile(ctx, "imported_bundle/nested/b.txt")
	if err != nil || string(gotBeta) != "beta" {
		t.Fatalf("ReadFile uploaded tree file: got %q, err %v", string(gotBeta), err)
	}

	// 3. Download single file to host
	downloadedSingle := filepath.Join(hostDstDir, "out_single.txt")
	if err := sbx.Download(ctx, "uploaded/single.txt", downloadedSingle); err != nil {
		t.Fatalf("Download single file: %v", err)
	}
	hostSingleBytes, err := os.ReadFile(downloadedSingle)
	if err != nil || string(hostSingleBytes) != "single-file-payload" {
		t.Fatalf("verify downloaded single file: got %q, err %v", string(hostSingleBytes), err)
	}

	// 4. Download directory tree to host
	downloadedTree := filepath.Join(hostDstDir, "out_bundle")
	if err := sbx.Download(ctx, "imported_bundle", downloadedTree); err != nil {
		t.Fatalf("Download directory tree: %v", err)
	}
	hostBetaBytes, err := os.ReadFile(filepath.Join(downloadedTree, "nested", "b.txt"))
	if err != nil || string(hostBetaBytes) != "beta" {
		t.Fatalf("verify downloaded tree file: got %q, err %v", string(hostBetaBytes), err)
	}

	// 5. Reject path escapes on Upload and Download
	if err := sbx.Upload(ctx, singleHostFile, "../escape.txt"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape on Upload escape, got %v", err)
	}
	if err := sbx.Download(ctx, "../escape.txt", filepath.Join(hostDstDir, "nope.txt")); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape on Download escape, got %v", err)
	}

	// 6. ReadOnly sandbox blocks Upload with ErrReadOnly, but allows Download
	roSbx, err := New(Config{
		WorkDir:  workDir,
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("New RO sandbox: %v", err)
	}
	defer roSbx.Close()

	if err := roSbx.Upload(ctx, singleHostFile, "blocked.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly on Upload to read-only sandbox, got %v", err)
	}
	roDownloadTarget := filepath.Join(hostDstDir, "ro_downloaded.txt")
	if err := roSbx.Download(ctx, "uploaded/single.txt", roDownloadTarget); err != nil {
		t.Fatalf("expected Download to succeed on read-only sandbox, got %v", err)
	}
}

func TestProcExecEnvIsolationAndConfinement(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()

	// Set a sensitive host daemon environment variable and verify it is NOT leaked.
	t.Setenv("TASKSMITH_HOST_SECRET_KEY", "super-secret-host-value")

	sbx, err := New(Config{
		WorkDir: workDir,
		Env: map[string]string{
			"SANDBOX_BASE_VAR": "from_config",
			"OVERRIDE_ME":      "initial",
		},
	})
	if err != nil {
		t.Fatalf("New sandbox: %v", err)
	}
	defer sbx.Close()

	res, err := sbx.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", `printf "HOST=%s|BASE=%s|OVER=%s|HOME=%s" "$TASKSMITH_HOST_SECRET_KEY" "$SANDBOX_BASE_VAR" "$OVERRIDE_ME" "$HOME"`},
		Env: map[string]string{
			"OVERRIDE_ME": "from_cmd",
		},
	})
	if err != nil {
		t.Fatalf("Proc.Exec failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", res.ExitCode, string(res.Stderr))
	}
	expectedPrefix := "HOST=|BASE=from_config|OVER=from_cmd|HOME="
	if !strings.HasPrefix(string(res.Stdout), expectedPrefix) {
		t.Fatalf("env isolation mismatch: expected prefix %q, got %q", expectedPrefix, string(res.Stdout))
	}
	procHome := strings.TrimPrefix(string(res.Stdout), expectedPrefix)
	if procHome == workDir || procHome == "" {
		t.Fatalf("expected HOME to not be workDir (%q), got %q", workDir, procHome)
	}

	// Verify non-zero exit code is captured cleanly without returning a Go error
	resNonZero, err := sbx.Proc().Exec(ctx, Command{
		Name:  "sh",
		Args:  []string{"-c", "cat >&2; exit 42"},
		Stdin: strings.NewReader("error-input"),
	})
	if err != nil {
		t.Fatalf("expected nil Go error on non-zero exit code, got %v", err)
	}
	if resNonZero.ExitCode != 42 || string(resNonZero.Stderr) != "error-input" {
		t.Fatalf("unexpected non-zero result: code=%d stderr=%q", resNonZero.ExitCode, string(resNonZero.Stderr))
	}

	// Verify Command.Dir confinement
	if _, err := sbx.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", "pwd"},
		Dir:  "../outside",
	}); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape for escaping Command.Dir, got %v", err)
	}

	// Verify timeout kills process group
	_, err = sbx.Proc().Exec(ctx, Command{
		Name:    "sh",
		Args:    []string{"-c", "sleep 10"},
		Timeout: 100 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestProcStartStreamingAndCloseCleanup(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()

	sbx, err := New(Config{WorkDir: workDir})
	if err != nil {
		t.Fatalf("New sandbox: %v", err)
	}

	// Start interactive streaming process
	proc, err := sbx.Proc().Start(ctx, Command{
		Name: "sh",
		Args: []string{"-c", "read line; echo \"ack:$line\"; sleep 30"},
	})
	if err != nil {
		t.Fatalf("Proc.Start: %v", err)
	}
	if proc.PID() <= 0 {
		t.Fatalf("expected positive PID, got %d", proc.PID())
	}

	if _, err := io.WriteString(proc.Stdin(), "ping\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	buf := make([]byte, 64)
	n, err := proc.Stdout().Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read stdout: %v", err)
	}
	if !bytes.Contains(buf[:n], []byte("ack:ping")) {
		t.Fatalf("expected stdout to contain ack:ping, got %q", string(buf[:n]))
	}

	pid := proc.PID()

	// Closing the sandbox must kill the active process group
	if err := sbx.Close(); err != nil {
		t.Fatalf("sbx.Close(): %v", err)
	}

	_, _ = proc.Wait()

	// Verify process is no longer alive
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("expected process %d to be terminated after Sandbox.Close(), got err=%v", pid, err)
	}

	// Subsequent operations on closed sandbox return ErrSandboxClosed
	if _, err := sbx.FS().ReadFile(ctx, "any.txt"); !errors.Is(err, ErrSandboxClosed) {
		t.Fatalf("expected ErrSandboxClosed on FS, got %v", err)
	}
	if _, err := sbx.Proc().Exec(ctx, Command{Name: "sh", Args: []string{"-c", "true"}}); !errors.Is(err, ErrSandboxClosed) {
		t.Fatalf("expected ErrSandboxClosed on Proc, got %v", err)
	}
	if _, err := sbx.Net().DialContext(ctx, "tcp", "example.com:80"); !errors.Is(err, ErrSandboxClosed) {
		t.Fatalf("expected ErrSandboxClosed on Net, got %v", err)
	}
}

func TestModeNativeExecution(t *testing.T) {
	if err := checkNativeAvailable(); err != nil {
		t.Skipf("skipping live ModeNative test: %v", err)
	}

	ctx := context.Background()
	workDir := t.TempDir()
	outsideDir := t.TempDir()

	sbxRW, err := New(Config{
		Mode:    ModeNative,
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("New ModeNative RW sandbox: %v", err)
	}
	defer sbxRW.Close()

	// Writing inside WorkDir must succeed
	resInside, err := sbxRW.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", "echo allowed > inside.txt"},
	})
	if err != nil || resInside.ExitCode != 0 {
		t.Fatalf("ModeNative write inside WorkDir failed: err=%v exit=%d stderr=%s", err, resInside.ExitCode, string(resInside.Stderr))
	}

	// Verify child process can write to $HOME, which resolves inside tempDir (not workDir or host HOME)
	resHome, err := sbxRW.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", `echo "$HOME" > home_path.txt && echo "home-ok" > "$HOME/test_home.txt"`},
	})
	if err != nil || resHome.ExitCode != 0 {
		t.Fatalf("ModeNative write inside $HOME failed: err=%v exit=%d stderr=%s", err, resHome.ExitCode, string(resHome.Stderr))
	}
	homePathBytes, err := sbxRW.FS().ReadFile(ctx, "home_path.txt")
	if err != nil {
		t.Fatalf("ReadFile home_path.txt: %v", err)
	}
	sbxHome := strings.TrimSpace(string(homePathBytes))
	hostHome, _ := os.UserHomeDir()
	if sbxHome == "" || sbxHome == workDir || (hostHome != "" && sbxHome == hostHome) {
		t.Fatalf("expected ModeNative $HOME to be isolated in tempDir, got %q (workDir=%q, hostHome=%q)", sbxHome, workDir, hostHome)
	}

	// Writing outside WorkDir must be blocked by the kernel sandbox
	outsideTarget := filepath.Join(outsideDir, "escaped.txt")
	resOutside, err := sbxRW.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", "echo forbidden > " + outsideTarget},
	})
	if err != nil {
		t.Fatalf("ModeNative Exec error: %v", err)
	}
	if resOutside.ExitCode == 0 {
		t.Fatalf("expected ModeNative kernel sandbox to block write outside WorkDir (%s)", outsideTarget)
	}
	if _, statErr := os.Stat(outsideTarget); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s not to exist, got statErr=%v", outsideTarget, statErr)
	}

	// ModeNative with ReadOnly: true must block child processes from writing inside WorkDir
	sbxRO, err := New(Config{
		Mode:     ModeNative,
		WorkDir:  workDir,
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("New ModeNative RO sandbox: %v", err)
	}
	defer sbxRO.Close()

	resRO, err := sbxRO.Proc().Exec(ctx, Command{
		Name: "sh",
		Args: []string{"-c", "echo should_fail > ro_attempt.txt"},
	})
	if err != nil {
		t.Fatalf("ModeNative RO Exec error: %v", err)
	}
	if resRO.ExitCode == 0 {
		t.Fatalf("expected ModeNative ReadOnly sandbox to block file write inside WorkDir")
	}
}

func TestNetPolicyEnforcement(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()

	// 1. Network Disabled
	sbxDisabled, err := New(Config{
		WorkDir: workDir,
		Network: NetworkPolicy{Disabled: true},
	})
	if err != nil {
		t.Fatalf("New disabled-net sandbox: %v", err)
	}
	defer sbxDisabled.Close()

	if _, err := sbxDisabled.Net().DialContext(ctx, "tcp", "example.com:80"); !errors.Is(err, ErrNetworkDisabled) {
		t.Fatalf("expected ErrNetworkDisabled, got %v", err)
	}

	// 2. AllowLoopback false by default
	sbxNoLoop, err := New(Config{
		WorkDir: workDir,
		Network: NetworkPolicy{AllowLoopback: false},
	})
	if err != nil {
		t.Fatalf("New no-loopback sandbox: %v", err)
	}
	defer sbxNoLoop.Close()

	for _, addr := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if _, err := sbxNoLoop.Net().DialContext(ctx, "tcp", addr); !errors.Is(err, ErrLoopbackBlocked) {
			t.Fatalf("DialContext(%q): expected ErrLoopbackBlocked, got %v", addr, err)
		}
	}

	// 3. AllowedHosts and BlockedHosts (including wildcard matching) + AllowLoopback HTTPClient test
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sandbox-net-ok"))
	}))
	defer srv.Close()

	parsedURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse httptest url: %v", err)
	}
	srvHost := parsedURL.Hostname()

	sbxPolicy, err := New(Config{
		WorkDir: workDir,
		Network: NetworkPolicy{
			AllowedHosts:  []string{srvHost, "*.trusted.example.com"},
			BlockedHosts:  []string{"blocked.trusted.example.com", "*.evil.org"},
			AllowLoopback: true,
		},
	})
	if err != nil {
		t.Fatalf("New policy sandbox: %v", err)
	}
	defer sbxPolicy.Close()

	// Blocked exact host inside allowed wildcard must be rejected (BlockedHosts takes precedence)
	if _, err := sbxPolicy.Net().DialContext(ctx, "tcp", "blocked.trusted.example.com:443"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("expected ErrHostBlocked for blocked subdomain, got %v", err)
	}
	// Blocked wildcard host
	if _, err := sbxPolicy.Net().DialContext(ctx, "tcp", "sub.evil.org:443"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("expected ErrHostBlocked for *.evil.org, got %v", err)
	}
	// Unlisted host when AllowedHosts is non-empty
	if _, err := sbxPolicy.Net().DialContext(ctx, "tcp", "unlisted.example.org:443"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("expected ErrHostBlocked for unlisted host, got %v", err)
	}

	// Allowed loopback httptest server works via HTTPClient()
	resp, err := sbxPolicy.Net().HTTPClient().Get(srv.URL)
	if err != nil {
		t.Fatalf("HTTPClient.Get(%q) failed: %v", srv.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "sandbox-net-ok" {
		t.Fatalf("unexpected response body %q (err=%v)", string(body), err)
	}

	// 4. ValidateURL checks
	if _, err := sbxDisabled.Net().ValidateURL("https://example.com/path"); !errors.Is(err, ErrNetworkDisabled) {
		t.Fatalf("expected ErrNetworkDisabled from ValidateURL, got %v", err)
	}

	invalidURLs := []string{
		"",
		"   ",
		"://bad-url",
		"ftp://example.com/file",
		"file:///etc/passwd",
		"http:///no-host",
	}
	for _, raw := range invalidURLs {
		if _, err := sbxNoLoop.Net().ValidateURL(raw); !errors.Is(err, ErrInvalidURL) {
			t.Fatalf("ValidateURL(%q): expected ErrInvalidURL, got %v", raw, err)
		}
	}

	for _, loopURL := range []string{
		"http://127.0.0.1:8080/test",
		"http://[::1]:8080/test",
		"https://localhost/api",
		"https://sub.localhost/api",
		"http://0.0.0.0:8080/",
	} {
		if _, err := sbxNoLoop.Net().ValidateURL(loopURL); !errors.Is(err, ErrLoopbackBlocked) {
			t.Fatalf("ValidateURL(%q): expected ErrLoopbackBlocked, got %v", loopURL, err)
		}
	}

	if _, err := sbxPolicy.Net().ValidateURL("https://blocked.trusted.example.com/v1"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("expected ErrHostBlocked for blocked subdomain URL, got %v", err)
	}
	if _, err := sbxPolicy.Net().ValidateURL("https://sub.evil.org/v1"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("expected ErrHostBlocked for blocked wildcard URL, got %v", err)
	}
	if _, err := sbxPolicy.Net().ValidateURL("https://unlisted.example.org/v1"); !errors.Is(err, ErrHostBlocked) {
		t.Fatalf("expected ErrHostBlocked for unlisted host URL, got %v", err)
	}

	u, err := sbxPolicy.Net().ValidateURL("https://api.trusted.example.com/v1/chat?x=1")
	if err != nil || u.Hostname() != "api.trusted.example.com" {
		t.Fatalf("expected valid URL for allowed wildcard host, got u=%v err=%v", u, err)
	}

	if _, err := sbxPolicy.Net().ValidateURL(srv.URL + "/health"); err != nil {
		t.Fatalf("expected valid URL for loopback server when AllowLoopback=true, got %v", err)
	}

	// Closed sandbox returns ErrSandboxClosed on ValidateURL
	_ = sbxPolicy.Close()
	if _, err := sbxPolicy.Net().ValidateURL("https://api.trusted.example.com"); !errors.Is(err, ErrSandboxClosed) {
		t.Fatalf("expected ErrSandboxClosed on closed sandbox ValidateURL, got %v", err)
	}
}
