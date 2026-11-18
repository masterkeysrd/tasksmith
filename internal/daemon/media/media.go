package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/hash"
	coremime "github.com/masterkeysrd/tasksmith/internal/core/mime"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// Modality identifies a multimodal input kind matching loom profile modalities.
type Modality string

const (
	ModalityImage Modality = Modality(profile.ModalityImage)
	ModalityPDF   Modality = Modality(profile.ModalityPDF)
	ModalityAudio Modality = Modality(profile.ModalityAudio)
)

// Per-modality payload size caps enforced at ingestion time.
const (
	MaxImageBytes  int64 = 20 << 20 // 20 MiB
	MaxPDFBytes    int64 = 32 << 20 // 32 MiB
	MaxAudioBytes  int64 = 25 << 20 // 25 MiB
	MaxUploadBytes       = MaxPDFBytes
)

// Extras keys persisted inside loom media block Extras maps so checkpoints and
// timelines never store raw binary or base64 payloads.
const (
	ExtraSHA256      = api.MediaExtraSHA256
	ExtraByteSize    = api.MediaExtraByteSize
	ExtraDisplayName = api.MediaExtraDisplayName
	ExtraPages       = api.MediaExtraPages
)

var (
	ErrTooLarge        = errors.New("media: file exceeds maximum allowed size")
	ErrUnsupportedType = errors.New("media: unsupported media type")
	ErrInvalidPages    = errors.New("media: invalid PDF page range")
)

// Info describes a detected multimodal file.
type Info struct {
	Modality Modality
	MIMEType string
}

// Detect inspects filename and header bytes (magic bytes + extension) to
// identify supported image, PDF, and audio files. SVG files deliberately return
// false so they remain on the UTF-8 text path for reading and editing.
func Detect(filename string, data []byte) (Info, bool) {
	mimeType, kind, ok := coremime.Detect(filename, data)
	if !ok {
		return Info{}, false
	}
	return Info{Modality: modalityFromKind(kind), MIMEType: mimeType}, true
}

func modalityFromKind(kind coremime.Kind) Modality {
	switch kind {
	case coremime.KindImage:
		return ModalityImage
	case coremime.KindPDF:
		return ModalityPDF
	case coremime.KindAudio:
		return ModalityAudio
	default:
		return ""
	}
}

// ModalityForMIME maps a supported MIME type to its modality.
func ModalityForMIME(mime string) (Modality, bool) {
	kind, ok := coremime.KindFor(mime)
	if !ok {
		return "", false
	}
	mod := modalityFromKind(kind)
	return mod, mod != ""
}

// CanonicalMIME normalizes equivalent MIME spellings (such as audio/wave -> audio/wav).
func CanonicalMIME(mime string) string {
	return coremime.Canonical(mime)
}

// MaxBytesForModality returns the byte-size cap for mod.
func MaxBytesForModality(mod Modality) int64 {
	switch mod {
	case ModalityImage:
		return MaxImageBytes
	case ModalityPDF:
		return MaxPDFBytes
	case ModalityAudio:
		return MaxAudioBytes
	default:
		return 0
	}
}

// CheckSize enforces the per-modality byte-size cap.
func CheckSize(mod Modality, size int64) error {
	limit := MaxBytesForModality(mod)
	if limit <= 0 {
		return fmt.Errorf("%w: %q", ErrUnsupportedType, mod)
	}
	if size <= 0 {
		return fmt.Errorf("%w: empty payload", ErrUnsupportedType)
	}
	if size > limit {
		return fmt.Errorf("%w: %s payload (%s) exceeds maximum %s limit (%s)",
			ErrTooLarge, mod, format.Bytes(size), mod, format.Bytes(limit))
	}
	return nil
}

// ParsePages validates and normalizes an optional PDF page range specification
// such as "1-10", "3", or "1-3, 5, 8-10". An empty input returns "", nil.
func ParsePages(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parts := strings.Split(trimmed, ",")
	norm := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", fmt.Errorf("%w: %q", ErrInvalidPages, raw)
		}
		if startStr, endStr, ok := strings.Cut(part, "-"); ok {
			start, err1 := strconv.Atoi(strings.TrimSpace(startStr))
			end, err2 := strconv.Atoi(strings.TrimSpace(endStr))
			if err1 != nil || err2 != nil || start <= 0 || end < start {
				return "", fmt.Errorf("%w: %q", ErrInvalidPages, raw)
			}
			if start == end {
				norm = append(norm, strconv.Itoa(start))
			} else {
				norm = append(norm, fmt.Sprintf("%d-%d", start, end))
			}
		} else {
			page, err := strconv.Atoi(part)
			if err != nil || page <= 0 {
				return "", fmt.Errorf("%w: %q", ErrInvalidPages, raw)
			}
			norm = append(norm, strconv.Itoa(page))
		}
	}
	return strings.Join(norm, ","), nil
}

