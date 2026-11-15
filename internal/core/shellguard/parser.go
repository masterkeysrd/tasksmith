package shellguard

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Parse parses a raw shell command string into a CommandChain.
func Parse(commandStr string) (CommandChain, error) {
	parser := syntax.NewParser()
	ast, err := parser.Parse(strings.NewReader(commandStr), "")
	if err != nil {
		return CommandChain{}, fmt.Errorf("parse shell syntax: %w", err)
	}
	return parseStmts(ast.Stmts)
}

func parseStmts(stmts []*syntax.Stmt) (CommandChain, error) {
	var chain CommandChain
	for _, stmt := range stmts {
		subChain, err := parseStmt(stmt)
		if err != nil {
			return CommandChain{}, err
		}
		chain.Pipelines = append(chain.Pipelines, subChain.Pipelines...)
		if subChain.IsCompound {
			chain.IsCompound = true
		}
	}
	return chain, nil
}

func parseStmt(stmt *syntax.Stmt) (CommandChain, error) {
	if stmt == nil {
		return CommandChain{}, nil
	}

	var chain CommandChain
	var err error
	if stmt.Cmd != nil {
		chain, err = parseCmd(stmt.Cmd)
		if err != nil {
			return CommandChain{}, err
		}
	} else if len(stmt.Redirs) > 0 {
		// Bare redirection statement (e.g., "> file.txt")
		chain = CommandChain{
			Pipelines: []Pipeline{{
				Commands: []ParsedCommand{{}},
			}},
		}
	} else {
		return CommandChain{}, nil
	}

	// Attach redirects from the Stmt to the first command in the chain, and
	// extract any command substitutions embedded in redirect targets or heredocs.
	for _, redir := range stmt.Redirs {
		parsedRedir := parseRedirect(redir)
		substCmds, substCompound := extractSubstFromRedirect(redir)
		if substCompound {
			chain.IsCompound = true
		}
		if len(chain.Pipelines) > 0 && len(chain.Pipelines[0].Commands) > 0 {
			cmd := &chain.Pipelines[0].Commands[0]
			cmd.Redirects = append(cmd.Redirects, parsedRedir)
			if len(substCmds) > 0 {
				cmd.HasCmdSubst = true
				cmd.SubstCommands = append(cmd.SubstCommands, substCmds...)
			}
		}
	}

	// If the statement is backgrounded, add & operator.
	if stmt.Background && len(chain.Pipelines) > 0 {
		chain.Pipelines[len(chain.Pipelines)-1].Operator = "&"
	}

	return chain, nil
}

