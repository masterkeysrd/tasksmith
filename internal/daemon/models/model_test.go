package models

import "testing"

func TestModelSupportsInputModality(t *testing.T) {
	unknown := Model{}
	for _, mod := range []string{"text", "image", "pdf", "audio"} {
		if !unknown.SupportsInputModality(mod) {
			t.Fatalf("unknown Modalities.Input should default to supported for %q", mod)
		}
	}

	vision := Model{Modalities: Modalities{Input: []string{"text", "image", "pdf"}}}
	if !vision.SupportsInputModality("image") || !vision.SupportsInputModality("pdf") {
		t.Fatal("expected image and pdf to be supported")
	}
	if vision.SupportsInputModality("audio") {
		t.Fatal("expected audio not to be supported")
	}

	textOnly := Model{Modalities: Modalities{Input: []string{"text"}}}
	if textOnly.SupportsInputModality("image") {
		t.Fatal("expected image not to be supported on text-only model")
	}
}
