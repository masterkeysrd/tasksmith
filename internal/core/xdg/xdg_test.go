package xdg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXDG(t *testing.T) {
	// Save and restore environment
	origHome := os.Getenv("HOME")
	origConfig := os.Getenv("XDG_CONFIG_HOME")
	origAppName := os.Getenv("TASKSMITH_APPNAME")
	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("XDG_CONFIG_HOME", origConfig)
		os.Setenv("TASKSMITH_APPNAME", origAppName)
		resetForTest()
	}()

	t.Run("DefaultAppName", func(t *testing.T) {
		os.Setenv("TASKSMITH_APPNAME", "")
		resetForTest()
		if AppName() != "tasksmith" {
			t.Errorf("expected appname tasksmith, got %s", AppName())
		}
	})

	t.Run("CustomAppName", func(t *testing.T) {
		os.Setenv("TASKSMITH_APPNAME", "my-app")
		resetForTest()
		if AppName() != "my-app" {
			t.Errorf("expected appname my-app, got %s", AppName())
		}
	})

	t.Run("DefaultDirs", func(t *testing.T) {
		os.Setenv("HOME", "/user/home")
		os.Setenv("XDG_CONFIG_HOME", "")
		resetForTest()

		path, err := SubConfigDir("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join("/user/home/.config", AppName(), "test")
		if path != expected {
			t.Errorf("expected %s, got %s", expected, path)
		}
	})

	t.Run("OverrideDirs", func(t *testing.T) {
		os.Setenv("XDG_CONFIG_HOME", "/custom/config")
		resetForTest()

		path, err := SubConfigDir("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join("/custom/config", AppName(), "test")
		if path != expected {
			t.Errorf("expected %s, got %s", expected, path)
		}
	})
}

func TestWorkspaceDir(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", "/user/home")
	resetForTest()

	t.Run("WorkspaceDir", func(t *testing.T) {
		path, err := WorkspaceDir("my-workspace")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(path, "workspaces") {
			t.Errorf("expected path to contain 'workspaces', got %s", path)
		}

		// Verify hashing is consistent
		path2, _ := WorkspaceDir("my-workspace")
		if path != path2 {
			t.Errorf("hashing inconsistent: %s vs %s", path, path2)
		}

		path3, _ := WorkspaceDir("other-workspace")
		if path == path3 {
			t.Errorf("different keys resulted in same path")
		}
	})

	t.Run("LogsDir", func(t *testing.T) {
		path, err := LogsDir()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(path, "logs") {
			t.Errorf("expected path to contain 'logs', got %s", path)
		}
	})

	t.Run("LogFilePath", func(t *testing.T) {
		filename := "app.log"
		path, err := LogFilePath(filename)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := filepath.Join(AppName(), "logs", filename)
		if !strings.Contains(path, expected) {
			t.Errorf("expected path to contain %s, got %s", expected, path)
		}
	})
}

func resetForTest() {
	varCache = make(map[VarType]string)
	loadAppName()
}