func parseCmd(cmd syntax.Command) (CommandChain, error) {
	switch n := cmd.(type) {
	case *syntax.CallExpr:
		pcmd, isCompound, err := parseCallExpr(n)
		if err != nil {
			return CommandChain{}, err
		}
		return CommandChain{
			Pipelines: []Pipeline{{
				Commands: []ParsedCommand{pcmd},
				Operator: "",
			}},
			IsCompound: isCompound,
		}, nil

	case *syntax.BinaryCmd:
		left, err := parseStmt(n.X)
		if err != nil {
			return CommandChain{}, err
		}
		right, err := parseStmt(n.Y)
		if err != nil {
			return CommandChain{}, err
		}
		isCompound := left.IsCompound || right.IsCompound
		op := n.Op.String()

		if len(left.Pipelines) > 0 && len(right.Pipelines) > 0 {
			last := len(left.Pipelines) - 1
			left.Pipelines[last].Commands = append(
				left.Pipelines[last].Commands,
				right.Pipelines[0].Commands...,
			)
			// Last operator wins.
			if right.Pipelines[0].Operator != "" {
				left.Pipelines[last].Operator = right.Pipelines[0].Operator
			} else {
				left.Pipelines[last].Operator = op
			}
			if len(right.Pipelines) > 1 {
				left.Pipelines = append(left.Pipelines, right.Pipelines[1:]...)
			}
			left.IsCompound = isCompound
			return left, nil
		}
		if len(left.Pipelines) > 0 {
			left.IsCompound = isCompound
			return left, nil
		}
		if len(right.Pipelines) > 0 {
			right.IsCompound = isCompound
			return right, nil
		}
		return CommandChain{IsCompound: isCompound}, nil

	case *syntax.Subshell:
		subChain, err := parseStmts(n.Stmts)
		if err != nil {
			return CommandChain{}, err
		}
		return CommandChain{
			Pipelines: []Pipeline{{
				Commands: []ParsedCommand{{
					Subshell: &subChain,
				}},
			}},
			IsCompound: subChain.IsCompound,
		}, nil

	case *syntax.Block:
		return parseStmts(n.Stmts)

	case *syntax.IfClause:
		return parseIfClause(n)

	case *syntax.ForClause:
		var chain CommandChain
		chain.IsCompound = true
		if wordIter, ok := n.Loop.(*syntax.WordIter); ok {
			for _, w := range wordIter.Items {
				substCmds, _ := extractSubstFromNode(w)
				if len(substCmds) > 0 {
					chain.Pipelines = append(chain.Pipelines, Pipeline{Commands: substCmds})
				}
			}
		}
		bodyChain, err := parseStmts(n.Do)
		if err != nil {
			return CommandChain{}, err
		}
		chain.Pipelines = append(chain.Pipelines, bodyChain.Pipelines...)
		return chain, nil

	case *syntax.WhileClause:
		var chain CommandChain
		chain.IsCompound = true
		condChain, err := parseStmts(n.Cond)
		if err != nil {
			return CommandChain{}, err
		}
		doChain, err := parseStmts(n.Do)
		if err != nil {
			return CommandChain{}, err
		}
		chain.Pipelines = append(chain.Pipelines, condChain.Pipelines...)
		chain.Pipelines = append(chain.Pipelines, doChain.Pipelines...)
		return chain, nil

	case *syntax.CaseClause:
		var chain CommandChain
		chain.IsCompound = true
		if n.Word != nil {
			substCmds, _ := extractSubstFromNode(n.Word)
			if len(substCmds) > 0 {
				chain.Pipelines = append(chain.Pipelines, Pipeline{Commands: substCmds})
			}
		}
		for _, item := range n.Items {
			for _, pat := range item.Patterns {
				substCmds, _ := extractSubstFromNode(pat)
				if len(substCmds) > 0 {
					chain.Pipelines = append(chain.Pipelines, Pipeline{Commands: substCmds})
				}
			}
			itemChain, err := parseStmts(item.Stmts)
			if err != nil {
				return CommandChain{}, err
			}
			chain.Pipelines = append(chain.Pipelines, itemChain.Pipelines...)
		}
		return chain, nil

	case *syntax.FuncDecl:
		var chain CommandChain
		chain.IsCompound = true
		if n.Body != nil {
			bodyChain, err := parseStmt(n.Body)
			if err != nil {
				return CommandChain{}, err
			}
			chain.Pipelines = append(chain.Pipelines, bodyChain.Pipelines...)
		}
		return chain, nil

	case *syntax.TimeClause:
		if n.Stmt != nil {
			return parseStmt(n.Stmt)
		}
		return CommandChain{}, nil

	case *syntax.CoprocClause:
		if n.Stmt != nil {
			return parseStmt(n.Stmt)
		}
		return CommandChain{}, nil

	case *syntax.DeclClause:
		var substCmds []ParsedCommand
		var isCompound bool
		for _, assign := range n.Args {
			cmds, comp := extractSubstFromNode(assign)
			substCmds = append(substCmds, cmds...)
			if comp {
				isCompound = true
			}
		}
		if len(substCmds) > 0 {
			return CommandChain{
				Pipelines:  []Pipeline{{Commands: substCmds}},
				IsCompound: isCompound,
			}, nil
		}
		return CommandChain{}, nil

	case *syntax.TestClause, *syntax.ArithmCmd, *syntax.LetClause:
		substCmds, isCompound := extractSubstFromNode(n)
		if len(substCmds) > 0 {
			return CommandChain{
				Pipelines:  []Pipeline{{Commands: substCmds}},
				IsCompound: isCompound,
			}, nil
		}
		return CommandChain{}, nil

	default:
		return CommandChain{}, nil
	}
}

