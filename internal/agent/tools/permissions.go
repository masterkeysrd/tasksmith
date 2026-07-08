package tools

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
)

func init() {
	// Group 1: File Modification Tools
	permissions.RegisterHandler("write", &FileModificationPermissionHandler{})
	permissions.RegisterHandler("edit", &FileModificationPermissionHandler{})
	permissions.RegisterHandler("multi_edit", &FileModificationPermissionHandler{})

	// Standalone Filesystem Tools
	permissions.RegisterHandler("remove", &RemovePermissionHandler{})
	permissions.RegisterHandler("view", &ViewPermissionHandler{})

	// Group 2: File Search Tools (Read-Only)
	permissions.RegisterHandler("ls", &FileSearchPermissionHandler{})
	permissions.RegisterHandler("grep", &FileSearchPermissionHandler{})
	permissions.RegisterHandler("glob", &FileSearchPermissionHandler{})

	// Web Tools
	permissions.RegisterHandler("web_fetch", &WebFetchPermissionHandler{})
	permissions.RegisterHandler("fetch", &WebFetchPermissionHandler{})
	permissions.RegisterHandler("download", &DownloadPermissionHandler{})
	permissions.RegisterHandler("web_search", &WebSearchPermissionHandler{})

	// Standalone Command execution
	permissions.RegisterHandler("bash", &BashPermissionHandler{})
}

func resolveAbsPath(ctx context.Context, rawPath string) (string, error) {
	wsCWD := permissions.GetWorkspaceCWD(ctx)
	if wsCWD == "" {
		var err error
		wsCWD, err = filepath.Abs(".")
		if err != nil {
			return "", err
		}
	}
	cleaned := cleanPath(rawPath)
	if filepath.IsAbs(cleaned) {
		return filepath.Abs(cleaned)
	}
	return filepath.Abs(filepath.Join(wsCWD, cleaned))
}

func isSafeWorkspacePath(ctx context.Context, absPath string) bool {
	wsCWD := permissions.GetWorkspaceCWD(ctx)
	if wsCWD == "" {
		return false
	}
	wsAbs, err := filepath.Abs(wsCWD)
	if err != nil {
		return false
	}
	if !strings.HasPrefix(absPath, wsAbs) {
		return false
	}
	rel, err := filepath.Rel(wsAbs, absPath)
	if err != nil {
		return false
	}
	parts := strings.SplitSeq(filepath.ToSlash(rel), "/")
	for part := range parts {
		if part == ".git" {
			return false
		}
	}
	return true
}

func getFileOptions(toolName string, rawPath string) []permissions.PermissionOption {
	var options []permissions.PermissionOption
	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow exact file path: %q", rawPath),
		Target:      rawPath,
		MatchMethod: "exact",
		Action:      permissions.ActionAllow,
	})

	parentDir := filepath.Dir(rawPath)
	if parentDir != "" && parentDir != "." && parentDir != "/" {
		options = append(options, permissions.PermissionOption{
			Label:       fmt.Sprintf("Allow all operations in directory: %q", parentDir),
			Target:      parentDir,
			MatchMethod: "path",
			Action:      permissions.ActionAllow,
		})
	}

	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow all %s operations", toolName),
		Target:      "*",
		MatchMethod: "wildcard",
		Action:      permissions.ActionAllow,
		IsDanger:    true,
	})

	return options
}

func getWebOptions(toolName string, rawURL string) []permissions.PermissionOption {
	var options []permissions.PermissionOption
	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow exact URL: %q", rawURL),
		Target:      rawURL,
		MatchMethod: "exact",
		Action:      permissions.ActionAllow,
	})

	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
		parts := strings.Split(trimmed, "/")
		if len(parts) > 0 && parts[0] != "" {
			domain := parts[0]
			options = append(options, permissions.PermissionOption{
				Label:       fmt.Sprintf("Allow all URLs from domain: %q", domain),
				Target:      domain,
				MatchMethod: "prefix",
				Action:      permissions.ActionAllow,
			})
		}
	}

	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow all %s operations", toolName),
		Target:      "*",
		MatchMethod: "wildcard",
		Action:      permissions.ActionAllow,
		IsDanger:    true,
	})

	return options
}

