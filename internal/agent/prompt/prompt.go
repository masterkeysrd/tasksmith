package prompt

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/vcs"
	"github.com/masterkeysrd/warp"
)

// RenderAgent compiles the prompt template of an agent, enriching it with Tasksmith-specific context globals.
func RenderAgent(resolvedAgent *warp.ResolvedAgent, ws *warp.Workspace, proj *warp.Project, extraGlobals map[string]any) (string, error) {
	if resolvedAgent == nil || resolvedAgent.Agent == nil {
		return "", fmt.Errorf("nil agent")
	}

	var cwd string
	if ws != nil {
		cwd = ws.RootPath
	}
	if cwd == "" && proj != nil {
		cwd = proj.RootPath
	}

	globals := buildGlobals(cwd, extraGlobals)
	globals["HasTool"] = func(name string) bool {
		for _, t := range resolvedAgent.Tools {
			if t.Metadata.Name == name {
				return true
			}
		}
		return false
	}

	opts := &warp.AgentRenderOptions{
		Workspace: ws,
		Project:   proj,
		Resolved:  resolvedAgent,
		Globals:   globals,
	}

	return resolvedAgent.Agent.Render(opts)
}

// RenderSkill renders the instructions of a skill, enriching it with Tasksmith-specific context globals.
func RenderSkill(skill *warp.Skill, agent *warp.Agent, ws *warp.Workspace, proj *warp.Project, extraGlobals map[string]any) (string, error) {
	if skill == nil {
		return "", fmt.Errorf("nil skill")
	}

	var cwd string
	if ws != nil {
		cwd = ws.RootPath
	}
	if cwd == "" && proj != nil {
		cwd = proj.RootPath
	}

	globals := buildGlobals(cwd, extraGlobals)

	opts := &warp.SkillRenderOptions{
		Workspace: ws,
		Project:   proj,
		Agent:     agent,
		Globals:   globals,
	}

	return skill.Render(opts)
}

// RenderCommand renders a command template, enriching it with Tasksmith-specific context globals.
func RenderCommand(cmd *warp.Command, ws *warp.Workspace, proj *warp.Project, args []string, extraGlobals map[string]any) (string, error) {
	if cmd == nil {
		return "", fmt.Errorf("nil command")
	}

	var cwd string
	if ws != nil {
		cwd = ws.RootPath
	}
	if cwd == "" && proj != nil {
		cwd = proj.RootPath
	}

	globals := buildGlobals(cwd, extraGlobals)

	opts := &warp.CommandRenderOptions{
		Workspace: ws,
		Project:   proj,
		Args:      args,
		Globals:   globals,
	}

	return cmd.Render(opts)
}

func buildGlobals(cwd string, extra map[string]any) map[string]any {
	globals := make(map[string]any)

	// Inject defaults
	globals["Date"] = time.Now().Format("2006-01-02")

	if cwd == "" {
		if dir, err := os.Getwd(); err == nil {
			cwd = dir
		}
	}
	globals["CWD"] = cwd
	globals["OS"] = runtime.GOOS

	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}
	globals["Terminal"] = term

	u := os.Getenv("USER")
	if u == "" {
		u = os.Getenv("USERNAME")
	}
	if u == "" {
		u = "user"
	}
	globals["User"] = u

	// Add requested runtime globals
	shell := os.Getenv("SHELL")
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = os.Getenv("COMSPEC")
			if shell == "" {
				shell = "cmd.exe"
			}
		} else {
			shell = "/bin/sh"
		}
	}
	globals["Shell"] = shell

	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	globals["Home"] = home

	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}
	globals["Host"] = host

	globals["Arch"] = runtime.GOARCH

	// Git integration (if within a git repo)
	if cwd != "" && vcs.IsRepo(cwd) {
		if branch, err := vcs.GetBranch(cwd); err == nil {
			globals["GitBranch"] = branch
		}
		if commit, err := vcs.GetCommit(cwd); err == nil {
			globals["GitCommit"] = commit
		}
	}

	// Merge extra globals
	maps.Copy(globals, extra)

	return globals
}
