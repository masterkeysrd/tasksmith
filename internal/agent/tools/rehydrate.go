package tools

import (
	"encoding/json"
	"os"

	"github.com/masterkeysrd/loom/message"
)

// FileCacheMetadata represents generic file caching metadata returned by tools.
type FileCacheMetadata struct {
	Path       string `json:"path"`
	CachedPath string `json:"cached_path"`
	MimeType   string `json:"mime_type"`
	IsBinary   bool   `json:"is_binary"`
}

// ParseFileCacheMetadata parses structured content into FileCacheMetadata.
func ParseFileCacheMetadata(structured any) (FileCacheMetadata, bool) {
	if structured == nil {
		return FileCacheMetadata{}, false
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return FileCacheMetadata{}, false
	}
	var out FileCacheMetadata
	if err := json.Unmarshal(data, &out); err != nil {
		return FileCacheMetadata{}, false
	}
	// A valid metadata should at least have a Path or CachedPath
	if out.Path == "" && out.CachedPath == "" {
		return FileCacheMetadata{}, false
	}
	return out, true
}

// RehydrateMessage populates binary data from the disk cache on a cloned message if necessary.
// It returns a cloned message with re-hydrated data, or the original message if no re-hydration was needed.
func RehydrateMessage(msg message.Message) message.Message {
	tMsg, ok := msg.(*message.Tool)
	if !ok {
		return msg
	}

	meta, ok := ParseFileCacheMetadata(tMsg.StructuredContent)
	if !ok || !meta.IsBinary {
		return msg
	}

	readPath := meta.CachedPath
	if readPath == "" {
		readPath = meta.Path
	}
	if readPath == "" {
		return msg
	}

	data, err := os.ReadFile(readPath)
	if err != nil {
		return msg
	}

	// Clone the content blocks and populate binary data
	clonedContent := make(message.Content, len(tMsg.Content))
	for j, b := range tMsg.Content {
		switch block := b.(type) {
		case *message.ImageBlock:
			clonedContent[j] = &message.ImageBlock{
				MIMEType: block.MIMEType,
				URL:      block.URL,
				Extras:   block.Extras,
				Data:     data,
			}
		case *message.DocumentBlock:
			clonedContent[j] = &message.DocumentBlock{
				MIMEType: block.MIMEType,
				URL:      block.URL,
				Extras:   block.Extras,
				Data:     data,
			}
		default:
			clonedContent[j] = b
		}
	}

	return &message.Tool{
		Base:              tMsg.Base,
		ToolCallID:        tMsg.ToolCallID,
		Name:              tMsg.Name,
		Content:           clonedContent,
		IsError:           tMsg.IsError,
		StructuredContent: tMsg.StructuredContent,
	}
}

// RehydrateMessagesForLLM clones and re-hydrates a list of messages.
func RehydrateMessagesForLLM(messages []message.Message) []message.Message {
	if len(messages) == 0 {
		return messages
	}
	rehydrated := make([]message.Message, len(messages))
	for i, m := range messages {
		rehydrated[i] = RehydrateMessage(m)
	}
	return rehydrated
}
