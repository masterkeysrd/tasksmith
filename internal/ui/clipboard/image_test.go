package clipboard

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePastedImages(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(txtPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	imgOnePath := filepath.Join(dir, "one photo.png")
	imgOneBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 1, 1, 1}
	if err := os.WriteFile(imgOnePath, imgOneBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	imgTwoPath := filepath.Join(dir, "two.png")
	imgTwoBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 2, 2, 2, 2, 2}
	if err := os.WriteFile(imgTwoPath, imgTwoBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	finderIcon := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9, 9, 9, 9}

	t.Run("non-image uri-list suppresses Finder icon image", func(t *testing.T) {
		cands := ResolvePastedImages(map[string][]byte{
			"text/uri-list": []byte((&url.URL{Scheme: "file", Path: txtPath}).String()),
			"image/png":     finderIcon,
			"text/plain":    []byte("readme.txt"),
		}, "readme.txt")
		if len(cands) != 0 {
			t.Fatalf("expected 0 candidates for non-image file URI, got %+v", cands)
		}
	})

	t.Run("image uri-list reads disk bytes instead of Finder icon", func(t *testing.T) {
		cands := ResolvePastedImages(map[string][]byte{
			"text/uri-list": []byte((&url.URL{Scheme: "file", Path: imgOnePath}).String()),
			"image/png":     finderIcon,
		}, "one photo.png")
		if len(cands) != 1 || cands[0].DisplayName != "one photo.png" || !bytes.Equal(cands[0].Data, imgOneBytes) {
			t.Fatalf("got %+v, want disk bytes for one photo.png", cands)
		}
	})

	t.Run("multi-file shell-escaped drop beats stale clipboard uri-list", func(t *testing.T) {
		dropped := strings.ReplaceAll(imgOnePath, " ", `\ `) + " " + imgTwoPath
		cands := ResolvePastedImages(map[string][]byte{
			"text/uri-list": []byte((&url.URL{Scheme: "file", Path: txtPath}).String()),
			"image/png":     finderIcon,
		}, dropped)
		if len(cands) != 2 {
			t.Fatalf("expected 2 dropped images, got %+v", cands)
		}
		if !bytes.Equal(cands[0].Data, imgOneBytes) || !bytes.Equal(cands[1].Data, imgTwoBytes) {
			t.Fatalf("unexpected dropped bytes: %+v", cands)
		}
	})

	t.Run("raw clipboard image used when no file path or uri-list", func(t *testing.T) {
		cands := ResolvePastedImages(map[string][]byte{
			"image/png":  finderIcon,
			"text/plain": []byte("https://example.com/pic.png"),
		}, "https://example.com/pic.png")
		if len(cands) != 1 || cands[0].MIMEType != "image/png" || !bytes.Equal(cands[0].Data, finderIcon) {
			t.Fatalf("got %+v, want raw clipboard image", cands)
		}
	})
}
