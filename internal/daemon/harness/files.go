package harness

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// FileMention represents an explicit file or directory reference attached to a user prompt.
type FileMention struct {
	Raw       string
	Path      string
	IsDir     bool
	StartLine int
	EndLine   int
}

func (r *AttachmentResolver) accessChecker() *permissions.FileAccessChecker {
	return permissions.NewFileAccessChecker(permissions.FileAccessConfig{
		WorkDir:   r.workDir,
		Workspace: r.workspace,
		Session:   r.perms,
	})
}

// CheckFileAccess verifies that mention stays within the sandbox root (including
// symlink resolution), is not sensitive or secret, and is permitted by explicit
// permission rules and policy ceilings.
func (r *AttachmentResolver) CheckFileAccess(ctx context.Context, mention FileMention) error {
	return r.checkFileAccess(ctx, mention, r.accessChecker())
}

func (r *AttachmentResolver) checkFileAccess(ctx context.Context, mention FileMention, checker *permissions.FileAccessChecker) error {
	clean := filepath.ToSlash(filepath.Clean(mention.Path))
	if (clean == "." || clean == "/" || clean == "") && !mention.IsDir {
		return errors.New("invalid file mention: empty path")
	}

	rel, err := r.sandbox.FS().ResolvePath(clean)
	if err != nil {
		return fmt.Errorf("file mention %q escapes workspace root: %w", mention.Path, err)
	}

	isDir := mention.IsDir
	linfo, lstatErr := r.sandbox.FS().Lstat(ctx, rel)
	if errors.Is(lstatErr, sandbox.ErrPathEscape) {
		return fmt.Errorf("file mention %q escapes workspace root: %w", mention.Path, lstatErr)
	}
	if lstatErr == nil {
		info, statErr := r.sandbox.FS().Stat(ctx, rel)
		if errors.Is(statErr, sandbox.ErrPathEscape) {
			return fmt.Errorf("file mention %q escapes workspace root: %w", mention.Path, statErr)
		}
		if statErr != nil && linfo.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("file mention %q points to an inaccessible symlink target: %w", mention.Path, statErr)
		}
		if statErr == nil && info.IsDir() {
			isDir = true
		}
	}

	return checker.Check(rel, cmp.Or(mention.Raw, mention.Path), isDir)
}

func fileMentionFromAttachment(att api.Attachment) (FileMention, bool) {
	target := strings.TrimSpace(att.Target)
	if target == "" {
		return FileMention{}, false
	}
	isDir := strings.HasSuffix(target, "/") || target == "."
	cleanPath := filepath.ToSlash(filepath.Clean(target))
	if isDir && !strings.HasSuffix(cleanPath, "/") && cleanPath != "." {
		cleanPath += "/"
	}
	startLine, endLine := normalizeLineRange(att.StartLine, att.EndLine)
	return FileMention{
		Raw:       cmp.Or(att.Ref, cleanPath),
		Path:      cleanPath,
		IsDir:     isDir,
		StartLine: startLine,
		EndLine:   endLine,
	}, true
}

func (r *AttachmentResolver) isModalitySupported(mod media.Modality) (string, bool) {
	if r.supportsModality == nil {
		return "", true
	}
	return r.supportsModality(mod)
}

func (r *AttachmentResolver) filterUserMediaBlocks(user *message.User) []string {
	if user == nil || len(user.Content) == 0 || r.supportsModality == nil {
		return nil
	}
	var notes []string
	kept := make(message.Content, 0, len(user.Content))
	for _, blk := range user.Content {
		desc, mod, isMedia := media.DescriptorFromBlock(blk)
		if !isMedia || mod == "" {
			kept = append(kept, blk)
			continue
		}
		if modelID, ok := r.supportsModality(mod); !ok {
			name := cmp.Or(desc.DisplayName, string(mod))
			notes = append(notes, fmt.Sprintf(
				"<context_media name=%q mime_type=%q status=\"omitted\">Cannot attach %s %q: active model %q does not support %s input.</context_media>",
				name, desc.MIMEType, mod, name, modelID, mod,
			))
			continue
		}
		kept = append(kept, blk)
	}
	if len(notes) > 0 {
		if len(kept) == 0 {
			kept = append(kept, &message.TextBlock{Text: "[media omitted: unsupported by current model]"})
		}
		user.Content = kept
	}
	return notes
}

