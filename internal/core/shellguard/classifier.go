package shellguard

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultPolicy returns the built-in safety classification rules.
func DefaultPolicy() Policy {
	p := Policy{
		DefaultCategory: ActionUnknown,
		Rules:           make(map[string]CommandRule),
	}

	// --- Static rules (executable → ActionType) ---
	p.addStatic("cat", ActionRead)
	p.addStatic("grep", ActionRead)
	p.addStatic("find", ActionRead)
	p.addStatic("ls", ActionRead)
	p.addStatic("pwd", ActionRead)
	p.addStatic("echo", ActionRead)
	p.addStatic("head", ActionRead)
	p.addStatic("tail", ActionRead)
	p.addStatic("file", ActionRead)
	p.addStatic("which", ActionRead)
	p.addStatic("rm", ActionDelete)
	p.addStatic("rmdir", ActionDelete)
	p.addStatic("dd", ActionDelete)
	p.addStatic("mkfs", ActionDelete)
	p.addStatic("sudo", ActionDelete)
	p.addStatic("mkdir", ActionWrite)
	p.addStatic("touch", ActionWrite)
	p.addStatic("cp", ActionWrite)

	// --- Git conditional rules ---
	gitRule := CommandRule{
		DefaultCategory: ActionWrite,
		ArgRules: []ArgRule{
			// ReadOnly git commands
			{
				Category:    ActionRead,
				Subcommands: []string{"status", "diff", "log", "show", "branch"},
			},
			// Destructive git commands
			{
				Category:    ActionDelete,
				Subcommands: []string{"rm"},
			},
			// Destructive: git reset --hard
			{
				Category:    ActionDelete,
				Subcommands: []string{"reset"},
				ContainsAll: []string{"--hard"},
			},
			// Destructive: git clean -f -d
			{
				Category:    ActionDelete,
				Subcommands: []string{"clean"},
				ContainsAll: []string{"-f", "-d"},
			},
			{
				Category:    ActionDelete,
				Subcommands: []string{"clean"},
				ContainsAny: []string{"--force", "--remove-untracked"},
			},
		},
	}
	p.Rules["git"] = gitRule

	return p
}

// addStatic adds a simple static rule mapping an executable to an action.
func (p *Policy) addStatic(exec string, action ActionType) {
	p.Rules[exec] = CommandRule{DefaultCategory: action}
}

type trackerState struct {
	virtualCWD   string
	isCWDUnknown bool
}

// TargetArg represents a target file argument.
type TargetArg struct {
	Path      string
	IsDynamic bool
}

// Analyze parses the command string and returns all meaningful operations.
func Analyze(commandStr string, workspaceDir string) ([]Operation, error) {
	chain, err := Parse(commandStr)
	if err != nil {
		return nil, err
	}

	policy := DefaultPolicy()
	state := &trackerState{
		virtualCWD:   workspaceDir,
		isCWDUnknown: false,
	}

	var ops []Operation

	for _, pipeline := range chain.Pipelines {
		for _, cmd := range pipeline.Commands {
			// Check for literal directory changes (cd)
			if cmd.Executable == "cd" {
				if len(cmd.Args) > 0 && len(cmd.ArgsIsDynamic) > 0 && cmd.ArgsIsDynamic[0] {
					state.isCWDUnknown = true
				} else {
					targetDir := "~"
					if len(cmd.Args) > 0 {
						targetDir = cmd.Args[0]
					}

					// Tilde Expansion:
					if targetDir == "~" {
						home, err := os.UserHomeDir()
						if err != nil {
							state.isCWDUnknown = true
						} else {
							targetDir = home
						}
					} else if strings.HasPrefix(targetDir, "~/") {
						home, err := os.UserHomeDir()
						if err != nil {
							state.isCWDUnknown = true
						} else {
							targetDir = home + targetDir[1:]
						}
					}

					// Path Resolution:
					if !state.isCWDUnknown {
						cleanTarget := filepath.Clean(targetDir)
						if strings.HasPrefix(cleanTarget, "/") {
							state.virtualCWD = cleanTarget
						} else {
							state.virtualCWD = filepath.Clean(filepath.Join(state.virtualCWD, targetDir))
						}
					}
				}
				// cd commands are collapsed, emitting 0 operations
				continue
			}

			// Generate operation for non-cd command
			ops = append(ops, determineOperation(cmd, workspaceDir, state, policy))
		}
	}

	return ops, nil
}

