package shellguard

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// excludedWrapperBuiltins are commands that only change the execution context
// or re-inject a program under a different identity (environment, privilege,
// interpreter). An excluded pattern matching one of these would silently
// extend to whatever it wraps, so commands invoking them are never excluded.
var excludedWrapperBuiltins = map[string]bool{
	"cd": true, "eval": true, "exec": true, "export": true, "set": true,
	"unset": true, "alias": true, "source": true, ".": true, "command": true,
	"env": true, "builtin": true, "sh": true, "bash": true, "zsh": true,
	"dash": true, "ksh": true, "sudo": true, "doas": true, "nohup": true,
	"time": true, "xargs": true,
}

// MatchExcludedCommand reports whether an entire shell command is exempt from
// kernel sandboxing because every simple command in its syntax tree matches at
// least one excludedCommands pattern (e.g. "git commit *", "docker compose *").
//
// It walks the raw mvdan.cc/sh syntax tree rather than the lossy ParsedCommand
// projection, and is deliberately strict: any statement form the projection
// could drop or flatten (decl/test/arith clauses, coprocesses, negation),
// background or disowned statements, redirects, subshells or other compounds,
// environment assignment prefixes, and any word with a non-literal part
// (parameter expansion, command substitution, globs, backticks — including the
// executable itself) disqualifies the whole command and keeps it sandboxed.
// Wrapper and context builtins (env, sudo, exec, source, command, sh, …) are
// rejected outright. A pattern is either an exact command line or a
// "<prefix> *" wildcard; matching is token-by-token against the static argv,
// so quoted arguments can never forge pattern boundaries.
func MatchExcludedCommand(commandStr string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	parsed, err := syntax.NewParser().Parse(strings.NewReader(commandStr), "")
	if err != nil || parsed == nil || len(parsed.Stmts) == 0 {
		return false
	}
	for _, stmt := range parsed.Stmts {
		if !stmtMatchesExcluded(stmt, patterns) {
			return false
		}
	}
	return true
}

func stmtMatchesExcluded(stmt *syntax.Stmt, patterns []string) bool {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return false
	}
	if len(stmt.Redirs) > 0 {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		// Only sequential/pipeline composition is accepted. PipeAll (|&)
		// spawns the left side as a coprocess and is rejected.
		if cmd.Op != syntax.AndStmt && cmd.Op != syntax.OrStmt && cmd.Op != syntax.Pipe {
			return false
		}
		return stmtMatchesExcluded(cmd.X, patterns) && stmtMatchesExcluded(cmd.Y, patterns)
	case *syntax.CallExpr:
		return callMatchesExcluded(cmd, patterns)
	default:
		// Any other command form (subshell, block, if/for/while, decl, test,
		// arith, assignment-only, heredoc-bound constructs, …) is excluded
		// from exemption.
		return false
	}
}

func callMatchesExcluded(call *syntax.CallExpr, patterns []string) bool {
	if len(call.Assigns) > 0 || len(call.Args) == 0 {
		return false
	}
	argv := make([]string, 0, len(call.Args))
	for _, word := range call.Args {
		lit, unq, ok := staticWordLiteral(word)
		if !ok || lit == "" {
			return false
		}
		// An unquoted glob character would let the shell expand the word to
		// different tokens at execution time.
		if strings.ContainsAny(unq, "*?[") {
			return false
		}
		argv = append(argv, lit)
	}
	// A qualified path (e.g. /usr/bin/env) wraps under the same identity.
	if excludedWrapperBuiltins[filepath.Base(argv[0])] {
		return false
	}
	for _, pattern := range patterns {
		if argvMatchesExcludedPattern(argv, pattern) {
			return true
		}
	}
	return false
}

// staticWordLiteral returns the fully literal value of a word plus the
// portion of it that appeared unquoted, or false when any part is dynamic
// (parameter expansion, command substitution, arithmetic, backticks, process
// substitution, …).
func staticWordLiteral(word *syntax.Word) (value string, unquotedText string, ok bool) {
	if word == nil {
		return "", "", false
	}
	var sb strings.Builder
	var unq strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
			unq.WriteString(p.Value)
		case *syntax.SglQuoted:
			// ANSI-C quoting ($'...') processes backslash escapes at
			// execution time, so the literal text is not what runs.
			if p.Dollar {
				return "", "", false
			}
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, isLit := inner.(*syntax.Lit)
				if !isLit {
					return "", "", false
				}
				sb.WriteString(lit.Value)
			}
		default:
			return "", "", false
		}
	}
	return sb.String(), unq.String(), true
}

// argvMatchesExcludedPattern matches the static argv token-by-token. A
// "<prefix> *" pattern matches the token sequence of <prefix> followed by zero
// or more additional tokens; any other pattern must equal the full token
// sequence. Patterns containing shell metacharacters or quotes are rejected as
// ambiguous.
func argvMatchesExcludedPattern(argv []string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	prefix := pattern
	wildcard := false
	if p, ok := strings.CutSuffix(pattern, " *"); ok {
		prefix = strings.TrimSpace(p)
		wildcard = true
	}
	if prefix == "" {
		return false
	}
	if strings.ContainsAny(prefix, "\"'`|&;()<>$\n\\") {
		return false
	}
	parts := strings.Fields(prefix)
	if len(parts) == 0 || len(argv) < len(parts) {
		return false
	}
	for i, part := range parts {
		if argv[i] != part {
			return false
		}
	}
	if !wildcard && len(argv) != len(parts) {
		return false
	}
	return true
}
