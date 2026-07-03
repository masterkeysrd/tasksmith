package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

// VarType represents the type of XDG directory.
type VarType int

// appname is the name of the application, used to create subdirectories in the XDG directories.
var appname string

const (
	// VarTypeConfig represents the XDG_CONFIG_HOME directory.
	VarTypeConfig VarType = iota
	// VarTypeData represents the XDG_DATA_HOME directory.
	VarTypeData
	// VarTypeCache represents the XDG_CACHE_HOME directory.
	VarTypeCache
)

var envVars = map[VarType]string{
	VarTypeConfig: "XDG_CONFIG_HOME",
	VarTypeData:   "XDG_DATA_HOME",
	VarTypeCache:  "XDG_CACHE_HOME",
}

var defaultDirs = map[VarType]string{
	VarTypeConfig: "$HOME/.config",
	VarTypeData:   "$HOME/.local/share",
	VarTypeCache:  "$HOME/.cache",
}

func init() {
	loadAppName()
}

func loadAppName() {
	name := os.Getenv("TASKSMITH_APPNAME")
	if name == "" {
		name = "tasksmith"
	}
	appname = name
}

// Home returns the application's base directory for the given VarType.
// For example, if VarType is VarTypeConfig, it might return "$HOME/.config/tasksmith".
func Home(key VarType) (string, error) {
	dir := GetVar(key)
	if dir == "" {
		return "", os.ErrNotExist
	}

	name := AppName()
	return filepath.Join(dir, name), nil
}

// SubDataDir returns a subdirectory within the application's data directory.
func SubDataDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeData, subdirs...)
}

// SubConfigDir returns a subdirectory within the application's configuration directory.
func SubConfigDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeConfig, subdirs...)
}

// SubCacheDir returns a subdirectory within the application's cache directory.
func SubCacheDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeCache, subdirs...)
}

// Subdir returns a subdirectory within the application's base directory for the given VarType.
func Subdir(key VarType, subdirs ...string) (string, error) {
	base, err := Home(key)
	if err != nil {
		return "", err
	}

	path := base
	for _, subdir := range subdirs {
		path = filepath.Join(path, subdir)
	}

	return path, nil
}

// AppName returns the name of the application.
func AppName() string {
	return appname
}

var varCache = make(map[VarType]string)

// GetVar returns the directory path for the given VarType, using the XDG
// Base Directory Specification.
func GetVar(varType VarType) string {
	if dir, ok := varCache[varType]; ok {
		return dir
	}

	env := envVars[varType]
	val := os.Getenv(env)
	if val == "" {
		val = defaultDirs[varType]
	}

	val = os.ExpandEnv(val)

	varCache[varType] = val
	return val
}

// ClearCache clears the internal cache of XDG directory paths.
// This is primarily used for testing purposes.
func ClearCache() {
	varCache = make(map[VarType]string)
}

// SetTestEnv configures the XDG environment variables to point to a temporary
// directory and clears the path cache. This is used in testing to prevent
// pollution of the real user configuration and data directories.
func SetTestEnv(tmpDir string, appName string) {
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	os.Setenv("XDG_DATA_HOME", tmpDir)
	os.Setenv("XDG_CACHE_HOME", tmpDir)
	os.Setenv("TASKSMITH_APPNAME", appName)
	ClearCache()
}

// RunWithTestEnv runs package tests within a redirected temporary XDG environment
// to avoid polluting the user's local workspace.
func RunWithTestEnv(m *testing.M, appName string) {
	tmpDir, err := os.MkdirTemp("", appName+"-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	// Save original env vars
	origXdgData := os.Getenv("XDG_DATA_HOME")
	origXdgConfig := os.Getenv("XDG_CONFIG_HOME")
	origXdgCache := os.Getenv("XDG_CACHE_HOME")
	origAppName := os.Getenv("TASKSMITH_APPNAME")

	// Set temporary ones
	SetTestEnv(tmpDir, appName)

	// Run tests
	code := m.Run()

	// Restore env vars
	os.Setenv("XDG_DATA_HOME", origXdgData)
	os.Setenv("XDG_CONFIG_HOME", origXdgConfig)
	os.Setenv("XDG_CACHE_HOME", origXdgCache)
	os.Setenv("TASKSMITH_APPNAME", origAppName)

	// Clear cache again
	ClearCache()

	os.Exit(code)
}
