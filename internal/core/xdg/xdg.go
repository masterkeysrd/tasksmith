package xdg

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// DefaultAppName is the default application directory name when TASKSMITH_APPNAME
// is unset or invalid.
const DefaultAppName = "tasksmith"

// VarType represents a category of XDG Base Directory (analogous to Neovim's stdpath types).
type VarType int

const (
	// VarTypeConfig represents the XDG_CONFIG_HOME directory.
	VarTypeConfig VarType = iota
	// VarTypeData represents the XDG_DATA_HOME directory.
	VarTypeData
	// VarTypeState represents the XDG_STATE_HOME directory.
	VarTypeState
	// VarTypeCache represents the XDG_CACHE_HOME directory.
	VarTypeCache
	// VarTypeRuntime represents the XDG_RUNTIME_DIR directory for ephemeral runtime files.
	VarTypeRuntime
)

var envVars = map[VarType]string{
	VarTypeConfig:  "XDG_CONFIG_HOME",
	VarTypeData:    "XDG_DATA_HOME",
	VarTypeState:   "XDG_STATE_HOME",
	VarTypeCache:   "XDG_CACHE_HOME",
	VarTypeRuntime: "XDG_RUNTIME_DIR",
}

func defaultDirFor(varType VarType) string {
	if runtime.GOOS == "windows" {
		switch varType {
		case VarTypeConfig, VarTypeData, VarTypeState:
			return "$LOCALAPPDATA"
		case VarTypeCache:
			return "$TEMP"
		case VarTypeRuntime:
			return "$LOCALAPPDATA"
		}
	}

	switch varType {
	case VarTypeConfig:
		return "$HOME/.config"
	case VarTypeData:
		return "$HOME/.local/share"
	case VarTypeState:
		return "$HOME/.local/state"
	case VarTypeCache:
		return "$HOME/.cache"
	case VarTypeRuntime:
		return "$HOME/.local/state"
	default:
		return ""
	}
}

var (
	mu       sync.RWMutex
	appname  string
	varCache = make(map[VarType]string)
)

func init() {
	mu.Lock()
	loadAppNameLocked()
	mu.Unlock()
}

// isValidAppName validates that name is a safe relative path without parent traversal.
func isValidAppName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return false
	}
	cleaned := filepath.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false
	}
	return !slices.Contains(strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\'
	}), "..")
}

func loadAppNameLocked() {
	name := os.Getenv("TASKSMITH_APPNAME")
	if !isValidAppName(name) {
		name = DefaultAppName
	}
	appname = name
}

// AppName returns the active application name, defaulting to "tasksmith" unless
// overridden by a valid TASKSMITH_APPNAME environment variable.
func AppName() string {
	mu.RLock()
	name := appname
	mu.RUnlock()
	if name != "" {
		return name
	}

	mu.Lock()
	defer mu.Unlock()
	if appname == "" {
		loadAppNameLocked()
	}
	return appname
}

