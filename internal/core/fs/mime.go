package fs

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var customExtensionMimes = map[string]string{
	".sql": "text/x-sql",
	".ts":  "text/typescript",
	".tsx": "text/typescript-jsx",
}

var nonBinaryMimes = map[string]struct{}{
	"application/json": {},
	"application/yaml": {},
	"application/x-sh": {},
	"application/sql":  {},
}

// DetectMIMEType detects the MIME type of a file based on its extension or content.
func DetectMIMEType(path string) string {
	ext := filepath.Ext(path)
	if ext != "" {
		extLower := strings.ToLower(ext)
		if customMime, ok := customExtensionMimes[extLower]; ok {
			return customMime
		}
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			if parts := strings.Split(mimeType, ";"); len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if n > 0 {
		mimeType := http.DetectContentType(buf[:n])
		if parts := strings.Split(mimeType, ";"); len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	return "application/octet-stream"
}

// IsBinaryMIME returns true if the MIME type indicates a binary file.
func IsBinaryMIME(mimeStr string) bool {
	if strings.HasPrefix(mimeStr, "text/") {
		return false
	}
	if _, ok := nonBinaryMimes[mimeStr]; ok {
		return false
	}
	return true
}
