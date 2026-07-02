package filetrack

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceTracker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-filetrack-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some initial files
	fileA := filepath.Join(tmpDir, "file_a.txt")
	fileB := filepath.Join(tmpDir, "file_b.txt")
	subDir := filepath.Join(tmpDir, "subdir")
	fileC := filepath.Join(subDir, "file_c.txt")

	if err := os.WriteFile(fileA, []byte("content A"), 0644); err != nil {
		t.Fatalf("failed to write file A: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("content B"), 0644); err != nil {
		t.Fatalf("failed to write file B: %v", err)
	}
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(fileC, []byte("content C"), 0644); err != nil {
		t.Fatalf("failed to write file C: %v", err)
	}

	wt := NewWorkspaceTracker(tmpDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := wt.Start(ctx); err != nil {
		t.Fatalf("failed to start workspace tracker: %v", err)
	}
	defer wt.Stop()

	// 1. Check initial scan results (Search)
	results := wt.Search("")
	if len(results) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(results), results)
	}

	// Verify we can find specific files
	matches := wt.Search("file_a")
	if len(matches) != 1 || matches[0] != "file_a.txt" {
		t.Errorf("expected to find file_a.txt, got %v", matches)
	}

	matchesSub := wt.Search("subdir/")
	if len(matchesSub) != 1 || matchesSub[0] != "subdir/file_c.txt" {
		t.Errorf("expected to find subdir/file_c.txt, got %v", matchesSub)
	}

	// 2. Test MRU ranking (NotifyTouch)
	wt.NotifyTouch("file_b.txt", false)
	
	searchResults := wt.Search("file_")
	if len(searchResults) < 2 {
		t.Fatalf("expected at least 2 search results, got %v", searchResults)
	}
	if searchResults[0] != "file_b.txt" {
		t.Errorf("expected file_b.txt (MRU) to be first, got %s in %v", searchResults[0], searchResults)
	}

	// 3. Test Subscription and Interest Registration
	ch, unsubscribe := wt.SubscribeSession("session-1")
	defer unsubscribe()

	// Register interest in file_a.txt
	wt.RegisterInterest("session-1", "file_a.txt")

	// Trigger a file modification on file_a.txt
	newContent := []byte("new content A!")
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(newContent))
	if err := os.WriteFile(fileA, newContent, 0644); err != nil {
		t.Fatalf("failed to modify file A: %v", err)
	}

	// Wait for event to propagate through watcher
	var receivedEvent *FileEvent
	select {
	case ev := <-ch:
		receivedEvent = &ev
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for file modification event")
	}

	if receivedEvent != nil {
		if receivedEvent.Path != "file_a.txt" {
			t.Errorf("expected event path to be 'file_a.txt', got %q", receivedEvent.Path)
		}
		if receivedEvent.Kind != Modified {
			t.Errorf("expected event kind to be 'modified', got %v", receivedEvent.Kind)
		}
		if receivedEvent.Hash != expectedHash {
			t.Errorf("expected event hash to be %q, got %q", expectedHash, receivedEvent.Hash)
		}
	}

	// 4. Test Wildcard subscription
	wildcardCh, wildcardUnsub := wt.SubscribeSession("wildcard:test-LSP")
	defer wildcardUnsub()

	// Create a new file (not registered in any session's interest)
	fileD := filepath.Join(tmpDir, "file_d.txt")
	newContentD := []byte("content D")
	expectedHashD := fmt.Sprintf("%x", sha256.Sum256(newContentD))
	if err := os.WriteFile(fileD, newContentD, 0644); err != nil {
		t.Fatalf("failed to write file D: %v", err)
	}

	// Session-1 has no interest in file_d.txt, so it should NOT receive an event
	select {
	case ev := <-ch:
		t.Errorf("session-1 received unexpected event for file_d.txt: %v", ev)
	case <-time.After(200 * time.Millisecond):
		// Expected: no event received
	}

	// Wildcard subscription should receive the event!
	select {
	case ev := <-wildcardCh:
		if ev.Path != "file_d.txt" {
			t.Errorf("expected wildcard event path to be 'file_d.txt', got %q", ev.Path)
		}
		if ev.Kind != Created {
			t.Errorf("expected wildcard event kind to be 'created', got %v", ev.Kind)
		}
		if ev.Hash != expectedHashD {
			t.Errorf("expected wildcard event hash to be %q, got %q", expectedHashD, ev.Hash)
		}
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for wildcard creation event")
	}
}

func TestWorkspaceTrackerGitignoreReload(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasksmith-filetrack-ignore-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy .git directory to satisfy git root detection
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	// Create a regular log file initially (not ignored)
	logFile := filepath.Join(tmpDir, "app.log")
	if err := os.WriteFile(logFile, []byte("log content"), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	wt := NewWorkspaceTracker(tmpDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := wt.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer wt.Stop()

	// Initial scan should find app.log
	results := wt.Search("app.log")
	if len(results) != 1 {
		t.Fatalf("expected to find app.log, got %v", results)
	}

	// Create a wildcard subscription to watch creations
	wildcardCh, wildcardUnsub := wt.SubscribeSession("wildcard:reload-test")
	defer wildcardUnsub()

	// Create a .gitignore file ignoring *.log files
	gitignoreFile := filepath.Join(tmpDir, ".gitignore")
	if err := os.WriteFile(gitignoreFile, []byte("*.log\n"), 0644); err != nil {
		t.Fatalf("failed to write gitignore: %v", err)
	}

	// Wait for the gitignore creation event to be processed and reload the ignorer
	select {
	case <-wildcardCh:
		// .gitignore was created and ignorer reloaded
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for gitignore creation event")
	}

	// Create another log file: it should now be ignored!
	ignoredLogFile := filepath.Join(tmpDir, "ignored.log")
	if err := os.WriteFile(ignoredLogFile, []byte("secret log content"), 0644); err != nil {
		t.Fatalf("failed to write ignored log file: %v", err)
	}

	// Wait a moment for the watcher to see it
	time.Sleep(500 * time.Millisecond)

	// Search should NOT find ignored.log
	resultsIgnored := wt.Search("ignored.log")
	if len(resultsIgnored) != 0 {
		t.Errorf("expected ignored.log to be ignored, but found: %v", resultsIgnored)
	}
}
