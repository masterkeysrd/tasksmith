package shellguard

import "testing"

func TestMatchExcludedCommand(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		patterns []string
		want     bool
	}{
		{
			name:     "wildcard prefix with args",
			command:  "git commit -m release",
			patterns: []string{"git commit *"},
			want:     true,
		},
		{
			name:     "wildcard prefix without args",
			command:  "git commit",
			patterns: []string{"git commit *"},
			want:     true,
		},
		{
			name:     "wildcard rejects sibling command",
			command:  "git commit-tree x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "exact pattern requires equality",
			command:  "git status",
			patterns: []string{"git status"},
			want:     true,
		},
		{
			name:     "exact pattern rejects extra args",
			command:  "git status --porcelain",
			patterns: []string{"git status"},
			want:     false,
		},
		{
			name:     "chained commands all match",
			command:  "git commit -m x && git push origin main",
			patterns: []string{"git commit *", "git push *"},
			want:     true,
		},
		{
			name:     "chained commands one unmatched",
			command:  "git commit -m x && rm -rf build",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "piped commands all match",
			command:  "git log --oneline | git apply",
			patterns: []string{"git log *", "git apply *"},
			want:     true,
		},
		{
			name:     "output redirect disqualifies",
			command:  "git commit -m x > out.txt",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "input redirect disqualifies",
			command:  "git apply < patch",
			patterns: []string{"git apply *"},
			want:     false,
		},
		{
			name:     "command substitution disqualifies",
			command:  "git commit -m $(date)",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "backtick substitution disqualifies",
			command:  "git commit -m `date`",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "variable expansion disqualifies",
			command:  "git commit -m $MSG",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "env assignment prefix disqualifies",
			command:  "FOO=1 git commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "subshell disqualifies",
			command:  "(git commit -m x)",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "cd disqualifies even when pattern matches",
			command:  "cd /tmp",
			patterns: []string{"cd *"},
			want:     false,
		},
		{
			name:     "eval disqualifies",
			command:  "eval git commit",
			patterns: []string{"eval *"},
			want:     false,
		},
		{
			name:     "background operator disqualifies",
			command:  "git commit -m x &",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "compound if block disqualifies",
			command:  "if true; then git commit -m x; fi",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "no patterns never matches",
			command:  "git commit -m x",
			patterns: nil,
			want:     false,
		},
		{
			name:     "bare wildcard pattern rejected",
			command:  "git commit -m x",
			patterns: []string{"*"},
			want:     false,
		},
		{
			name:     "unparseable command rejected",
			command:  "git commit -m \"unterminated",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "multi-pattern second match",
			command:  "docker compose up -d",
			patterns: []string{"git commit *", "docker compose *"},
			want:     true,
		},
		{
			name:     "quoted static cat heredoc substitution matches",
			command:  "git commit -m \"$(cat <<'EOF'\nfeat: add reviewer\n\nBody text.\nEOF\n)\"",
			patterns: []string{"git commit *"},
			want:     true,
		},
		{
			name:     "unquoted static cat heredoc substitution rejected due to word splitting",
			command:  "git commit -m $(cat <<'EOF'\nfeat: add reviewer\nEOF\n)",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "dynamic heredoc substitution rejected",
			command:  "git commit -m \"$(cat <<EOF\nfeat: $(whoami)\nEOF\n)\"",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "stdin static heredoc matches",
			command:  "git commit -F - <<'EOF'\nfeat: add reviewer\nEOF\n",
			patterns: []string{"git commit *"},
			want:     true,
		},
		{
			name:     "stderr to dev null and fd dup match",
			command:  "git commit -m release 2>/dev/null && docker compose ps 2>&1",
			patterns: []string{"git commit *", "docker compose *"},
			want:     true,
		},

		// --- Raw-AST bypasses (must stay sandboxed) ---
		{
			name:     "export clause before chain drops left side",
			command:  "export GIT_SSH_COMMAND='evil' && git commit -m x",
			patterns: []string{"git commit *", "export *"},
			want:     false,
		},
		{
			name:     "test clause in chain",
			command:  "[[ -f x ]] && git commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "arith clause in chain",
			command:  "((i=1)) && git commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "dynamic executable word",
			command:  "\"$VAR\"git commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "parameter-expanded executable",
			command:  "$GIT commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "quoted executable cannot forge pattern boundary",
			command:  "\"git commit\" -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "quoted arg cannot forge exact match",
			command:  "docker \"compose up\"",
			patterns: []string{"docker compose up"},
			want:     false,
		},
		{
			name:     "glob arg disqualifies",
			command:  "git commit -m *",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "negated statement disqualifies",
			command:  "! git commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "coproc disqualifies",
			command:  "git commit -m x |& cat",
			patterns: []string{"git commit *", "cat *"},
			want:     false,
		},
		{
			name:     "env wrapper builtin disqualifies",
			command:  "env GIT_SSH_COMMAND=evil git commit -m x",
			patterns: []string{"git commit *", "env *"},
			want:     false,
		},
		{
			name:     "sudo wrapper disqualifies",
			command:  "sudo git commit -m x",
			patterns: []string{"sudo *", "git commit *"},
			want:     false,
		},
		{
			name:     "exec wrapper disqualifies",
			command:  "exec git commit -m x",
			patterns: []string{"exec *", "git commit *"},
			want:     false,
		},
		{
			name:     "shell interpreter wrapper disqualifies",
			command:  "sh -c 'git commit -m x'",
			patterns: []string{"sh *", "git commit *"},
			want:     false,
		},
		{
			name:     "command builtin disqualifies",
			command:  "command git commit -m x",
			patterns: []string{"command *", "git commit *"},
			want:     false,
		},
		{
			name:     "block statement disqualifies",
			command:  "{ export FOO=1; } && git commit -m x",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "pattern with metacharacters rejected",
			command:  "git commit -m x",
			patterns: []string{"git commit $(echo *) *"},
			want:     false,
		},
		{
			name:     "single-quoted dollar is a static literal",
			command:  "git commit -m '$(id)'",
			patterns: []string{"git commit *"},
			want:     true,
		},
		{
			name:     "ANSI-C quoted escape sequences disqualify",
			command:  "git commit -m $'a\\nb'",
			patterns: []string{"git commit *"},
			want:     false,
		},
		{
			name:     "qualified wrapper path disqualifies",
			command:  "/usr/bin/env GIT_SSH_COMMAND=evil git commit -m x",
			patterns: []string{"git commit *", "/usr/bin/env *"},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchExcludedCommand(tc.command, tc.patterns); got != tc.want {
				t.Errorf("MatchExcludedCommand(%q, %v) = %v, want %v", tc.command, tc.patterns, got, tc.want)
			}
		})
	}
}
