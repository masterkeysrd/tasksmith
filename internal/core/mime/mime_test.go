package mime

import "testing"

func TestDetectAndSVGTextPath(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		data     []byte
		wantOK   bool
		wantMIME string
		wantKind Kind
	}{
		{
			name:     "png",
			filename: "shot.png",
			data:     []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0},
			wantOK:   true,
			wantMIME: "image/png",
			wantKind: KindImage,
		},
		{
			name:     "jpeg",
			filename: "photo.jpg",
			data:     []byte{0xff, 0xd8, 0xff, 0xe0, 0},
			wantOK:   true,
			wantMIME: "image/jpeg",
			wantKind: KindImage,
		},
		{
			name:     "webp",
			filename: "pic.webp",
			data:     []byte("RIFF\x00\x00\x00\x00WEBP"),
			wantOK:   true,
			wantMIME: "image/webp",
			wantKind: KindImage,
		},
		{
			name:     "gif",
			filename: "anim.gif",
			data:     []byte("GIF89a\x01\x00"),
			wantOK:   true,
			wantMIME: "image/gif",
			wantKind: KindImage,
		},
		{
			name:     "pdf",
			filename: "spec.pdf",
			data:     []byte("%PDF-1.7\n"),
			wantOK:   true,
			wantMIME: "application/pdf",
			wantKind: KindPDF,
		},
		{
			name:     "wav",
			filename: "clip.wav",
			data:     []byte("RIFF\x00\x00\x00\x00WAVEfmt "),
			wantOK:   true,
			wantMIME: "audio/wav",
			wantKind: KindAudio,
		},
		{
			name:     "mp3 id3",
			filename: "clip.mp3",
			data:     []byte("ID3\x04\x00"),
			wantOK:   true,
			wantMIME: "audio/mpeg",
			wantKind: KindAudio,
		},
		{
			name:     "mp3 headerless",
			filename: "clip.mp3",
			data:     []byte{0xff, 0xfb, 0x90, 0x00},
			wantOK:   true,
			wantMIME: "audio/mpeg",
			wantKind: KindAudio,
		},
		{
			name:     "m4a",
			filename: "clip.m4a",
			data:     []byte("\x00\x00\x00\x20ftypM4A "),
			wantOK:   true,
			wantMIME: "audio/mp4",
			wantKind: KindAudio,
		},
		{
			name:     "ogg",
			filename: "clip.ogg",
			data:     []byte("OggS\x00\x02"),
			wantOK:   true,
			wantMIME: "audio/ogg",
			wantKind: KindAudio,
		},
		{
			name:     "flac",
			filename: "clip.flac",
			data:     []byte("fLaC\x00\x00"),
			wantOK:   true,
			wantMIME: "audio/flac",
			wantKind: KindAudio,
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
			gotMIME, gotKind, ok := Detect(tc.filename, tc.data)
			if ok != tc.wantOK {
				t.Fatalf("Detect(%q) ok = %v, want %v", tc.filename, ok, tc.wantOK)
			}
			if ok && (gotMIME != tc.wantMIME || gotKind != tc.wantKind) {
				t.Fatalf("Detect(%q) = (%q, %q), want (%q, %q)", tc.filename, gotMIME, gotKind, tc.wantMIME, tc.wantKind)
			}
		})
	}
}

func TestCanonicalAndKindFor(t *testing.T) {
	if got := Canonical("IMAGE/JPG; charset=utf-8"); got != "image/jpeg" {
		t.Fatalf("Canonical = %q, want image/jpeg", got)
	}
	if got := Canonical("audio/wave"); got != "audio/wav" {
		t.Fatalf("Canonical = %q, want audio/wav", got)
	}
	if kind, ok := KindFor("audio/mp3"); !ok || kind != KindAudio {
		t.Fatalf("KindFor(audio/mp3) = (%q, %v), want (audio, true)", kind, ok)
	}
	if _, ok := KindFor("image/svg+xml"); ok {
		t.Fatal("expected image/svg+xml not to be classified as media")
	}
}
