// Package colorscheme provides a Neovim-inspired colorsystem with support
// for highlight groups, linking, and topological resolution.
package colorscheme

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// Default is the name of the default colorscheme.
const Default = "dark"

//go:embed builtin/*.json
var builtinFS embed.FS

// Highlight represents a Neovim-style highlight group with optional properties.
// A property is nil when not set (zero-value safe).
// Link specifies another highlight group to inherit from; if set, explicit
// properties override the linked group's resolved properties.
type Highlight struct {
	Fg        *string `json:"fg,omitempty"`
	Bg        *string `json:"bg,omitempty"`
	Bold      *bool   `json:"bold,omitempty"`
	Underline *bool   `json:"underline,omitempty"`
	Italic    *bool   `json:"italic,omitempty"`
	Reverse   *bool   `json:"reverse,omitempty"`
	Link      *string `json:"link,omitempty"`
}

// Colorscheme represents a complete terminal colorscheme.
type Colorscheme struct {
	Name    string               `json:"name"`
	Palette map[string]string    `json:"palette,omitempty"`
	Groups  map[string]Highlight `json:"groups"`
}

// ResolvedColor is the final resolved color for a highlight group
// after applying all links and overrides.
type ResolvedColor struct {
	Fg        string
	Bg        string
	Bold      bool
	Underline bool
	Italic    bool
	Reverse   bool
}

// hexPattern matches a valid 6 or 8-character hex color with optional # prefix.
// 8-char format includes alpha channel (e.g., #ffffff0d).
var hexPattern = regexp.MustCompile(`^#?[0-9a-fA-F]{6}(?:[0-9a-fA-F]{2})?$`)

// Load parses a JSON colorscheme file from the given path.
// Returns an error if the file is invalid or missing.
func Load(path string) (*Colorscheme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read colorscheme file: %w", err)
	}

	var cs Colorscheme
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, fmt.Errorf("invalid colorscheme JSON: %w", err)
	}

	resolvePalette(&cs)
	return &cs, nil
}

// getBuiltin returns a built-in colorscheme by name.
func getBuiltin(name string) (*Colorscheme, error) {
	path := filepath.Join("builtin", name+".json")
	data, err := builtinFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("builtin colorscheme %q not found: %w", name, err)
	}

	var cs Colorscheme
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, fmt.Errorf("failed to parse builtin colorscheme %q: %w", name, err)
	}

	resolvePalette(&cs)
	return &cs, nil
}

// resolvePalette replaces named colors in highlight groups with hex values from the palette.
func resolvePalette(cs *Colorscheme) {
	if cs.Palette == nil || cs.Groups == nil {
		return
	}

	for group, h := range cs.Groups {
		if h.Fg != nil {
			if val, ok := cs.Palette[*h.Fg]; ok {
				h.Fg = &val
			}
		}
		if h.Bg != nil {
			if val, ok := cs.Palette[*h.Bg]; ok {
				h.Bg = &val
			}
		}
		cs.Groups[group] = h
	}
}

// ListBuiltin returns a list of all built-in colorscheme names.
func ListBuiltin() []string {
	var names []string
	entries, _ := builtinFS.ReadDir("builtin")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if before, ok := strings.CutSuffix(name, ".json"); ok {
			names = append(names, before)
		}
	}
	return names
}

// ListUser returns a list of all user-defined colorscheme names found in the
// XDG configuration directory.
func ListUser() []string {
	var names []string
	dir, err := xdg.SubConfigDir("colorschemes")
	if err != nil {
		return names
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if before, ok := strings.CutSuffix(name, ".json"); ok {
			names = append(names, before)
		}
	}
	return names
}