func matchFile(grantTarget, matchMethod, targetValue string) bool {
	if grantTarget == "*" {
		return true
	}
	switch matchMethod {
	case "exact":
		return grantTarget == targetValue
	case "prefix":
		return strings.HasPrefix(targetValue, grantTarget)
	case "path":
		gClean := filepath.Clean(grantTarget)
		tClean := filepath.Clean(targetValue)
		if gClean == tClean {
			return true
		}
		return strings.HasPrefix(tClean, gClean+string(filepath.Separator))
	case "wildcard":
		match, err := filepath.Match(grantTarget, targetValue)
		return err == nil && match
	default:
		return grantTarget == targetValue
	}
}

func matchGeneric(grantTarget, matchMethod, targetValue string) bool {
	if grantTarget == "*" {
		return true
	}
	switch matchMethod {
	case "exact":
		return grantTarget == targetValue
	case "prefix":
		return strings.HasPrefix(targetValue, grantTarget)
	case "wildcard":
		match, err := path.Match(grantTarget, targetValue)
		return err == nil && match
	default:
		return grantTarget == targetValue
	}
}

func evaluateFileGrants(grants []permissions.Permission, targetVal string) (permissions.PermissionState, bool) {
	return permissions.EvaluateGrants(grants, func(p permissions.Permission) bool {
		return matchFile(p.Target, p.MatchMethod, targetVal)
	})
}

func evaluateGenericGrants(grants []permissions.Permission, targetVal string) (permissions.PermissionState, bool) {
	return permissions.EvaluateGrants(grants, func(p permissions.Permission) bool {
		return matchGeneric(p.Target, p.MatchMethod, targetVal)
	})
}

// --- Group 1: FileModificationPermissionHandler (write, edit, multi_edit) ---

type FileModificationPermissionHandler struct{}

func (h *FileModificationPermissionHandler) GetPermissionGroup() string {
	return "edit_file"
}

func (h *FileModificationPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: nil,
		}
	}

	args := req.Args
	rawPath, _ := args["path"].(string)
	if state, found := evaluateFileGrants(grants, rawPath); found {
		return permissions.EvaluationResult{State: state}
	}

	absPath, err := resolveAbsPath(ctx, rawPath)
	if err != nil {
		return permissions.EvaluationResult{State: permissions.StateRequiresAuth, Hints: []string{"Failed to resolve target file path"}}
	}

	if mode == permissions.ModeAuto {
		if isSafeWorkspacePath(ctx, absPath) {
			return permissions.EvaluationResult{State: permissions.StateAuto}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Auto mode blocked: editing path %q is outside the workspace or inside .git", rawPath)},
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: nil,
	}
}

func (h *FileModificationPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	rawPath, _ := args["path"].(string)
	return getFileOptions(req.ToolName, rawPath)
}

func (h *FileModificationPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	rawPath, _ := args["path"].(string)
	absPath, err := resolveAbsPath(ctx, rawPath)
	if err != nil {
		return nil, err
	}

	var oldContent string
	if oldBytes, err := os.ReadFile(absPath); err == nil {
		oldContent = string(oldBytes)
	}

	var newContent string
	wsCWD := permissions.GetWorkspaceCWD(ctx)
	relPath, err := filepath.Rel(wsCWD, absPath)
	if err != nil {
		relPath = rawPath
	}

	switch req.ToolName {
	case "write":
		newContent, _ = args["content"].(string)
	case "edit":
		target, _ := args["target"].(string)
		replacement, _ := args["replacement"].(string)
		replaceAll, _ := args["replace_all"].(bool)

		contentNorm := strings.ReplaceAll(oldContent, "\r\n", "\n")
		var count int
		newContent, count, _ = SmartReplace(contentNorm, target, replacement, replaceAll)
		if count == 0 {
			return preview.DefaultTextPreview{Text: fmt.Sprintf("Edit file %q:\nWarning: Target block not found in file (even with smart replace).\nReplace %q\nWith %q", rawPath, target, replacement)}, nil
		}
	case "multi_edit":
		rawEdits, ok := args["edits"].([]any)
		if !ok {
			return preview.DefaultTextPreview{Text: fmt.Sprintf("Multi-edit file %q", rawPath)}, nil
		}
		current := strings.ReplaceAll(oldContent, "\r\n", "\n")
		for _, eVal := range rawEdits {
			eMap, ok := eVal.(map[string]any)
			if !ok {
				continue
			}
			target, _ := eMap["target"].(string)
			replacement, _ := eMap["replacement"].(string)
			replaceAll, _ := eMap["replace_all"].(bool)
			if target == "" {
				continue
			}
			current, _, _ = SmartReplace(current, target, replacement, replaceAll)
		}
		newContent = current
	default:
		return preview.DefaultTextPreview{Text: fmt.Sprintf("Modify file: %q", rawPath)}, nil
	}

	unifiedDiff := diff.FormatUnified(relPath, relPath, oldContent, newContent)
	if unifiedDiff == "" {
		return preview.DefaultTextPreview{Text: "No changes made (target and replacement are identical)."}, nil
	}
	return preview.FileEditPreview{
		Path: rawPath,
		Diff: unifiedDiff,
	}, nil
}

