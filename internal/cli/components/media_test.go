package components

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/terminal"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/hash"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

type stubImageClipboard struct {
	images [][]byte
	idx    atomic.Int32
	text   string
}

func (s *stubImageClipboard) ListFormats() *promise.Promise[[]string] {
	cur := int(s.idx.Add(1)) - 1
	if cur >= 0 && cur < len(s.images) {
		if s.text != "" {
			return promise.Resolved([]string{"image/png", "text/plain"})
		}
		return promise.Resolved([]string{"image/png"})
	}
	if s.text != "" {
		return promise.Resolved([]string{"text/plain"})
	}
	return promise.Resolved([]string(nil))
}

func (s *stubImageClipboard) ReadText() *promise.Promise[string] {
	return promise.Resolved(s.text)
}

func (s *stubImageClipboard) WriteText(string) *promise.Promise[struct{}] {
	return promise.Resolved(struct{}{})
}

func (s *stubImageClipboard) Write(string, []byte) *promise.Promise[struct{}] {
	return promise.Resolved(struct{}{})
}

func (s *stubImageClipboard) Read(mime string) *promise.Promise[[]byte] {
	if mime == "text/plain" {
		return promise.Resolved([]byte(s.text))
	}
	if !strings.HasPrefix(mime, "image/") {
		return promise.Resolved([]byte(nil))
	}
	cur := int(s.idx.Load()) - 1
	if cur < 0 || cur >= len(s.images) {
		return promise.Resolved([]byte(nil))
	}
	return promise.Resolved(s.images[cur])
}

var _ terminal.Clipboard = (*stubImageClipboard)(nil)

func TestComposerClipboardImagePasteAndPill(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	store.ClearSlot()
	store.ResetApp()
	store.ResetSession()
	defer store.ClearSlot()
	defer store.ResetApp()
	defer store.ResetSession()

	pngBytes1 := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	pngBytes2 := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 5, 6, 7, 8}

	var submittedText string
	var submittedAtts []api.Attachment
	inst, b := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string, atts []api.Attachment) {
			submittedText = text
			submittedAtts = atts
		},
	})
	cbStub := &stubImageClipboard{
		images: [][]byte{pngBytes1, pngBytes2},
		text:   "tail-text",
	}
	inst.Engine().Document().SetClipboardProvider(cbStub)

	// Ctrl+V with both image/png and text/plain on the clipboard pastes only [Image #1].
	pressKey(inst, 'v', key.ModCtrl)
	waitForSurface := func(substr string) string {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			inst.Frame()
			surf := testutils.SurfaceText(b)
			if strings.Contains(surf, substr) {
				return surf
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %q on surface, got:\n%s", substr, testutils.SurfaceText(b))
		return ""
	}
	surf := waitForSurface("[Image #1]")
	if !strings.Contains(surf, "Image #1 · 12 B") {
		t.Fatalf("expected inline staged pill in composer, got:\n%s", surf)
	}
	if strings.Contains(surf, "tail-text") {
		t.Fatalf("expected text/plain not to be pasted alongside image, got:\n%s", surf)
	}

	// Alt+V pastes a second image as [Image #2] pill, still suppressing text/plain.
	typeText(inst, "and ")
	pressKey(inst, 'v', key.ModAlt)
	surf = waitForSurface("[Image #2]")
	if strings.Contains(surf, "tail-text") {
		t.Fatalf("expected text/plain not to be pasted alongside second image, got:\n%s", surf)
	}

	// Third Ctrl+V has only text/plain on the clipboard, so TextArea inserts it normally.
	pressKey(inst, 'v', key.ModCtrl)
	waitForSurface("tail-text")

	pressKey(inst, key.KeyEnter, 0)
	if !strings.Contains(submittedText, "[Image #1]") || !strings.Contains(submittedText, "[Image #2]") || !strings.Contains(submittedText, "tail-text") {
		t.Fatalf("submittedText = %q", submittedText)
	}
	if len(submittedAtts) != 2 {
		t.Fatalf("expected 2 media attachments, got %+v", submittedAtts)
	}
	for i, att := range submittedAtts {
		if att.Kind != api.AttachmentMedia || att.Media == nil || att.Media.MIMEType != "image/png" {
			t.Fatalf("attachment %d = %+v, want AttachmentMedia image/png", i, att)
		}
	}
}

type staticMapClipboard struct {
	items map[string][]byte
}

func (s *staticMapClipboard) ListFormats() *promise.Promise[[]string] {
	var out []string
	for k := range s.items {
		out = append(out, k)
	}
	return promise.Resolved(out)
}

func (s *staticMapClipboard) ReadText() *promise.Promise[string] {
	return promise.Resolved(string(s.items["text/plain"]))
}

