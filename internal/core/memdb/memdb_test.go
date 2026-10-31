package memdb

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

type testItem struct {
	ID    string
	Value string
}

func testKey(t testItem) string {
	return t.ID
}

func TestSaveAndGet(t *testing.T) {
	db := New(testKey)

	snap0 := db.Snapshot()
	if snap0.Len() != 0 {
		t.Fatalf("expected empty snapshot, got len=%d", snap0.Len())
	}
	if _, ok := snap0.Get("a"); ok {
		t.Fatal("expected 'a' not found in empty snapshot")
	}

	db.Save(testItem{ID: "b", Value: "val-b"}, testItem{ID: "a", Value: "val-a"})

	// Old snapshot must remain unchanged (immutable)
	if snap0.Len() != 0 {
		t.Fatalf("snap0 mutated: len=%d", snap0.Len())
	}

	snap1 := db.Snapshot()
	if snap1.Len() != 2 {
		t.Fatalf("snap1 len=%d, want 2", snap1.Len())
	}
	itemA, ok := snap1.Get("a")
	if !ok || itemA.Value != "val-a" {
		t.Fatalf("snap1 get 'a': got %+v, ok=%v", itemA, ok)
	}

	// Update existing
	db.Save(testItem{ID: "a", Value: "val-a-updated"})
	snap2 := db.Snapshot()
	if snap2.Len() != 2 {
		t.Fatalf("snap2 len=%d, want 2", snap2.Len())
	}
	itemAUpdated, ok := snap2.Get("a")
	if !ok || itemAUpdated.Value != "val-a-updated" {
		t.Fatalf("snap2 get 'a': got %+v, ok=%v", itemAUpdated, ok)
	}
	// snap1 must still see old value
	if itemAOld, _ := snap1.Get("a"); itemAOld.Value != "val-a" {
		t.Fatalf("snap1 mutated: got %+v", itemAOld)
	}
}

func TestDelete(t *testing.T) {
	db := New(testKey)
	db.Save(
		testItem{ID: "1", Value: "one"},
		testItem{ID: "2", Value: "two"},
		testItem{ID: "3", Value: "three"},
	)

	db.Delete("2", "nonexistent")
	snap := db.Snapshot()
	if snap.Len() != 2 {
		t.Fatalf("len=%d, want 2", snap.Len())
	}
	if _, ok := snap.Get("2"); ok {
		t.Fatal("expected '2' to be deleted")
	}
	if _, ok := snap.Get("1"); !ok {
		t.Fatal("expected '1' to remain")
	}
	if _, ok := snap.Get("3"); !ok {
		t.Fatal("expected '3' to remain")
	}

	// Delete with empty arguments does nothing
	db.Delete()
	if db.Snapshot().Len() != 2 {
		t.Fatalf("len=%d after empty delete, want 2", db.Snapshot().Len())
	}
}

func TestReplace(t *testing.T) {
	db := New(testKey)
	db.Save(testItem{ID: "old1"}, testItem{ID: "old2"})

	db.Replace(testItem{ID: "new1", Value: "v1"}, testItem{ID: "new2", Value: "v2"})
	snap := db.Snapshot()
	if snap.Len() != 2 {
		t.Fatalf("len=%d, want 2", snap.Len())
	}
	if _, ok := snap.Get("old1"); ok {
		t.Fatal("expected old1 to be gone")
	}
	if _, ok := snap.Get("new1"); !ok {
		t.Fatal("expected new1 to be present")
	}

	// Replace with empty clears db
	db.Replace()
	if db.Snapshot().Len() != 0 {
		t.Fatalf("len=%d after empty replace, want 0", db.Snapshot().Len())
	}
}

