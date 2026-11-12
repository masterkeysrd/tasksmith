package permissions

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// FileAccessConfig configures a [FileAccessChecker] for workspace file and
// directory read checks.
type FileAccessConfig struct {
	WorkDir   string
	Workspace *workspace.Workspace
	Manager   Manager
	Session   SessionManager
}

// FileAccessChecker evaluates workspace file and directory read access against
// repository internals (.git, .bare), secret/sensitive path guards, explicit
// deny rules (session, project, and global), tool policy ceilings, and
// canonical symlink targets.
type FileAccessChecker struct {
	workDir       string
	realWorkDir   string
	denyRules     []string
	wsPolicies    *warp.Policies
	agentPolicies *warp.Policies
	ws            *workspace.Workspace
}

// NewFileAccessChecker constructs a reusable [FileAccessChecker] from cfg,
// pre-resolving canonical WorkDir symlinks and effective deny rules once.
func NewFileAccessChecker(cfg FileAccessConfig) *FileAccessChecker {
	ws := cfg.Workspace
	mgr := cfg.Manager
	if sm, ok := cfg.Session.(*sessionManager); ok && sm != nil {
		if ws == nil {
			ws = sm.ws
		}
		if mgr == nil {
			mgr = sm.manager
		}
	}

	var wsPolicies, agentPolicies *warp.Policies
	if cfg.Session != nil {
		wsPolicies = cfg.Session.Policies()
		agentPolicies = cfg.Session.AgentPolicies()
	} else if ws != nil && ws.Catalog() != nil {
		wsPolicies = ws.Catalog().Policies()
	}

	var denyRules []string
	if mgr != nil {
		if eff, err := mgr.Effective(); err == nil {
			denyRules = append(denyRules, eff.Deny...)
		}
	}
	if cfg.Session != nil {
		denyRules = deduplicateRules(append(denyRules, cfg.Session.Permissions().Deny...))
	}

	realWorkDir, _ := fsutils.CleanPath(cfg.WorkDir)

	return &FileAccessChecker{
		workDir:       cfg.WorkDir,
		realWorkDir:   realWorkDir,
		denyRules:     denyRules,
		wsPolicies:    wsPolicies,
		agentPolicies: agentPolicies,
		ws:            ws,
	}
}

// Check verifies that path (displayed in error messages as displayPath when
// non-empty) may be read or listed as a workspace file or directory.
func (c *FileAccessChecker) Check(path, displayPath string, isDir bool) error {
	if c == nil {
		return nil
	}
	label := cmp.Or(displayPath, path)
	clean := filepath.ToSlash(filepath.Clean(path))
	if filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("file mention %q escapes workspace root", label)
	}

	if err := c.checkSingle(clean, label, isDir, c.workDir); err != nil {
		return err
	}

	if c.realWorkDir != "" {
		targetAbs := filepath.Join(c.workDir, filepath.FromSlash(clean))
		realTarget, err := filepath.EvalSymlinks(targetAbs)
		if err != nil {
			if linfo, lerr := os.Lstat(targetAbs); lerr == nil && linfo.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf("file mention %q points to an inaccessible symlink target: %w", label, err)
			}
			return nil
		}
		if !fsutils.IsInsideDir(realTarget, c.realWorkDir) {
			return fmt.Errorf("file mention %q escapes workspace root", label)
		}
		relReal, relErr := filepath.Rel(c.realWorkDir, realTarget)
		if relErr != nil {
			return fmt.Errorf("file mention %q escapes workspace root", label)
		}
		relRealSlash := filepath.ToSlash(relReal)
		if relRealSlash != clean {
			if err := c.checkSingle(relRealSlash, label, isDir, c.realWorkDir); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *FileAccessChecker) checkSingle(rel, label string, isDir bool, workDir string) error {
	if guard.IsProtectedPath(rel, "", ".git", ".bare") {
		return fmt.Errorf("access to internal repository path %q is prohibited", label)
	}
	for part := range strings.SplitSeq(rel, "/") {
		if guard.IsSecretEnvFile(part) {
			return fmt.Errorf("access to secret environment file %q is prohibited", label)
		}
	}

	targetAbs := filepath.Join(workDir, filepath.FromSlash(rel))
	if guard.IsSensitivePath(targetAbs, "") {
		return fmt.Errorf("access to sensitive path %q is prohibited", label)
	}

	toolName := "view"
	if isDir {
		toolName = "ls"
	}
	plan := &tools.Plan{
		ToolName: toolName,
		Target:   rel,
		ReadOnly: true,
		Safety:   api.SafetySafe,
	}

	for _, rule := range c.denyRules {
		if MatchRule(rule, plan) {
			return fmt.Errorf("access to %s %q is denied by rule %q", toolName, label, rule)
		}
	}

	if action := evaluatePolicy(plan, c.wsPolicies, c.agentPolicies, c.ws); action == warp.ToolPolicyDeny {
		return fmt.Errorf("access to %s %q is prohibited by policy", toolName, label)
	}

	return nil
}
