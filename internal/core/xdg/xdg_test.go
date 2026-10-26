package xdg

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func saveAndRestoreEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
		"XDG_CACHE_HOME",
		"XDG_RUNTIME_DIR",
		"TASKSMITH_APPNAME",
	}
	orig := make(map[string]string, len(keys))
	for _, k := range keys {
		orig[k] = os.Getenv(k)
	}
	t.Cleanup(func() {
		for _, k := range keys {
			_ = os.Setenv(k, orig[k])
		}
		ClearCache()
	})
}

func TestAppName(t *testing.T) {
	saveAndRestoreEnv(t)

	t.Run("DefaultAppName", func(t *testing.T) {
		_ = os.Setenv("TASKSMITH_APPNAME", "")
		ClearCache()
		if got := AppName(); got != "tasksmith" {
			t.Errorf("expected default appname %q, got %q", "tasksmith", got)
		}
	})

	t.Run("CustomAppName", func(t *testing.T) {
		_ = os.Setenv("TASKSMITH_APPNAME", "tasksmith-dev")
		ClearCache()
		if got := AppName(); got != "tasksmith-dev" {
			t.Errorf("expected custom appname %q, got %q", "tasksmith-dev", got)
		}
	})

	t.Run("RejectInvalidAppName", func(t *testing.T) {
		invalidNames := []string{
			"../evil",
			"foo/../../evil",
			"..",
			".",
			"/abs/path",
			`\abs\windows`,
		}
		for _, invalid := range invalidNames {
			_ = os.Setenv("TASKSMITH_APPNAME", invalid)
			ClearCache()
			if got := AppName(); got != "tasksmith" {
				t.Errorf("for TASKSMITH_APPNAME=%q: expected fallback %q, got %q", invalid, "tasksmith", got)
			}
		}
	})
}

func TestDirectories(t *testing.T) {
	saveAndRestoreEnv(t)

	t.Run("DefaultDirs", func(t *testing.T) {
		_ = os.Setenv("HOME", "/user/home")
		_ = os.Setenv("XDG_CONFIG_HOME", "")
		_ = os.Setenv("XDG_DATA_HOME", "")
		_ = os.Setenv("XDG_STATE_HOME", "")
		_ = os.Setenv("XDG_CACHE_HOME", "")
		_ = os.Setenv("XDG_RUNTIME_DIR", "")
		_ = os.Setenv("TASKSMITH_APPNAME", "")
		ClearCache()

		cfg, err := SubConfigDir("settings")
		if err != nil {
			t.Fatalf("SubConfigDir error: %v", err)
		}
		if want := filepath.Join("/user/home/.config/tasksmith/settings"); cfg != want {
			t.Errorf("SubConfigDir: want %q, got %q", want, cfg)
		}

		data, err := SubDataDir("db")
		if err != nil {
			t.Fatalf("SubDataDir error: %v", err)
		}
		if want := filepath.Join("/user/home/.local/share/tasksmith/db"); data != want {
			t.Errorf("SubDataDir: want %q, got %q", want, data)
		}

		state, err := SubStateDir("sessions")
		if err != nil {
			t.Fatalf("SubStateDir error: %v", err)
		}
		if want := filepath.Join("/user/home/.local/state/tasksmith/sessions"); state != want {
			t.Errorf("SubStateDir: want %q, got %q", want, state)
		}

		cache, err := SubCacheDir("http")
		if err != nil {
			t.Fatalf("SubCacheDir error: %v", err)
		}
		if want := filepath.Join("/user/home/.cache/tasksmith/http"); cache != want {
			t.Errorf("SubCacheDir: want %q, got %q", want, cache)
		}

		// When XDG_RUNTIME_DIR is unset, Home(VarTypeRuntime) falls back to <StateHome>/run
		rt, err := SubRuntimeDir("brain.sock")
		if err != nil {
			t.Fatalf("SubRuntimeDir error: %v", err)
		}
		if want := filepath.Join("/user/home/.local/state/tasksmith/run/brain.sock"); rt != want {
			t.Errorf("SubRuntimeDir (unset fallback): want %q, got %q", want, rt)
		}
	})

	t.Run("OverrideDirs", func(t *testing.T) {
		_ = os.Setenv("XDG_CONFIG_HOME", "/custom/config")
		_ = os.Setenv("XDG_DATA_HOME", "/custom/data")
		_ = os.Setenv("XDG_STATE_HOME", "/custom/state")
		_ = os.Setenv("XDG_CACHE_HOME", "/custom/cache")
		_ = os.Setenv("XDG_RUNTIME_DIR", "/custom/run/user/1000")
		_ = os.Setenv("TASKSMITH_APPNAME", "custom-app")
		ClearCache()

		cfg, err := SubConfigDir("init.toml")
		if err != nil {
			t.Fatalf("SubConfigDir error: %v", err)
		}
		if want := filepath.Join("/custom/config/custom-app/init.toml"); cfg != want {
			t.Errorf("SubConfigDir: want %q, got %q", want, cfg)
		}

		data, err := SubDataDir("store")
		if err != nil {
			t.Fatalf("SubDataDir error: %v", err)
		}
		if want := filepath.Join("/custom/data/custom-app/store"); data != want {
			t.Errorf("SubDataDir: want %q, got %q", want, data)
		}

		state, err := SubStateDir("history")
		if err != nil {
			t.Fatalf("SubStateDir error: %v", err)
		}
		if want := filepath.Join("/custom/state/custom-app/history"); state != want {
			t.Errorf("SubStateDir: want %q, got %q", want, state)
		}

		cache, err := SubCacheDir("blobs")
		if err != nil {
			t.Fatalf("SubCacheDir error: %v", err)
		}
		if want := filepath.Join("/custom/cache/custom-app/blobs"); cache != want {
			t.Errorf("SubCacheDir: want %q, got %q", want, cache)
		}

		rt, err := SubRuntimeDir("brain.sock")
		if err != nil {
			t.Fatalf("SubRuntimeDir error: %v", err)
		}
		if want := filepath.Join("/custom/run/user/1000/custom-app/brain.sock"); rt != want {
			t.Errorf("SubRuntimeDir (set): want %q, got %q", want, rt)
		}
	})
}

