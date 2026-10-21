package shellguard

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/guard"
)

var defaultPolicy = DefaultPolicy()

// DefaultPolicy returns the built-in declarative classification and argument
// extraction ruleset.
func DefaultPolicy() Policy {
	p := Policy{
		DefaultCategory: ActionUnknown,
		Rules:           make(map[string]CommandRule),
	}

	// Read-only commands whose positional arguments are file paths (TargetsAll).
	for _, exec := range []string{
		"cat", "fd", "ls", "file", "wc", "sort", "uniq", "stat", "tree", "diff",
	} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionRead,
			TargetMode:      TargetsAll,
		}
	}

	// Read-only commands that take no file path positional arguments (TargetsNone).
	for _, exec := range []string{
		"pwd", "echo", "printf", "which", "true", "false", "sleep",
		"whoami", "uname", "date", "env", "printenv", "type",
	} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionRead,
			TargetMode:      TargetsNone,
		}
	}

	// head / tail: -n and -c consume a count value; remaining positional args are paths.
	for _, exec := range []string{"head", "tail"} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionRead,
			TargetMode:      TargetsAll,
			ValueFlags:      []string{"-n", "-c", "--lines", "--bytes"},
		}
	}

	// grep / rg: first positional arg is search pattern (unless -e/-f is used); rest are paths.
	for _, exec := range []string{"grep", "rg"} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionRead,
			TargetMode:      TargetsSkipFirst,
			ValueFlags: []string{
				"-e", "--regexp", "-m", "--max-count", "-A", "--after-context",
				"-B", "--before-context", "-C", "--context", "--include", "--exclude",
				"--exclude-dir", "--color", "--colour", "--label", "-d", "--directories",
				"--devices", "-g", "--glob", "--iglob", "-t", "--type", "--type-not",
				"--type-add", "-M", "--max-columns", "--max-depth", "--max-filesize",
				"--replace", "-j", "--threads", "--encoding",
			},
			PathFlags: []string{"-f", "--file", "--exclude-from"},
		}
	}

	// jq / yq: first positional arg is filter expression (unless -f is used); rest are paths.
	for _, exec := range []string{"jq", "yq"} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionRead,
			TargetMode:      TargetsSkipFirst,
			PathFlags:       []string{"-f", "--from-file"},
		}
	}

	// find: positional args before the first "-" predicate/operator are search paths.
	p.Rules["find"] = CommandRule{
		DefaultCategory: ActionRead,
		TargetMode:      TargetsPredicateStop,
		PathFlags:       []string{"-f"},
		ArgRules: []ArgRule{
			{
				Category:    ActionDelete,
				ContainsAny: []string{"-delete"},
			},
			{
				Category:    ActionExec,
				ContainsAny: []string{"-exec", "-execdir", "-ok", "-okdir"},
			},
		},
	}

	// sed: first positional arg is script expression (unless -e/-f is used); -i makes it ActionWrite.
	p.Rules["sed"] = CommandRule{
		DefaultCategory: ActionRead,
		TargetMode:      TargetsSkipFirst,
		ValueFlags:      []string{"-e", "--expression"},
		PathFlags:       []string{"-f", "--file"},
		ArgRules: []ArgRule{
			{
				Category:    ActionWrite,
				ContainsAny: []string{"-i", "--in-place"},
			},
		},
	}

	// Write commands whose positional arguments are paths.
	for _, exec := range []string{"mkdir", "touch", "mv", "ln", "tee"} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionWrite,
			TargetMode:      TargetsAll,
		}
	}

	// cp: destination is the last positional argument (or all paths checked).
	p.Rules["cp"] = CommandRule{
		DefaultCategory: ActionWrite,
		TargetMode:      TargetsLast,
	}

	// chmod / chown: first positional arg is mode or owner; remaining positional args are paths.
	for _, exec := range []string{"chmod", "chown"} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionWrite,
			TargetMode:      TargetsSkipFirst,
		}
	}

	// Delete / Destructive commands.
	for _, exec := range []string{"rm", "rmdir", "dd", "mkfs", "sudo", "shred", "unlink"} {
		p.Rules[exec] = CommandRule{
			DefaultCategory: ActionDelete,
			TargetMode:      TargetsAll,
		}
	}

	// Git: declarative subcommand target modes, path flags (-C), value flags (-m, etc.), and action rules.
	p.Rules["git"] = CommandRule{
		DefaultCategory: ActionWrite,
		TargetMode:      TargetsAll,
		PathFlags:       []string{"-C", "--git-dir", "--work-tree", "-F", "--file"},
		ValueFlags: []string{
			"-m", "--message", "-c", "--config-env", "-u", "--set-upstream-to",
			"--author", "--date", "--cleanup", "--format", "--pretty", "--encoding",
			"-n", "--max-count", "--skip", "--depth", "--shallow-since", "--shallow-exclude",
			"-s", "--strategy", "-X", "--strategy-option", "--grep", "--committer",
			"-b", "-B", "--orphan",
		},
		SubcommandModes: map[string]TargetMode{
			"branch":      TargetsAfterDashDash,
			"tag":         TargetsAfterDashDash,
			"switch":      TargetsAfterDashDash,
			"checkout":    TargetsAfterDashDash,
			"merge":       TargetsAfterDashDash,
			"rebase":      TargetsAfterDashDash,
			"cherry-pick": TargetsAfterDashDash,
			"revert":      TargetsAfterDashDash,
			"stash":       TargetsAfterDashDash,
			"fetch":       TargetsAfterDashDash,
			"pull":        TargetsAfterDashDash,
			"push":        TargetsAfterDashDash,
			"remote":      TargetsAfterDashDash,
			"config":      TargetsAfterDashDash,
			"rev-parse":   TargetsAfterDashDash,
			"log":         TargetsAfterDashDash,
			"show":        TargetsAfterDashDash,
		},
		ArgRules: []ArgRule{
			{
				Category:    ActionDelete,
				Subcommands: []string{"rm"},
			},
			{
				Category:    ActionDelete,
				Subcommands: []string{"reset"},
				ContainsAll: []string{"--hard"},
			},
			{
				Category:    ActionDelete,
				Subcommands: []string{"clean"},
				ContainsAny: []string{"-f", "-fd", "-df", "-fx", "-fX", "--force", "--remove-untracked"},
			},
			{
				Category:    ActionDelete,
				Subcommands: []string{"branch"},
				ContainsAny: []string{"-D", "-d", "--delete"},
			},
			{
				Category:    ActionDelete,
				Subcommands: []string{"tag"},
				ContainsAny: []string{"-d", "--delete"},
			},
			{
				Category:    ActionDelete,
				Subcommands: []string{"push"},
				ContainsAny: []string{"--force", "-f", "--force-with-lease"},
			},
			{
				Category:    ActionRead,
				Subcommands: []string{"status", "diff", "log", "show", "branch", "rev-parse", "ls-files", "remote", "tag"},
			},
		},
	}

	return p
}