func parseIfClause(n *syntax.IfClause) (CommandChain, error) {
	var chain CommandChain
	chain.IsCompound = true
	if n == nil {
		return chain, nil
	}
	condChain, err := parseStmts(n.Cond)
	if err != nil {
		return CommandChain{}, err
	}
	thenChain, err := parseStmts(n.Then)
	if err != nil {
		return CommandChain{}, err
	}
	chain.Pipelines = append(chain.Pipelines, condChain.Pipelines...)
	chain.Pipelines = append(chain.Pipelines, thenChain.Pipelines...)
	if n.Else != nil {
		elseChain, err := parseIfClause(n.Else)
		if err != nil {
			return CommandChain{}, err
		}
		chain.Pipelines = append(chain.Pipelines, elseChain.Pipelines...)
	}
	return chain, nil
}

func parseCallExpr(node *syntax.CallExpr) (ParsedCommand, bool, error) {
	var pcmd ParsedCommand
	var isCompound bool

	// Collect environment variable assignments.
	for _, assign := range node.Assigns {
		if assign == nil || assign.Name == nil {
			continue
		}
		name := assign.Name.Value
		var value string
		if assign.Value != nil {
			value = wordString(assign.Value)
			if hasCmdSubst(assign.Value) {
				pcmd.HasCmdSubst = true
			}
		}
		substCmds, comp := extractSubstFromNode(assign)
		if len(substCmds) > 0 {
			pcmd.HasCmdSubst = true
			pcmd.SubstCommands = append(pcmd.SubstCommands, substCmds...)
		}
		if comp {
			isCompound = true
		}
		pcmd.Env = append(pcmd.Env, EnvVar{Name: name, Value: value})
	}

	// The first non-assign argument is the executable.
	if len(node.Args) == 0 {
		return pcmd, isCompound, nil
	}
	pcmd.Executable = wordString(node.Args[0])
	if hasCmdSubst(node.Args[0]) {
		pcmd.HasCmdSubst = true
	}
	if substCmds, comp := extractSubstFromNode(node.Args[0]); len(substCmds) > 0 {
		pcmd.HasCmdSubst = true
		pcmd.SubstCommands = append(pcmd.SubstCommands, substCmds...)
		if comp {
			isCompound = true
		}
	}

	// Collect remaining arguments.
	for _, word := range node.Args[1:] {
		if word != nil {
			pcmd.Args = append(pcmd.Args, wordString(word))
			pcmd.ArgsIsDynamic = append(pcmd.ArgsIsDynamic, hasVariableOrCmdSubst(word))
			if hasCmdSubst(word) {
				pcmd.HasCmdSubst = true
			}
			substCmds, comp := extractSubstFromNode(word)
			if len(substCmds) > 0 {
				pcmd.HasCmdSubst = true
				pcmd.SubstCommands = append(pcmd.SubstCommands, substCmds...)
			}
			if comp {
				isCompound = true
			}
		}
	}

	// Wrapper resolution for sudo, npx, bash -c, sh -c.
	switch pcmd.Executable {
	case "sudo", "npx":
		if len(node.Args) > 1 {
			subStr := wordString(node.Args[1])
			for _, word := range node.Args[2:] {
				if word != nil {
					subStr = subStr + " " + wordString(word)
				}
			}
			subChain, err := Parse(subStr)
			if err != nil {
				return pcmd, isCompound, fmt.Errorf("parse subcommand for %s: %w", pcmd.Executable, err)
			}
			if subChain.IsCompound {
				isCompound = true
			}
			if len(subChain.Pipelines) > 0 && len(subChain.Pipelines[0].Commands) > 0 {
				pcmd.SubCommand = &subChain.Pipelines[0].Commands[0]
			}
			if countChainCommands(subChain) > 1 || subChain.IsCompound {
				pcmd.SubChain = &subChain
			}
		}

	case "bash", "sh", "zsh":
		for i, word := range node.Args {
			if word != nil && wordString(word) == "-c" && i+1 < len(node.Args) {
				subStr := wordString(node.Args[i+1])
				subChain, err := Parse(subStr)
				if err != nil {
					return pcmd, isCompound, fmt.Errorf("parse subcommand for %s -c: %w", pcmd.Executable, err)
				}
				cmdCount := countChainCommands(subChain)
				if subChain.IsCompound || cmdCount > 1 {
					isCompound = true
					pcmd.SubChain = &subChain
				}
				if len(subChain.Pipelines) > 0 && len(subChain.Pipelines[0].Commands) > 0 {
					pcmd.SubCommand = &subChain.Pipelines[0].Commands[0]
				}
				break
			}
		}
	}

	if isInlineInterpreterScript(pcmd) {
		isCompound = true
	}

	return pcmd, isCompound, nil
}