func TestHelpers(t *testing.T) {
	saveAndRestoreEnv(t)

	tmpDir := t.TempDir()
	SetTestEnv(tmpDir, "tasksmith-test")

	t.Run("WorkspaceDir", func(t *testing.T) {
		path1, err := WorkspaceDir("/Users/dev/project-a")
		if err != nil {
			t.Fatalf("WorkspaceDir error: %v", err)
		}
		if !strings.Contains(path1, filepath.Join("tasksmith-test", "workspaces")) {
			t.Errorf("expected path to contain tasksmith-test/workspaces, got %q", path1)
		}
		base := filepath.Base(path1)
		if len(base) != 16 {
			t.Errorf("expected 16-char hex prefix, got %q (len %d)", base, len(base))
		}

		// Deterministic
		path2, err := WorkspaceDir("/Users/dev/project-a")
		if err != nil {
			t.Fatalf("WorkspaceDir error: %v", err)
		}
		if path1 != path2 {
			t.Errorf("expected deterministic workspace dir: %q != %q", path1, path2)
		}

		// Distinct keys yield distinct dirs
		path3, err := WorkspaceDir("/Users/dev/project-b")
		if err != nil {
			t.Fatalf("WorkspaceDir error: %v", err)
		}
		if path1 == path3 {
			t.Errorf("expected distinct workspace dirs for different keys, both got %q", path1)
		}
	})

	t.Run("LogsDirAndLogFilePath", func(t *testing.T) {
		logsDir, err := LogsDir()
		if err != nil {
			t.Fatalf("LogsDir error: %v", err)
		}
		wantLogsDir := filepath.Join(tmpDir, "state", "tasksmith-test", "logs")
		if logsDir != wantLogsDir {
			t.Errorf("LogsDir: want %q, got %q", wantLogsDir, logsDir)
		}

		logFile, err := LogFilePath("brain.log")
		if err != nil {
			t.Fatalf("LogFilePath error: %v", err)
		}
		wantLogFile := filepath.Join(wantLogsDir, "brain.log")
		if logFile != wantLogFile {
			t.Errorf("LogFilePath: want %q, got %q", wantLogFile, logFile)
		}
	})

	t.Run("SocketPath", func(t *testing.T) {
		sock, err := SocketPath("brain.sock")
		if err != nil {
			t.Fatalf("SocketPath error: %v", err)
		}
		wantSock := filepath.Join(tmpDir, "runtime", "tasksmith-test", "brain.sock")
		if sock != wantSock {
			t.Errorf("SocketPath: want %q, got %q", wantSock, sock)
		}
	})

	t.Run("HistoryDirAndHistoryFilePath", func(t *testing.T) {
		histDir, err := HistoryDir()
		if err != nil {
			t.Fatalf("HistoryDir error: %v", err)
		}
		wantHistDir := filepath.Join(tmpDir, "state", "tasksmith-test", "history")
		if histDir != wantHistDir {
			t.Errorf("HistoryDir: want %q, got %q", wantHistDir, histDir)
		}

		histFile, err := HistoryFilePath("repl.jsonl")
		if err != nil {
			t.Fatalf("HistoryFilePath error: %v", err)
		}
		wantHistFile := filepath.Join(wantHistDir, "repl.jsonl")
		if histFile != wantHistFile {
			t.Errorf("HistoryFilePath: want %q, got %q", wantHistFile, histFile)
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	saveAndRestoreEnv(t)
	tmpDir := t.TempDir()
	SetTestEnv(tmpDir, "tasksmith-race")

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 50 {
				if j%15 == 0 {
					ClearCache()
				}
				_ = AppName()
				_, _ = SubConfigDir("cfg")
				_, _ = SubDataDir("data")
				_, _ = SubStateDir("state")
				_, _ = SubCacheDir("cache")
				_, _ = SubRuntimeDir("run")
				_, _ = WorkspaceDir("ws")
				_, _ = LogFilePath("app.log")
				_, _ = SocketPath("brain.sock")
				_, _ = HistoryFilePath("repl.jsonl")
			}
		}(i)
	}
	wg.Wait()
}