func (s *staticMapClipboard) WriteText(string) *promise.Promise[struct{}] {
	return promise.Resolved(struct{}{})
}

func (s *staticMapClipboard) Write(string, []byte) *promise.Promise[struct{}] {
	return promise.Resolved(struct{}{})
}

func (s *staticMapClipboard) Read(mime string) *promise.Promise[[]byte] {
	return promise.Resolved(s.items[mime])
}

func TestComposerClipboardFileCopyAndDrop(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	store.ClearSlot()
	store.ResetApp()
	store.ResetSession()
	defer store.ClearSlot()
	defer store.ResetApp()
	defer store.ResetSession()

	dir := t.TempDir()
	notesPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notesPath, []byte("plain notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	photoPath := filepath.Join(dir, "photo.png")
	photoBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9, 9, 9, 9}
	if err := os.WriteFile(photoPath, photoBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	dropPath := filepath.Join(dir, "my drop.png")
	dropBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 7, 7, 7, 7, 7}
	if err := os.WriteFile(dropPath, dropBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	finderIconBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}

	var submittedText string
	var submittedAtts []api.Attachment
	inst, b := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string, atts []api.Attachment) {
			submittedText = text
			submittedAtts = atts
		},
	})

	waitForSurface := func(substr string) string {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			inst.Frame()
			surf := testutils.SurfaceText(b)
			flat := strings.ReplaceAll(surf, "\n  ", "")
			if strings.Contains(surf, substr) || strings.Contains(flat, substr) {
				return surf
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %q on surface, got:\n%s", substr, testutils.SurfaceText(b))
		return ""
	}

	// 1. Finder copy of a non-image file: text/uri-list + Finder icon image/png + text/plain.
	// Must NOT attach Finder's icon PNG; must paste "notes.txt" as text.
	cb := &staticMapClipboard{
		items: map[string][]byte{
			"text/uri-list": []byte((&url.URL{Scheme: "file", Path: notesPath}).String()),
			"image/png":     finderIconBytes,
			"text/plain":    []byte("notes.txt"),
		},
	}
	inst.Engine().Document().SetClipboardProvider(cb)
	pressKey(inst, 'v', key.ModCtrl)
	surf := waitForSurface("notes.txt")
	if strings.Contains(surf, "[Image #1]") {
		t.Fatalf("must not attach Finder icon for non-image file:\n%s", surf)
	}

	// 2. Finder copy of an image file: must stage the real file bytes (not Finder's icon).
	cb.items = map[string][]byte{
		"text/uri-list": []byte((&url.URL{Scheme: "file", Path: photoPath}).String()),
		"image/png":     finderIconBytes,
		"text/plain":    []byte("photo.png"),
	}
	typeText(inst, " ")
	pressKey(inst, 'v', key.ModCtrl)
	surf = waitForSurface("[Image #1]")
	if strings.Contains(surf, "photo.png") {
		t.Fatalf("must not paste filename text alongside image:\n%s", surf)
	}

	// 3. Drag-and-drop (RawBracketedPaste) of a shell-escaped image path on disk.
	escapedDrop := strings.ReplaceAll(dropPath, " ", `\ `) + " "
	inst.Engine().ProcessRawEvent(&backend.RawBracketedPaste{Text: escapedDrop})
	surf = waitForSurface("[Image #2]")
	if strings.Contains(surf, "my drop.png") {
		t.Fatalf("must not paste raw dropped path for image:\n%s", surf)
	}

	// 4. Drag-and-drop of a non-image file while an old screenshot sits in the clipboard.
	// Must paste notesPath as text and NOT hijack it with the stale clipboard image.
	cb.items = map[string][]byte{
		"image/png": finderIconBytes,
	}
	inst.Engine().ProcessRawEvent(&backend.RawBracketedPaste{Text: notesPath})
	surf = waitForSurface(notesPath)
	if strings.Contains(surf, "[Image #3]") {
		t.Fatalf("must not hijack non-image drop with stale clipboard image:\n%s", surf)
	}

	pressKey(inst, key.KeyEnter, 0)
	if !strings.Contains(submittedText, "notes.txt") || !strings.Contains(submittedText, "[Image #1]") || !strings.Contains(submittedText, "[Image #2]") || !strings.Contains(submittedText, notesPath) {
		t.Fatalf("submittedText = %q", submittedText)
	}
	if len(submittedAtts) != 2 {
		t.Fatalf("expected 2 media attachments, got %+v", submittedAtts)
	}
	if submittedAtts[0].Media.DisplayName != "photo.png" || submittedAtts[0].Media.SHA256 != hash.SHA256Hex(photoBytes) || submittedAtts[0].Media.ByteSize != int64(len(photoBytes)) {
		t.Fatalf("att[0] = %+v, want DisplayName=photo.png SHA256=%s", submittedAtts[0].Media, hash.SHA256Hex(photoBytes))
	}
	if submittedAtts[1].Media.DisplayName != "my drop.png" || submittedAtts[1].Media.SHA256 != hash.SHA256Hex(dropBytes) || submittedAtts[1].Media.ByteSize != int64(len(dropBytes)) {
		t.Fatalf("att[1] = %+v, want DisplayName=my drop.png SHA256=%s", submittedAtts[1].Media, hash.SHA256Hex(dropBytes))
	}
}