type trackerState struct {
	virtualCWD   string
	isCWDUnknown bool
}

// Analyze parses the command string and returns operations for permission evaluation.
// If the command chain produces more than g.maxOperations operations or
// contains compound control-flow clauses / multi-statement inline scripts, it
// collapses them into a single summary Operation (IsCompound: true).
func (g *Guard) Analyze(commandStr string) ([]Operation, error) {
	chain, ops, err := g.analyzeChain(commandStr)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return ops, nil
	}
	if len(ops) > g.maxOperations || chain.IsCompound {
		return []Operation{collapseOperations(ops, commandStr)}, nil
	}
	return ops, nil
}

// AnalyzeDetailed parses the command string and returns all individual granular
// operations without compound collapsing.
func (g *Guard) AnalyzeDetailed(commandStr string) ([]Operation, error) {
	_, ops, err := g.analyzeChain(commandStr)
	if err != nil {
		return nil, err
	}
	return ops, nil
}

func (g *Guard) analyzeChain(commandStr string) (CommandChain, []Operation, error) {
	chain, err := Parse(commandStr)
	if err != nil {
		return CommandChain{}, nil, err
	}

	state := &trackerState{
		virtualCWD:   g.workspaceDir,
		isCWDUnknown: false,
	}
	var ops []Operation

	g.analyzePipelines(chain.Pipelines, state, &ops)
	return chain, ops, nil
}

