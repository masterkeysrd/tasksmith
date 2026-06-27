package cmdclassify

import (
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/cmdparse"
)

// DefaultPolicy returns the built-in safety classification rules.
func DefaultPolicy() Policy {
	p := Policy{
		DefaultCategory: Unknown,
		Rules:           make(map[string]CommandRule),
	}

	// --- Static rules (executable → category) ---
	p.addStatic("cat", ReadOnly)
	p.addStatic("grep", ReadOnly)
	p.addStatic("find", ReadOnly)
	p.addStatic("ls", ReadOnly)
	p.addStatic("pwd", ReadOnly)
	p.addStatic("echo", ReadOnly)
	p.addStatic("head", ReadOnly)
	p.addStatic("tail", ReadOnly)
	p.addStatic("file", ReadOnly)
	p.addStatic("which", ReadOnly)
	p.addStatic("rm", Destructive)
	p.addStatic("rmdir", Destructive)
	p.addStatic("dd", Destructive)
	p.addStatic("mkfs", Destructive)
	p.addStatic("sudo", Destructive)
	p.addStatic("mkdir", SafeWrite)
	p.addStatic("touch", SafeWrite)
	p.addStatic("cp", SafeWrite)

	// --- Git conditional rules ---
	gitRule := CommandRule{
		DefaultCategory: SafeWrite,
		ArgRules: []ArgRule{
			// ReadOnly git commands
			{
				Category:    ReadOnly,
				Subcommands: []string{"status", "diff", "log", "show", "branch"},
			},
			// Destructive: git reset --hard
			{
				Category:    Destructive,
				Subcommands: []string{"reset"},
				ContainsAll: []string{"--hard"},
			},
			// Destructive: git clean -f -d
			{
				Category:    Destructive,
				Subcommands: []string{"clean"},
				ContainsAll: []string{"-f", "-d"},
			},
			{
				Category:    Destructive,
				Subcommands: []string{"clean"},
				ContainsAny: []string{"--force", "--remove-untracked"},
			},
		},
	}
	p.Rules["git"] = gitRule

	return p
}

// addStatic adds a simple static rule mapping an executable to a category.
func (p *Policy) addStatic(exec string, cat Category) {
	p.Rules[exec] = CommandRule{DefaultCategory: cat}
}

// Classify parses and determines the safety category of a command string.
func Classify(commandStr string, workspaceDir string) Category {
	policy := DefaultPolicy()
	chain, err := cmdparse.Parse(commandStr)
	if err != nil {
		return Unknown
	}

	// Track the worst category found.
	worst := Unknown
	for _, pipeline := range chain.Pipelines {
		for _, cmd := range pipeline.Commands {
			cat := classifyCommand(cmd, policy, workspaceDir)
			worst = worstCategory(worst, cat)
		}
	}

	return worst
}

// worstCategory returns the more severe of two categories.
// Order: Unknown < ReadOnly < SafeWrite < UnsafeWrite < Destructive
func worstCategory(a, b Category) Category {
	order := map[Category]int{
		Unknown:     0,
		ReadOnly:    1,
		SafeWrite:   2,
		UnsafeWrite: 3,
		Destructive: 4,
	}
	if order[a] >= order[b] {
		return a
	}
	return b
}

// classifyCommand evaluates a single ParsedCommand against the policy.
func classifyCommand(cmd cmdparse.ParsedCommand, policy Policy, workspaceDir string) Category {
	// Check redirects first (dynamic check).
	if redirCat := checkRedirects(cmd.Redirects, workspaceDir); redirCat == UnsafeWrite {
		return UnsafeWrite
	}

	// Handle sudo specially: always Destructive, no wrapper unpacking needed.
	if cmd.Executable == "sudo" {
		return Destructive
	}

	// Resolve the effective executable after wrapper unpacking.
	effectiveExec, effectiveArgs := resolveExecutableAndArgs(cmd)

	// Lookup static rules.
	if rule, ok := policy.Rules[effectiveExec]; ok {
		return matchRule(rule, effectiveArgs, workspaceDir)
	}

	// Fallback to default category.
	return policy.DefaultCategory
}

// resolveExecutableAndArgs unpacks wrapper commands (npx) to get the real executable.
func resolveExecutableAndArgs(cmd cmdparse.ParsedCommand) (string, []string) {
	// sudo is handled separately in classifyCommand, so only unpack npx.
	if cmd.Executable == "npx" && cmd.SubCommand != nil {
		sub := cmd.SubCommand
		return sub.Executable, sub.Args
	}
	if cmd.SubCommand != nil {
		sub := cmd.SubCommand
		return sub.Executable, sub.Args
	}
	return cmd.Executable, cmd.Args
}

