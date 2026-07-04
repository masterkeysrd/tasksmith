package preview

import (
	"encoding/json"
	"time"
)

// ToolPreview represents a generic structured preview for a tool's execution or result.
type ToolPreview interface {
	// Type returns the visual category of the preview (e.g., "file_edit", "shell_command").
	Type() string
}

// FileEditPreview represents a file modification diff.
type FileEditPreview struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

func (FileEditPreview) Type() string { return "file_edit" }

func (p FileEditPreview) MarshalJSON() ([]byte, error) {
	type Alias FileEditPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// BashPreview represents a shell command execution.
type BashPreview struct {
	Command string `json:"command"`
}

func (BashPreview) Type() string { return "shell_command" }

func (p BashPreview) MarshalJSON() ([]byte, error) {
	type Alias BashPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// FileListPreview represents a list of files (e.g., glob matches).
type FileListPreview struct {
	Files []string `json:"files"`
}

func (FileListPreview) Type() string { return "file_list" }

func (p FileListPreview) MarshalJSON() ([]byte, error) {
	type Alias FileListPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// GrepMatch represents a single search match line.
type GrepMatch struct {
	Path       string `json:"path"`
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
}

// GrepMatchesPreview represents a list of grep search results.
type GrepMatchesPreview struct {
	Matches []GrepMatch `json:"matches"`
}

func (GrepMatchesPreview) Type() string { return "grep_matches" }

func (p GrepMatchesPreview) MarshalJSON() ([]byte, error) {
	type Alias GrepMatchesPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// LsEntry represents a single directory listing entry.
type LsEntry struct {
	Name        string    `json:"name"`
	IsDir       bool      `json:"is_dir"`
	SizeBytes   int64     `json:"size_bytes"`
	IsSymlink   bool      `json:"is_symlink"`
	Depth       int       `json:"depth"`
	LinkTarget  string    `json:"link_target"`
	Permissions string    `json:"permissions"`
	Links       uint64    `json:"links"`
	Owner       string    `json:"owner"`
	Group       string    `json:"group"`
	Modified    time.Time `json:"modified"`
}

// LsPreview represents a directory listing.
type LsPreview struct {
	Detailed bool      `json:"detailed"`
	Entries  []LsEntry `json:"entries"`
}

func (LsPreview) Type() string { return "ls_entries" }

func (p LsPreview) MarshalJSON() ([]byte, error) {
	type Alias LsPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// DefaultTextPreview represents simple text fallbacks.
type DefaultTextPreview struct {
	Text string `json:"text"`
}

func (DefaultTextPreview) Type() string { return "default_text" }

func (p DefaultTextPreview) MarshalJSON() ([]byte, error) {
	type Alias DefaultTextPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// FileViewPreview represents viewing the content of a file (e.g. read_file).
type FileViewPreview struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	IsBinary  bool   `json:"is_binary"`
	MimeType  string `json:"mime_type"`
	StartLine int    `json:"start_line"`
}

func (FileViewPreview) Type() string { return "file_view" }

func (p FileViewPreview) MarshalJSON() ([]byte, error) {
	type Alias FileViewPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// MarkdownPreview represents simple markdown content.
type MarkdownPreview struct {
	Markdown string `json:"markdown"`
}

func (MarkdownPreview) Type() string { return "markdown" }

func (p MarkdownPreview) MarshalJSON() ([]byte, error) {
	type Alias MarkdownPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}

// SymbolViewPreview represents a symbol definition snippet, documentation, diagnostics, and workspace info.
type SymbolViewPreview struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	File            string   `json:"file"`
	Snippet         string   `json:"snippet"`
	Docs            string   `json:"docs"`
	Diagnostics     string   `json:"diagnostics"`
	References      []string `json:"references"`
	Implementations []string `json:"implementations"`
}

func (SymbolViewPreview) Type() string { return "symbol_view" }

func (p SymbolViewPreview) MarshalJSON() ([]byte, error) {
	type Alias SymbolViewPreview
	return json.Marshal(&struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  p.Type(),
		Alias: Alias(p),
	})
}