func (g *Guard) analyzePipelines(
	pipelines []Pipeline,
	state *trackerState,
	ops *[]Operation,
) {
	for _, pipeline := range pipelines {
		for _, cmd := range pipeline.Commands {
			// Subshell (...): evaluate against an isolated value copy of trackerState
			// so directory changes inside the subshell never leak to outer commands.
			if cmd.Subshell != nil {
				subState := *state
				g.analyzePipelines(cmd.Subshell.Pipelines, &subState, ops)
				continue
			}

			// Check for literal directory changes (cd)
			if cmd.Executable == "cd" {
				g.applyCdCommand(cmd, state)
				// cd commands are collapsed, emitting 0 operations
				continue
			}

			// Multi-command wrapper invocation (e.g., bash -c "cmd1 && cmd2"):
			// evaluate in an isolated subshell state copy.
			if cmd.Executable != "sudo" && cmd.SubChain != nil && countChainCommands(*cmd.SubChain) > 1 {
				subState := *state
				g.analyzePipelines(cmd.SubChain.Pipelines, &subState, ops)
				continue
			}

			// Skip bare variable assignments without executable, redirects, or command substitutions
			if cmd.Executable == "" && len(cmd.Redirects) == 0 && !cmd.HasCmdSubst && len(cmd.SubstCommands) == 0 {
				continue
			}

			*ops = append(*ops, g.determineOperation(cmd, state))
		}
	}
}

func (g *Guard) applyCdCommand(cmd ParsedCommand, state *trackerState) {
	if len(cmd.Args) > 0 && len(cmd.ArgsIsDynamic) > 0 && cmd.ArgsIsDynamic[0] {
		state.isCWDUnknown = true
		return
	}

	targetDir := "~"
	if len(cmd.Args) > 0 {
		targetDir = cmd.Args[0]
	}

	// Tilde expansion using the Guard's configured homeDir (no OS syscall during evaluation)
	if targetDir == "~" {
		if g.homeDir == "" {
			state.isCWDUnknown = true
			return
		}
		targetDir = g.homeDir
	} else if strings.HasPrefix(targetDir, "~/") {
		if g.homeDir == "" {
			state.isCWDUnknown = true
			return
		}
		targetDir = g.homeDir + targetDir[1:]
	}

	if !state.isCWDUnknown {
		cleanTarget := filepath.Clean(targetDir)
		if strings.HasPrefix(cleanTarget, "/") {
			state.virtualCWD = cleanTarget
		} else {
			state.virtualCWD = filepath.Clean(filepath.Join(state.virtualCWD, targetDir))
		}
	}
}

func collapseOperations(ops []Operation, commandStr string) Operation {
	var primaryExec string
	for _, op := range ops {
		if op.Command != nil && op.Command.Executable != "" {
			primaryExec = op.Command.Executable
			break
		}
	}

	worstAction := ops[0].Action
	worstSafety := ops[0].Safety
	criticalPath := ops[0].Path
	bestSafety := ops[0].Safety
	bestAction := ops[0].Action

	for _, op := range ops[1:] {
		if op.Action.Severity() > worstAction.Severity() {
			worstAction = op.Action
		}
		if op.Safety.Severity() > worstSafety.Severity() {
			worstSafety = op.Safety
		}

		if op.Safety.Severity() > bestSafety.Severity() ||
			(op.Safety.Severity() == bestSafety.Severity() && op.Action.Severity() > bestAction.Severity()) ||
			(criticalPath == "" && op.Path != "") {
			bestSafety = op.Safety
			bestAction = op.Action
			if op.Path != "" {
				criticalPath = op.Path
			}
		}
	}

	return Operation{
		Command: &ParsedCommand{
			Executable: primaryExec,
			Raw:        commandStr,
		},
		Action:     worstAction,
		Safety:     worstSafety,
		Path:       criticalPath,
		CWD:        ops[0].CWD,
		IsCompound: true,
	}
}