// --- Standalone: RemovePermissionHandler ---

type RemovePermissionHandler struct{}

func (h *RemovePermissionHandler) GetPermissionGroup() string {
	return "delete_file"
}

func (h *RemovePermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Strict mode: authorization required to remove file/dir %q", req.Args["path"])},
		}
	}

	args := req.Args
	rawPath, _ := args["path"].(string)
	if state, found := evaluateFileGrants(grants, rawPath); found {
		return permissions.EvaluationResult{State: state}
	}

	absPath, err := resolveAbsPath(ctx, rawPath)
	if err != nil {
		return permissions.EvaluationResult{State: permissions.StateRequiresAuth, Hints: []string{"Failed to resolve target file path"}}
	}

	if mode == permissions.ModeAuto {
		if isSafeWorkspacePath(ctx, absPath) {
			return permissions.EvaluationResult{State: permissions.StateAuto}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Auto mode blocked: removing path %q is outside the workspace or inside .git", rawPath)},
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to remove file/dir %q", rawPath)},
	}
}

func (h *RemovePermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	rawPath, _ := args["path"].(string)
	return getFileOptions("remove", rawPath)
}

func (h *RemovePermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	rawPath, _ := args["path"].(string)
	recursive, _ := args["recursive"].(bool)
	label := "file"
	if recursive {
		label = "file/directory recursively"
	}
	return preview.DefaultTextPreview{Text: fmt.Sprintf("Remove %s: %q", label, rawPath)}, nil
}

// --- Standalone: ViewPermissionHandler ---

type ViewPermissionHandler struct{}

func (h *ViewPermissionHandler) GetPermissionGroup() string {
	return "read_file"
}

func (h *ViewPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: nil,
		}
	}

	args := req.Args
	rawPath, _ := args["path"].(string)
	if state, found := evaluateFileGrants(grants, rawPath); found {
		return permissions.EvaluationResult{State: state}
	}

	absPath, err := resolveAbsPath(ctx, rawPath)
	if err != nil {
		return permissions.EvaluationResult{State: permissions.StateRequiresAuth, Hints: []string{"Failed to resolve file path"}}
	}

	if mode == permissions.ModeAuto {
		if isSafeWorkspacePath(ctx, absPath) {
			return permissions.EvaluationResult{State: permissions.StateAuto}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Auto mode blocked: viewing path %q outside the workspace or inside .git", rawPath)},
		}
	}

	if mode == permissions.ModeDefault {
		if isSafeWorkspacePath(ctx, absPath) {
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: nil,
	}
}

func (h *ViewPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	rawPath, _ := args["path"].(string)
	return getFileOptions("view", rawPath)
}

func (h *ViewPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	rawPath, _ := args["path"].(string)
	start, _ := args["start_line"].(float64)
	end, _ := args["end_line"].(float64)

	var details string
	if start > 0 || end > 0 {
		details = fmt.Sprintf(" (lines %.0f-%.0f)", start, end)
	}
	return preview.DefaultTextPreview{Text: fmt.Sprintf("View file: %q%s", rawPath, details)}, nil
}

