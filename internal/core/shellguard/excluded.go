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
	for _, r := range stmt.Redirs {
		if !safeExcludedRedirect(r) {
			return false
		}
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
				switch ip := inner.(type) {
				case *syntax.Lit:
					sb.WriteString(ip.Value)
				case *syntax.CmdSubst:
					lit, ok := staticCatHeredocLiteral(ip)
					if !ok {
						return "", "", false
					}
					sb.WriteString(lit)
				default:
					return "", "", false
				}
			}
		default:
			return "", "", false
		}
	}
	return sb.String(), unq.String(), true
}

// safeExcludedRedirect reports whether a redirection cannot touch host files or
// alter argv: static stdin heredocs/herestrings, standard descriptor
// duplications between fds 0–2, and redirects to /dev/null.
func safeExcludedRedirect(r *syntax.Redirect) bool {
	if r == nil {
		return false
	}
	fd := ""
	if r.N != nil {
		fd = r.N.Value
	}
	isStdFD := fd == "" || fd == "0" || fd == "1" || fd == "2"
	switch r.Op {
	case syntax.Hdoc, syntax.DashHdoc:
		if fd != "" && fd != "0" {
			return false
		}
		_, ok := staticHeredocBody(r)
		return ok
	case syntax.WordHdoc:
		if fd != "" && fd != "0" {
			return false
		}
		_, unq, ok := staticWordLiteral(r.Word)
		return ok && !strings.ContainsAny(unq, "*?[")
	case syntax.DplOut, syntax.DplIn:
		if !isStdFD {
			return false
		}
		lit, unq, ok := staticWordLiteral(r.Word)
		return ok && !strings.ContainsAny(unq, "*?[") && (lit == "0" || lit == "1" || lit == "2" || lit == "-")
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll, syntax.RdrIn:
		if !isStdFD {
			return false
		}
		lit, unq, ok := staticWordLiteral(r.Word)
		return ok && lit == "/dev/null" && !strings.ContainsAny(unq, "*?[")
	default:
		return false
	}
}

// staticCatHeredocLiteral folds the pure multi-line string idiom
// $(cat <<'EOF' ... EOF) to its literal text when cat receives no arguments,
// environment assignments, or extra redirects and the heredoc body is static.
func staticCatHeredocLiteral(cs *syntax.CmdSubst) (string, bool) {
	if cs == nil || cs.TempFile || cs.ReplyVar || len(cs.Stmts) != 1 {
		return "", false
	}
	stmt := cs.Stmts[0]
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) != 1 {
		return "", false
	}
	r := stmt.Redirs[0]
	if r == nil || (r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc) || (r.N != nil && r.N.Value != "0") {
		return "", false
	}
	body, ok := staticHeredocBody(r)
	if !ok {
		return "", false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || len(call.Args) != 1 {
		return "", false
	}
	lit, unq, ok := staticWordLiteral(call.Args[0])
	if !ok || lit != "cat" || strings.ContainsAny(unq, "*?[") {
		return "", false
	}
	return strings.TrimRight(body, "\n"), true
}

// staticHeredocBody returns the exact literal body of a heredoc when no
// runtime parameter, command, arithmetic, or backslash expansion can occur.
func staticHeredocBody(r *syntax.Redirect) (string, bool) {
	if r == nil || r.Word == nil || r.Hdoc == nil || (r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc) {
		return "", false
	}
	quotedDelim, ok := isStaticHeredocDelim(r.Word)
	if !ok {
		return "", false
	}
	var sb strings.Builder
	for _, part := range r.Hdoc.Parts {
		lit, isLit := part.(*syntax.Lit)
		if !isLit {
			return "", false
		}
		if !quotedDelim && strings.Contains(lit.Value, "\\") {
			return "", false
		}
		sb.WriteString(lit.Value)
	}
	body := sb.String()
	if r.Op == syntax.DashHdoc {
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimLeft(line, "\t")
		}
		body = strings.Join(lines, "\n")
	}
	return body, true
}

func isStaticHeredocDelim(word *syntax.Word) (quoted bool, ok bool) {
	if word == nil || len(word.Parts) == 0 {
		return false, false
	}
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			if strings.Contains(p.Value, "\\") {
				quoted = true
			}
		case *syntax.SglQuoted:
			if p.Dollar {
				return false, false
			}
			quoted = true
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				if _, isLit := inner.(*syntax.Lit); !isLit {
					return false, false
				}
			}
			quoted = true
		default:
			return false, false
		}
	}
	return quoted, true
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