// List returns all available colorscheme names (builtin + user).
func List() []string {
	seen := make(map[string]bool)
	var names []string

	// Add user schemes first so they can shadow builtins if desired
	for _, name := range ListUser() {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	for _, name := range ListBuiltin() {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	return names
}

// Find looks for a colorscheme by name in user configuration first, then
// falls back to built-ins.
func Find(name string) (*Colorscheme, error) {
	// Try user directory first
	dir, err := xdg.SubConfigDir("colorschemes")
	if err == nil {
		path := filepath.Join(dir, name+".json")
		if cs, err := Load(path); err == nil {
			return cs, nil
		}
	}

	// Fallback to built-in
	return getBuiltin(name)
}

// Merge overlays colorschemes from left to right and returns a merged copy.
// Later schemes replace earlier groups wholesale. Nil schemes are ignored.
func Merge(schemes ...*Colorscheme) *Colorscheme {
	merged := &Colorscheme{
		Groups: map[string]Highlight{},
	}

	for _, scheme := range schemes {
		if scheme == nil {
			continue
		}

		if scheme.Name != "" {
			merged.Name = scheme.Name
		}

		for group, highlight := range scheme.Groups {
			merged.Groups[group] = cloneHighlight(highlight)
		}
	}

	return merged
}

// ResolveAll validates the colorscheme (no circular links, no missing targets)
// and returns an error if validation fails.
// Call this before Resolve() to ensure safety.
func ResolveAll(c *Colorscheme) error {
	if c == nil || c.Groups == nil {
		return nil
	}

	// Validate hex colors in all highlights
	for group, h := range c.Groups {
		if h.Fg != nil && !isValidHexColor(*h.Fg) {
			return fmt.Errorf("invalid hex color in highlight %q: fg=%q", group, *h.Fg)
		}
		if h.Bg != nil && !isValidHexColor(*h.Bg) {
			return fmt.Errorf("invalid hex color in highlight %q: bg=%q", group, *h.Bg)
		}
	}

	// Build adjacency list from link fields
	links := buildLinks(c.Groups)

	// Check for missing targets
	for group, deps := range links {
		for _, dep := range deps {
			if _, ok := c.Groups[dep]; !ok {
				return fmt.Errorf("highlight %q links to non-existent group %q", group, dep)
			}
		}
	}

	// Detect circular dependencies using DFS
	if hasCycle(links) {
		return fmt.Errorf("circular dependency detected in colorscheme links")
	}

	return nil
}

// Resolve returns a map of all highlight groups with their fully resolved
// colors. Groups are resolved in topological order: links are resolved
// before the groups that depend on them.
// Returns map[groupName]ResolvedColor.
func Resolve(c *Colorscheme) map[string]ResolvedColor {
	result := make(map[string]ResolvedColor)
	if c == nil || c.Groups == nil {
		return result
	}

	links := buildLinks(c.Groups)
	visited := make(map[string]bool)

	// Process groups in topological order
	for group := range c.Groups {
		resolveGroup(c, group, links, result, visited)
	}

	return result
}

// GetResolved returns the resolved color for a single group.
// Returns (ResolvedColor, false) if the group does not exist.
func GetResolved(c *Colorscheme, group string) (ResolvedColor, bool) {
	if c == nil || c.Groups == nil {
		return ResolvedColor{}, false
	}

	resolved := Resolve(c)
	color, ok := resolved[group]
	return color, ok
}

// resolveGroup recursively resolves a highlight group and its dependencies.
func resolveGroup(c *Colorscheme, group string, links map[string][]string, result map[string]ResolvedColor, visited map[string]bool) ResolvedColor {
	if color, ok := result[group]; ok {
		return color
	}

	visited[group] = true
	highlight, exists := c.Groups[group]
	if !exists {
		visited[group] = false
		return ResolvedColor{}
	}

	var resolved ResolvedColor

	// If linked, resolve the target first and inherit
	if highlight.Link != nil {
		target := *highlight.Link
		if targetColor, ok := result[target]; ok {
			resolved = targetColor
		} else if _, ok := visited[target]; !ok {
			resolved = resolveGroup(c, target, links, result, visited)
		}
	}

	// Apply explicit properties on top of inherited ones
	if highlight.Fg != nil {
		resolved.Fg = *highlight.Fg
	}
	if highlight.Bg != nil {
		resolved.Bg = *highlight.Bg
	}
	if highlight.Bold != nil {
		resolved.Bold = *highlight.Bold
	}
	if highlight.Underline != nil {
		resolved.Underline = *highlight.Underline
	}
	if highlight.Italic != nil {
		resolved.Italic = *highlight.Italic
	}
	if highlight.Reverse != nil {
		resolved.Reverse = *highlight.Reverse
	}

	result[group] = resolved
	visited[group] = false
	return resolved
}

// buildLinks constructs an adjacency list from link fields.
func buildLinks(highlights map[string]Highlight) map[string][]string {
	links := make(map[string][]string)
	for group, h := range highlights {
		if h.Link != nil {
			links[group] = []string{*h.Link}
		}
	}
	return links
}

// hasCycle detects cycles in the dependency graph using DFS.
func hasCycle(links map[string][]string) bool {
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	for node := range links {
		if !visited[node] {
			if hasCycleRecursive(node, links, visited, recursionStack) {
				return true
			}
		}
	}
	return false
}

func hasCycleRecursive(node string, links map[string][]string, visited, stack map[string]bool) bool {
	visited[node] = true
	stack[node] = true

	for _, neighbor := range links[node] {
		if !visited[neighbor] {
			if hasCycleRecursive(neighbor, links, visited, stack) {
				return true
			}
		} else if stack[neighbor] {
			return true
		}
	}

	stack[node] = false
	return false
}

// isValidHexColor checks if a string is a valid CSS-style hex color.
func isValidHexColor(s string) bool {
	return hexPattern.MatchString(s)
}

func cloneHighlight(h Highlight) Highlight {
	res := Highlight{
		Bold:      copyBool(h.Bold),
		Underline: copyBool(h.Underline),
		Italic:    copyBool(h.Italic),
		Reverse:   copyBool(h.Reverse),
	}
	if h.Fg != nil {
		s := *h.Fg
		res.Fg = &s
	}
	if h.Bg != nil {
		s := *h.Bg
		res.Bg = &s
	}
	if h.Link != nil {
		s := *h.Link
		res.Link = &s
	}
	return res
}

func copyBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}