// --- Group 2: FileSearchPermissionHandler (ls, grep, glob) ---

type FileSearchPermissionHandler struct{}

func (h *FileSearchPermissionHandler) GetPermissionGroup() string {
	return "search_file"
}

func (h *FileSearchPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	args := req.Args
	var targetVal string
	var pathVal string
	if req.ToolName == "glob" {
		targetVal, _ = args["pattern"].(string)
		pathVal, _ = args["path"].(string)
		if pathVal == "" {
			pathVal = "."
		}
	} else {
		targetVal, _ = args["path"].(string)
		if targetVal == "" {
			targetVal = "."
		}
		pathVal = targetVal
	}

	if state, found := evaluateFileGrants(grants, pathVal); found {
		return permissions.EvaluationResult{State: state}
	}

	// Glob checks pattern safety, others resolve path safety
	isSafe := true
	if req.ToolName == "glob" {
		isSafe = !strings.Contains(targetVal, "../")
		if isSafe {
			absPath, err := resolveAbsPath(ctx, pathVal)
			if err != nil {
				return permissions.EvaluationResult{State: permissions.StateRequiresAuth, Hints: []string{"Failed to resolve directory path"}}
			}
			isSafe = isSafeWorkspacePath(ctx, absPath)
		}
	} else {
		absPath, err := resolveAbsPath(ctx, targetVal)
		if err != nil {
			return permissions.EvaluationResult{State: permissions.StateRequiresAuth, Hints: []string{"Failed to resolve directory path"}}
		}
		isSafe = isSafeWorkspacePath(ctx, absPath)
	}

	if mode == permissions.ModeAuto {
		if isSafe {
			return permissions.EvaluationResult{State: permissions.StateAuto}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Auto mode blocked: searching path %q outside workspace or inside .git", targetVal)},
		}
	}

	if mode == permissions.ModeDefault {
		if isSafe {
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: nil,
	}
}

func (h *FileSearchPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	if req.ToolName == "glob" {
		pattern, _ := args["pattern"].(string)
		return []permissions.PermissionOption{
			{
				Label:       fmt.Sprintf("Allow exact glob pattern: %q", pattern),
				Target:      pattern,
				MatchMethod: "exact",
				Action:      permissions.ActionAllow,
			},
			{
				Label:       "Allow all glob search operations",
				Target:      "*",
				MatchMethod: "wildcard",
				Action:      permissions.ActionAllow,
				IsDanger:    true,
			},
		}
	}

	rawPath, _ := args["path"].(string)
	if rawPath == "" {
		rawPath = "."
	}
	return getFileOptions(req.ToolName, rawPath)
}

func (h *FileSearchPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	switch req.ToolName {
	case "ls":
		rawPath, _ := args["path"].(string)
		if rawPath == "" {
			rawPath = "."
		}
		return preview.DefaultTextPreview{Text: fmt.Sprintf("List directory contents: %q", rawPath)}, nil
	case "grep":
		rawPath, _ := args["path"].(string)
		pattern, _ := args["pattern"].(string)
		if rawPath == "" {
			rawPath = "."
		}
		return preview.DefaultTextPreview{Text: fmt.Sprintf("Grep files for pattern %q in path %q", pattern, rawPath)}, nil
	case "glob":
		pattern, _ := args["pattern"].(string)
		return preview.DefaultTextPreview{Text: fmt.Sprintf("Find files matching glob pattern: %q", pattern)}, nil
	default:
		return preview.DefaultTextPreview{Text: fmt.Sprintf("Search files via %s", req.ToolName)}, nil
	}
}

// --- Group 3: Split Web Handlers (web_fetch, web_search, download) ---

type WebFetchPermissionHandler struct{}

func (h *WebFetchPermissionHandler) GetPermissionGroup() string {
	return "web_fetch"
}

func (h *WebFetchPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Strict mode: authorization required to perform %s: %q", req.ToolName, req.Args["url"])},
		}
	}

	args := req.Args
	urlVal, _ := args["url"].(string)

	if state, found := evaluateGenericGrants(grants, urlVal); found {
		return permissions.EvaluationResult{State: state}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{State: permissions.StateAuto}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to perform %s: %q", req.ToolName, urlVal)},
	}
}

