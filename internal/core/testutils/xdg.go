package testutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func xdgEnv(baseDir, appName string) map[string]string {
	return map[string]string{
		"HOME":              baseDir,
		"XDG_CONFIG_HOME":   filepath.Join(baseDir, "config"),
		"XDG_DATA_HOME":     filepath.Join(baseDir, "data"),
		"XDG_STATE_HOME":    filepath.Join(baseDir, "state"),
		"XDG_CACHE_HOME":    filepath.Join(baseDir, "cache"),
		"XDG_RUNTIME_DIR":   filepath.Join(baseDir, "runtime"),
		"TASKSMITH_APPNAME": appName,
	}
}

func SetXDGEnv(baseDir, appName string) {
	for key, value := range xdgEnv(baseDir, appName) {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	xdg.ClearCache()
}

func SetupXDG(tb testing.TB, appName string) string {
	tb.Helper()
	baseDir := tb.TempDir()
	tb.Cleanup(xdg.ClearCache)
	for key, value := range xdgEnv(baseDir, appName) {
		tb.Setenv(key, value)
	}
	xdg.ClearCache()
	return baseDir
}

func RunWithXDG(m *testing.M, appName string) int {
	baseDir, err := os.MkdirTemp("", "xdg-*")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := os.RemoveAll(baseDir); err != nil {
			panic(err)
		}
	}()
	SetXDGEnv(baseDir, appName)
	return m.Run()
}
