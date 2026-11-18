package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

func TestAttachmentResolverMediaAndUnsupportedOmission(t *testing.T) {
	ctx := context.Background()
	resolver, _, workDir := setupTestResolver(t)

	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	pdfBytes := []byte("%PDF-1.7\n")
	wavBytes := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	for name, data := range map[string][]byte{
		"screenshot.png": pngBytes,
		"spec.pdf":       pdfBytes,
		"clip.wav":       wavBytes,
	} {
		if err := os.WriteFile(filepath.Join(workDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mediaStore, err := media.NewStore(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	resolver.mediaStore = mediaStore
	resolver.supportsModality = func(mod media.Modality) (string, bool) {
		return "google/gemini-2.5", mod == media.ModalityImage || mod == media.ModalityPDF
	}

	msg := message.NewUserText("check @screenshot.png @spec.pdf @clip.wav")
	SetAttachments(msg, []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@screenshot.png", Target: "screenshot.png"},
		{Kind: api.AttachmentFile, Ref: "@spec.pdf", Target: "spec.pdf"},
		{Kind: api.AttachmentFile, Ref: "@clip.wav", Target: "clip.wav"},
	})

	_, reminder, _ := resolver.ResolveUserMessage(ctx, msg, State{})
	if reminder == nil {
		t.Fatal("expected <system_reminders> reminder message")
	}
	remText := reminder.Content.Text()
	if !strings.Contains(remText, "<context_media") || strings.Contains(remText, "<context_file") {
		t.Fatalf("unexpected reminder content:\n%s", remText)
	}
	if !strings.Contains(remText, `status="omitted"`) || !strings.Contains(remText, "does not support audio input") {
		t.Fatalf("expected audio omission notice in reminder:\n%s", remText)
	}

	// User message should carry image + pdf descriptors, and NO audio block.
	if len(msg.Content) != 3 {
		t.Fatalf("len(msg.Content) = %d, want 3 (text + image + pdf)", len(msg.Content))
	}
	if _, ok := msg.Content[1].(*message.ImageBlock); !ok {
		t.Fatalf("msg.Content[1] = %T, want *message.ImageBlock", msg.Content[1])
	}
	if _, ok := msg.Content[2].(*message.DocumentBlock); !ok {
		t.Fatalf("msg.Content[2] = %T, want *message.DocumentBlock", msg.Content[2])
	}

	st := State{Messages: message.MessageList{msg}}
	mods := st.ActiveModalities()
	if len(mods) != 2 || mods[0] != media.ModalityImage || mods[1] != media.ModalityPDF {
		t.Fatalf("ActiveModalities = %v, want [image pdf]", mods)
	}

	// Compaction replaces Messages with text summary, clearing active modalities.
	st.Messages = message.MessageList{buildContextSummaryMessage([]string{"check"}, nil, "summary", "manual", "", false)}
	st = st.ApplyCompaction(StaticContext{}, models.Limits{})
	if got := st.ActiveModalities(); len(got) != 0 {
		t.Fatalf("ActiveModalities after compaction = %v, want empty", got)
	}
}

func TestHydrateMessagesForLLM(t *testing.T) {
	ctx := context.Background()
	store, err := media.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	sha, sz, err := store.Put(ctx, pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := media.BlockFromDescriptor(api.Media{
		SHA256:      sha,
		MIMEType:    "image/png",
		ByteSize:    sz,
		DisplayName: "ui.png",
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("hydrates descriptor without mutating state message", func(t *testing.T) {
		m := models.Model{
			ID:           "anthropic/claude-sonnet",
			ProviderType: "anthropic",
			Modalities:   models.Modalities{Input: []string{"text", "image"}},
		}
		userMsg := message.NewUser(&message.TextBlock{Text: "check"}, blk)
		got := hydrateMessagesForLLM(store, m, []message.Message{userMsg})

		if ib := userMsg.Content[1].(*message.ImageBlock); len(ib.Data) != 0 {
			t.Fatalf("original state block mutated with %d raw bytes", len(ib.Data))
		}
		gotImg := got[0].GetContent()[1].(*message.ImageBlock)
		if string(gotImg.Data) != string(pngBytes) {
			t.Fatalf("hydrated bytes = %x, want %x", gotImg.Data, pngBytes)
		}
	})

	t.Run("promotes tool media for openai and antigravity", func(t *testing.T) {
		m := models.Model{
			ID:           "openai/gpt-vision",
			ProviderType: "openai",
			Modalities:   models.Modalities{Input: []string{"text", "image"}},
		}
		asst := &message.Assistant{Content: message.Content{&message.ToolCall{ID: "call-1", Name: "view"}}}
		toolRes := &message.Tool{
			ToolCallID: "call-1",
			Name:       "view",
			Content:    message.Content{&message.TextBlock{Text: "Viewed ui.png (image/png, 12 B)"}, blk},
		}
		got := hydrateMessagesForLLM(store, m, []message.Message{message.NewUserText("view it"), asst, toolRes})
		if len(got) != 4 {
			t.Fatalf("len(Messages) = %d, want 4 (with promoted user media message)", len(got))
		}
		promoted, ok := got[3].(*message.User)
		if !ok {
			t.Fatalf("expected trailing *message.User, got %T", got[3])
		}
		gotImg := promoted.Content[1].(*message.ImageBlock)
		if string(gotImg.Data) != string(pngBytes) {
			t.Fatalf("promoted bytes = %x, want %x", gotImg.Data, pngBytes)
		}
	})

	t.Run("degrades unsupported historical media to text placeholder", func(t *testing.T) {
		m := models.Model{
			ID:           "anthropic/text-only",
			ProviderType: "anthropic",
			Modalities:   models.Modalities{Input: []string{"text"}},
		}
		userMsg := message.NewUser(&message.TextBlock{Text: "earlier image"}, blk)
		got := hydrateMessagesForLLM(store, m, []message.Message{userMsg})
		tb := got[0].GetContent()[1].(*message.TextBlock)
		if !strings.Contains(tb.Text, "[image ui.png omitted: unsupported by current model]") {
			t.Fatalf("degraded placeholder = %q", tb.Text)
		}
	})

	t.Run("missing media on disk degrades to unavailable placeholder", func(t *testing.T) {
		m := models.Model{
			ID:           "anthropic/claude-sonnet",
			ProviderType: "anthropic",
			Modalities:   models.Modalities{Input: []string{"text", "image"}},
		}
		missingBlk, err := media.BlockFromDescriptor(api.Media{
			SHA256:      strings.Repeat("b", 64),
			MIMEType:    "image/png",
			ByteSize:    64,
			DisplayName: "missing.png",
		})
		if err != nil {
			t.Fatal(err)
		}
		userMsg := message.NewUser(&message.TextBlock{Text: "check"}, missingBlk)
		got := hydrateMessagesForLLM(store, m, []message.Message{userMsg})
		tb := got[0].GetContent()[1].(*message.TextBlock)
		if !strings.Contains(tb.Text, "[image missing.png omitted: media unavailable on disk]") {
			t.Fatalf("unavailable placeholder = %q", tb.Text)
		}
	})

	t.Run("hydrates PDF with pages annotation", func(t *testing.T) {
		pdfBytes := []byte("%PDF-1.7\n")
		pdfSHA, pdfSz, err := store.Put(ctx, pdfBytes)
		if err != nil {
			t.Fatal(err)
		}
		pdfBlk, err := media.BlockFromDescriptor(api.Media{
			SHA256:      pdfSHA,
			MIMEType:    "application/pdf",
			ByteSize:    pdfSz,
			DisplayName: "spec.pdf",
			Pages:       "1-5",
		})
		if err != nil {
			t.Fatal(err)
		}
		m := models.Model{
			ID:           "anthropic/claude-sonnet",
			ProviderType: "anthropic",
			Modalities:   models.Modalities{Input: []string{"text", "pdf"}},
		}
		userMsg := message.NewUser(&message.TextBlock{Text: "summarize"}, pdfBlk)
		got := hydrateMessagesForLLM(store, m, []message.Message{userMsg})
		gotContent := got[0].GetContent()
		if len(gotContent) != 3 {
			t.Fatalf("len(Content) = %d, want 3 (text + page note + document)", len(gotContent))
		}
		note := gotContent[1].(*message.TextBlock).Text
		if !strings.Contains(note, "[Document spec.pdf: examine pages 1-5]") {
			t.Fatalf("expected page range annotation, got %q", note)
		}
		doc := gotContent[2].(*message.DocumentBlock)
		if string(doc.Data) != string(pdfBytes) {
			t.Fatalf("hydrated PDF bytes = %q, want %q", doc.Data, pdfBytes)
		}
	})
}
