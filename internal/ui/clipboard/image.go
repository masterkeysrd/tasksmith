package clipboard

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	coremime "github.com/masterkeysrd/tasksmith/internal/core/mime"
)

// MaxImageBytes is the maximum byte size accepted for pasted or dropped images.
const MaxImageBytes = 20 << 20

// ImageCandidate describes an image resolved from a paste or file drop event.
type ImageCandidate struct {
	MIMEType    string
	Data        []byte
	DisplayName string
	TooLarge    int64
}

// ResolvePastedImages extracts image candidates from a clipboard event's MIME
// items and text payload using strict source precedence:
//  1. Dropped local file paths in text (so stale clipboard data cannot hijack drops).
//  2. OS file-manager copies in items["text/uri-list"] (ignoring Finder icon thumbnails).
//  3. Raw image payloads in items (screenshots, Preview copies, browser images).
func ResolvePastedImages(items map[string][]byte, text string) []ImageCandidate {
	if paths := parseDroppedLocalPaths(text); len(paths) > 0 {
		return readLocalImageCandidates(paths)
	}

	if rawURIs := items["text/uri-list"]; len(rawURIs) > 0 {
		paths := parseURIListPaths(string(rawURIs))
		if len(paths) == 0 {
			return nil
		}
		return readLocalImageCandidates(paths)
	}

	if mimeType, data, ok := firstClipboardImage(items); ok {
		if int64(len(data)) > MaxImageBytes {
			return []ImageCandidate{{TooLarge: int64(len(data))}}
		}
		return []ImageCandidate{{
			MIMEType: mimeType,
			Data:     data,
		}}
	}
	return nil
}

// ExtForImageMIME returns the conventional file extension for a supported image MIME type.
func ExtForImageMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func parseURIListPaths(raw string) []string {
	var paths []string
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if p, ok := resolveFileURI(line); ok {
			paths = append(paths, p)
		}
	}
	return paths
}

func resolveFileURI(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil || p == "" {
		return "", false
	}
	return filepath.Clean(p), true
}

func parseDroppedLocalPaths(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	var lines []string
	for line := range strings.SplitSeq(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}

	if len(lines) == 1 {
		if p, ok := resolveExistingRegularFile(lines[0]); ok {
			return []string{p}
		}
		// Terminals separate multi-file drops with unescaped spaces.
		tokens := splitShellDropTokens(lines[0])
		if len(tokens) > 1 {
			var paths []string
			for _, tok := range tokens {
				p, ok := resolveExistingRegularFile(tok)
				if !ok {
					return nil
				}
				paths = append(paths, p)
			}
			return paths
		}
		return nil
	}

	var paths []string
	for _, line := range lines {
		p, ok := resolveExistingRegularFile(line)
		if !ok {
			return nil
		}
		paths = append(paths, p)
	}
	return paths
}

func resolveExistingRegularFile(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		s = s[1 : len(s)-1]
	} else {
		s = unescapePOSIXPath(s)
	}
	if strings.HasPrefix(strings.ToLower(s), "file://") {
		var ok bool
		s, ok = resolveFileURI(s)
		if !ok {
			return "", false
		}
	} else if strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", false
		}
		s = filepath.Join(home, s[2:])
	}
	if !filepath.IsAbs(s) {
		return "", false
	}
	info, err := os.Stat(s)
	if err != nil || info.IsDir() {
		return "", false
	}
	return filepath.Clean(s), true
}

func unescapePOSIXPath(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func splitShellDropTokens(s string) []string {
	var tokens []string
	var cur strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			cur.WriteByte('\\')
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '\'', '"':
			quote = ch
		case ' ', '\t':
			flush()
		default:
			cur.WriteByte(ch)
		}
	}
	if escaped {
		cur.WriteByte('\\')
	}
	flush()
	return tokens
}

func readLocalImageCandidates(paths []string) []ImageCandidate {
	var out []ImageCandidate
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var header [512]byte
		n, _ := io.ReadFull(f, header[:])
		if n == 0 {
			_ = f.Close()
			continue
		}
		mimeType, kind, ok := coremime.Detect(p, header[:n])
		if !ok || kind != coremime.KindImage {
			_ = f.Close()
			continue
		}
		if info.Size() > MaxImageBytes {
			_ = f.Close()
			out = append(out, ImageCandidate{
				DisplayName: filepath.Base(p),
				TooLarge:    info.Size(),
			})
			continue
		}
		rest, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
		_ = f.Close()
		if err != nil {
			continue
		}
		data := make([]byte, n+len(rest))
		copy(data, header[:n])
		copy(data[n:], rest)
		if int64(len(data)) > MaxImageBytes {
			out = append(out, ImageCandidate{
				DisplayName: filepath.Base(p),
				TooLarge:    int64(len(data)),
			})
			continue
		}
		if detected, dkind, dok := coremime.Detect(p, data); dok && dkind == coremime.KindImage {
			mimeType = detected
		}
		out = append(out, ImageCandidate{
			MIMEType:    mimeType,
			Data:        data,
			DisplayName: filepath.Base(p),
		})
	}
	return out
}

func firstClipboardImage(items map[string][]byte) (string, []byte, bool) {
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp", "image/gif"} {
		if data := items[mime]; len(data) > 0 {
			if detected, kind, ok := coremime.Detect("", data); ok && kind == coremime.KindImage {
				return detected, data, true
			}
		}
	}
	for _, data := range items {
		if len(data) == 0 {
			continue
		}
		if detected, kind, ok := coremime.Detect("", data); ok && kind == coremime.KindImage {
			return detected, data, true
		}
	}
	return "", nil, false
}