func (h *WebFetchPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	urlVal, _ := args["url"].(string)
	return getWebOptions(req.ToolName, urlVal)
}

func (h *WebFetchPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	urlVal, _ := args["url"].(string)
	return preview.DefaultTextPreview{Text: fmt.Sprintf("Fetch web page content: %q", urlVal)}, nil
}

type DownloadPermissionHandler struct{}

func resolveDownloadDest(ctx context.Context, rawURL, dest string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		parsedURL = &url.URL{}
	}
	if dest == "" {
		base := path.Base(parsedURL.Path)
		if base == "" || base == "." || base == "/" {
			base = "downloaded_file"
		}
		dest = base
	}
	return resolveAbsPath(ctx, dest)
}

func (h *DownloadPermissionHandler) GetPermissionGroup() string {
	return "download"
}

func (h *DownloadPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: nil,
		}
	}

	args := req.Args
	urlVal, _ := args["url"].(string)
	destVal, _ := args["destination"].(string)

	if state, found := evaluateGenericGrants(grants, urlVal); found {
		return permissions.EvaluationResult{State: state}
	}

	absDest, err := resolveDownloadDest(ctx, urlVal, destVal)
	if err != nil {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{"Failed to resolve download destination path"},
		}
	}

	if !isSafeWorkspacePath(ctx, absDest) {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Destination path %q is outside the workspace or inside .git", destVal)},
		}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{State: permissions.StateAuto}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to download from URL %q", urlVal)},
	}
}

func (h *DownloadPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	urlVal, _ := args["url"].(string)
	destVal, _ := args["destination"].(string)

	var options []permissions.PermissionOption

	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow exact URL: %q", urlVal),
		Target:      urlVal,
		MatchMethod: "exact",
		Action:      permissions.ActionAllow,
	})

	if strings.HasPrefix(urlVal, "http://") || strings.HasPrefix(urlVal, "https://") {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(urlVal, "https://"), "http://")
		parts := strings.Split(trimmed, "/")
		if len(parts) > 0 && parts[0] != "" {
			domain := parts[0]
			options = append(options, permissions.PermissionOption{
				Label:       fmt.Sprintf("Allow all URLs from domain: %q", domain),
				Target:      domain,
				MatchMethod: "prefix",
				Action:      permissions.ActionAllow,
			})
		}
	}

	if destVal == "" {
		if parsedURL, err := url.Parse(urlVal); err == nil {
			base := path.Base(parsedURL.Path)
			if base != "" && base != "." && base != "/" {
				destVal = base
			}
		}
	}

	if destVal != "" {
		options = append(options, permissions.PermissionOption{
			Label:       fmt.Sprintf("Allow exact file path: %q", destVal),
			Target:      destVal,
			MatchMethod: "exact",
			Action:      permissions.ActionAllow,
		})

		parentDir := filepath.Dir(destVal)
		if parentDir != "" && parentDir != "." && parentDir != "/" {
			options = append(options, permissions.PermissionOption{
				Label:       fmt.Sprintf("Allow all operations in directory: %q", parentDir),
				Target:      parentDir,
				MatchMethod: "path",
				Action:      permissions.ActionAllow,
			})
		}
	}

	options = append(options, permissions.PermissionOption{
		Label:       "Allow all download operations",
		Target:      "*",
		MatchMethod: "wildcard",
		Action:      permissions.ActionAllow,
		IsDanger:    true,
	})

	return options
}

func (h *DownloadPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	urlVal, _ := args["url"].(string)
	destVal, _ := args["destination"].(string)
	var destText string
	if destVal != "" {
		destText = fmt.Sprintf(" to %q", destVal)
	}
	return preview.DefaultTextPreview{Text: fmt.Sprintf("Download file from URL: %q%s", urlVal, destText)}, nil
}

type WebSearchPermissionHandler struct{}

