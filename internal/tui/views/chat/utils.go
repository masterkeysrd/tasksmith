package chat

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
)

// parseLsOutput extracts FileEntry values and metadata from a tool's StructuredContent.
// It handles both same-process typed values and JSON-deserialized map[string]any forms.
func parseLsOutput(structured any) (files []tools.FileEntry, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.LsOutput](structured)
	if !ok {
		return
	}
	for _, f := range out.Files {
		fe := tools.FileEntry{
			Name:          f.Name,
			Permissions:   f.Permissions,
			Links:         uint64(f.Links),
			Owner:         f.Owner,
			Group:         f.Group,
			Size:          int64(f.Size),
			IsDir:         f.IsDir,
			IsSymlink:     f.IsSymlink,
			NameTruncated: f.NameTruncated,
			LinkTarget:    f.LinkTarget,
		}
		if t, err := time.Parse(time.RFC3339, f.Modified); err == nil {
			fe.Modified = t
		}
		files = append(files, fe)
	}
	return files, out.TotalCount, out.Truncated
}

// parseGlobOutput extracts structured file lists, count, and truncation from a glob tool result.
func parseGlobOutput(structured any) (matches []string, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.GlobOutput](structured)
	if ok {
		return out.Matches, out.TotalCount, out.Truncated
	}
	return
}

// parseGrepOutput extracts structured matches, count, and truncation from a grep tool result.
func parseGrepOutput(structured any) (matches []tools.GrepOutputMatchesItem, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.GrepOutput](structured)
	if ok {
		return out.Matches, out.TotalCount, out.Truncated
	}
	return
}

// parseWebSearchOutput extracts structured results from a web_search tool result.
func parseWebSearchOutput(structured any) (results []tools.WebSearchOutputResultsItem) {
	out, ok := parseStructuredOutput[tools.WebSearchOutput](structured)
	if ok {
		return out.Results
	}
	return nil
}

// parseWebFetchStructuredOutput extracts structured WebFetchOutput fields from a web_fetch tool result.
func parseWebFetchStructuredOutput(structured any) (out tools.WebFetchOutput, ok bool) {
	return parseStructuredOutput[tools.WebFetchOutput](structured)
}

// parseDownloadOutput extracts structured DownloadOutput fields from a download tool result.
func parseDownloadOutput(structured any) (out tools.DownloadOutput, ok bool) {
	return parseStructuredOutput[tools.DownloadOutput](structured)
}

// parseFetchOutput extracts structured FetchOutput fields from a fetch tool result.
func parseFetchOutput(structured any) (out tools.FetchOutput, ok bool) {
	return parseStructuredOutput[tools.FetchOutput](structured)
}

// parseTasksOutput extracts structured TasksOutput fields from a tasks tool result.
func parseTasksOutput(structured any) (out tools.TasksOutput, ok bool) {
	return parseStructuredOutput[tools.TasksOutput](structured)
}

func getToolOutput(content message.Content) string {
	var sb strings.Builder
	for _, block := range content {
		if tb, ok := block.(*message.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

// parseStructuredOutput converts a generic interface to a structured type T.
// Handles both same-process typed assertions and cross-process JSON fallback.
func parseStructuredOutput[T any](structured any) (T, bool) {
	if structured == nil {
		var zero T
		return zero, false
	}
	if val, ok := structured.(T); ok {
		return val, true
	}
	if val, ok := structured.(*T); ok && val != nil {
		return *val, true
	}
	data, err := json.Marshal(structured)
	if err != nil {
		var zero T
		return zero, false
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		var zero T
		return zero, false
	}
	return out, true
}

func parseViewStructuredOutput(structured any) (tools.ViewOutput, bool) {
	return parseStructuredOutput[tools.ViewOutput](structured)
}

func parseWriteStructuredOutput(structured any) (tools.WriteOutput, bool) {
	return parseStructuredOutput[tools.WriteOutput](structured)
}

func parseEditStructuredOutput(structured any) (tools.EditOutput, bool) {
	return parseStructuredOutput[tools.EditOutput](structured)
}

func parseMultiEditStructuredOutput(structured any) (tools.MultiEditOutput, bool) {
	return parseStructuredOutput[tools.MultiEditOutput](structured)
}

func parseRemoveStructuredOutput(structured any) (tools.RemoveOutput, bool) {
	return parseStructuredOutput[tools.RemoveOutput](structured)
}

// parseLspDiagnosticsOutput extracts diagnostics from the tool's StructuredContent.
func parseLspDiagnosticsOutput(structured any) (diags []tools.LspDiagnosticsOutputDiagnosticsItem, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.LspDiagnosticsOutput](structured)
	if ok {
		return out.Diagnostics, out.TotalCount, out.Truncated
	}
	return
}

// parseLspRestartOutput extracts restart output from the tool's StructuredContent.
func parseLspRestartOutput(structured any) (out tools.LspRestartOutput, ok bool) {
	return parseStructuredOutput[tools.LspRestartOutput](structured)
}

// parseLspSymbolsOutput extracts search results from the tool's StructuredContent.
func parseLspSymbolsOutput(structured any) (results []tools.LspSymbolsOutputResultsItem) {
	out, ok := parseStructuredOutput[tools.LspSymbolsOutput](structured)
	if ok {
		return out.Results
	}
	return nil
}

func tryExtractTextFromJSON(input string) string {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "{") && !strings.HasPrefix(input, "[") {
		return input
	}

	// Try to unmarshal as a full message struct
	var msgObj struct {
		Role    string `json:"role"`
		Content []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(input), &msgObj); err == nil && len(msgObj.Content) > 0 {
		var parts []string
		for _, b := range msgObj.Content {
			if (b.Kind == "text" || b.Kind == "") && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	// Try to unmarshal as a content array
	var contentArr []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(input), &contentArr); err == nil && len(contentArr) > 0 {
		var parts []string
		for _, b := range contentArr {
			if (b.Kind == "text" || b.Kind == "") && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	return input
}

func stripLinePrefixes(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		before, after, ok := strings.Cut(line, " | ")
		if ok {
			isNum := true
			prefix := before
			if len(prefix) == 0 {
				isNum = false
			}
			for _, r := range prefix {
				if r < '0' || r > '9' {
					isNum = false
					break
				}
			}
			if isNum {
				lines[i] = after
			}
		}
	}
	return strings.Join(lines, "\n")
}

func openWithSystemViewer(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default:
		return
	}
	_ = cmd.Start()
}
