package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVCS(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available in PATH, skipping VCS tests")
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "tasksmith-vcs-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. Verify not a repository initially
	if IsRepo(tmpDir) {
		t.Error("expected temp dir to not be a git repository initially")
	}

	// 2. Initialize git repository
	cmd := exec.Command("git", "init", tmpDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to initialize git repository: %v", err)
	}

	// 3. Verify is repository now
	if !IsRepo(tmpDir) {
		t.Error("expected temp dir to be detected as a git repository after git init")
	}

	// 4. Create a file and commit it to ensure HEAD exists
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cmdAdd := exec.Command("git", "-C", tmpDir, "add", ".")
	if err := cmdAdd.Run(); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}

	cmdCommit := exec.Command("git", "-C", tmpDir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", "initial commit")
	if err := cmdCommit.Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// 5. Verify branch can be fetched after initial commit
	branch, err := GetBranch(tmpDir)
	if err != nil {
		t.Errorf("failed to get active branch name: %v", err)
	}
	if branch == "" {
		t.Error("expected active branch name to be non-empty")
	}

	// 6. Modify the file and check status/diff
	if err := os.WriteFile(testFile, []byte("hello world modified"), 0644); err != nil {
		t.Fatalf("failed to write modified file: %v", err)
	}

	status, err := GetStatus(tmpDir)
	if err != nil {
		t.Errorf("failed to get status: %v", err)
	}
	if status == "" {
		t.Error("expected status listing modified file to be non-empty")
	}

	diff, err := GetDiff(tmpDir)
	if err != nil {
		t.Errorf("failed to get diff: %v", err)
	}
	if diff == "" {
		t.Error("expected diff of modified changes to be non-empty")
	}
}
