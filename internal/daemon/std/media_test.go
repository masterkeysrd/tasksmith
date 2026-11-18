package std

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
)

func TestViewMediaAndSVGTextPath(t *testing.T) {
	s, tracker, workDir := newTestStd(t, true)
	ctx := context.Background()

	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 1}
	pdfBytes := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	wavBytes := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	svgText := "<svg xmlns=\"http://www.w3.org/2000/svg\">\n  <rect width=\"10\" height=\"10\"/>\n</svg>\n"

	for name, content := range map[string][]byte{
		"shot.png": pngBytes,
		"spec.pdf": pdfBytes,
		"clip.wav": wavBytes,
		"icon.svg": []byte(svgText),
	} {
		if err := os.WriteFile(filepath.Join(workDir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// PNG ignores offset/limit and records SHA-256 read hash for CAS.
	pngRes, err := s.View(ctx, ViewRequest{Path: "shot.png", Offset: 10, Limit: 5})
	if err != nil {
		t.Fatalf("View shot.png: %v", err)
	}
	if pngRes.Media == nil || pngRes.Media.MIMEType != "image/png" || pngRes.Media.Modality != media.ModalityImage {
		t.Fatalf("unexpected PNG media result: %+v", pngRes.Media)
	}
	if _, ok := tracker.LastHash("shot.png"); !ok {
		t.Fatal("expected CAS read hash to be recorded for shot.png")
	}

	// PDF validates and normalizes pages range.
	pdfRes, err := s.View(ctx, ViewRequest{Path: "spec.pdf", Pages: " 1 - 10 "})
	if err != nil {
		t.Fatalf("View spec.pdf: %v", err)
	}
	if pdfRes.Media == nil || pdfRes.Media.MIMEType != "application/pdf" || pdfRes.Media.Pages != "1-10" {
		t.Fatalf("unexpected PDF media result: %+v", pdfRes.Media)
	}

	// Invalid PDF page range fails cleanly.
	if _, err := s.View(ctx, ViewRequest{Path: "spec.pdf", Pages: "10-5"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid PDF pages err = %v, want ErrInvalidArgument", err)
	}

	// Audio WAV detected.
	wavRes, err := s.View(ctx, ViewRequest{Path: "clip.wav"})
	if err != nil {
		t.Fatalf("View clip.wav: %v", err)
	}
	if wavRes.Media == nil || wavRes.Media.MIMEType != "audio/wav" || wavRes.Media.Modality != media.ModalityAudio {
		t.Fatalf("unexpected WAV media result: %+v", wavRes.Media)
	}

	// SVG stays on UTF-8 text path so agents can edit XML directly.
	svgRes, err := s.View(ctx, ViewRequest{Path: "icon.svg"})
	if err != nil {
		t.Fatalf("View icon.svg: %v", err)
	}
	if svgRes.Media != nil {
		t.Fatalf("expected SVG to stay on text path, got media: %+v", svgRes.Media)
	}
	if !strings.Contains(svgRes.Numbered, "<svg") {
		t.Fatalf("expected numbered SVG content, got %q", svgRes.Numbered)
	}

	// Oversized image fails with ErrTooLarge.
	huge := make([]byte, media.MaxImageBytes+1)
	copy(huge, pngBytes)
	if err := os.WriteFile(filepath.Join(workDir, "huge.png"), huge, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.View(ctx, ViewRequest{Path: "huge.png"}); !errors.Is(err, media.ErrTooLarge) {
		t.Fatalf("huge.png err = %v, want ErrTooLarge", err)
	}

	// CAS remove succeeds after media View recorded read hash.
	if _, err := s.Remove(ctx, RemoveRequest{Path: "shot.png"}); err != nil {
		t.Fatalf("Remove after View shot.png failed: %v", err)
	}
}