func countChainCommands(chain CommandChain) int {
	count := 0
	for _, p := range chain.Pipelines {
		for _, c := range p.Commands {
			if c.Subshell != nil {
				count += countChainCommands(*c.Subshell)
			} else {
				count++
			}
		}
	}
	return count
}

// isInlineInterpreterScript checks if the command invokes an inline script
// interpreter such as python -c, python3 -c, node -e, ruby -e, or perl -e.
func isInlineInterpreterScript(cmd ParsedCommand) bool {
	exec, args := resolveExecutableAndArgs(cmd)
	switch exec {
	case "python", "python2", "python3":
		return slices.Contains(args, "-c")
	case "node", "nodejs", "bun", "deno":
		return slices.Contains(args, "-e") || slices.Contains(args, "--eval")
	case "ruby", "perl":
		return slices.Contains(args, "-e")
	}
	return false
}

// extractSubstFromNode walks a syntax.Node and extracts all commands inside
// command substitutions $(...) / `...` and process substitutions <(...) / >(...).
func extractSubstFromNode(node syntax.Node) ([]ParsedCommand, bool) {
	if node == nil {
		return nil, false
	}
	var cmds []ParsedCommand
	var isCompound bool

	syntax.Walk(node, func(n syntax.Node) bool {
		if n == nil {
			return false
		}
		switch sub := n.(type) {
		case *syntax.CmdSubst:
			subChain, err := parseStmts(sub.Stmts)
			if err == nil {
				if subChain.IsCompound {
					isCompound = true
				}
				for _, p := range subChain.Pipelines {
					cmds = append(cmds, p.Commands...)
				}
			}
			return false
		case *syntax.ProcSubst:
			subChain, err := parseStmts(sub.Stmts)
			if err == nil {
				if subChain.IsCompound {
					isCompound = true
				}
				for _, p := range subChain.Pipelines {
					cmds = append(cmds, p.Commands...)
				}
			}
			return false
		}
		return true
	})

	return cmds, isCompound
}

func extractSubstFromRedirect(redir *syntax.Redirect) ([]ParsedCommand, bool) {
	if redir == nil {
		return nil, false
	}
	var cmds []ParsedCommand
	var isCompound bool
	if redir.Word != nil {
		c, comp := extractSubstFromNode(redir.Word)
		cmds = append(cmds, c...)
		if comp {
			isCompound = true
		}
	}
	if redir.Hdoc != nil {
		c, comp := extractSubstFromNode(redir.Hdoc)
		cmds = append(cmds, c...)
		if comp {
			isCompound = true
		}
	}
	return cmds, isCompound
}

// wordString extracts the literal string from a Word, handling DblQuoted, SglQuoted, etc.
func wordString(word *syntax.Word) string {
	if word == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, qp := range p.Parts {
				if lit, ok := qp.(*syntax.Lit); ok {
					b.WriteString(lit.Value)
				}
			}
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		default:
			// For other types (ParamExp, CmdSubst, etc.), skip.
		}
	}
	return b.String()
}

// parseRedirect converts a syntax.Redirect into our Redirect type.
func parseRedirect(redir *syntax.Redirect) Redirect {
	if redir == nil {
		return Redirect{}
	}
	op := redir.Op.String()
	target := ""
	var isDynamic bool
	if redir.Word != nil {
		target = wordString(redir.Word)
		isDynamic = hasVariableOrCmdSubst(redir.Word)
	}
	fd := 1 // default stdout
	if redir.N != nil {
		fd, _ = strconv.Atoi(redir.N.Value)
	}
	return Redirect{Op: op, Fd: fd, Target: target, IsDynamic: isDynamic}
}

func hasCmdSubst(word *syntax.Word) bool {
	if word == nil {
		return false
	}
	found := false
	syntax.Walk(word, func(n syntax.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			found = true
			return false
		}
		return true
	})
	return found
}

func hasVariableOrCmdSubst(word *syntax.Word) bool {
	if word == nil {
		return false
	}
	found := false
	syntax.Walk(word, func(n syntax.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ProcSubst, *syntax.ArithmExp:
			found = true
			return false
		}
		return true
	})
	return found
}
