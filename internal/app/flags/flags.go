// Package flags provides functionality to parse command-line flags
// and return application options.
package flags

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

// Flags defines the application configuration.
type Flags struct {
	CWD          string
	LogLevel     log.Level
	DevToolsAddr string
	Debug        bool
	Agent        string
	Args         []string
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
	var debugFlag bool
	if flag.Lookup("cwd") == nil {
		flag.StringVar(&opts.CWD, "cwd", opts.CWD, "Current working directory")
		flag.StringVar(&logLevelStr, "log-level", "info", "Log level (debug, info, warn, error)")
		flag.StringVar(&opts.DevToolsAddr, "devtools", "", "DevTools server address (e.g., :8080)")
		flag.BoolVar(&debugFlag, "debug", false, "Expose pprof and enable debug features")
		flag.StringVar(&opts.Agent, "agent", "", "Default agent name to start the session with")
	} else {
		if f := flag.Lookup("cwd"); f != nil {
			opts.CWD = f.Value.String()
		}
		if f := flag.Lookup("log-level"); f != nil {
			logLevelStr = f.Value.String()
		}
		if f := flag.Lookup("devtools"); f != nil {
			opts.DevToolsAddr = f.Value.String()
		}
		if f := flag.Lookup("debug"); f != nil {
			if b, ok := f.Value.(flag.Getter).Get().(bool); ok {
				debugFlag = b
			}
		}
		if f := flag.Lookup("agent"); f != nil {
			opts.Agent = f.Value.String()
		}
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s login [provider]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  login [provider]  Authenticate with an LLM provider (default: antigravity)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	if !flag.Parsed() {
		flag.Parse()
	}

	opts.Args = flag.Args()
	opts.Debug = debugFlag || os.Getenv("TASKSMITH_DEBUG") != ""

	switch strings.ToLower(logLevelStr) {
	case "debug":
		opts.LogLevel = log.LevelDebug
	case "info":
		if opts.Debug {
			opts.LogLevel = log.LevelDebug
		} else {
			opts.LogLevel = log.LevelInfo
		}
	case "warn":
		opts.LogLevel = log.LevelWarn
	case "error":
		opts.LogLevel = log.LevelError
	default:
		if opts.Debug {
			opts.LogLevel = log.LevelDebug
		} else {
			opts.LogLevel = log.LevelInfo
		}
	}

	return opts, nil
}
