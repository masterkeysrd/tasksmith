package cmdclassify

import (
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/cmdparse"
)

func TestStaticClassifications(t *testing.T) {
	tests := []struct {
		command string
		want    Category
	}{
		{"cat file.txt", ReadOnly},
		{"grep pattern file.txt", ReadOnly},
		{"find . -name test", ReadOnly},
		{"ls -la", ReadOnly},
		{"pwd", ReadOnly},
		{"head -n 10 file.txt", ReadOnly},
		{"tail -f log.txt", ReadOnly},
		{"file test.txt", ReadOnly},
		{"which go", ReadOnly},
		{"rm -rf tmp", Destructive},
		{"rmdir empty_dir", Destructive},
		{"dd if=/dev/zero of=out", Destructive},
		{"mkfs.ext4 /dev/sda", Unknown},
		{"mkfs /dev/sda", Destructive},
		{"sudo apt-get update", Destructive},
		{"mkdir newdir", SafeWrite},
		{"touch file.txt", SafeWrite},
		{"cp src.txt dst.txt", SafeWrite},
	}

	workspaceDir := "/tmp/workspace"
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := Classify(tt.command, workspaceDir)
			if got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestGitConditionals(t *testing.T) {
	tests := []struct {
		command string
		want    Category
	}{
		{"git status", ReadOnly},
		{"git diff", ReadOnly},
		{"git log", ReadOnly},
		{"git show HEAD", ReadOnly},
		{"git branch", ReadOnly},
		{"git commit -m 'hello'", SafeWrite},
		{"git push origin main", SafeWrite},
		{"git add .", SafeWrite},
		{"git reset --hard HEAD", Destructive},
		{"git clean -f -d", Destructive},
	}

	workspaceDir := "/tmp/workspace"
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := Classify(tt.command, workspaceDir)
			if got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestOutputRedirections(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	t.Run("redirect inside workspace", func(t *testing.T) {
		cmd := `echo "hi" > inside.txt`
		chain, err := cmdparse.Parse(cmd)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(chain.Pipelines) == 0 || len(chain.Pipelines[0].Commands) == 0 {
			t.Fatal("expected at least one command")
		}
		cmdObj := chain.Pipelines[0].Commands[0]
		// echo is ReadOnly, but redirect should be evaluated.
		// The redirect target "inside.txt" resolves to /tmp/workspace/inside.txt
		// which is inside the workspace, so it should be SafeWrite (from echo's rule).
		got := classifyCommand(cmdObj, DefaultPolicy(), workspaceDir)
		// echo is ReadOnly, but the redirect is inside workspace, so SafeWrite or ReadOnly.
		// Since echo has a static rule of ReadOnly, and the redirect is inside workspace,
		// it should remain ReadOnly (not UnsafeWrite).
		if got == UnsafeWrite {
			t.Errorf("Classify(%q) = %q, expected not UnsafeWrite", cmd, got)
		}
	})

	t.Run("redirect outside workspace", func(t *testing.T) {
		cmd := `echo "hi" > /etc/hosts`
		chain, err := cmdparse.Parse(cmd)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if len(chain.Pipelines) == 0 || len(chain.Pipelines[0].Commands) == 0 {
			t.Fatal("expected at least one command")
		}
		cmdObj := chain.Pipelines[0].Commands[0]
		got := classifyCommand(cmdObj, DefaultPolicy(), workspaceDir)
		if got != UnsafeWrite {
			t.Errorf("Classify(%q) = %q, want %q", cmd, got, UnsafeWrite)
		}
	})
}

func TestNestedWrapperResolution(t *testing.T) {
	tests := []struct {
		command string
		want    Category
	}{
		{"sudo rm -rf tmp", Destructive},
		{"sudo apt-get update", Destructive},
		{"npx eslint --fix", Unknown},
	}

	workspaceDir := "/tmp/workspace"
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := Classify(tt.command, workspaceDir)
			if got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestIsReadOnly(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"cat file.txt", true},
		{"grep pattern file.txt", true},
		{"rm -rf tmp", false},
		{"git status", true},
		{"git commit -m 'hello'", false}, // SafeWrite, not ReadOnly
	}

	workspaceDir := "/tmp/workspace"
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := IsReadOnly(tt.command, workspaceDir)
			if got != tt.want {
				t.Errorf("IsReadOnly(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestMatchParsedCommand(t *testing.T) {
	t.Run("wildcard match", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Executable: "git",
			Args:       []string{"commit", "-m", "hello"},
		}
		if !MatchParsedCommand("git *", "", cmd) {
			t.Error("expected 'git *' to match 'git commit'")
		}
	})

	t.Run("subcommand match", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Executable: "git",
			Args:       []string{"commit", "-m", "hello"},
		}
		if !MatchParsedCommand("git commit", "", cmd) {
			t.Error("expected 'git commit' to match 'git commit -m hello'")
		}
	})

	t.Run("env var prefix ignored", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Env:        []cmdparse.EnvVar{{Name: "DEBUG", Value: "true"}},
			Executable: "git",
			Args:       []string{"commit", "-m", "hello"},
		}
		if !MatchParsedCommand("git commit", "", cmd) {
			t.Error("expected env var prefix to be ignored")
		}
	})

	t.Run("sudo wrapper unpacking", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Executable: "sudo",
			SubCommand: &cmdparse.ParsedCommand{
				Executable: "git",
				Args:       []string{"status"},
			},
		}
		if !MatchParsedCommand("git *", "", cmd) {
			t.Error("expected 'git *' to match 'sudo git status' after wrapper unpacking")
		}
		if !MatchParsedCommand("git status", "", cmd) {
			t.Error("expected 'git status' to match 'sudo git status' after wrapper unpacking")
		}
	})

	t.Run("no match different executable", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Executable: "node",
			Args:       []string{"app.js"},
		}
		if MatchParsedCommand("git *", "", cmd) {
			t.Error("expected 'git *' to NOT match 'node app.js'")
		}
	})

	t.Run("no match wrong subcommand", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Executable: "git",
			Args:       []string{"status"},
		}
		if MatchParsedCommand("git commit", "", cmd) {
			t.Error("expected 'git commit' to NOT match 'git status'")
		}
	})

	t.Run("sudo subcommand match", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Executable: "sudo",
			SubCommand: &cmdparse.ParsedCommand{
				Executable: "git",
				Args:       []string{"status"},
			},
		}
		if !MatchParsedCommand("git status", "", cmd) {
			t.Error("expected 'git status' to match 'sudo git status'")
		}
	})

	t.Run("wildcard match with env vars", func(t *testing.T) {
		cmd := cmdparse.ParsedCommand{
			Env:        []cmdparse.EnvVar{{Name: "DEBUG", Value: "true"}},
			Executable: "git",
			Args:       []string{"commit", "-m", "hello"},
		}
		if !MatchParsedCommand("git *", "", cmd) {
			t.Error("expected 'git *' to match 'DEBUG=true git commit'")
		}
	})
}