func (r *AttachmentResolver) resolveFileAndMediaBlocks(ctx context.Context, atts []api.Attachment) ([]string, []message.Block) {
	if len(atts) == 0 {
		return nil, nil
	}

	var blocks []string
	var mediaBlocks []message.Block
	seen := make(map[string]struct{})
	seenMedia := make(map[string]struct{})
	checker := r.accessChecker()

	for _, att := range atts {
		if att.Kind == api.AttachmentMedia {
			if att.Media == nil {
				continue
			}
			mod, ok := media.ModalityForMIME(att.Media.MIMEType)
			if !ok {
				continue
			}
			if _, dup := seenMedia[att.Media.SHA256]; dup {
				continue
			}
			seenMedia[att.Media.SHA256] = struct{}{}
			name := cmp.Or(att.Media.DisplayName, att.Ref, att.Media.SHA256)
			if modelID, supported := r.isModalitySupported(mod); !supported {
				blocks = append(blocks, fmt.Sprintf(
					"<context_media name=%q mime_type=%q status=\"omitted\">Cannot attach %s %q: active model %q does not support %s input.</context_media>",
					name, att.Media.MIMEType, mod, name, modelID, mod,
				))
				continue
			}
			blk, err := media.BlockFromDescriptor(*att.Media)
			if err != nil {
				continue
			}
			mediaBlocks = append(mediaBlocks, blk)
			blocks = append(blocks, fmt.Sprintf(
				"<context_media name=%q mime_type=%q size=%q />",
				name, att.Media.MIMEType, format.Bytes(att.Media.ByteSize),
			))
			continue
		}

		if att.Kind != api.AttachmentFile {
			continue
		}
		m, ok := fileMentionFromAttachment(att)
		if !ok {
			continue
		}

		dedupKey := fmt.Sprintf("%s:%d:%d", m.Path, m.StartLine, m.EndLine)
		if _, exists := seen[dedupKey]; exists {
			continue
		}
		seen[dedupKey] = struct{}{}

		if err := r.checkFileAccess(ctx, m, checker); err != nil {
			r.logger.Warn("file mention access rejected", log.String("path", m.Path), log.Any("err", err))
			continue
		}

		rel, err := r.sandbox.FS().ResolvePath(m.Path)
		if err != nil {
			continue
		}

		if !m.IsDir {
			if info, err := r.sandbox.FS().Stat(ctx, rel); err == nil && info.IsDir() {
				m.IsDir = true
			}
		}

		refAttr := ""
		if att.Ref != "" {
			refAttr = fmt.Sprintf(" ref=%q", att.Ref)
		}

		if m.IsDir {
			res, err := r.std.List(ctx, std.ListRequest{
				Path:          rel,
				Limit:         100,
				IncludeHidden: true,
			})
			if err != nil {
				r.logger.Warn("file mention: list failed", log.String("path", rel), log.Any("err", err))
				continue
			}

			var entryLines []string
			for _, entry := range res.Entries {
				if err := r.checkFileAccess(ctx, FileMention{Path: entry.Path, IsDir: entry.IsDir}, checker); err != nil {
					continue
				}
				entryType := "file"
				if entry.IsDir {
					entryType = "dir"
				}
				entryLines = append(entryLines, fmt.Sprintf("- %s (%s, %d bytes)", entry.Name, entryType, entry.Size))
			}
			content := strings.Join(entryLines, "\n")
			if res.Truncated {
				content += "\n... (truncated)"
			}
			blocks = append(blocks, fmt.Sprintf("<context_directory%s path=%q>\n%s\n</context_directory>", refAttr, m.Path, content))
		} else {
			offset := max(1, m.StartLine)
			limit := 0
			if m.EndLine >= offset {
				limit = m.EndLine - offset + 1
			}

			res, err := r.std.View(ctx, std.ViewRequest{
				Path:   rel,
				Offset: offset,
				Limit:  limit,
			})
			if err != nil {
				if errors.Is(err, media.ErrTooLarge) {
					blocks = append(blocks, fmt.Sprintf("<context_media%s path=%q status=\"omitted\">%v</context_media>", refAttr, m.Path, err))
				} else {
					r.logger.Warn("file mention: view failed", log.String("path", rel), log.Any("err", err))
				}
				continue
			}

			if res.Media != nil {
				if modelID, supported := r.isModalitySupported(res.Media.Modality); !supported {
					blocks = append(blocks, fmt.Sprintf(
						"<context_media%s path=%q mime_type=%q status=\"omitted\">Cannot attach %s %q: active model %q does not support %s input.</context_media>",
						refAttr, m.Path, res.Media.MIMEType, res.Media.Modality, m.Path, modelID, res.Media.Modality,
					))
					continue
				}
				sha := res.Hash
				if r.mediaStore != nil {
					stored, _, err := r.mediaStore.Put(ctx, res.Media.Data)
					if err != nil {
						r.logger.Warn("file mention: store media failed", log.String("path", rel), log.Any("err", err))
						continue
					}
					sha = stored
				}
				if _, dup := seenMedia[sha]; dup {
					continue
				}
				seenMedia[sha] = struct{}{}
				blk, err := media.BlockFromDescriptor(api.Media{
					SHA256:      sha,
					MIMEType:    res.Media.MIMEType,
					ByteSize:    res.Media.ByteSize,
					DisplayName: filepath.Base(m.Path),
				})
				if err != nil {
					continue
				}
				mediaBlocks = append(mediaBlocks, blk)
				blocks = append(blocks, fmt.Sprintf(
					"<context_media%s path=%q mime_type=%q size=%q />",
					refAttr, m.Path, res.Media.MIMEType, format.Bytes(res.Media.ByteSize),
				))
				continue
			}

			linesAttr := ""
			if m.StartLine > 0 {
				linesAttr = fmt.Sprintf(" lines=%q", fmt.Sprintf("%d-%d", res.StartLine, res.EndLine))
			}

			blocks = append(blocks, fmt.Sprintf("<context_file%s path=%q%s>\n%s\n</context_file>", refAttr, m.Path, linesAttr, res.Numbered))
		}
	}

	return blocks, mediaBlocks
}
