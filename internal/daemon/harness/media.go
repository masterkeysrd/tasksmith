package harness

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/media"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

// hydrateMessagesForLLM builds a transient copy of msgs for an LLM invocation,
// hydrating lightweight media descriptors from store, degrading unsupported or
// missing blobs to text placeholders, annotating PDF page ranges, and promoting
// tool-result media to a trailing user message for providers whose adapters do
// not preserve media inside role: tool. The checkpointed State.Messages slice
// is never mutated.
func hydrateMessagesForLLM(store *media.Store, model models.Model, msgs []message.Message) []message.Message {
	if !messagesHaveMediaBlocks(msgs) {
		return msgs
	}

	out := make([]message.Message, 0, len(msgs))
	var pendingToolMedia []message.Block

	flushToolMedia := func() {
		if len(pendingToolMedia) == 0 {
			return
		}
		blocks := make(message.Content, 0, len(pendingToolMedia)+1)
		blocks = append(blocks, &message.TextBlock{Text: "Attached media from tool output:"})
		blocks = append(blocks, pendingToolMedia...)
		out = append(out, message.NewUser(blocks...))
		pendingToolMedia = nil
	}

	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role() != message.RoleTool {
			flushToolMedia()
		}

		cloned := message.CloneMessage(msg)
		content := cloned.GetContent()
		if len(content) == 0 {
			out = append(out, cloned)
			continue
		}

		nextContent := make(message.Content, 0, len(content))
		changed := false
		for _, blk := range content {
			desc, mod, isMedia := media.DescriptorFromBlock(blk)
			if !isMedia {
				nextContent = append(nextContent, blk)
				continue
			}
			changed = true

			if mod != "" && !model.SupportsInputModality(string(mod)) {
				nextContent = append(nextContent, &message.TextBlock{Text: media.OmittedPlaceholder(mod, desc)})
				continue
			}

			hydratedBlk, ok := hydrateSingleBlock(blk, desc, store)
			if !ok {
				nextContent = append(nextContent, &message.TextBlock{Text: media.UnavailablePlaceholder(mod, desc)})
				continue
			}

			var pageNote *message.TextBlock
			if mod == media.ModalityPDF && desc.Pages != "" && !strings.Contains(content.Text(), "pages "+desc.Pages) {
				pageNote = &message.TextBlock{
					Text: fmt.Sprintf("[Document %s: examine pages %s]", cmp.Or(desc.DisplayName, "PDF"), desc.Pages),
				}
			}

			if msg.Role() == message.RoleTool && !providerPreservesToolMedia(model.ProviderType, hydratedBlk, desc.MIMEType) {
				if pageNote != nil {
					pendingToolMedia = append(pendingToolMedia, pageNote)
				}
				pendingToolMedia = append(pendingToolMedia, hydratedBlk)
				continue
			}
			if pageNote != nil {
				nextContent = append(nextContent, pageNote)
			}
			nextContent = append(nextContent, hydratedBlk)
		}

		if changed {
			if len(nextContent) == 0 {
				nextContent = append(nextContent, &message.TextBlock{Text: "[Media attached]"})
			}
			setMessageContent(cloned, nextContent)
		}
		out = append(out, cloned)
	}
	flushToolMedia()
	return out
}

func messagesHaveMediaBlocks(msgs []message.Message) bool {
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		for _, blk := range msg.GetContent() {
			if _, _, ok := media.DescriptorFromBlock(blk); ok {
				return true
			}
		}
	}
	return false
}

func hydrateSingleBlock(blk message.Block, desc api.Media, store *media.Store) (message.Block, bool) {
	switch b := blk.(type) {
	case *message.ImageBlock:
		if len(b.Data) > 0 || b.URL != "" {
			return b, true
		}
		if store == nil || desc.SHA256 == "" {
			return nil, false
		}
		data, err := store.Read(desc.SHA256)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		cp := *b
		cp.Data = data
		return &cp, true

	case *message.DocumentBlock:
		if len(b.Data) > 0 || b.URL != "" {
			return b, true
		}
		if store == nil || desc.SHA256 == "" {
			return nil, false
		}
		data, err := store.Read(desc.SHA256)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		cp := *b
		cp.Data = data
		return &cp, true

	case *message.AudioBlock:
		if len(b.Data) > 0 || b.URL != "" {
			return b, true
		}
		if store == nil || desc.SHA256 == "" {
			return nil, false
		}
		data, err := store.Read(desc.SHA256)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		cp := *b
		cp.Data = data
		return &cp, true

	default:
		return blk, true
	}
}

func providerPreservesToolMedia(provider string, block message.Block, mime string) bool {
	switch provider {
	case "", "genai", "google-genai", "gemini":
		return true
	case "anthropic":
		switch block.(type) {
		case *message.ImageBlock:
			return true
		case *message.DocumentBlock:
			return mime == "application/pdf"
		}
		return false
	default:
		return false
	}
}

func setMessageContent(msg message.Message, content message.Content) {
	switch m := msg.(type) {
	case *message.User:
		m.Content = content
	case *message.Assistant:
		m.Content = content
	case *message.Tool:
		m.Content = content
	case *message.System:
		m.Content = content
	}
}
