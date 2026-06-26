package statusline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/internal/core/scheduler"
	"github.com/masterkeysrd/tasksmith/internal/core/vcs"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// Fragment represents a config element in the status line.
type Fragment struct {
	Type     string `json:"type"`               // "builtin" or "command"
	Name     string `json:"name,omitempty"`     // "mode", "git_branch", "stats", "status"
	Exec     string `json:"exec,omitempty"`     // shell command to execute
	Interval string `json:"interval,omitempty"` // interval string (e.g., "1m", "5m")
}

// Config defines the structure of the left and right status line layout.
type Config struct {
	Left  []Fragment `json:"left"`
	Right []Fragment `json:"right"`
}

// FileConfig matches the top-level configuration key in statusline.json.
type FileConfig struct {
	StatusLine Config `json:"statusline"`
}

// State represents the current reactive data for the status line components.
type State struct {
	Config         Config
	CommandOutputs map[string]string
	GitBranch      string
	Revision       int
}

var (
	store *kites.Store[State]
	sched *scheduler.Scheduler
	cwd   string
	cwdMu sync.Mutex
)

func init() {
	defaultState := State{
		Config: Config{
			Left: []Fragment{
				{Type: "builtin", Name: "mode"},
				{Type: "builtin", Name: "git_branch"},
				{Type: "builtin", Name: "diagnostics"},
			},
			Right: []Fragment{
				{Type: "builtin", Name: "provider"},
				{Type: "builtin", Name: "model"},
				{Type: "builtin", Name: "agent"},
				{Type: "builtin", Name: "stats"},
				{Type: "builtin", Name: "status"},
			},
		},
		CommandOutputs: make(map[string]string),
		GitBranch:      "MAIN",
		Revision:       0,
	}

	store = kites.Create(defaultState)
	sched = scheduler.New(context.Background())

	// Load configuration from theme.json / statusline.json in the XDG directory
	if dir, err := xdg.SubConfigDir(); err == nil {
		cfgPath := filepath.Join(dir, "statusline.json")
		var loaded FileConfig
		if data, err := os.ReadFile(cfgPath); err == nil {
			if err := json.Unmarshal(data, &loaded); err == nil {
				store.Set(func(s State) State {
					s.Config = loaded.StatusLine
					s.Revision++
					return s
				})
			}
		} else if os.IsNotExist(err) {
			// Write the default file to help the user customize it
			fileCfg := FileConfig{StatusLine: defaultState.Config}
			if bytes, err := json.MarshalIndent(fileCfg, "", "  "); err == nil {
				_ = os.WriteFile(cfgPath, bytes, 0644)
			}
		}
	}

	// Start the core scheduler loops
	go sched.Start(context.Background())
}

// Use returns the reactive state of the status line configuration and outputs.
func Use() State {
	// Register components to re-render when state revision changes
	_ = kites.Use(store, func(s State) int {
		return s.Revision
	})
	return store.Get()
}

// SetCWD registers or re-registers the statusline tasks under the specified directory path.
func SetCWD(path string) {
	cwdMu.Lock()
	defer cwdMu.Unlock()

	if path == "" || path == cwd {
		return
	}
	cwd = path

	// Unregister existing tasks to avoid duplicates
	sched.Unregister("git-branch")
	state := store.Get()
	for i := range append(state.Config.Left, state.Config.Right...) {
		sched.Unregister(fmt.Sprintf("statusline-cmd-%d", i))
	}

	// 1. Git branch task
	hasGit := false
	for _, f := range append(state.Config.Left, state.Config.Right...) {
		if f.Type == "builtin" && f.Name == "git_branch" {
			hasGit = true
			break
		}
	}

	if hasGit && vcs.IsRepo(cwd) {
		_ = sched.Register("git-branch", "Git Branch Monitor", "10s", true, func(ctx context.Context) error {
			branch, err := vcs.GetBranch(cwd)
			if err != nil {
				branch = "MAIN"
			} else {
				branch = strings.ToUpper(branch)
			}
			store.Set(func(s State) State {
				s.GitBranch = branch
				s.Revision++
				return s
			})
			return nil
		})
	}

	// 2. Command tasks
	for i, f := range append(state.Config.Left, state.Config.Right...) {
		if f.Type == "command" && f.Exec != "" {
			interval := f.Interval
			if interval == "" {
				interval = "1m"
			} else if _, err := time.ParseDuration(interval); err != nil {
				interval = "1m"
			}

			execCmd := f.Exec
			taskID := fmt.Sprintf("statusline-cmd-%d", i)

			_ = sched.Register(taskID, "StatusLine Command: "+execCmd, interval, true, func(ctx context.Context) error {
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", execCmd)
				cmd.Dir = cwd // Execute command in the workspace directory context
				out, err := cmd.Output()
				output := ""
				var runErr error
				if err != nil {
					output = fmt.Sprintf("Error: %v", err)
					runErr = err
				} else {
					output = strings.TrimSpace(string(out))
				}
				store.Set(func(s State) State {
					if s.CommandOutputs == nil {
						s.CommandOutputs = make(map[string]string)
					}
					newOutputs := make(map[string]string)
					for k, v := range s.CommandOutputs {
						newOutputs[k] = v
					}
					newOutputs[execCmd] = output
					s.CommandOutputs = newOutputs
					s.Revision++
					return s
				})
				return runErr
			})
		}
	}
}
