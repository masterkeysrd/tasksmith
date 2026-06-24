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

// --- Common Helper Functions ---

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
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
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
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("⚠️ Auto mode blocked: editing path %q is outside the workspace or inside .git", rawPath)},
		}
	}

	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("🔒 Strict mode: authorization required to modify file %q", rawPath)},
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to modify file %q", rawPath)},
	}
}

func (h *FileModificationPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	rawPath, _ := args["path"].(string)
	return getFileOptions(req.ToolName, rawPath)
}

func (h *FileModificationPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	rawPath, _ := args["path"].(string)
	absPath, err := resolveAbsPath(ctx, rawPath)
	if err != nil {
		return "", err
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
		if !strings.Contains(oldContent, target) {
			return fmt.Sprintf("Edit file %q:\nWarning: Target block not found in file.\nReplace %q\nWith %q", rawPath, target, replacement), nil
		}
		replaceAll, _ := args["replace_all"].(bool)
		if replaceAll {
			newContent = strings.ReplaceAll(oldContent, target, replacement)
		} else {
			newContent = strings.Replace(oldContent, target, replacement, 1)
		}
	case "multi_edit":
		rawEdits, ok := args["edits"].([]any)
		if !ok {
			return fmt.Sprintf("Multi-edit file %q", rawPath), nil
		}
		current := oldContent
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
			if replaceAll {
				current = strings.ReplaceAll(current, target, replacement)
			} else {
				current = strings.Replace(current, target, replacement, 1)
			}
		}
		newContent = current
	default:
		return fmt.Sprintf("Modify file: %q", rawPath), nil
	}

	unifiedDiff := diff.FormatUnified(relPath, relPath, oldContent, newContent)
	if unifiedDiff == "" {
		return "No changes made (target and replacement are identical).", nil
	}
	return unifiedDiff, nil
}

// --- Standalone: RemovePermissionHandler ---

type RemovePermissionHandler struct{}

func (h *RemovePermissionHandler) GetPermissionGroup() string {
	return "delete_file"
}

func (h *RemovePermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
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
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Auto mode blocked: removing path %q is outside the workspace or inside .git", rawPath)},
		}
	}

	if mode == permissions.ModeStrict {
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Strict mode: authorization required to remove file/dir %q", rawPath)},
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

func (h *RemovePermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	rawPath, _ := args["path"].(string)
	recursive, _ := args["recursive"].(bool)
	label := "file"
	if recursive {
		label = "file/directory recursively"
	}
	return fmt.Sprintf("Remove %s: %q", label, rawPath), nil
}

// --- Standalone: ViewPermissionHandler ---

type ViewPermissionHandler struct{}

func (h *ViewPermissionHandler) GetPermissionGroup() string {
	return "read_file"
}

func (h *ViewPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
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
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("⚠️ Auto mode blocked: viewing path %q outside the workspace or inside .git", rawPath)},
		}
	}

	if mode == permissions.ModeDefault {
		if isSafeWorkspacePath(ctx, absPath) {
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to view file %q", rawPath)},
	}
}

func (h *ViewPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	rawPath, _ := args["path"].(string)
	return getFileOptions("view", rawPath)
}

func (h *ViewPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	rawPath, _ := args["path"].(string)
	start, _ := args["start_line"].(float64)
	end, _ := args["end_line"].(float64)

	var details string
	if start > 0 || end > 0 {
		details = fmt.Sprintf(" (lines %.0f-%.0f)", start, end)
	}
	return fmt.Sprintf("View file: %q%s", rawPath, details), nil
}

// --- Group 2: FileSearchPermissionHandler (ls, grep, glob) ---

type FileSearchPermissionHandler struct{}

func (h *FileSearchPermissionHandler) GetPermissionGroup() string {
	return "search_file"
}

