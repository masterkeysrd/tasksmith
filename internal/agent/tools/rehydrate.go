package tools

import (
	"encoding/json"
	"os"

	"github.com/masterkeysrd/loom/message"
)

// FileCacheMetadata represents generic file caching metadata returned by tools.
type FileCacheMetadata struct {
	Source     string `json:"source"`
	CachedPath string `json:"cached_path"`
	MimeType   string `json:"mime_type"`
	IsBinary   bool   `json:"is_binary"`
}

// FileCacheProvider is implemented by tools that cache files.
type FileCacheProvider interface {
	GetFileCacheMetadata() []FileCacheMetadata
}

// ParseFileCacheMetadata parses structured content into a slice of FileCacheMetadata.
func ParseFileCacheMetadata(structured any) ([]FileCacheMetadata, bool) {
	if structured == nil {
		return nil, false
	}

	// Fast path: assert FileCacheProvider
	if provider, ok := structured.(FileCacheProvider); ok {
		return provider.GetFileCacheMetadata(), true
	}

	// Slow path: try unmarshalling JSON
	data, err := json.Marshal(structured)
	if err != nil {
		return nil, false
	}

	// Try as a slice
	var sliceOut []FileCacheMetadata
	if err := json.Unmarshal(data, &sliceOut); err == nil {
		var valid []FileCacheMetadata
		for _, m := range sliceOut {
			if m.Source != "" || m.CachedPath != "" {
				valid = append(valid, m)
			}
		}
		if len(valid) > 0 {
			return valid, true
		}
	}

	// Try as a single metadata struct
	var singleOut FileCacheMetadata
	if err := json.Unmarshal(data, &singleOut); err == nil {
		if singleOut.Source != "" || singleOut.CachedPath != "" {
			return []FileCacheMetadata{singleOut}, true
		}
	}

	return nil, false
}

// RehydrateMessage populates binary data from the disk cache on a cloned message if necessary.
// It returns a cloned message with re-hydrated data, or the original message if no re-hydration was needed.
func RehydrateMessage(msg message.Message) message.Message {
	tMsg, ok := msg.(*message.Tool)
	if !ok {
		return msg
	}

	metaList, ok := ParseFileCacheMetadata(tMsg.StructuredContent)
	if !ok {
		return msg
	}

	hasBinary := false
	for _, m := range metaList {
		if m.IsBinary {
			hasBinary = true
			break
		}
	}
	if !hasBinary {
		return msg
	}

	// Pre-read file data for each binary metadata
	cachedData := make(map[string][]byte)
	for _, meta := range metaList {
		if !meta.IsBinary {
			continue
		}
		readPath := meta.CachedPath
		if readPath == "" {
			readPath = meta.Source
		}
		if readPath == "" {
			continue
		}
		if _, ok := cachedData[readPath]; !ok {
			if data, err := os.ReadFile(readPath); err == nil {
				cachedData[readPath] = data
			}
		}
	}

	var binaryMetas []FileCacheMetadata
	for _, meta := range metaList {
		if !meta.IsBinary {
			continue
		}
		readPath := meta.CachedPath
		if readPath == "" {
			readPath = meta.Source
		}
		if _, ok := cachedData[readPath]; ok {
			binaryMetas = append(binaryMetas, meta)
		}
	}

	if len(binaryMetas) == 0 {
		return msg
	}

	clonedContent := make(message.Content, len(tMsg.Content))
	binaryBlockIndex := 0
	for j, b := range tMsg.Content {
		switch block := b.(type) {
		case *message.ImageBlock:
			var bestMeta *FileCacheMetadata
			if block.URL != "" {
				for i := range binaryMetas {
					if binaryMetas[i].CachedPath == block.URL || binaryMetas[i].Source == block.URL {
						bestMeta = &binaryMetas[i]
						break
					}
				}
			}
			if bestMeta == nil && len(binaryMetas) == 1 {
				bestMeta = &binaryMetas[0]
			}
			if bestMeta == nil && binaryBlockIndex < len(binaryMetas) {
				bestMeta = &binaryMetas[binaryBlockIndex]
			}

			if bestMeta != nil {
				readPath := bestMeta.CachedPath
				if readPath == "" {
					readPath = bestMeta.Source
				}
				clonedContent[j] = &message.ImageBlock{
					MIMEType: block.MIMEType,
					URL:      block.URL,
					Extras:   block.Extras,
					Data:     cachedData[readPath],
				}
			} else {
				clonedContent[j] = b
			}
			binaryBlockIndex++

		case *message.DocumentBlock:
			var bestMeta *FileCacheMetadata
			if block.URL != "" {
				for i := range binaryMetas {
					if binaryMetas[i].CachedPath == block.URL || binaryMetas[i].Source == block.URL {
						bestMeta = &binaryMetas[i]
						break
					}
				}
			}
			if bestMeta == nil && len(binaryMetas) == 1 {
				bestMeta = &binaryMetas[0]
			}
			if bestMeta == nil && binaryBlockIndex < len(binaryMetas) {
				bestMeta = &binaryMetas[binaryBlockIndex]
			}

			if bestMeta != nil {
				readPath := bestMeta.CachedPath
				if readPath == "" {
					readPath = bestMeta.Source
				}
				clonedContent[j] = &message.DocumentBlock{
					MIMEType: block.MIMEType,
					URL:      block.URL,
					Extras:   block.Extras,
					Data:     cachedData[readPath],
				}
			} else {
				clonedContent[j] = b
			}
			binaryBlockIndex++

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
		msg := RehydrateMessage(m)
		if tMsg, ok := msg.(*message.Tool); ok && tMsg.Name == "remove" && tMsg.StructuredContent != nil {
			var out RemoveOutput
			if val, ok := tMsg.StructuredContent.(RemoveOutput); ok {
				out = val
			} else if val, ok := tMsg.StructuredContent.(*RemoveOutput); ok && val != nil {
				out = *val
			} else {
				data, _ := json.Marshal(tMsg.StructuredContent)
				_ = json.Unmarshal(data, &out)
			}
			out.Content = ""
			msg = &message.Tool{
				Base:              tMsg.Base,
				ToolCallID:        tMsg.ToolCallID,
				Name:              tMsg.Name,
				Content:           tMsg.Content,
				IsError:           tMsg.IsError,
				StructuredContent: out,
			}
		}
		rehydrated[i] = msg
	}
	return rehydrated
}