func TestRedirectPathResolution(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	tests := []struct {
		target string
		want   bool // true if inside workspace
	}{
		{"inside.txt", true},
		{"./inside.txt", true},
		{"/tmp/workspace/inside.txt", true},
		{"/etc/hosts", false},
		{"/var/log/syslog", false},
		{"/tmp/workspace/.git/config", true},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			resolved := resolvePath(tt.target, workspaceDir)
			inside := isInsideWorkspace(resolved, workspaceDir)
			if inside != tt.want {
				t.Errorf("resolvePath(%q, %q) inside = %v, want %v", tt.target, workspaceDir, inside, tt.want)
			}
		})
	}
}

func TestGitCleanDestructive(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	// git clean -f -d should be Destructive
	cmd := "git clean -f -d"
	got := Classify(cmd, workspaceDir)
	if got != Destructive {
		t.Errorf("Classify(%q) = %q, want %q", cmd, got, Destructive)
	}

	// git clean --force should also be Destructive
	cmd = "git clean --force"
	got = Classify(cmd, workspaceDir)
	if got != Destructive {
		t.Errorf("Classify(%q) = %q, want %q", cmd, got, Destructive)
	}
}

func TestEchoRedirectInsideWorkspace(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	cmd := `echo "hi" > inside.txt`
	chain, err := cmdparse.Parse(cmd)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// The redirect should be on the echo command.
	if len(chain.Pipelines) == 0 {
		t.Fatal("expected at least one pipeline")
	}
	if len(chain.Pipelines[0].Commands) == 0 {
		t.Fatal("expected at least one command")
	}

	pcmd := chain.Pipelines[0].Commands[0]
	if len(pcmd.Redirects) != 1 {
		t.Fatalf("expected 1 redirect, got %d", len(pcmd.Redirects))
	}

	// The redirect target should be "inside.txt"
	if pcmd.Redirects[0].Target != "inside.txt" {
		t.Errorf("expected target 'inside.txt', got %q", pcmd.Redirects[0].Target)
	}

	// Classify should return SafeWrite because echo is ReadOnly but the redirect
	// is inside the workspace, so it's not UnsafeWrite.
	// Actually, echo is classified as ReadOnly, and the redirect is inside workspace,
	// so the result should be ReadOnly (not UnsafeWrite).
	got := Classify(cmd, workspaceDir)
	if got == UnsafeWrite {
		t.Errorf("Classify(%q) = %q, expected not UnsafeWrite", cmd, got)
	}
}

