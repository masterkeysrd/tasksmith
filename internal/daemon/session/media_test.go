package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

func TestSessionMediaUploadForkAndCapabilityGate(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			return []message.AssistantChunk{{
				BaseChunk: message.BaseChunk{ID: "reply-1"},
				Model:     req.Model,
				Content:   []message.Block{&message.TextBlock{Text: "looked at it"}},
				Done:      true,
			}}, nil
		},
	}

	_, ws, _, _, mgr := setupTestEnv(t, prov)
	defer func() { _ = mgr.Close() }()

	// Declare a vision-capable model and a text-only model in workspace providers.
	provDir := filepath.Join(ws.RootDir(), ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}
	providerYAML := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: fake
spec:
  type: anthropic
  auth:
    type: none
  models:
    - id: vision-model
      capabilities:
        completion: true
        tools: true
        modalities:
          input: [text, image]
    - id: text-model
      capabilities:
        completion: true
        tools: true
        modalities:
          input: [text]
`
	if err := os.WriteFile(filepath.Join(provDir, "fake.provider.yaml"), []byte(providerYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}
	rebuilt, err := models.Build(ws.Catalog(), mgr.cfg.Gateway())
	if err != nil {
		t.Fatalf("models.Build: %v", err)
	}
	mgr.cfg.Catalog = func() *models.Catalog { return rebuilt }

	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{
		Title:  "Media session",
		Config: api.SessionConfig{Model: "fake/vision-model"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	uploaded, err := sess.UploadMedia(ctx, "image/png", "ui.png", pngBytes)
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	if uploaded.SHA256 == "" || uploaded.MIMEType != "image/png" || uploaded.ByteSize != int64(len(pngBytes)) {
		t.Fatalf("unexpected UploadMedia descriptor: %+v", uploaded)
	}

	err = sess.Send(ctx, api.SendRequest{
		Content: "What is in [Image #1]?",
		Attachments: []api.Attachment{{
			Kind:   api.AttachmentMedia,
			Ref:    "[Image #1]",
			Target: uploaded.SHA256,
			Media:  uploaded,
		}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	// Timeline must hold lightweight descriptor blocks with zero raw binary/base64.
	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline.List: %v", err)
	}
	if len(entries) < 1 || entries[0].Message == nil {
		t.Fatalf("expected user message in timeline, got %+v", entries)
	}
	var sawImg bool
	for _, blk := range entries[0].Message.GetContent() {
		if ib, ok := blk.(*message.ImageBlock); ok {
			sawImg = true
			if len(ib.Data) != 0 {
				t.Fatalf("timeline image block must not persist raw bytes, got %d", len(ib.Data))
			}
		}
	}
	if !sawImg {
		t.Fatal("expected *message.ImageBlock descriptor in persisted user message")
	}

	// Fork hardlinks the media blob in O(1).
	forked, err := sess.Fork(ctx, "Media fork")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	defer func() { _ = forked.Close() }()

	srcInfo, err := os.Stat(filepath.Join(sess.mediaDir(), uploaded.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	dstInfo, err := os.Stat(filepath.Join(forked.mediaDir(), uploaded.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Fatal("expected forked media blob to be hardlinked")
	}

	// Switching to a text-only model without Force fails with ErrCapabilityMismatch.
	err = sess.Update(ctx, api.UpdateSessionRequest{
		Config: api.SessionConfig{Model: "fake/text-model"},
	})
	if !errors.Is(err, ErrCapabilityMismatch) {
		t.Fatalf("Update without Force = %v, want ErrCapabilityMismatch", err)
	}
	var capErr *CapabilityMismatchError
	if !errors.As(err, &capErr) || len(capErr.MissingModalities) != 1 || capErr.MissingModalities[0] != "image" {
		t.Fatalf("unexpected CapabilityMismatchError: %+v", capErr)
	}

	// Retrying with Force: true succeeds.
	if err := sess.Update(ctx, api.UpdateSessionRequest{
		Config: api.SessionConfig{Model: "fake/text-model"},
		Force:  true,
	}); err != nil {
		t.Fatalf("Update with Force: %v", err)
	}
}

func TestContextForkCommandLinksParentMediaStore(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			return []message.AssistantChunk{{
				BaseChunk: message.BaseChunk{ID: "reply-1"},
				Model:     req.Model,
				Content:   []message.Block{&message.TextBlock{Text: "ok"}},
				Done:      true,
			}}, nil
		},
	}

	_, ws, _, _, mgr := setupTestEnv(t, prov)
	defer func() { _ = mgr.Close() }()

	cmdDir := filepath.Join(ws.RootDir(), ".agents", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdMD := "---\napiVersion: warp/v1alpha1\nkind: Command\nmetadata:\n  name: fork-review\nspec:\n  context: fork\n---\nReview the shared media.\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "fork-review.command.md"), []byte(cmdMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}

	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Parent"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9, 8, 7, 6}
	uploaded, err := sess.UploadMedia(ctx, "image/png", "parent.png", pngBytes)
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	if err := sess.Send(ctx, api.SendRequest{
		Content: "Initial image",
		Attachments: []api.Attachment{{
			Kind:   api.AttachmentMedia,
			Ref:    "[Image #1]",
			Target: uploaded.SHA256,
			Media:  uploaded,
		}},
	}); err != nil {
		t.Fatalf("Send parent: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait parent: %v", err)
	}

	if err := sess.Send(ctx, api.SendRequest{Content: "/fork-review"}); err != nil {
		t.Fatalf("Send /fork-review: %v", err)
	}

	children := sess.Children()
	if len(children) != 1 {
		t.Fatalf("len(Children) = %d, want 1", len(children))
	}
	child := children[0]
	if err := child.Wait(ctx); err != nil {
		t.Fatalf("Wait child: %v", err)
	}

	parentInfo, err := os.Stat(filepath.Join(sess.mediaDir(), uploaded.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	childInfo, err := os.Stat(filepath.Join(child.mediaDir(), uploaded.SHA256))
	if err != nil {
		t.Fatalf("expected child media blob to be linked: %v", err)
	}
	if !os.SameFile(parentInfo, childInfo) {
		t.Fatal("expected context: fork command child to hardlink parent media blob")
	}
}