// FormatViewSummary builds the compact metadata text returned for a viewed media file.
func FormatViewSummary(path, mime string, size int64, pages string) string {
	if pages != "" {
		return fmt.Sprintf("Viewed %s (%s, %s, pages %s)", path, mime, format.Bytes(size), pages)
	}
	return fmt.Sprintf("Viewed %s (%s, %s)", path, mime, format.Bytes(size))
}

// BlockFromDescriptor constructs a lightweight loom media block carrying only
// descriptor metadata in Extras (never raw binary or base64 bytes).
func BlockFromDescriptor(desc api.Media) (message.Block, error) {
	if !hash.IsValidSHA256(desc.SHA256) {
		return nil, fmt.Errorf("media: invalid sha256 %q", desc.SHA256)
	}
	mime := CanonicalMIME(desc.MIMEType)
	mod, ok := ModalityForMIME(mime)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedType, desc.MIMEType)
	}
	if desc.ByteSize <= 0 {
		return nil, fmt.Errorf("media: byte_size must be positive")
	}
	extras := map[string]any{
		ExtraSHA256:   desc.SHA256,
		ExtraByteSize: desc.ByteSize,
	}
	if desc.DisplayName != "" {
		extras[ExtraDisplayName] = desc.DisplayName
	}
	if desc.Pages != "" {
		extras[ExtraPages] = desc.Pages
	}
	switch mod {
	case ModalityImage:
		return &message.ImageBlock{MIMEType: mime, Extras: extras}, nil
	case ModalityPDF:
		return &message.DocumentBlock{MIMEType: mime, Extras: extras}, nil
	case ModalityAudio:
		return &message.AudioBlock{MIMEType: mime, Extras: extras}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedType, mime)
	}
}

// DescriptorFromBlock extracts a media descriptor and modality from a loom block.
// It returns ok=true for any ImageBlock, AudioBlock, or PDF DocumentBlock.
func DescriptorFromBlock(b message.Block) (api.Media, Modality, bool) {
	var mime string
	var mod Modality
	var byteSize int64
	var extras map[string]any

	switch v := b.(type) {
	case *message.ImageBlock:
		if v == nil {
			return api.Media{}, "", false
		}
		mime = CanonicalMIME(v.MIMEType)
		mod = ModalityImage
		byteSize = int64(len(v.Data))
		extras = v.Extras
	case *message.DocumentBlock:
		if v == nil {
			return api.Media{}, "", false
		}
		mime = CanonicalMIME(v.MIMEType)
		if mime == "application/pdf" {
			mod = ModalityPDF
		}
		byteSize = int64(len(v.Data))
		extras = v.Extras
	case *message.AudioBlock:
		if v == nil {
			return api.Media{}, "", false
		}
		mime = CanonicalMIME(v.MIMEType)
		mod = ModalityAudio
		byteSize = int64(len(v.Data))
		extras = v.Extras
	default:
		return api.Media{}, "", false
	}

	desc := api.Media{
		MIMEType: mime,
		ByteSize: byteSize,
	}
	if extras != nil {
		if sha, ok := extras[ExtraSHA256].(string); ok {
			desc.SHA256 = sha
		}
		if sz := extraInt64(extras[ExtraByteSize]); sz > 0 {
			desc.ByteSize = sz
		}
		if name, ok := extras[ExtraDisplayName].(string); ok {
			desc.DisplayName = name
		}
		if pages, ok := extras[ExtraPages].(string); ok {
			desc.Pages = pages
		}
	}
	return desc, mod, true
}

// ModalityOfBlock returns the input modality represented by block, or "" if text/non-media.
func ModalityOfBlock(b message.Block) (Modality, bool) {
	_, mod, ok := DescriptorFromBlock(b)
	return mod, ok && mod != ""
}

// OmittedPlaceholder formats the fallback text placeholder used when historical
// media is unsupported by the current model.
func OmittedPlaceholder(mod Modality, desc api.Media) string {
	name := strings.TrimSpace(desc.DisplayName)
	if name == "" {
		name = string(mod)
	}
	label := string(mod)
	if label == "" {
		label = "media"
	}
	return fmt.Sprintf("[%s %s omitted: unsupported by current model]", label, name)
}

// UnavailablePlaceholder formats the fallback text placeholder used when
// persisted media bytes are missing or unreadable in the session media store.
func UnavailablePlaceholder(mod Modality, desc api.Media) string {
	name := strings.TrimSpace(desc.DisplayName)
	if name == "" {
		name = string(mod)
	}
	label := string(mod)
	if label == "" {
		label = "media"
	}
	return fmt.Sprintf("[%s %s omitted: media unavailable on disk]", label, name)
}

func extraInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}