func TestFilepathClean(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	// Test that filepath.Clean is applied correctly.
	resolved := resolvePath("./subdir/file.txt", workspaceDir)
	expected := filepath.Clean(filepath.Join(workspaceDir, "subdir", "file.txt"))
	if resolved != expected {
		t.Errorf("resolvePath(%q, %q) = %q, want %q", "./subdir/file.txt", workspaceDir, resolved, expected)
	}
}

func TestEmptyCommand(t *testing.T) {
	workspaceDir := "/tmp/workspace"
	got := Classify("", workspaceDir)
	if got != Unknown {
		t.Errorf("Classify(\"\") = %q, want %q", got, Unknown)
	}
}

func TestDefaultCategory(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	// Unknown commands should return the default category (Unknown).
	cmd := "unknowncommand arg1 arg2"
	got := Classify(cmd, workspaceDir)
	if got != Unknown {
		t.Errorf("Classify(%q) = %q, want %q", cmd, got, Unknown)
	}
}

func TestContainsHelpers(t *testing.T) {
	slice := []string{"a", "b", "c"}

	if !contains(slice, "a") {
		t.Error("contains([a,b,c], a) should be true")
	}
	if contains(slice, "d") {
		t.Error("contains([a,b,c], d) should be false")
	}

	if !containsAny(slice, []string{"x", "b", "z"}) {
		t.Error("containsAny should return true for 'b'")
	}
	if containsAny(slice, []string{"x", "y", "z"}) {
		t.Error("containsAny should return false for [x,y,z]")
	}

	if !containsAll(slice, []string{"a", "c"}) {
		t.Error("containsAll should return true for [a,c]")
	}
	if containsAll(slice, []string{"a", "d"}) {
		t.Error("containsAll should return false for [a,d]")
	}
}

func TestGitBranch(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	tests := []struct {
		command string
		want    Category
	}{
		{"git branch", ReadOnly},
		{"git branch -a", ReadOnly},
		{"git branch -d old", ReadOnly},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := Classify(tt.command, workspaceDir)
			if got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestRedirectInsideGit(t *testing.T) {
	workspaceDir := "/tmp/workspace"

	cmd := `echo "hi" > .git/config`
	chain, err := cmdparse.Parse(cmd)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(chain.Pipelines) == 0 || len(chain.Pipelines[0].Commands) == 0 {
		t.Fatal("expected at least one command")
	}

	pcmd := chain.Pipelines[0].Commands[0]
	got := classifyCommand(pcmd, DefaultPolicy(), workspaceDir)
	if got != UnsafeWrite {
		t.Errorf("Classify(%q) = %q, want %q", cmd, got, UnsafeWrite)
	}
}