// checkRedirects evaluates redirection targets against the workspace boundary.
func checkRedirects(redirs []cmdparse.Redirect, workspaceDir string) Category {
	for _, redir := range redirs {
		if redir.Target == "" {
			continue
		}
		// Resolve the target path relative to workspace.
		resolved := resolvePath(redir.Target, workspaceDir)

		// Check if it's outside the workspace.
		if !isInsideWorkspace(resolved, workspaceDir) {
			return UnsafeWrite
		}

		// Check if it's inside .git directory.
		if isInsideGitDirectory(resolved, workspaceDir) {
			return UnsafeWrite
		}
	}
	return ReadOnly
}

// resolvePath resolves a redirect target to an absolute path.
func resolvePath(target string, workspaceDir string) string {
	// If already absolute, return as-is.
	if strings.HasPrefix(target, "/") {
		return filepath.Clean(target)
	}
	// Otherwise, resolve relative to workspace.
	return filepath.Clean(filepath.Join(workspaceDir, target))
}

// isInsideWorkspace checks if a resolved path is inside the workspace directory.
func isInsideWorkspace(resolved string, workspaceDir string) bool {
	workspaceDir = filepath.Clean(workspaceDir)
	resolved = filepath.Clean(resolved)

	// The resolved path must start with the workspace directory.
	return strings.HasPrefix(resolved, workspaceDir+string(filepath.Separator)) ||
		resolved == workspaceDir
}

// isInsideGitDirectory checks if a resolved path is inside a .git directory.
func isInsideGitDirectory(resolved string, workspaceDir string) bool {
	// Check if the path contains /.git/ or ends with /.git
	gitSep := string(filepath.Separator) + ".git" + string(filepath.Separator)
	gitSuffix := string(filepath.Separator) + ".git"

	return strings.Contains(resolved, gitSep) ||
		strings.HasSuffix(resolved, gitSuffix)
}

// matchRule evaluates a CommandRule against the given arguments.
func matchRule(rule CommandRule, args []string, workspaceDir string) Category {
	// Check each arg rule in order (first match wins).
	for _, argRule := range rule.ArgRules {
		if matchesArgRule(args, argRule) {
			return argRule.Category
		}
	}
	return rule.DefaultCategory
}

// matchesArgRule checks if args satisfy an ArgRule's conditions.
func matchesArgRule(args []string, rule ArgRule) bool {
	// Check Subcommands: first argument must match one of these.
	if len(rule.Subcommands) > 0 && len(args) > 0 {
		if !contains(rule.Subcommands, args[0]) {
			return false
		}
	}

	// Check ContainsAny: at least one of these must be in args.
	if len(rule.ContainsAny) > 0 {
		if !containsAny(args, rule.ContainsAny) {
			return false
		}
	}

	// Check ContainsAll: all of these must be in args.
	if len(rule.ContainsAll) > 0 {
		if !containsAll(args, rule.ContainsAll) {
			return false
		}
	}

	return true
}

// contains checks if a slice contains a specific string.
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// containsAny checks if any of the targets are in the slice.
func containsAny(args []string, targets []string) bool {
	for _, target := range targets {
		if contains(args, target) {
			return true
		}
	}
	return false
}

// containsAll checks if all of the targets are in the slice.
func containsAll(args []string, targets []string) bool {
	for _, target := range targets {
		if !contains(args, target) {
			return false
		}
	}
	return true
}

// IsReadOnly returns true if the command is completely ReadOnly.
func IsReadOnly(commandStr string, workspaceDir string) bool {
	return Classify(commandStr, workspaceDir) == ReadOnly
}

// MatchParsedCommand checks if a parsed command matches a user's permission grant target.
// It supports wrapper unpacking, wildcard argument matching (e.g. "git *"),
// and subcommand matching (e.g. "git commit").
func MatchParsedCommand(grantTarget string, matchMethod string, cmd cmdparse.ParsedCommand) bool {
	// Resolve the effective executable after wrapper unpacking.
	effectiveExec, effectiveArgs := resolveExecutableAndArgs(cmd)

	// Parse the grant target.
	parts := strings.Fields(grantTarget)
	if len(parts) == 0 {
		return false
	}

	grantExec := parts[0]

	// Match the executable.
	if !matchExecutable(effectiveExec, grantExec) {
		return false
	}

	// If only executable in grant (e.g. "git *"), match is confirmed.
	if len(parts) == 1 {
		return true
	}

	// Wildcard support: "git *" matches any git command.
	if len(parts) == 2 && parts[1] == "*" {
		return true
	}

	// Subcommand prefix matching: grant is "git commit", args must start with ["commit"].
	grantArgs := parts[1:]
	if len(grantArgs) > 0 {
		if len(effectiveArgs) < len(grantArgs) {
			return false
		}
		for i, ga := range grantArgs {
			if effectiveArgs[i] != ga {
				return false
			}
		}
	}

	return true
}

// matchExecutable checks if the effective executable matches the grant executable.
func matchExecutable(effective, grant string) bool {
	return effective == grant
}
