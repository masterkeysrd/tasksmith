package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/tool"
	corefs "github.com/masterkeysrd/tasksmith/internal/core/fs"
)

const (
	// DefaultLsLimit is the maximum number of entries returned when no limit is specified.
	DefaultLsLimit = 200
	// MaxFilenameChars is the maximum number of characters shown in the formatted
	// ls -l line before the name is truncated with a marker.
	MaxFilenameChars = 128
	// MaxRecursionDepth is the maximum allowed depth for ls.
	MaxRecursionDepth = 4
)

// FileEntry represents a single directory entry.
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
	Depth         int       `json:"depth"`
	NameTruncated bool      `json:"name_truncated,omitempty"`
	LinkTarget    string    `json:"link_target,omitempty"`
}

// Ls lists files in a directory, respecting gitignore and predefined ignore rules.
func (h *ToolHandlers) Ls(ctx context.Context, in LsArgs) (LsOutput, error) {
	depth := in.Depth
	if depth <= 0 {
		depth = 1
	}

	if depth > MaxRecursionDepth {
		return LsOutput{}, tool.NewError(fmt.Sprintf("recursion depth %d exceeds maximum allowed depth of %d; please use the 'glob' tool for deep recursive searches", depth, MaxRecursionDepth))
	}

	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return LsOutput{}, fmt.Errorf("failed to resolve path %s: %w", in.Path, err)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLsLimit
	}

	var allEntries []FileEntry
	totalCount := 0

	// Recursive walk function
	var walk func(currentPath string, currentDepth int) error
	walk = func(currentPath string, currentDepth int) error {
		if currentDepth >= depth {
			return nil
		}

		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return fmt.Errorf("failed to read directory %s: %w", currentPath, err)
		}

		ig, err := corefs.NewIgnorer(currentPath)
		if err != nil {
			ig, _ = corefs.NewIgnorer("") // fallback: predefined rules only
		}

		for _, entry := range entries {
			name := entry.Name()
			fullPath := filepath.Join(currentPath, name)

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
					if entry.Type()&os.ModeSymlink == 0 {
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

			if len(allEntries) < limit {
				isSymlink := entry.Type()&os.ModeSymlink != 0
				var fe FileEntry

				if in.Detailed {
					linfo, lerr := os.Lstat(fullPath)
					if lerr == nil {
						sizeInfo := linfo
						if isSymlink {
							if target, serr := os.Stat(fullPath); serr == nil {
								sizeInfo = target
							}
						}

						owner, group := getFileOwnerGroup(linfo)
						links := getHardLinkCount(linfo)

						fe = FileEntry{
							Name:          name,
							Permissions:   linfo.Mode().String(),
							Links:         links,
							Owner:         owner,
							Group:         group,
							Size:          sizeInfo.Size(),
							Modified:      sizeInfo.ModTime(),
							IsDir:         entry.IsDir(),
							IsSymlink:     isSymlink,
							Depth:         currentDepth,
							NameTruncated: len(name) > MaxFilenameChars,
						}
						if isSymlink {
							if linkTarget, serr := os.Readlink(fullPath); serr == nil {
								fe.LinkTarget = linkTarget
							}
						}
					}
				} else {
					fe = FileEntry{
						Name:      name,
						IsDir:     entry.IsDir(),
						IsSymlink: isSymlink,
						Depth:     currentDepth,
					}
				}

				if fe.Name != "" {
					allEntries = append(allEntries, fe)
				}
			}

			// Recurse into directories if within depth limit
			if entry.IsDir() && currentDepth+1 < depth {
				if err := walk(fullPath, currentDepth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(abs, 0); err != nil {
		return LsOutput{}, err
	}

	var outFiles []LsOutputFilesItem
	for _, f := range allEntries {
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
			Depth:         f.Depth,
			NameTruncated: f.NameTruncated,
			LinkTarget:    f.LinkTarget,
		})
	}

	return LsOutput{
		Files:      outFiles,
		TotalCount: totalCount,
		Truncated:  totalCount > limit,
		Detailed:   in.Detailed,
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable tree-like listing instead of a raw JSON blob.
func (o LsOutput) TextContent() string {
	var sb strings.Builder

	for _, f := range o.Files {
		indent := strings.Repeat("  ", f.Depth)
		if o.Detailed {
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
			sb.WriteString(indent)
			sb.WriteString(formatLsLine(fe))
		} else {
			sb.WriteString(indent)
			sb.WriteString(f.Name)
			if f.IsDir {
				sb.WriteByte('/')
			}
		}
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