func (g *Guard) determineOperation(cmd ParsedCommand, state *trackerState) Operation {
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
	rule, hasRule := g.policy.Rules[effectiveExec]

	// Determine Action
	var action ActionType
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
	} else if hasRule {
		action = matchRule(rule, effectiveArgs)
	} else {
		action = g.policy.DefaultCategory
	}

	// Collect write and input redirections across the wrapper hierarchy
	var writeRedirs []Redirect
	var readRedirs []Redirect
	currRedir := &cmd
	for currRedir != nil {
		for _, redir := range currRedir.Redirects {
			switch redir.Op {
			case ">", ">>", ">|", "&>", "&>>":
				writeRedirs = append(writeRedirs, redir)
			case "<":
				readRedirs = append(readRedirs, redir)
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
		forAction ActionType
	}
	var checks []PathCheck

	redirAction := ActionWrite
	if action == ActionDelete {
		redirAction = ActionDelete
	}
	for _, redir := range writeRedirs {
		checks = append(checks, PathCheck{
			path:      redir.Target,
			isDynamic: redir.IsDynamic,
			forAction: redirAction,
		})
	}

	for _, redir := range readRedirs {
		checks = append(checks, PathCheck{
			path:      redir.Target,
			isDynamic: redir.IsDynamic,
			forAction: ActionRead,
		})
	}

	// Declarative command-aware target extraction
	for _, t := range extractTargets(rule, cmd) {
		checks = append(checks, PathCheck{
			path:      t.Path,
			isDynamic: t.IsDynamic,
			forAction: action,
		})
	}

	var op Operation
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
				pathToCheck = g.resolveTarget(effectiveExec, state.virtualCWD)
			}
			safety = g.evaluatePath(pathToCheck, action)
		}

		pathToReport := state.virtualCWD
		if action == ActionExec && strings.Contains(effectiveExec, "/") {
			pathToReport = g.resolveTarget(effectiveExec, state.virtualCWD)
		}

		op = Operation{
			Command: &cmd,
			Action:  action,
			Safety:  safety,
			Path:    pathToReport,
			CWD:     state.virtualCWD,
		}
	} else {
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
				if !state.isCWDUnknown && !check.isDynamic && check.path != "" {
					resolved = g.resolveTarget(check.path, state.virtualCWD)
				}
			} else if state.isCWDUnknown || check.isDynamic || check.path == "" {
				safety = SafetyUnknown
			} else {
				resolved = g.resolveTarget(check.path, state.virtualCWD)
				safety = g.evaluatePath(resolved, check.forAction)
			}

			if safety.Severity() > worstSafety.Severity() {
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

		op = Operation{
			Command: &cmd,
			Action:  action,
			Safety:  worstSafety,
			Path:    chosenPath,
			CWD:     state.virtualCWD,
		}
	}

	// Evaluate any commands nested inside command substitutions $(...) or `...`
	// against an isolated value copy of trackerState, upgrading Action, Safety,
	// and Path if any substituted command is more severe.
	var substCmds []ParsedCommand
	curr := &cmd
	for curr != nil {
		substCmds = append(substCmds, curr.SubstCommands...)
		curr = curr.SubCommand
	}
	if len(substCmds) > 0 {
		subState := *state
		var substOps []Operation
		g.analyzePipelines([]Pipeline{{Commands: substCmds}}, &subState, &substOps)
		for _, subOp := range substOps {
			if subOp.Action.Severity() > op.Action.Severity() {
				op.Action = subOp.Action
			}
			if subOp.Safety.Severity() > op.Safety.Severity() {
				op.Safety = subOp.Safety
				if subOp.Path != "" {
					op.Path = subOp.Path
				}
			} else if subOp.Safety == op.Safety && subOp.Action.Severity() > action.Severity() && subOp.Path != "" {
				op.Path = subOp.Path
			}
		}
	}

	return op
}

// resolveExecutableAndArgs unpacks wrapper commands recursively.
func resolveExecutableAndArgs(cmd ParsedCommand) (string, []string) {
	curr := &cmd
	for curr.SubCommand != nil {
		curr = curr.SubCommand
	}
	return curr.Executable, curr.Args
}

