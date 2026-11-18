package mime

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
)

// Kind categorizes a detected media MIME type without coupling to AI model profiles.
type Kind string

const (
	KindImage Kind = "image"
	KindPDF   Kind = "pdf"
	KindAudio Kind = "audio"
)

// Detect inspects filename and header bytes (magic bytes + extension) to
// identify supported image, PDF, and audio MIME types. SVG files deliberately
// return false so callers keep them on the UTF-8 text path.
func Detect(filename string, data []byte) (string, Kind, bool) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if ext == ".svg" {
		return "", "", false
	}

	// http.DetectContentType handles PNG, JPEG, GIF, WebP (14+ bytes), PDF, WAVE,
	// ID3 MP3, and OGG. Check formats it lacks (12-byte WebP, FLAC, MP4/M4A,
	// headerless MP3) and whitespace-prefixed PDFs first.
	switch {
	case hasPDFMagic(data):
		return "application/pdf", KindPDF, true
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", KindImage, true
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("fLaC")):
		return "audio/flac", KindAudio, true
	case ext == ".mp3" && len(data) >= 2 && data[0] == 0xff && (data[1]&0xe0) == 0xe0:
		return "audio/mpeg", KindAudio, true
	case len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) && isAudioMP4(ext, data[8:12]):
		return "audio/mp4", KindAudio, true
	}

	sniffed := Canonical(http.DetectContentType(data))
	if sniffed == "application/ogg" && ext != ".ogv" && ext != ".ogx" {
		sniffed = "audio/ogg"
	}
	if kind, ok := KindFor(sniffed); ok {
		return sniffed, kind, true
	}
	return "", "", false
}

func hasPDFMagic(data []byte) bool {
	trimmed := bytes.TrimLeft(data[:min(len(data), 64)], " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("%PDF-"))
}

func isAudioMP4(ext string, brand []byte) bool {
	if ext == ".m4a" || ext == ".mp4" || ext == ".aac" {
		return true
	}
	return bytes.Equal(brand, []byte("M4A "))
}

// KindFor maps a supported MIME type to its media kind.
func KindFor(mimeType string) (Kind, bool) {
	switch Canonical(mimeType) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return KindImage, true
	case "application/pdf":
		return KindPDF, true
	case "audio/wav", "audio/mpeg", "audio/mp4", "audio/ogg", "audio/flac":
		return KindAudio, true
	default:
		return "", false
	}
}

// Canonical normalizes equivalent MIME spellings (such as audio/wave -> audio/wav).
func Canonical(mimeType string) string {
	clean := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch clean {
	case "image/jpg":
		return "image/jpeg"
	case "audio/wave", "audio/x-wav":
		return "audio/wav"
	case "audio/mp3":
		return "audio/mpeg"
	case "audio/m4a":
		return "audio/mp4"
	default:
		return clean
	}
}
