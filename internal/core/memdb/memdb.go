package memdb

import (
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	// ErrEmptyPrefix is returned by GetByPrefix when prefix is empty.
	ErrEmptyPrefix = errors.New("memdb: prefix cannot be empty")
	// ErrNotFound is returned by GetByPrefix when no item matches prefix.
	ErrNotFound = errors.New("memdb: not found")
	// ErrAmbiguous is returned by GetByPrefix when multiple items match prefix.
	ErrAmbiguous = errors.New("memdb: multiple items match prefix")
)

type viewState[T any] struct {
	byKey  map[string]T
	sorted []T
}

// DB is a generic, concurrency-safe, copy-on-write in-memory store.
type DB[T any] struct {
	keyFn func(T) string
	mu    sync.Mutex
	ptr   atomic.Pointer[viewState[T]]
}

// New creates a new DB where keyFn extracts the unique string key from T.
// Items in All() are kept sorted by key ascending.
func New[T any](keyFn func(T) string) *DB[T] {
	if keyFn == nil {
		panic("memdb: keyFn cannot be nil")
	}
	db := &DB[T]{
		keyFn: keyFn,
	}
	db.ptr.Store(&viewState[T]{
		byKey:  make(map[string]T),
		sorted: nil,
	})
	return db
}

// Snapshot returns a consistent, point-in-time read-only View.
// Reads are lock-free via atomic.Pointer.
func (db *DB[T]) Snapshot() *View[T] {
	return &View[T]{
		keyFn: db.keyFn,
		state: db.ptr.Load(),
	}
}

// Save inserts or updates one or more items atomically.
func (db *DB[T]) Save(items ...T) {
	if len(items) == 0 {
		return
	}
	db.mu.Lock()
	defer db.mu.Unlock()

	cur := db.ptr.Load()
	next := maps.Clone(cur.byKey)
	for _, item := range items {
		next[db.keyFn(item)] = item
	}
	db.commitLocked(next)
}

// Delete removes items by ID atomically.
func (db *DB[T]) Delete(ids ...string) {
	if len(ids) == 0 {
		return
	}
	db.mu.Lock()
	defer db.mu.Unlock()

	cur := db.ptr.Load()
	next := maps.Clone(cur.byKey)
	for _, id := range ids {
		delete(next, id)
	}
	db.commitLocked(next)
}

// Replace atomically replaces all items in the DB with items.
func (db *DB[T]) Replace(items ...T) {
	db.mu.Lock()
	defer db.mu.Unlock()

	next := make(map[string]T, len(items))
	for _, item := range items {
		next[db.keyFn(item)] = item
	}
	db.commitLocked(next)
}

// WithTxn executes fn inside a write transaction and commits atomically if fn returns nil.
func (db *DB[T]) WithTxn(fn func(tx *Txn[T]) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	cur := db.ptr.Load()
	tx := &Txn[T]{
		keyFn:  db.keyFn,
		staged: maps.Clone(cur.byKey),
	}

	if err := fn(tx); err != nil {
		return err
	}

	db.commitLocked(tx.staged)
	return nil
}

func (db *DB[T]) commitLocked(byKey map[string]T) {
	sorted := make([]T, 0, len(byKey))
	for _, item := range byKey {
		sorted = append(sorted, item)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return db.keyFn(sorted[i]) < db.keyFn(sorted[j])
	})

	db.ptr.Store(&viewState[T]{
		byKey:  byKey,
		sorted: sorted,
	})
}

// Txn represents an isolated write transaction.
type Txn[T any] struct {
	keyFn  func(T) string
	staged map[string]T
}

// Save inserts or updates items within the transaction.
func (tx *Txn[T]) Save(items ...T) {
	for _, item := range items {
		tx.staged[tx.keyFn(item)] = item
	}
}

// Delete removes items by ID within the transaction.
func (tx *Txn[T]) Delete(ids ...string) {
	for _, id := range ids {
		delete(tx.staged, id)
	}
}

// View represents a consistent point-in-time snapshot.
type View[T any] struct {
	keyFn func(T) string
	state *viewState[T]
}

// Get returns the item with the exact key, or false if not found.
func (v *View[T]) Get(id string) (T, bool) {
	item, ok := v.state.byKey[id]
	return item, ok
}

// GetByPrefix resolves an item by prefix.
// Returns ErrEmptyPrefix if prefix is empty.
// If an exact match exists, it is returned immediately.
// If multiple items match prefix, ErrAmbiguous is returned.
// If no item matches prefix, ErrNotFound is returned.
func (v *View[T]) GetByPrefix(prefix string) (T, error) {
	var zero T
	if prefix == "" {
		return zero, ErrEmptyPrefix
	}

	if item, ok := v.state.byKey[prefix]; ok {
		return item, nil
	}

	// Binary search for the first item with key >= prefix
	idx := sort.Search(len(v.state.sorted), func(i int) bool {
		return v.keyFn(v.state.sorted[i]) >= prefix
	})

	var matches []T
	for i := idx; i < len(v.state.sorted); i++ {
		key := v.keyFn(v.state.sorted[i])
		if !strings.HasPrefix(key, prefix) {
			break
		}
		matches = append(matches, v.state.sorted[i])
		if len(matches) > 1 {
			return zero, ErrAmbiguous
		}
	}

	if len(matches) == 0 {
		return zero, ErrNotFound
	}
	return matches[0], nil
}

// All returns a slice of all items sorted by key ascending.
func (v *View[T]) All() []T {
	return slices.Clone(v.state.sorted)
}

// Len returns the number of items in the view.
func (v *View[T]) Len() int {
	return len(v.state.sorted)
}
