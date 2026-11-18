package builtin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
)

func TestViewToolMediaAndModelCapabilityCheck(t *testing.T) {
	ctx := context.Background()
	dir, _, stdInst, sb, ws := setupBuiltinEnv(t, false)

	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	if err := os.WriteFile(filepath.Join(dir, "ui.png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := media.NewStore(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}

	guardInst := shellguard.New(shellguard.Config{WorkspaceDir: dir})

	t.Run("supported model stores blob and returns media descriptor", func(t *testing.T) {
		list, err := builtin.New(builtin.Config{
			Std:          stdInst,
			Sandbox:      sb,
			Guard:        guardInst,
			Workspace:    ws,
			AllowedTools: []string{builtin.ToolView},
			MediaStore:   store,
			SupportsModality: func(mod media.Modality) (string, bool) {
				return "anthropic/claude-sonnet", true
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		container, err := tools.NewContainer(tools.Config{
			Tools:         list,
			Sandbox:       sb,
			Workspace:     ws,
			Logger:        log.Default(),
			CommandRunner: stdInst,
			Tasks:         tasks.New(tasks.Config{}),
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := container.Execute(ctx, &message.ToolCall{
			ID:   "call-1",
			Name: builtin.ToolView,
			Args: map[string]any{"path": "ui.png"},
		})
		if err != nil || res.IsError {
			t.Fatalf("Execute view: res=%+v err=%v", res, err)
		}
		if len(res.Content) != 2 {
			t.Fatalf("len(Content) = %d, want 2 (text + image descriptor)", len(res.Content))
		}
		txt := res.Content[0].(*message.TextBlock).Text
		if !strings.Contains(txt, "Viewed ui.png (image/png,") {
			t.Fatalf("summary text = %q", txt)
		}
		img := res.Content[1].(*message.ImageBlock)
		if len(img.Data) != 0 {
			t.Fatalf("descriptor block must not carry raw bytes, got %d", len(img.Data))
		}
		desc, mod, ok := media.DescriptorFromBlock(img)
		if !ok || mod != media.ModalityImage || !store.Exists(desc.SHA256) {
			t.Fatalf("expected stored descriptor, got (%+v, %q, %v)", desc, mod, ok)
		}
	})

	t.Run("unsupported model returns non-fatal text notice without media block", func(t *testing.T) {
		list, err := builtin.New(builtin.Config{
			Std:          stdInst,
			Sandbox:      sb,
			Guard:        guardInst,
			Workspace:    ws,
			AllowedTools: []string{builtin.ToolView},
			MediaStore:   store,
			SupportsModality: func(mod media.Modality) (string, bool) {
				return "local/text-only", false
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		container, err := tools.NewContainer(tools.Config{
			Tools:         list,
			Sandbox:       sb,
			Workspace:     ws,
			Logger:        log.Default(),
			CommandRunner: stdInst,
			Tasks:         tasks.New(tasks.Config{}),
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := container.Execute(ctx, &message.ToolCall{
			ID:   "call-2",
			Name: builtin.ToolView,
			Args: map[string]any{"path": "ui.png"},
		})
		if err != nil || res.IsError {
			t.Fatalf("Execute view: res=%+v err=%v", res, err)
		}
		if len(res.Content) != 1 {
			t.Fatalf("len(Content) = %d, want 1 (text only, no poisoned media block)", len(res.Content))
		}
		out := res.StructuredContent.(api.ViewOutput)
		if !out.Omitted || !strings.Contains(out.Content, "does not support image input") {
			t.Fatalf("unexpected ViewOutput: %+v", out)
		}
	})
}
