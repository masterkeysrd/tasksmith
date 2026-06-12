// Package flags provides functionality to parse command-line flags
// and return application options.
package flags

import (
	"flag"
	"os"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

// Flags defines the application configuration.
type Flags struct {
	CWD          string
	LogLevel     log.Level
	DevToolsAddr string
}

// Load parses command-line flags and returns the application options.
func Load() (*Flags, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	opts := &Flags{
		CWD:      cwd,
		LogLevel: log.LevelInfo,
	}

	var logLevelStr string
	flag.StringVar(&opts.CWD, "cwd", opts.CWD, "Current working directory")
	flag.StringVar(&logLevelStr, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&opts.DevToolsAddr, "devtools", "", "DevTools server address (e.g., :8080)")
	flag.Parse()

	switch strings.ToLower(logLevelStr) {
	case "debug":
		opts.LogLevel = log.LevelDebug
	case "info":
		opts.LogLevel = log.LevelInfo
	case "warn":
		opts.LogLevel = log.LevelWarn
	case "error":
		opts.LogLevel = log.LevelError
	default:
		opts.LogLevel = log.LevelInfo
	}

	return opts, nil
}
