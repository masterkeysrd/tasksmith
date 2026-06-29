package shellguard

import (
	"fmt"
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

	var chain CommandChain
	for _, stmt := range ast.Stmts {
		subChain, err := parseStmt(stmt)
		if err != nil {
			return CommandChain{}, err
		}
		chain.Pipelines = append(chain.Pipelines, subChain.Pipelines...)
	}
	return chain, nil
}

func parseStmt(stmt *syntax.Stmt) (CommandChain, error) {
	if stmt.Cmd == nil {
		return CommandChain{Pipelines: nil}, nil
	}
	chain, err := parseCmd(stmt.Cmd)
	if err != nil {
		return CommandChain{}, err
	}
	// Attach redirects from the Stmt to the first command.
	for _, redir := range stmt.Redirs {
		if len(chain.Pipelines) > 0 && len(chain.Pipelines[0].Commands) > 0 {
			chain.Pipelines[0].Commands[0].Redirects = append(
				chain.Pipelines[0].Commands[0].Redirects,
				parseRedirect(redir),
			)
		}
	}
	// If the statement is backgrounded, add & operator.
	if stmt.Background {
		if len(chain.Pipelines) > 0 {
			chain.Pipelines[len(chain.Pipelines)-1].Operator = "&"
		}
	}
	return chain, nil
}

func parseCmd(cmd syntax.Command) (CommandChain, error) {
	switch n := cmd.(type) {
	case *syntax.CallExpr:
		pcmd, err := parseCallExpr(n)
		if err != nil {
			return CommandChain{}, err
		}
		return CommandChain{Pipelines: []Pipeline{{
			Commands: []ParsedCommand{pcmd},
			Operator: "",
		}}}, nil

	case *syntax.BinaryCmd:
		left, err := parseStmt(n.X)
		if err != nil {
			return CommandChain{}, err
		}
		right, err := parseStmt(n.Y)
		if err != nil {
			return CommandChain{}, err
		}
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
			return left, nil
		}
		return CommandChain{Pipelines: nil}, nil

	default:
		return CommandChain{Pipelines: nil}, nil
	}
}

func parseCallExpr(node *syntax.CallExpr) (ParsedCommand, error) {
	var pcmd ParsedCommand

	// Collect environment variable assignments.
	for _, assign := range node.Assigns {
		if assign == nil || assign.Name == nil {
			continue
		}
		name := assign.Name.Value
		var value string
		if assign.Value != nil {
			value = assign.Value.Lit()
			if hasCmdSubst(assign.Value) {
				pcmd.HasCmdSubst = true
			}
		}
		pcmd.Env = append(pcmd.Env, EnvVar{Name: name, Value: value})
	}

	// The first non-assign argument is the executable.
	if len(node.Args) == 0 {
		return pcmd, nil
	}
	pcmd.Executable = node.Args[0].Lit()
	if hasCmdSubst(node.Args[0]) {
		pcmd.HasCmdSubst = true
	}

	// Collect remaining arguments.
	for _, word := range node.Args[1:] {
		if word != nil {
			pcmd.Args = append(pcmd.Args, wordString(word))
			pcmd.ArgsIsDynamic = append(pcmd.ArgsIsDynamic, hasVariableOrCmdSubst(word))
			if hasCmdSubst(word) {
				pcmd.HasCmdSubst = true
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
				return pcmd, fmt.Errorf("parse subcommand for %s: %w", pcmd.Executable, err)
			}
			if len(subChain.Pipelines) > 0 && len(subChain.Pipelines[0].Commands) > 0 {
				pcmd.SubCommand = &subChain.Pipelines[0].Commands[0]
			}
		}

	case "bash", "sh":
		for i, word := range node.Args {
			if word != nil && word.Lit() == "-c" && i+1 < len(node.Args) {
				subStr := wordString(node.Args[i+1])
				subChain, err := Parse(subStr)
				if err != nil {
					return pcmd, fmt.Errorf("parse subcommand for %s -c: %w", pcmd.Executable, err)
				}
				if len(subChain.Pipelines) > 0 && len(subChain.Pipelines[0].Commands) > 0 {
					pcmd.SubCommand = &subChain.Pipelines[0].Commands[0]
				}
				break
			}
		}
	}

	return pcmd, nil
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
		target = redir.Word.Lit()
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
	var walk func(parts []syntax.WordPart) bool
	walk = func(parts []syntax.WordPart) bool {
		for _, part := range parts {
			switch p := part.(type) {
			case *syntax.CmdSubst:
				return true
			case *syntax.DblQuoted:
				if walk(p.Parts) {
					return true
				}
			}
		}
		return false
	}
	return walk(word.Parts)
}

func hasVariableOrCmdSubst(word *syntax.Word) bool {
	if word == nil {
		return false
	}
	var walk func(parts []syntax.WordPart) bool
	walk = func(parts []syntax.WordPart) bool {
		for _, part := range parts {
			switch p := part.(type) {
			case *syntax.ParamExp, *syntax.CmdSubst:
				return true
			case *syntax.DblQuoted:
				if walk(p.Parts) {
					return true
				}
			}
		}
		return false
	}
	return walk(word.Parts)
}