func TestViewToolWidgetMediaAndOmitted(t *testing.T) {
	t.Run("media output displays mime, size, and pdf pages", func(t *testing.T) {
		outJSON, _ := json.Marshal(api.ViewOutput{
			Path:     "docs/spec.pdf",
			Content:  "Viewed docs/spec.pdf (application/pdf, 142 KB, pages 1-10)",
			Hash:     "abc",
			MimeType: "application/pdf",
			ByteSize: 142 * 1024,
			Modality: "pdf",
			Pages:    "1-10",
		})
		rendered := testutils.Render(t, ViewToolWidget(ViewToolWidgetProps{
			Tool: store.ToolPart{
				ID:     "call-1",
				Status: store.ToolSucceeded,
				Call: &api.ToolCall{
					ID:   "call-1",
					Name: "view",
					Args: map[string]any{"path": "docs/spec.pdf", "pages": "1-10"},
				},
				Response: &api.Message{
					ID:                "msg-1",
					Role:              api.RoleTool,
					Name:              "view",
					ToolCallID:        "call-1",
					StructuredContent: outJSON,
				},
			},
		}), 100, 10)
		if strings.Contains(rendered, "0 lines") {
			t.Fatalf("ViewToolWidget must not show 0 lines for media:\n%s", rendered)
		}
		if !strings.Contains(rendered, "application/pdf, 142 KB, pages 1-10") {
			t.Fatalf("ViewToolWidget missing media metadata:\n%s", rendered)
		}
	})

	t.Run("omitted media displays warning subline", func(t *testing.T) {
		outJSON, _ := json.Marshal(api.ViewOutput{
			Path:     "shot.png",
			Content:  `Cannot view shot.png (image/png, 12 B): active model "text/m" does not support image input.`,
			Hash:     "abc",
			MimeType: "image/png",
			ByteSize: 12,
			Modality: "image",
			Omitted:  true,
		})
		rendered := testutils.Render(t, ViewToolWidget(ViewToolWidgetProps{
			Tool: store.ToolPart{
				ID:     "call-2",
				Status: store.ToolSucceeded,
				Call:   &api.ToolCall{ID: "call-2", Name: "view", Args: map[string]any{"path": "shot.png"}},
				Response: &api.Message{
					ID:                "msg-2",
					Role:              api.RoleTool,
					Name:              "view",
					ToolCallID:        "call-2",
					StructuredContent: outJSON,
				},
			},
		}), 120, 10)
		if !strings.Contains(rendered, "image/png, 12 B, omitted") {
			t.Fatalf("ViewToolWidget missing omitted suffix:\n%s", rendered)
		}
		if !strings.Contains(rendered, "does not support image input") {
			t.Fatalf("ViewToolWidget missing omitted warning subline:\n%s", rendered)
		}
	})
}

func TestModelSwitchPromptRenderAndSelection(t *testing.T) {
	var forced, canceled bool
	env := testutils.NewEnv(t, 100, 20)
	out := env.Render(ModelSwitchPrompt(ModelSwitchPromptProps{
		Request: store.ModelSwitchRequest{
			Model:             "local/llama",
			MissingModalities: []string{"image", "pdf"},
		},
		OnForce:  func() { forced = true },
		OnCancel: func() { canceled = true },
	}))
	if !strings.Contains(out, "local/llama does not support image, pdf used in this session") {
		t.Fatalf("missing capability warning in ModelSwitchPrompt:\n%s", out)
	}
	if !strings.Contains(out, "Switch anyway") || !strings.Contains(out, "Cancel") {
		t.Fatalf("missing confirmation options in ModelSwitchPrompt:\n%s", out)
	}

	// Default first option is "Switch anyway" -> Enter triggers OnForce.
	env.Press(key.Key{Code: key.KeyEnter})
	if !forced || canceled {
		t.Fatalf("after Enter: forced=%v canceled=%v, want forced=true", forced, canceled)
	}

	// Esc triggers OnCancel.
	forced = false
	env.Press(key.Key{Code: key.KeyEscape})
	if !canceled || forced {
		t.Fatalf("after Esc: forced=%v canceled=%v, want canceled=true", forced, canceled)
	}
}