func (h *WebSearchPermissionHandler) GetPermissionGroup() string {
	return "web_search"
}

func (h *WebSearchPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Strict mode: authorization required to perform web_search: %q", req.Args["query"])},
		}
	}

	args := req.Args
	query, _ := args["query"].(string)

	if state, found := evaluateGenericGrants(grants, query); found {
		return permissions.EvaluationResult{State: state}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{State: permissions.StateAuto}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to perform web_search: %q", query)},
	}
}

func (h *WebSearchPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	query, _ := args["query"].(string)
	return []permissions.PermissionOption{
		{
			Label:       fmt.Sprintf("Allow exact search query: %q", query),
			Target:      query,
			MatchMethod: "exact",
			Action:      permissions.ActionAllow,
		},
		{
			Label:       "Allow all web search queries",
			Target:      "*",
			MatchMethod: "wildcard",
			Action:      permissions.ActionAllow,
			IsDanger:    true,
		},
	}
}

func (h *WebSearchPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	query, _ := args["query"].(string)
	return preview.DefaultTextPreview{Text: fmt.Sprintf("Web Search query: %q", query)}, nil
}

// --- Standalone: BashPermissionHandler ---

type BashPermissionHandler struct{}

func (h *BashPermissionHandler) GetPermissionGroup() string {
	return "command"
}

func (h *BashPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	if mode == permissions.ModeStrict {
		cmd, _ := req.Args["command"].(string)
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Strict mode: authorization required to run bash command: %q", cmd)},
		}
	}

	reqs := h.GetGrantRequests(ctx, req, mode, grants)
	if len(reqs) > 0 {
		var hints []string
		cmd, _ := req.Args["command"].(string)
		wsCWD := permissions.GetWorkspaceCWD(ctx)
		ops, err := shellguard.Analyze(cmd, wsCWD)
		if err == nil {
			for _, op := range ops {
				if op.Action == shellguard.ActionDelete {
					hints = append(hints, "WARNING: Command classified as Destructive.")
					break
				}
			}
			for _, op := range ops {
				if op.Safety == shellguard.SafetyUnsafe {
					hints = append(hints, "WARNING: Command classified as Unsafe.")
					break
				}
			}
		} else {
			hints = append(hints, fmt.Sprintf("Authorization required to run bash command: %q", cmd))
		}

		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: hints,
		}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{
			State: permissions.StateAuto,
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateExplicitAllow,
	}
}

func (h *BashPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	cmd, _ := req.Args["command"].(string)
	words := strings.Fields(cmd)
	exec := ""
	if len(words) > 0 {
		exec = words[0]
	}
	return h.GetOptionsForCommand(cmd, exec)
}

func (h *BashPermissionHandler) GetOptionsForCommand(cmd, exec string) []permissions.PermissionOption {
	var options []permissions.PermissionOption

	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow exact command: %q", cmd),
		Target:      cmd,
		MatchMethod: "exact",
		Action:      permissions.ActionAllow,
	})

	if exec != "" {
		options = append(options, permissions.PermissionOption{
			Label:       fmt.Sprintf("Allow all commands starting with %q", exec),
			Target:      exec,
			MatchMethod: "prefix",
			Action:      permissions.ActionAllow,
		})
	}

	options = append(options, permissions.PermissionOption{
		Label:       "Allow all bash commands",
		Target:      "*",
		MatchMethod: "wildcard",
		Action:      permissions.ActionAllow,
		IsDanger:    true,
	})

	return options
}

