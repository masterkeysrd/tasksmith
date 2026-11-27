package command

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestChangesBrowserDiffAndBack(t *testing.T) {
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/sessions/test/changes":
			return mcpJSONResponse(http.StatusOK, []api.FileChange{
				{Path: "main.go", Kind: api.KindCreated, AfterHash: "new", Additions: 1},
			})
		case "/v1/sessions/test/diff":
			if path := req.URL.Query().Get("path"); path != "main.go" {
				return nil, fmt.Errorf("unexpected diff path: %q", path)
			}
			return mcpJSONResponse(http.StatusOK, api.DiffResponse{Diff: "@@ -0,0 +1 @@\n+package main"})
		default:
			return nil, fmt.Errorf("unexpected request: %s", req.URL)
		}
	})
	c, err := client.New("test.sock", client.WithCWD("."), client.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	store.SetClient(c)
	store.SetSession(&api.Session{ID: "test"})
	t.Cleanup(store.ResetSession)

	app := testutils.NewApp(t, ChangesBrowser("slot-changes"), 100, 24)
	app.WaitFor(func(text string) bool { return strings.Contains(text, "main.go") })
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(text string) bool {
		return strings.Contains(text, "Diff for main.go (created):") && strings.Contains(text, "package main")
	})
	out := app.Press(key.Key{Code: key.KeyEscape})
	if !strings.Contains(out, "Tracked Changes") || !strings.Contains(out, "main.go") || strings.Contains(out, "Diff for") {
		t.Fatalf("expected the changes list after Escape:\n%s", out)
	}
}

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
