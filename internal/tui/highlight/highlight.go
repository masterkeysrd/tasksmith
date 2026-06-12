// Package highlight provides registered highlight groups backed by a cached style map.
package highlight

import (
	"sort"
	"sync"

	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
	"github.com/masterkeysrd/tasksmith/internal/tui/styles"
)

// Group is a typed handle for a registered highlight group.
type Group struct {
	name string
}

// Name returns the underlying highlight group name.
func (g Group) Name() string {
	return g.name
}

func (g Group) Empty() bool {
	return g.name == ""
}

type registration struct {
	link string
}

// Option customizes a highlight group registration.
type Option func(*registration)

var (
	mu       sync.RWMutex
	registry = map[string]registration{}
	cache    = map[string]style.Style{}
)

// Link makes a registered highlight group inherit from a base highlight name.
func Link(base string) Option {
	return func(reg *registration) {
		reg.link = base
	}
}

// Set adds a highlight group to the registry and returns its stable handle.
// Re-registering the same name is idempotent and preserves the first registration.
func Set(name string, opts ...Option) Group {
	if name == "" {
		return Group{}
	}

	mu.Lock()
	defer mu.Unlock()

	if _, ok := registry[name]; ok {
		return Group{name: name}
	}

	reg := registration{}
	for _, opt := range opts {
		if opt != nil {
			opt(&reg)
		}
	}
	registry[name] = reg

	return Group{name: name}
}

// Style returns the cached style for the group or the zero-value style when absent.
func Style(group Group) style.Style {
	if group.name == "" {
		return style.S()
	}

	mu.RLock()
	defer mu.RUnlock()

	return cache[group.name]
}

// Groups returns a stable snapshot of all registered groups sorted by name.
func Groups() []Group {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	groups := make([]Group, 0, len(names))
	for _, name := range names {
		groups = append(groups, Group{name: name})
	}

	return groups
}

// Reload atomically replaces the cached styles built from the resolved colorscheme.
func Reload(resolved map[string]colorscheme.ResolvedColor) {
	next := styles.BuildFrom(resolved)
	if next == nil {
		next = map[string]style.Style{}
	}

	mu.Lock()
	cache = next
	mu.Unlock()
}

// Reset clears the registry and cache. It is intended for tests.
func Reset() {
	mu.Lock()
	registry = map[string]registration{}
	cache = map[string]style.Style{}
	mu.Unlock()
}