func (h *BashPermissionHandler) GetGrantRequests(
	ctx context.Context,
	req permissions.ToolCallRequest,
	mode permissions.PermissionMode,
	grants []permissions.Permission,
) []permissions.PermissionGrantRequest {
	args := req.Args
	cmd, _ := args["command"].(string)
	wsCWD := permissions.GetWorkspaceCWD(ctx)

	chain, err := shellguard.Parse(cmd)
	if err != nil {
		return []permissions.PermissionGrantRequest{
			{
				ID:          "raw_command",
				Description: fmt.Sprintf("Authorization required to run bash command: %q", cmd),
				Options:     h.GetOptionsForCommand(cmd, ""),
			},
		}
	}

	ops, err := shellguard.Analyze(cmd, wsCWD)
	if err != nil {
		return []permissions.PermissionGrantRequest{
			{
				ID:          "raw_command",
				Description: fmt.Sprintf("Authorization required to run bash command: %q", cmd),
				Options:     h.GetOptionsForCommand(cmd, ""),
			},
		}
	}

	var reqs []permissions.PermissionGrantRequest
	cmdCount := 0

	for _, pipeline := range chain.Pipelines {
		for _, parsedCmd := range pipeline.Commands {
			curr := &parsedCmd
			for curr.SubCommand != nil {
				curr = curr.SubCommand
			}

			if curr.Executable == "" || curr.Executable == "pwd" {
				continue
			}

			if curr.Executable == "cd" {
				targetDir := ""
				if len(curr.Args) > 0 {
					targetDir = curr.Args[0]
				} else {
					if home, err := os.UserHomeDir(); err == nil {
						targetDir = home
					}
				}
				absTarget, err := resolveAbsPath(ctx, targetDir)
				if err == nil && isSafeWorkspacePath(ctx, absTarget) {
					continue
				}
			}

			effectiveCmdStr := strings.Join(append([]string{curr.Executable}, curr.Args...), " ")
			if effectiveCmdStr == "" {
				continue
			}

			var op *shellguard.Operation
			for i := range ops {
				if ops[i].Command != nil {
					if ops[i].Command == &parsedCmd || (ops[i].Command.Executable == parsedCmd.Executable && len(ops[i].Command.Args) == len(parsedCmd.Args)) {
						op = &ops[i]
						break
					}
				}
			}

			hasGrant := false
			for _, grant := range grants {
				if shellguard.MatchParsedCommand(grant.Target, grant.MatchMethod, parsedCmd) {
					cmdCWD := wsCWD
					if op != nil && op.CWD != "" {
						cmdCWD = op.CWD
					}
					if matchAllowedDirectory(cmdCWD, grant.AllowedDirectory) {
						if grant.Action == permissions.ActionAllow {
							hasGrant = true
							break
						}
					}
				}
			}

			if hasGrant {
				continue
			}

			requiresPrompt := false
			if curr.Executable == "cd" {
				requiresPrompt = true
			} else {
				if op == nil {
					requiresPrompt = true
				} else {
					if mode == permissions.ModeAuto {
						if op.Action == shellguard.ActionDelete || op.Safety == shellguard.SafetyUnsafe {
							requiresPrompt = true
						}
					} else {
						requiresPrompt = true
					}
				}
			}

			if requiresPrompt {
				cmdCount++
				cmdCWD := wsCWD
				if op != nil && op.CWD != "" {
					cmdCWD = op.CWD
				}

				dirOpts := []permissions.PermissionOption{
					{
						Label:       fmt.Sprintf("Restrict to %s", cmdCWD),
						Target:      cmdCWD,
						MatchMethod: "path",
						Action:      permissions.ActionAllow,
					},
					{
						Label:       "Anywhere (*)",
						Target:      "*",
						MatchMethod: "wildcard",
						Action:      permissions.ActionAllow,
					},
				}

				reqs = append(reqs, permissions.PermissionGrantRequest{
					ID:               fmt.Sprintf("cmd_%d", cmdCount),
					Description:      fmt.Sprintf("Permission required for: %s", effectiveCmdStr),
					Options:          h.GetOptionsForCommand(effectiveCmdStr, curr.Executable),
					DirectoryOptions: dirOpts,
				})
			}
		}
	}

	return reqs
}

func matchAllowedDirectory(cmdCWD, allowedDir string) bool {
	if allowedDir == "" || allowedDir == "*" {
		return true
	}
	cClean := filepath.Clean(cmdCWD)
	aClean := filepath.Clean(allowedDir)
	if cClean == aClean {
		return true
	}
	return strings.HasPrefix(cClean, aClean+string(filepath.Separator))
}

func (h *BashPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (preview.ToolPreview, error) {
	args := req.Args
	cmd, _ := args["command"].(string)
	return preview.BashPreview{Command: cmd}, nil
}