// extractTargets declaratively extracts target file or directory arguments from
// a command using its CommandRule configuration (TargetMode, ValueFlags, PathFlags,
// and SubcommandModes).
func extractTargets(rule CommandRule, cmd ParsedCommand) []TargetArg {
	if isInlineInterpreterScript(cmd) {
		return nil
	}

	curr := &cmd
	for curr.SubCommand != nil {
		curr = curr.SubCommand
	}
	args := curr.Args
	argsDyn := curr.ArgsIsDynamic
	isDyn := func(i int) bool {
		return i >= 0 && i < len(argsDyn) && argsDyn[i]
	}

	if len(args) == 0 {
		return nil
	}

	// TargetsPredicateStop (e.g. find): collect path arguments before the first
	// "-" predicate or expression operator token.
	if rule.TargetMode == TargetsPredicateStop {
		var targets []TargetArg
		for i := 0; i < len(args); i++ {
			arg := args[i]
			if slices.Contains(rule.PathFlags, arg) && i+1 < len(args) {
				i++
				targets = append(targets, TargetArg{Path: args[i], IsDynamic: isDyn(i)})
				continue
			}
			if strings.HasPrefix(arg, "-") || arg == "(" || arg == ")" || arg == "!" || arg == "," {
				break
			}
			targets = append(targets, TargetArg{Path: arg, IsDynamic: isDyn(i)})
		}
		return targets
	}

	var targets []TargetArg
	var beforeDash []TargetArg
	var afterDash []TargetArg
	hasExplicitPatternOrScript := false
	endOfFlags := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !endOfFlags {
			if arg == "--" {
				endOfFlags = true
				continue
			}
			if matchedPath, nextIdx, ok := matchPathFlag(rule.PathFlags, args, i, isDyn); ok {
				targets = append(targets, matchedPath)
				if isPatternOrScriptFlag(arg) {
					hasExplicitPatternOrScript = true
				}
				i = nextIdx
				continue
			}
			if nextIdx, ok := matchValueFlag(rule.ValueFlags, args, i); ok {
				if isPatternOrScriptFlag(arg) {
					hasExplicitPatternOrScript = true
				}
				i = nextIdx
				continue
			}
			// Handle BSD sed -i '' empty backup suffix
			if arg == "-i" && i+1 < len(args) && args[i+1] == "" {
				i++
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
		}

		if endOfFlags {
			afterDash = append(afterDash, TargetArg{Path: arg, IsDynamic: isDyn(i)})
		} else {
			beforeDash = append(beforeDash, TargetArg{Path: arg, IsDynamic: isDyn(i)})
		}
	}

	mode := rule.TargetMode
	if len(rule.SubcommandModes) > 0 && len(beforeDash) > 0 {
		subcmd := beforeDash[0].Path
		beforeDash = beforeDash[1:]
		if subMode, ok := rule.SubcommandModes[subcmd]; ok {
			mode = subMode
		}
	}

	positional := append(beforeDash, afterDash...)
	switch mode {
	case TargetsNone:
		return targets
	case TargetsAfterDashDash:
		return append(targets, afterDash...)
	case TargetsSkipFirst:
		if !hasExplicitPatternOrScript && len(positional) > 0 {
			return append(targets, positional[1:]...)
		}
		return append(targets, positional...)
	case TargetsLast:
		if len(positional) > 0 {
			return append(targets, positional[len(positional)-1])
		}
		return targets
	default: // TargetsAll
		return append(targets, positional...)
	}
}

func matchPathFlag(pathFlags []string, args []string, idx int, isDyn func(int) bool) (TargetArg, int, bool) {
	arg := args[idx]
	for _, pf := range pathFlags {
		if arg == pf {
			if idx+1 < len(args) {
				return TargetArg{Path: args[idx+1], IsDynamic: isDyn(idx + 1)}, idx + 1, true
			}
			return TargetArg{}, idx, true
		}
		if strings.HasPrefix(pf, "--") {
			if after, ok := strings.CutPrefix(arg, pf+"="); ok {
				return TargetArg{Path: after, IsDynamic: isDyn(idx)}, idx, true
			}
		} else if len(pf) == 2 && pf[0] == '-' && strings.HasPrefix(arg, pf) && len(arg) > 2 {
			return TargetArg{Path: arg[2:], IsDynamic: isDyn(idx)}, idx, true
		}
	}
	return TargetArg{}, idx, false
}

func matchValueFlag(valueFlags []string, args []string, idx int) (int, bool) {
	arg := args[idx]
	for _, vf := range valueFlags {
		if arg == vf {
			return idx + 1, true
		}
		if strings.HasPrefix(vf, "--") && strings.HasPrefix(arg, vf+"=") {
			return idx, true
		}
		// Combined short flag ending in a value-consuming flag char (e.g. git commit -am "msg")
		if len(vf) == 2 && vf[0] == '-' && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") &&
			len(arg) > 2 && strings.HasSuffix(arg, vf[1:]) {
			return idx + 1, true
		}
	}
	return idx, false
}

func isPatternOrScriptFlag(arg string) bool {
	return arg == "-e" || arg == "-f" || arg == "--regexp" || arg == "--expression" ||
		arg == "--file" || arg == "--from-file" ||
		strings.HasPrefix(arg, "--regexp=") || strings.HasPrefix(arg, "--expression=") ||
		strings.HasPrefix(arg, "--file=") || strings.HasPrefix(arg, "--from-file=")
}

// resolveTarget resolves a target against baseDir, expanding ~ with the Guard's homeDir.
func (g *Guard) resolveTarget(target string, baseDir string) string {
	t := strings.TrimSpace(target)
	if g.homeDir != "" {
		if t == "~" {
			return g.homeDir
		}
		if strings.HasPrefix(t, "~/") {
			return filepath.Clean(g.homeDir + t[1:])
		}
	}
	if strings.HasPrefix(t, "/") {
		return filepath.Clean(t)
	}
	return filepath.Clean(filepath.Join(baseDir, t))
}