func determineOperation(cmd ParsedCommand, workspaceDir string, state *trackerState, policy Policy) Operation {
	// Sudo wrapper check
	if cmd.Executable == "sudo" {
		return Operation{
			Command: &cmd,
			Action:  ActionDelete,
			Safety:  SafetyUnsafe,
			Path:    state.virtualCWD,
			CWD:     state.virtualCWD,
		}
	}

	effectiveExec, effectiveArgs := resolveExecutableAndArgs(cmd)

	// Determine Action
	var action ActionType
	// Check for eval or cmd substitution in wrapper hierarchy
	hasEval := (effectiveExec == "eval")
	hasSubst := cmd.HasCmdSubst
	currSub := &cmd
	for currSub.SubCommand != nil {
		currSub = currSub.SubCommand
		if currSub.Executable == "eval" {
			hasEval = true
		}
		if currSub.HasCmdSubst {
			hasSubst = true
		}
	}

	if hasEval || hasSubst {
		action = ActionExec
	} else if strings.Contains(effectiveExec, "/") {
		action = ActionExec
	} else {
		// Look up policy rule
		if rule, ok := policy.Rules[effectiveExec]; ok {
			action = matchRule(rule, effectiveArgs)
		} else {
			action = policy.DefaultCategory
		}
	}

	// Check if there are write redirections
	var writeRedirs []Redirect
	// Traverse redirection hierarchy
	currRedir := &cmd
	for currRedir != nil {
		for _, redir := range currRedir.Redirects {
			if redir.Op == ">" || redir.Op == ">>" {
				writeRedirs = append(writeRedirs, redir)
			}
		}
		currRedir = currRedir.SubCommand
	}

	if len(writeRedirs) > 0 && action != ActionDelete {
		action = ActionWrite
	}

	// Gather all paths to evaluate
	type PathCheck struct {
		path      string
		isDynamic bool
	}
	var checks []PathCheck

	// Add write redirections
	for _, redir := range writeRedirs {
		checks = append(checks, PathCheck{path: redir.Target, isDynamic: redir.IsDynamic})
	}

	// Add targets for non-read/delete actions (so ActionWrite, ActionExec, ActionUnknown, ActionRead)
	// We want to check target paths for all operations to ensure they do not escape the workspace
	targets := getSafeWriteTargets(effectiveExec, cmd)
	for _, t := range targets {
		checks = append(checks, PathCheck{path: t.Path, isDynamic: t.IsDynamic})
	}

	// If no checks, default to virtualCWD or executable path
	if len(checks) == 0 {
		safety := SafetySafe
		if action == ActionDelete {
			safety = SafetyUnsafe
		} else if action == ActionExec && (hasEval || hasSubst) {
			safety = SafetyUnknown
		} else if state.isCWDUnknown {
			safety = SafetyUnknown
		} else {
			pathToCheck := state.virtualCWD
			if action == ActionExec && strings.Contains(effectiveExec, "/") {
				pathToCheck = resolvePath(effectiveExec, state.virtualCWD)
			}
			if !isInsideWorkspace(pathToCheck, workspaceDir) || isInsideGitDirectory(pathToCheck, workspaceDir) {
				safety = SafetyUnsafe
			}
		}

		pathToReport := state.virtualCWD
		if action == ActionExec && strings.Contains(effectiveExec, "/") {
			pathToReport = resolvePath(effectiveExec, state.virtualCWD)
		}

		return Operation{
			Command: &cmd,
			Action:  action,
			Safety:  safety,
			Path:    pathToReport,
			CWD:     state.virtualCWD,
		}
	}

	// Evaluate all checks to find worst safety and path
	worstSafety := SafetySafe
	if action == ActionExec && (hasEval || hasSubst) {
		worstSafety = SafetyUnknown
	}

	var chosenPath string
	for _, check := range checks {
		var resolved string
		var safety SafetyLevel
		if action == ActionDelete {
			safety = SafetyUnsafe
		} else if state.isCWDUnknown || check.isDynamic || check.path == "" {
			safety = SafetyUnknown
		} else {
			resolved = resolvePath(check.path, state.virtualCWD)
			if !isInsideWorkspace(resolved, workspaceDir) || isInsideGitDirectory(resolved, workspaceDir) {
				safety = SafetyUnsafe
			} else {
				safety = SafetySafe
			}
		}

		// Update chosen path. We prefer showing the unsafe path or dynamic path if there is one.
		if worstSafety == SafetySafe && safety != SafetySafe {
			worstSafety = safety
			if resolved != "" {
				chosenPath = resolved
			} else {
				chosenPath = check.path
			}
		} else if worstSafety == SafetyUnknown && safety == SafetyUnsafe {
			worstSafety = safety
			if resolved != "" {
				chosenPath = resolved
			} else {
				chosenPath = check.path
			}
		} else if chosenPath == "" {
			if resolved != "" {
				chosenPath = resolved
			} else {
				chosenPath = check.path
			}
		}
	}

	return Operation{
		Command: &cmd,
		Action:  action,
		Safety:  worstSafety,
		Path:    chosenPath,
		CWD:     state.virtualCWD,
	}
}

