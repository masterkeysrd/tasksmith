package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corefs "github.com/masterkeysrd/tasksmith/internal/core/fs"
)

const (
	// DefaultLsLimit is the maximum number of entries returned when no limit is specified.
	DefaultLsLimit = 200
	// MaxFilenameChars is the maximum number of characters shown in the formatted
	// ls -l line before the name is truncated with a marker.
	MaxFilenameChars = 128
)

// FileEntry represents a single directory entry in ls -l format.
type FileEntry struct {
	Name          string    `json:"name"`
	Permissions   string    `json:"permissions"`
	Links         uint64    `json:"links"`
	Owner         string    `json:"owner"`
	Group         string    `json:"group"`
	Size          int64     `json:"size"`
	Modified      time.Time `json:"modified"`
	IsDir         bool      `json:"is_dir"`
	IsSymlink     bool      `json:"is_symlink"`
	NameTruncated bool      `json:"name_truncated,omitempty"`
	LinkTarget    string    `json:"link_target,omitempty"`
}

// Ls lists files in a directory, respecting gitignore and predefined ignore rules.
func (h *ToolHandlers) Ls(ctx context.Context, in LsArgs) (LsOutput, error) {
	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return LsOutput{}, fmt.Errorf("failed to resolve path %s: %w", in.Path, err)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return LsOutput{}, fmt.Errorf("failed to read directory %s: %w", abs, err)
	}

	ig, err := corefs.NewIgnorer(abs)
	if err != nil {
		ig, _ = corefs.NewIgnorer("") // fallback: predefined rules only
	}

	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLsLimit
	}

	var files []FileEntry
	totalCount := 0

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(abs, name)

		if ig.ShouldIgnore(name, fullPath, entry.IsDir()) {
			continue
		}

		// Type filter.
		if in.Type != "" {
			switch in.Type {
			case "file":
				if entry.IsDir() {
					continue
				}
			case "dir":
				if !entry.IsDir() {
					continue
				}
			case "symlink":
				linfo, lerr := os.Lstat(fullPath)
				if lerr != nil || linfo.Mode()&os.ModeSymlink == 0 {
					continue
				}
			}
		}

		// Pattern filter.
		if in.Pattern != "" {
			matched, matchErr := filepath.Match(in.Pattern, name)
			if matchErr != nil || !matched {
				continue
			}
		}

		totalCount++

		if len(files) >= limit {
			continue // keep counting but don't build entries
		}

		linfo, lerr := os.Lstat(fullPath)
		if lerr != nil {
			continue
		}

		isSymlink := linfo.Mode()&os.ModeSymlink != 0

		sizeInfo := linfo
		if isSymlink {
			if target, serr := os.Stat(fullPath); serr == nil {
				sizeInfo = target
			}
		}

		owner, group := getFileOwnerGroup(linfo)
		links := getHardLinkCount(linfo)

		nameTruncated := len(name) > MaxFilenameChars

		fe := FileEntry{
			Name:          name,
			Permissions:   linfo.Mode().String(),
			Links:         links,
			Owner:         owner,
			Group:         group,
			Size:          sizeInfo.Size(),
			Modified:      sizeInfo.ModTime(),
			IsDir:         entry.IsDir(),
			IsSymlink:     isSymlink,
			NameTruncated: nameTruncated,
		}

		if isSymlink {
			if linkTarget, serr := os.Readlink(fullPath); serr == nil {
				fe.LinkTarget = linkTarget
			}
		}

		files = append(files, fe)
	}

	var outFiles []LsOutputFilesItem
	for _, f := range files {
		outFiles = append(outFiles, LsOutputFilesItem{
			Name:          f.Name,
			Permissions:   f.Permissions,
			Links:         int(f.Links),
			Owner:         f.Owner,
			Group:         f.Group,
			Size:          int(f.Size),
			Modified:      f.Modified.Format(time.RFC3339),
			IsDir:         f.IsDir,
			IsSymlink:     f.IsSymlink,
			NameTruncated: f.NameTruncated,
			LinkTarget:    f.LinkTarget,
		})
	}

	return LsOutput{
		Files:      outFiles,
		TotalCount: totalCount,
		Truncated:  totalCount > limit,
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable ls -l listing instead of a raw JSON blob.
// The full structured LsOutput is still available via message.Tool.Structured.
func (o LsOutput) TextContent() string {
	var sb strings.Builder

	for _, f := range o.Files {
		fe := FileEntry{
			Name:          f.Name,
			Permissions:   f.Permissions,
			Links:         uint64(f.Links),
			Owner:         f.Owner,
			Group:         f.Group,
			Size:          int64(f.Size),
			IsDir:         f.IsDir,
			IsSymlink:     f.IsSymlink,
			NameTruncated: f.NameTruncated,
			LinkTarget:    f.LinkTarget,
		}
		if t, err := time.Parse(time.RFC3339, f.Modified); err == nil {
			fe.Modified = t
		}
		sb.WriteString(formatLsLine(fe))
		sb.WriteByte('\n')
	}

	if o.Truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: Showing %d of %d entries. Call ls again with a higher limit to see more.]",
			len(o.Files), o.TotalCount)
	} else {
		fmt.Fprintf(&sb, "\n[%d entries]", o.TotalCount)
	}

	return sb.String()
}

// FormatSize returns a compact, human-readable representation of a byte count.
// The result is always at most 6 characters, suitable for a fixed-width column.
//
//	< 1 KiB  →  "18B"
//	< 1 MiB  →  "1.2K"
//	< 1 GiB  →  "4.5M"
//	< 1 TiB  →  "2.1G"
//	≥ 1 TiB  →  "1.3T"
func FormatSize(b int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)
	switch {
	case b < kib:
		return fmt.Sprintf("%dB", b)
	case b < mib:
		return fmt.Sprintf("%.1fK", float64(b)/kib)
	case b < gib:
		return fmt.Sprintf("%.1fM", float64(b)/mib)
	case b < tib:
		return fmt.Sprintf("%.1fG", float64(b)/gib)
	default:
		return fmt.Sprintf("%.1fT", float64(b)/tib)
	}
}

// formatLsLine renders a FileEntry as a single ls -l line.
// Size is displayed in human-readable form (e.g. "18B", "1.2K", "4.5M").
func formatLsLine(e FileEntry) string {
	displayName := e.Name
	if e.NameTruncated {
		displayName = e.Name[:MaxFilenameChars] + fmt.Sprintf(" ... [name truncated: %d chars]", len(e.Name))
	}
	mod := e.Modified.Format("Jan _2 15:04")
	// %6s right-aligns the size in a 6-char column, keeping columns stable.
	line := fmt.Sprintf("%-11s %3d %-8s %-8s %6s %s %s",
		e.Permissions, e.Links, e.Owner, e.Group, FormatSize(e.Size), mod, displayName)
	if e.IsSymlink && e.LinkTarget != "" {
		line += " -> " + e.LinkTarget
	}
	return line
}