// GetVar returns the base XDG directory path for the given VarType before appending
// the application name.
func GetVar(varType VarType) string {
	mu.RLock()
	if dir, ok := varCache[varType]; ok {
		mu.RUnlock()
		return dir
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if dir, ok := varCache[varType]; ok {
		return dir
	}

	val := resolveVarLocked(varType)
	varCache[varType] = val
	return val
}

func resolveVarLocked(varType VarType) string {
	env := envVars[varType]
	val := os.Getenv(env)
	if val == "" {
		if varType == VarTypeRuntime {
			// Fall back to the resolved XDG_STATE_HOME base directory when XDG_RUNTIME_DIR is unset.
			if stateDir, ok := varCache[VarTypeState]; ok && stateDir != "" {
				return stateDir
			}
			stateVal := os.Getenv(envVars[VarTypeState])
			if stateVal == "" {
				stateVal = defaultDirFor(VarTypeState)
			}
			return os.ExpandEnv(stateVal)
		}
		val = defaultDirFor(varType)
	}

	return os.ExpandEnv(val)
}

// Home returns the application's base directory for the given VarType.
// For VarTypeConfig, VarTypeData, VarTypeState, and VarTypeCache, it returns
// "<GetVar(key)>/<AppName()>".
// For VarTypeRuntime, it returns "$XDG_RUNTIME_DIR/<AppName()>" when XDG_RUNTIME_DIR
// is set, or "<Home(VarTypeState)>/run" when XDG_RUNTIME_DIR is unset.
func Home(key VarType) (string, error) {
	if key == VarTypeRuntime && os.Getenv("XDG_RUNTIME_DIR") == "" {
		stateHome, err := Home(VarTypeState)
		if err != nil {
			return "", err
		}
		return filepath.Join(stateHome, "run"), nil
	}

	dir := GetVar(key)
	if dir == "" {
		return "", os.ErrNotExist
	}

	return filepath.Join(dir, AppName()), nil
}

// Subdir returns a subdirectory within the application's base directory for the given VarType.
func Subdir(key VarType, subdirs ...string) (string, error) {
	base, err := Home(key)
	if err != nil {
		return "", err
	}

	elems := make([]string, 0, 1+len(subdirs))
	elems = append(elems, base)
	elems = append(elems, subdirs...)
	return filepath.Join(elems...), nil
}

// SubConfigDir returns a subdirectory within the application's configuration directory.
func SubConfigDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeConfig, subdirs...)
}

// SubDataDir returns a subdirectory within the application's data directory.
func SubDataDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeData, subdirs...)
}

// SubStateDir returns a subdirectory within the application's state directory.
func SubStateDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeState, subdirs...)
}

// SubCacheDir returns a subdirectory within the application's cache directory.
func SubCacheDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeCache, subdirs...)
}

// SubRuntimeDir returns a subdirectory within the application's runtime directory.
func SubRuntimeDir(subdirs ...string) (string, error) {
	return Subdir(VarTypeRuntime, subdirs...)
}

// ClearCache clears the cached XDG base directory paths and reloads TASKSMITH_APPNAME.
func ClearCache() {
	mu.Lock()
	varCache = make(map[VarType]string)
	loadAppNameLocked()
	mu.Unlock()
}

// SetTestEnv configures all XDG environment variables and TASKSMITH_APPNAME to use
// a temporary directory and refreshes the cache.
func SetTestEnv(tmpDir string, appName string) {
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config"))
	_ = os.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
	_ = os.Setenv("XDG_STATE_HOME", filepath.Join(tmpDir, "state"))
	_ = os.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))
	_ = os.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmpDir, "runtime"))
	_ = os.Setenv("TASKSMITH_APPNAME", appName)
	ClearCache()
}

// RunWithTestEnv executes a test suite within an isolated temporary XDG environment.
func RunWithTestEnv(m *testing.M, appName string) {
	tmpDir, err := os.MkdirTemp("", appName+"-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	origConfig := os.Getenv("XDG_CONFIG_HOME")
	origData := os.Getenv("XDG_DATA_HOME")
	origState := os.Getenv("XDG_STATE_HOME")
	origCache := os.Getenv("XDG_CACHE_HOME")
	origRuntime := os.Getenv("XDG_RUNTIME_DIR")
	origAppName := os.Getenv("TASKSMITH_APPNAME")

	SetTestEnv(tmpDir, appName)

	code := m.Run()

	_ = os.Setenv("XDG_CONFIG_HOME", origConfig)
	_ = os.Setenv("XDG_DATA_HOME", origData)
	_ = os.Setenv("XDG_STATE_HOME", origState)
	_ = os.Setenv("XDG_CACHE_HOME", origCache)
	_ = os.Setenv("XDG_RUNTIME_DIR", origRuntime)
	_ = os.Setenv("TASKSMITH_APPNAME", origAppName)
	ClearCache()

	os.Exit(code)
}
