package media

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/hash"
)

func TestDetectMagicAndSVGTextPath(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		data     []byte
		wantOK   bool
		wantMIME string
		wantMod  Modality
	}{
		{
			name:     "png",
			filename: "shot.png",
			data:     []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0},
			wantOK:   true,
			wantMIME: "image/png",
			wantMod:  ModalityImage,
		},
		{
			name:     "jpeg",
			filename: "photo.jpg",
			data:     []byte{0xff, 0xd8, 0xff, 0xe0, 0},
			wantOK:   true,
			wantMIME: "image/jpeg",
			wantMod:  ModalityImage,
		},
		{
			name:     "webp",
			filename: "pic.webp",
			data:     []byte("RIFF\x00\x00\x00\x00WEBP"),
			wantOK:   true,
			wantMIME: "image/webp",
			wantMod:  ModalityImage,
		},
		{
			name:     "gif",
			filename: "anim.gif",
			data:     []byte("GIF89a\x01\x00"),
			wantOK:   true,
			wantMIME: "image/gif",
			wantMod:  ModalityImage,
		},
		{
			name:     "pdf",
			filename: "spec.pdf",
			data:     []byte("%PDF-1.7\n"),
			wantOK:   true,
			wantMIME: "application/pdf",
			wantMod:  ModalityPDF,
		},
		{
			name:     "wav",
			filename: "clip.wav",
			data:     []byte("RIFF\x00\x00\x00\x00WAVEfmt "),
			wantOK:   true,
			wantMIME: "audio/wav",
			wantMod:  ModalityAudio,
		},
		{
			name:     "mp3 id3",
			filename: "clip.mp3",
			data:     []byte("ID3\x04\x00"),
			wantOK:   true,
			wantMIME: "audio/mpeg",
			wantMod:  ModalityAudio,
		},
		{
			name:     "m4a",
			filename: "clip.m4a",
			data:     []byte("\x00\x00\x00\x20ftypM4A "),
			wantOK:   true,
			wantMIME: "audio/mp4",
			wantMod:  ModalityAudio,
		},
		{
			name:     "ogg",
			filename: "clip.ogg",
			data:     []byte("OggS\x00\x02"),
			wantOK:   true,
			wantMIME: "audio/ogg",
			wantMod:  ModalityAudio,
		},
		{
			name:     "flac",
			filename: "clip.flac",
			data:     []byte("fLaC\x00\x00"),
			wantOK:   true,
			wantMIME: "audio/flac",
			wantMod:  ModalityAudio,
		},
		{
			name:     "svg stays text",
			filename: "icon.svg",
			data:     []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
			wantOK:   false,
		},
		{
			name:     "plain text",
			filename: "main.go",
			data:     []byte("package main\n"),
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := Detect(tc.filename, tc.data)
			if ok != tc.wantOK {
				t.Fatalf("Detect(%q) ok = %v, want %v", tc.filename, ok, tc.wantOK)
			}
			if ok && (info.MIMEType != tc.wantMIME || info.Modality != tc.wantMod) {
				t.Fatalf("Detect(%q) = %+v, want (%s, %s)", tc.filename, info, tc.wantMIME, tc.wantMod)
			}
		})
	}
}

func TestParsePages(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"", ""},
		{" 1 - 10 ", "1-10"},
		{"3", "3"},
		{"2-2", "2"},
		{"1-3, 5, 8-10", "1-3,5,8-10"},
	}
	for _, tc := range valid {
		got, err := ParsePages(tc.in)
		if err != nil {
			t.Fatalf("ParsePages(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParsePages(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{"0", "-1", "10-5", "1-a", "1,,2", "1-"} {
		if _, err := ParsePages(bad); !errors.Is(err, ErrInvalidPages) {
			t.Fatalf("ParsePages(%q) error = %v, want ErrInvalidPages", bad, err)
		}
	}
}

func TestStorePutReadAndHardlinkFork(t *testing.T) {
	ctx := context.Background()
	srcDir := filepath.Join(t.TempDir(), "media")
	dstDir := filepath.Join(t.TempDir(), "media-fork")

	src, err := NewStore(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	sha, sz, err := src.Put(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !hash.IsValidSHA256(sha) || sz != int64(len(payload)) {
		t.Fatalf("Put = (%q, %d)", sha, sz)
	}

	// Put again should deduplicate cleanly.
	sha2, sz2, err := src.Put(ctx, payload)
	if err != nil || sha2 != sha || sz2 != sz {
		t.Fatalf("second Put = (%q, %d, %v)", sha2, sz2, err)
	}

	dst, err := NewStore(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.LinkAllFrom(src.Dir()); err != nil {
		t.Fatal(err)
	}

	srcInfo, err := os.Stat(filepath.Join(srcDir, sha))
	if err != nil {
		t.Fatal(err)
	}
	dstInfo, err := os.Stat(filepath.Join(dstDir, sha))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Fatalf("expected hardlinked file on fork, got different inodes")
	}

	got, err := dst.Read(sha)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("Read = (%x, %v)", got, err)
	}

	// Traversal sha must be rejected.
	if _, err := dst.Read("../escape"); err == nil {
		t.Fatal("expected traversal sha to be rejected")
	}
}

func TestDescriptorRoundTripThroughJSON(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	desc := api.Media{
		SHA256:      sha,
		MIMEType:    "application/pdf",
		ByteSize:    4096,
		DisplayName: "spec.pdf",
		Pages:       "1-5",
	}
	blk, err := BlockFromDescriptor(desc)
	if err != nil {
		t.Fatal(err)
	}
	msg := message.NewUser(blk)
	raw, err := json.Marshal(message.MessageList{msg})
	if err != nil {
		t.Fatal(err)
	}
	var decoded message.MessageList
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	got, mod, ok := DescriptorFromBlock(decoded[0].GetContent()[0])
	if !ok || mod != ModalityPDF {
		t.Fatalf("DescriptorFromBlock = (%+v, %q, %v)", got, mod, ok)
	}
	if got != desc {
		t.Fatalf("got descriptor %+v, want %+v", got, desc)
	}
}