// IsProtectedPath reports whether targetPath (either workspace-relative or
// absolute within the workspace) resides inside one of the Guard's configured
// protected directories (such as .git, .bare, or .agents).
func (g *Guard) IsProtectedPath(targetPath string) bool {
	return guard.IsProtectedPath(targetPath, g.workspaceDir, g.protectedDirs...)
}

// evaluatePath classifies resolvedPath using guard.EvaluatePath.
func (g *Guard) evaluatePath(resolvedPath string, action ActionType) SafetyLevel {
	_, safety := guard.EvaluatePath(resolvedPath, g.workspaceDir, g.homeDir, action, g.protectedDirs...)
	return safety
}

// matchRule evaluates a CommandRule against the given arguments.
func matchRule(rule CommandRule, args []string) ActionType {
	subcmd := findSubcommand(rule, args)
	for _, argRule := range rule.ArgRules {
		if matchesArgRule(rule, subcmd, args, argRule) {
			return argRule.Category
		}
	}
	return rule.DefaultCategory
}

// matchesArgRule checks if args satisfy an ArgRule's conditions.
func matchesArgRule(rule CommandRule, subcmd string, args []string, argRule ArgRule) bool {
	if len(argRule.Subcommands) > 0 {
		if subcmd == "" || !slices.Contains(argRule.Subcommands, subcmd) {
			return false
		}
	}
	if len(argRule.ContainsAny) > 0 {
		if !containsAny(args, argRule.ContainsAny) {
			return false
		}
	}
	if len(argRule.ContainsAll) > 0 {
		if !containsAll(args, argRule.ContainsAll) {
			return false
		}
	}
	return true
}

func findSubcommand(rule CommandRule, args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if slices.Contains(rule.PathFlags, arg) || slices.Contains(rule.ValueFlags, arg) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func containsAny(args []string, targets []string) bool {
	for _, target := range targets {
		if slices.Contains(args, target) {
			return true
		}
		// Support attached short/long flags such as sed -i.bak / --in-place=.bak
		if target == "-i" || target == "--in-place" {
			for _, arg := range args {
				if strings.HasPrefix(arg, "-i") || strings.HasPrefix(arg, "--in-place=") {
					return true
				}
			}
		}
	}
	return false
}

func containsAll(args []string, targets []string) bool {
	for _, target := range targets {
		if !slices.Contains(args, target) {
			return false
		}
	}
	return true
}

// Match checks if a parsed command matches a user's permission grant target.
// It supports wrapper unpacking, wildcard argument matching (e.g. "git *"),
// and subcommand matching (e.g. "git commit").
func (g *Guard) Match(grantTarget string, matchMethod string, cmd ParsedCommand) bool {
	grantTarget = strings.TrimSpace(grantTarget)
	if grantTarget == "" {
		return false
	}
	if grantTarget == "*" {
		return true
	}

	// If cmd is a collapsed compound command with Raw set, support exact matching on Raw.
	if cmd.Raw != "" && matchMethod == "exact" && strings.TrimSpace(cmd.Raw) == grantTarget {
		return true
	}

	// Resolve the effective executable after wrapper unpacking.
	effectiveExec, effectiveArgs := resolveExecutableAndArgs(cmd)

	// Parse the grant target.
	parts := strings.Fields(grantTarget)
	if len(parts) == 0 {
		return false
	}

	grantExec := parts[0]

	// Match the executable.
	if effectiveExec != grantExec {
		return false
	}

	// If only executable in grant (e.g. "git"), match is confirmed.
	if len(parts) == 1 {
		return true
	}

	// Wildcard support: "git *" matches any git command.
	if len(parts) == 2 && parts[1] == "*" {
		return true
	}

	// Subcommand prefix matching: grant is "git commit", args must start with ["commit"]
	// (or when global flags precede the subcommand, match the resolved subcommand).
	grantArgs := parts[1:]
	if len(grantArgs) > 0 {
		if len(effectiveArgs) < len(grantArgs) {
			return false
		}
		if !slices.Equal(effectiveArgs[:len(grantArgs)], grantArgs) {
			if rule, ok := g.policy.Rules[effectiveExec]; ok && len(grantArgs) == 1 && findSubcommand(rule, effectiveArgs) == grantArgs[0] {
				return true
			}
			return false
		}
	}

	return true
}
