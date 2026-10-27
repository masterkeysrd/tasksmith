package command

import (
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestAggregateFileChanges(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		res := aggregateFileChanges(nil)
		if len(res) != 0 {
			t.Fatalf("expected nil or empty, got %v", res)
		}
	})

	t.Run("SingleFileMultipleEdits", func(t *testing.T) {
		now := time.Now()
		entries := []api.FileChange{
			{
				ID:         "1",
				Path:       "main.go",
				Kind:       "modified",
				BeforeHash: "base",
				AfterHash:  "hash1",
				Additions:  5,
				Deletions:  1,
				Timestamp:  now,
			},
			{
				ID:         "2",
				Path:       "main.go",
				Kind:       "modified",
				BeforeHash: "hash1",
				AfterHash:  "hash2",
				Additions:  3,
				Deletions:  2,
				Timestamp:  now.Add(time.Second),
			},
		}

		res := aggregateFileChanges(entries)
		if len(res) != 1 {
			t.Fatalf("expected 1 aggregated file change, got %d", len(res))
		}
		ch := res[0]
		if ch.Path != "main.go" {
			t.Errorf("expected Path 'main.go', got %q", ch.Path)
		}
		if ch.Kind != "modified" {
			t.Errorf("expected Kind 'modified', got %q", ch.Kind)
		}
		if ch.Additions != 8 {
			t.Errorf("expected Additions 8, got %d", ch.Additions)
		}
		if ch.Deletions != 3 {
			t.Errorf("expected Deletions 3, got %d", ch.Deletions)
		}
	})

	t.Run("RevertedFileSkipped", func(t *testing.T) {
		now := time.Now()
		entries := []api.FileChange{
			{
				ID:         "1",
				Path:       "config.yaml",
				Kind:       "modified",
				BeforeHash: "base",
				AfterHash:  "hash1",
				Additions:  5,
				Deletions:  2,
				Timestamp:  now,
			},
			{
				ID:         "2",
				Path:       "config.yaml",
				Tool:       "revert",
				Kind:       "modified",
				BeforeHash: "hash1",
				AfterHash:  "base", // Restored to baseline
				Additions:  2,
				Deletions:  5,
				Timestamp:  now.Add(time.Second),
			},
		}

		res := aggregateFileChanges(entries)
		if len(res) != 0 {
			t.Fatalf("expected reverted file to be skipped, got %d changes", len(res))
		}
	})

	t.Run("CreatedAndDeleted", func(t *testing.T) {
		now := time.Now()
		// File created and then deleted in session: net changes relative to baseline = 0
		entries := []api.FileChange{
			{
				ID:         "1",
				Path:       "temp.txt",
				Kind:       "created",
				BeforeHash: "",
				AfterHash:  "hash1",
				Additions:  10,
				Deletions:  0,
				Timestamp:  now,
			},
			{
				ID:         "2",
				Path:       "temp.txt",
				Kind:       "deleted",
				BeforeHash: "hash1",
				AfterHash:  "",
				Additions:  0,
				Deletions:  10,
				Timestamp:  now.Add(time.Second),
			},
		}

		res := aggregateFileChanges(entries)
		if len(res) != 0 {
			t.Fatalf("expected created-then-deleted file to be skipped, got %d changes", len(res))
		}
	})

	t.Run("NetCreatedAndNetDeletedAndMultipleFiles", func(t *testing.T) {
		now := time.Now()
		entries := []api.FileChange{
			{
				ID:         "1",
				Path:       "new.go",
				Kind:       "created",
				BeforeHash: "",
				AfterHash:  "hashA",
				Additions:  20,
				Deletions:  0,
				Timestamp:  now,
			},
			{
				ID:         "2",
				Path:       "old.go",
				Kind:       "deleted",
				BeforeHash: "hashOld",
				AfterHash:  "",
				Additions:  0,
				Deletions:  15,
				Timestamp:  now.Add(time.Second),
			},
			{
				ID:         "3",
				Path:       "new.go",
				Kind:       "modified",
				BeforeHash: "hashA",
				AfterHash:  "hashB",
				Additions:  5,
				Deletions:  1,
				Timestamp:  now.Add(2 * time.Second),
			},
		}

		res := aggregateFileChanges(entries)
		if len(res) != 2 {
			t.Fatalf("expected 2 aggregated changes, got %d", len(res))
		}

		// First file should be new.go (order of appearance preserved)
		if res[0].Path != "new.go" || res[0].Kind != "created" || res[0].Additions != 25 || res[0].Deletions != 1 {
			t.Errorf("unexpected new.go: %+v", res[0])
		}

		// Second file should be old.go
		if res[1].Path != "old.go" || res[1].Kind != "deleted" || res[1].Deletions != 15 {
			t.Errorf("unexpected old.go: %+v", res[1])
		}
	})
}