func TestWithTxn(t *testing.T) {
	db := New(testKey)
	db.Save(testItem{ID: "a", Value: "1"})

	// Aborted transaction
	errAbort := errors.New("abort transaction")
	err := db.WithTxn(func(tx *Txn[testItem]) error {
		tx.Save(testItem{ID: "b", Value: "2"})
		tx.Delete("a")
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("WithTxn error = %v, want %v", err, errAbort)
	}

	snap := db.Snapshot()
	if snap.Len() != 1 {
		t.Fatalf("after aborted txn, len=%d, want 1", snap.Len())
	}
	if item, ok := snap.Get("a"); !ok || item.Value != "1" {
		t.Fatalf("after aborted txn, item 'a' changed: %+v, ok=%v", item, ok)
	}

	// Committed transaction
	err = db.WithTxn(func(tx *Txn[testItem]) error {
		tx.Save(testItem{ID: "b", Value: "2"}, testItem{ID: "c", Value: "3"})
		tx.Delete("a")
		return nil
	})
	if err != nil {
		t.Fatalf("WithTxn commit error: %v", err)
	}

	snap2 := db.Snapshot()
	if snap2.Len() != 2 {
		t.Fatalf("after committed txn, len=%d, want 2", snap2.Len())
	}
	if _, ok := snap2.Get("a"); ok {
		t.Fatal("expected 'a' deleted in committed txn")
	}
	if item, ok := snap2.Get("b"); !ok || item.Value != "2" {
		t.Fatalf("expected 'b' saved in committed txn: %+v, ok=%v", item, ok)
	}
	if item, ok := snap2.Get("c"); !ok || item.Value != "3" {
		t.Fatalf("expected 'c' saved in committed txn: %+v, ok=%v", item, ok)
	}
}

func TestAllSorted(t *testing.T) {
	db := New(testKey)
	db.Save(
		testItem{ID: "delta"},
		testItem{ID: "alpha"},
		testItem{ID: "charlie"},
		testItem{ID: "bravo"},
	)

	snap := db.Snapshot()
	all := snap.All()
	if len(all) != 4 {
		t.Fatalf("All() len=%d, want 4", len(all))
	}
	wantOrder := []string{"alpha", "bravo", "charlie", "delta"}
	for i, want := range wantOrder {
		if all[i].ID != want {
			t.Errorf("all[%d].ID = %q, want %q", i, all[i].ID, want)
		}
	}

	// Mutating the returned slice should not affect subsequent All() calls
	all[0] = testItem{ID: "mutated"}
	allAgain := snap.All()
	if allAgain[0].ID != "alpha" {
		t.Fatalf("All() returned mutable internal slice; allAgain[0].ID = %q", allAgain[0].ID)
	}
}

func TestGetByPrefix(t *testing.T) {
	db := New(testKey)
	db.Save(
		testItem{ID: "app-server-1", Value: "srv1"},
		testItem{ID: "app-server-2", Value: "srv2"},
		testItem{ID: "app-worker", Value: "wrk"},
		testItem{ID: "database", Value: "db"},
		testItem{ID: "cache", Value: "c"},
	)

	view := db.Snapshot()

	// 1. Empty prefix
	_, err := view.GetByPrefix("")
	if !errors.Is(err, ErrEmptyPrefix) {
		t.Fatalf("GetByPrefix(\"\") error = %v, want ErrEmptyPrefix", err)
	}

	// 2. Exact match takes precedence
	item, err := view.GetByPrefix("cache")
	if err != nil || item.Value != "c" {
		t.Fatalf("GetByPrefix(\"cache\") = %+v, err=%v", item, err)
	}

	// 3. Unique prefix match
	item, err = view.GetByPrefix("data")
	if err != nil || item.Value != "db" {
		t.Fatalf("GetByPrefix(\"data\") = %+v, err=%v", item, err)
	}
	item, err = view.GetByPrefix("app-w")
	if err != nil || item.Value != "wrk" {
		t.Fatalf("GetByPrefix(\"app-w\") = %+v, err=%v", item, err)
	}

	// 4. Ambiguous prefix match
	_, err = view.GetByPrefix("app-server")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("GetByPrefix(\"app-server\") error = %v, want ErrAmbiguous", err)
	}
	_, err = view.GetByPrefix("app-")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("GetByPrefix(\"app-\") error = %v, want ErrAmbiguous", err)
	}

	// 5. Not found
	_, err = view.GetByPrefix("redis")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByPrefix(\"redis\") error = %v, want ErrNotFound", err)
	}
	_, err = view.GetByPrefix("z")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByPrefix(\"z\") error = %v, want ErrNotFound", err)
	}
}

func TestConcurrentSnapshotReadsDuringWrites(t *testing.T) {
	db := New(testKey)
	const numWriters = 4
	const numReaders = 8
	const itemsPerWriter = 200

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-start
			for i := 0; i < itemsPerWriter; i++ {
				id := fmt.Sprintf("item-%d-%d", workerID, i)
				db.Save(testItem{ID: id, Value: fmt.Sprintf("val-%d", i)})
			}
		}(w)
	}

	// Readers taking concurrent snapshots
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 500; i++ {
				snap := db.Snapshot()
				all := snap.All()
				// Verify sortedness
				for j := 1; j < len(all); j++ {
					if all[j-1].ID >= all[j].ID {
						t.Errorf("snapshot out of order: %s >= %s", all[j-1].ID, all[j].ID)
					}
				}
				// Verify consistency between Len() and len(All())
				if snap.Len() != len(all) {
					t.Errorf("Len()=%d != len(All())=%d", snap.Len(), len(all))
				}
			}
		}()
	}

	close(start)
	wg.Wait()

	finalSnap := db.Snapshot()
	expectedTotal := numWriters * itemsPerWriter
	if finalSnap.Len() != expectedTotal {
		t.Fatalf("final snap Len() = %d, want %d", finalSnap.Len(), expectedTotal)
	}
}