func (h *FileSearchPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	args := req.Args
	var targetVal string
	if req.ToolName == "glob" {
		targetVal, _ = args["pattern"].(string)
	} else {
		targetVal, _ = args["path"].(string)
		if targetVal == "" {
			targetVal = "."
		}
	}

	if state, found := evaluateFileGrants(grants, targetVal); found {
		return permissions.EvaluationResult{State: state}
	}

	// Glob checks pattern safety, others resolve path safety
	isSafe := true
	if req.ToolName == "glob" {
		isSafe = !strings.Contains(targetVal, "../")
	} else {
		absPath, err := resolveAbsPath(ctx, targetVal)
		if err != nil {
			return permissions.EvaluationResult{State: permissions.StateRequiresAuth, Hints: []string{"Failed to resolve directory path"}}
		}
		isSafe = isSafeWorkspacePath(ctx, absPath)
	}

	if mode == permissions.ModeAuto {
		if isSafe {
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
		return permissions.EvaluationResult{
			State: permissions.StateRequiresAuth,
			Hints: []string{fmt.Sprintf("⚠️ Auto mode blocked: searching path %q outside workspace or inside .git", targetVal)},
		}
	}

	if mode == permissions.ModeDefault {
		if isSafe {
			return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
		}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to execute %s in %q", req.ToolName, targetVal)},
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

func (h *FileSearchPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	switch req.ToolName {
	case "ls":
		rawPath, _ := args["path"].(string)
		if rawPath == "" {
			rawPath = "."
		}
		return fmt.Sprintf("List directory contents: %q", rawPath), nil
	case "grep":
		rawPath, _ := args["path"].(string)
		pattern, _ := args["pattern"].(string)
		if rawPath == "" {
			rawPath = "."
		}
		return fmt.Sprintf("Grep files for pattern %q in path %q", pattern, rawPath), nil
	case "glob":
		pattern, _ := args["pattern"].(string)
		return fmt.Sprintf("Find files matching glob pattern: %q", pattern), nil
	default:
		return fmt.Sprintf("Search files via %s", req.ToolName), nil
	}
}

// --- Group 3: Split Web Handlers (web_fetch, web_search, download) ---

type WebFetchPermissionHandler struct{}

func (h *WebFetchPermissionHandler) GetPermissionGroup() string {
	return "web_fetch"
}

func (h *WebFetchPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	args := req.Args
	urlVal, _ := args["url"].(string)

	if state, found := evaluateGenericGrants(grants, urlVal); found {
		return permissions.EvaluationResult{State: state}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
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

func (h *WebFetchPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	urlVal, _ := args["url"].(string)
	return fmt.Sprintf("Fetch web page content: %q", urlVal), nil
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
		return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
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

func (h *DownloadPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	urlVal, _ := args["url"].(string)
	destVal, _ := args["destination"].(string)
	var destText string
	if destVal != "" {
		destText = fmt.Sprintf(" to %q", destVal)
	}
	return fmt.Sprintf("Download file from URL: %q%s", urlVal, destText), nil
}

type WebSearchPermissionHandler struct{}

func (h *WebSearchPermissionHandler) GetPermissionGroup() string {
	return "web_search"
}

func (h *WebSearchPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	args := req.Args
	query, _ := args["query"].(string)

	if state, found := evaluateGenericGrants(grants, query); found {
		return permissions.EvaluationResult{State: state}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
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

func (h *WebSearchPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	query, _ := args["query"].(string)
	return fmt.Sprintf("Web Search query: %q", query), nil
}

// --- Standalone: BashPermissionHandler ---

type BashPermissionHandler struct{}

func (h *BashPermissionHandler) GetPermissionGroup() string {
	return "command"
}

func (h *BashPermissionHandler) Evaluate(ctx context.Context, req permissions.ToolCallRequest, mode permissions.PermissionMode, grants []permissions.Permission) permissions.EvaluationResult {
	args := req.Args
	cmd, _ := args["command"].(string)
	if state, found := evaluateGenericGrants(grants, cmd); found {
		return permissions.EvaluationResult{State: state}
	}

	if mode == permissions.ModeAuto {
		return permissions.EvaluationResult{State: permissions.StateExplicitAllow}
	}

	return permissions.EvaluationResult{
		State: permissions.StateRequiresAuth,
		Hints: []string{fmt.Sprintf("Authorization required to run bash command: %q", cmd)},
	}
}

func (h *BashPermissionHandler) GetOptions(req permissions.ToolCallRequest) []permissions.PermissionOption {
	args := req.Args
	cmd, _ := args["command"].(string)
	var options []permissions.PermissionOption

	options = append(options, permissions.PermissionOption{
		Label:       fmt.Sprintf("Allow exact command: %q", cmd),
		Target:      cmd,
		MatchMethod: "exact",
		Action:      permissions.ActionAllow,
	})

	words := strings.Fields(cmd)
	if len(words) > 0 {
		prefix := words[0]
		options = append(options, permissions.PermissionOption{
			Label:       fmt.Sprintf("Allow all commands starting with %q", prefix),
			Target:      prefix,
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

func (h *BashPermissionHandler) GetPreview(ctx context.Context, req permissions.ToolCallRequest) (string, error) {
	args := req.Args
	cmd, _ := args["command"].(string)
	desc, _ := args["description"].(string)

	var descText string
	if desc != "" {
		descText = fmt.Sprintf("\nIntended action: %q", desc)
	}
	return fmt.Sprintf("Execute shell command:\n%s%s", cmd, descText), nil
}