// resolveExecutableAndArgs unpacks wrapper commands recursively.
func resolveExecutableAndArgs(cmd ParsedCommand) (string, []string) {
	curr := &cmd
	for curr.SubCommand != nil {
		curr = curr.SubCommand
	}
	return curr.Executable, curr.Args
}

// getSafeWriteTargets returns target write paths for ActionWrite commands.
func getSafeWriteTargets(exec string, cmd ParsedCommand) []TargetArg {
	curr := &cmd
	for curr.SubCommand != nil {
		curr = curr.SubCommand
	}
	var nonFlagArgs []string
	var nonFlagIsDynamic []bool
	for i, arg := range curr.Args {
		if !strings.HasPrefix(arg, "-") {
			nonFlagArgs = append(nonFlagArgs, arg)
			isDyn := false
			if i < len(curr.ArgsIsDynamic) {
				isDyn = curr.ArgsIsDynamic[i]
			}
			nonFlagIsDynamic = append(nonFlagIsDynamic, isDyn)
		}
	}
	if len(nonFlagArgs) == 0 {
		return nil
	}

	var targets []TargetArg
	switch exec {
	case "cp":
		// For cp, the target is the last argument (destination)
		lastIdx := len(nonFlagArgs) - 1
		targets = append(targets, TargetArg{
			Path:      nonFlagArgs[lastIdx],
			IsDynamic: nonFlagIsDynamic[lastIdx],
		})
	case "git":
		// Ignore the first non-flag arg which is the git subcommand
		if len(nonFlagArgs) > 1 {
			for i := 1; i < len(nonFlagArgs); i++ {
				targets = append(targets, TargetArg{
					Path:      nonFlagArgs[i],
					IsDynamic: nonFlagIsDynamic[i],
				})
			}
		}
	default:
		for i := 0; i < len(nonFlagArgs); i++ {
			targets = append(targets, TargetArg{
				Path:      nonFlagArgs[i],
				IsDynamic: nonFlagIsDynamic[i],
			})
		}
	}
	return targets
}

// resolvePath resolves a target to an absolute path.
func resolvePath(target string, baseDir string) string {
	if strings.HasPrefix(target, "/") {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(baseDir, target))
}

// isInsideWorkspace checks if a resolved path is inside the workspace directory.
func isInsideWorkspace(resolved string, workspaceDir string) bool {
	workspaceDir = filepath.Clean(workspaceDir)
	resolved = filepath.Clean(resolved)
	return strings.HasPrefix(resolved, workspaceDir+string(filepath.Separator)) ||
		resolved == workspaceDir
}

// isInsideGitDirectory checks if a resolved path is inside a .git directory.
func isInsideGitDirectory(resolved string, workspaceDir string) bool {
	gitSep := string(filepath.Separator) + ".git" + string(filepath.Separator)
	gitSuffix := string(filepath.Separator) + ".git"
	return strings.Contains(resolved, gitSep) ||
		strings.HasSuffix(resolved, gitSuffix)
}

// matchRule evaluates a CommandRule against the given arguments.
func matchRule(rule CommandRule, args []string) ActionType {
	for _, argRule := range rule.ArgRules {
		if matchesArgRule(args, argRule) {
			return argRule.Category
		}
	}
	return rule.DefaultCategory
}

// matchesArgRule checks if args satisfy an ArgRule's conditions.
func matchesArgRule(args []string, rule ArgRule) bool {
	if len(rule.Subcommands) > 0 && len(args) > 0 {
		if !contains(rule.Subcommands, args[0]) {
			return false
		}
	}
	if len(rule.ContainsAny) > 0 {
		if !containsAny(args, rule.ContainsAny) {
			return false
		}
	}
	if len(rule.ContainsAll) > 0 {
		if !containsAll(args, rule.ContainsAll) {
			return false
		}
	}
	return true
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func containsAny(args []string, targets []string) bool {
	for _, target := range targets {
		if contains(args, target) {
			return true
		}
	}
	return false
}

func containsAll(args []string, targets []string) bool {
	for _, target := range targets {
		if !contains(args, target) {
			return false
		}
	}
	return true
}

// MatchParsedCommand checks if a parsed command matches a user's permission grant target.
// It supports wrapper unpacking, wildcard argument matching (e.g. "git *"),
// and subcommand matching (e.g. "git commit").
func MatchParsedCommand(grantTarget string, matchMethod string, cmd ParsedCommand) bool {
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
