package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func resolveReadTarget(sb sandbox.Sandbox, g *shellguard.Guard, rawPath string) (string, tools.SafetyLevel, error) {
	resolved, safety := g.EvaluatePath(rawPath, tools.ActionRead)
	if rel, err := sb.FS().ResolvePath(resolved); err == nil {
		return rel, safety, nil
	}
	return filepath.ToSlash(resolved), safety, nil
}

type viewTool struct {
	std     *std.Std
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
}

func (t *viewTool) Prepare(_ context.Context, in api.ViewInput) (*tools.Plan, error) {
	if strings.TrimSpace(in.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", std.ErrInvalidArgument)
	}
	target, safety, err := resolveReadTarget(t.sandbox, t.guard, in.Path)
	if err != nil {
		return nil, err
	}
	return &tools.Plan{
		ToolName: ToolView,
		Action:   tools.ActionRead,
		Safety:   safety,
		Target:   target,
		Summary:  fmt.Sprintf("Read file: %s", target),
		ReadOnly: true,
	}, nil
}

func (t *viewTool) Execute(ctx context.Context, in api.ViewInput) (api.ViewOutput, error) {
	res, err := t.std.View(ctx, std.ViewRequest{
		Path:   in.Path,
		Offset: in.Offset,
		Limit:  in.Limit,
	})
	if err != nil {
		return api.ViewOutput{}, err
	}
	return api.ViewOutput{
		Path:       res.Path,
		Content:    res.Numbered,
		StartLine:  res.StartLine,
		EndLine:    res.EndLine,
		TotalLines: res.TotalLines,
		Truncated:  res.Truncated,
		Hash:       res.Hash,
	}, nil
}

type lsTool struct {
	std     *std.Std
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
}

func (t *lsTool) Prepare(_ context.Context, in api.LsInput) (*tools.Plan, error) {
	rawPath := in.Path
	if strings.TrimSpace(rawPath) == "" {
		rawPath = "."
	}
	target, safety, err := resolveReadTarget(t.sandbox, t.guard, rawPath)
	if err != nil {
		return nil, err
	}
	return &tools.Plan{
		ToolName: ToolLS,
		Action:   tools.ActionRead,
		Safety:   safety,
		Target:   target,
		Summary:  fmt.Sprintf("List directory: %s", target),
		ReadOnly: true,
	}, nil
}

func (t *lsTool) Execute(ctx context.Context, in api.LsInput) (api.LsOutput, error) {
	res, err := t.std.List(ctx, std.ListRequest{
		Path:          in.Path,
		Recursive:     in.Recursive,
		MaxDepth:      in.MaxDepth,
		IncludeHidden: in.IncludeHidden,
		Ignore:        in.Ignore,
		Limit:         in.Limit,
	})
	if err != nil {
		return api.LsOutput{}, err
	}
	entries := make([]api.LsEntry, 0, len(res.Entries))
	for _, e := range res.Entries {
		entries = append(entries, api.LsEntry{
			Path:  e.Path,
			Name:  e.Name,
			IsDir: e.IsDir,
			Size:  e.Size,
		})
	}
	return api.LsOutput{
		Path:      res.Path,
		Entries:   entries,
		Truncated: res.Truncated,
	}, nil
}

type globTool struct {
	std     *std.Std
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
}

func (t *globTool) Prepare(_ context.Context, in api.GlobInput) (*tools.Plan, error) {
	if strings.TrimSpace(in.Pattern) == "" {
		return nil, fmt.Errorf("%w: pattern is required", std.ErrInvalidArgument)
	}
	rawPath := in.Path
	if strings.TrimSpace(rawPath) == "" {
		rawPath = "."
	}
	target, safety, err := resolveReadTarget(t.sandbox, t.guard, rawPath)
	if err != nil {
		return nil, err
	}
	return &tools.Plan{
		ToolName: ToolGlob,
		Action:   tools.ActionRead,
		Safety:   safety,
		Target:   target,
		Summary:  fmt.Sprintf("Glob pattern %q in %s", in.Pattern, target),
		ReadOnly: true,
	}, nil
}

func (t *globTool) Execute(ctx context.Context, in api.GlobInput) (api.GlobOutput, error) {
	res, err := t.std.Glob(ctx, std.GlobRequest{
		Pattern:       in.Pattern,
		Path:          in.Path,
		IncludeHidden: in.IncludeHidden,
		Ignore:        in.Ignore,
		Limit:         in.Limit,
	})
	if err != nil {
		return api.GlobOutput{}, err
	}
	return api.GlobOutput{
		Pattern:   res.Pattern,
		Path:      res.Path,
		Matches:   res.Matches,
		Truncated: res.Truncated,
	}, nil
}

type grepTool struct {
	std     *std.Std
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
}

func (t *grepTool) Prepare(_ context.Context, in api.GrepInput) (*tools.Plan, error) {
	if strings.TrimSpace(in.Pattern) == "" {
		return nil, fmt.Errorf("%w: pattern is required", std.ErrInvalidArgument)
	}
	rawPath := in.Path
	if strings.TrimSpace(rawPath) == "" {
		rawPath = "."
	}
	target, safety, err := resolveReadTarget(t.sandbox, t.guard, rawPath)
	if err != nil {
		return nil, err
	}
	return &tools.Plan{
		ToolName: ToolGrep,
		Action:   tools.ActionRead,
		Safety:   safety,
		Target:   target,
		Summary:  fmt.Sprintf("Search pattern %q in %s", in.Pattern, target),
		ReadOnly: true,
	}, nil
}

func (t *grepTool) Execute(ctx context.Context, in api.GrepInput) (api.GrepOutput, error) {
	res, err := t.std.Grep(ctx, std.GrepRequest{
		Pattern:         in.Pattern,
		Path:            in.Path,
		Include:         in.Include,
		CaseInsensitive: in.CaseInsensitive,
		ContextLines:    in.ContextLines,
		IncludeHidden:   in.IncludeHidden,
		Limit:           in.Limit,
	})
	if err != nil {
		return api.GrepOutput{}, err
	}
	matches := make([]api.GrepMatch, 0, len(res.Matches))
	for _, m := range res.Matches {
		matches = append(matches, api.GrepMatch{
			Path:   m.Path,
			Line:   m.Line,
			Column: m.Column,
			Text:   m.Text,
			Before: m.Before,
			After:  m.After,
		})
	}
	return api.GrepOutput{
		Pattern:   res.Pattern,
		Matches:   matches,
		Truncated: res.Truncated,
	}, nil
}

type writeTool struct {
	std   *std.Std
	guard *shellguard.Guard
}

func (t *writeTool) Prepare(ctx context.Context, in api.WriteInput) (*tools.Plan, error) {
	res, err := t.std.Write(ctx, std.WriteRequest{
		Path:    in.Path,
		Content: in.Content,
		Force:   in.Force,
		DryRun:  true,
	})
	if err != nil {
		return nil, err
	}
	_, safety := t.guard.EvaluatePath(res.Path, tools.ActionWrite)
	return &tools.Plan{
		ToolName: ToolWrite,
		Action:   tools.ActionWrite,
		Safety:   safety,
		Target:   res.Path,
		Summary:  fmt.Sprintf("Write file: %s", res.Path),
		Diff:     res.Diff,
		ReadOnly: false,
	}, nil
}

const (
	maxToolDiffBytes = 8 * 1024
	maxToolDiffLines = 200
)

func capToolDiff(diffStr string) string {
	if diffStr == "" {
		return ""
	}
	lines := strutils.CountLines(diffStr)
	if len(diffStr) <= maxToolDiffBytes && lines <= maxToolDiffLines {
		return diffStr
	}
	return fmt.Sprintf("[diff omitted: %d bytes (%d lines) exceeds limit]", len(diffStr), lines)
}

func (t *writeTool) Execute(ctx context.Context, in api.WriteInput) (api.WriteOutput, error) {
	res, err := t.std.Write(ctx, std.WriteRequest{
		Path:    in.Path,
		Content: in.Content,
		Force:   in.Force,
		DryRun:  false,
	})
	if err != nil {
		return api.WriteOutput{}, err
	}
	return api.WriteOutput{
		Path:         res.Path,
		Created:      res.Created,
		BytesWritten: res.BytesWritten,
		Additions:    res.Additions,
		Deletions:    res.Deletions,
		Diff:         capToolDiff(res.Diff),
		BeforeHash:   res.BeforeHash,
		AfterHash:    res.AfterHash,
	}, nil
}

type editTool struct {
	std   *std.Std
	guard *shellguard.Guard
}

func (t *editTool) Prepare(ctx context.Context, in api.EditInput) (*tools.Plan, error) {
	res, err := t.std.Edit(ctx, std.EditRequest{
		Path:       in.Path,
		OldText:    in.OldText,
		NewText:    in.NewText,
		ReplaceAll: in.ReplaceAll,
		Force:      in.Force,
		DryRun:     true,
	})
	if err != nil {
		return nil, err
	}
	_, safety := t.guard.EvaluatePath(res.Path, tools.ActionWrite)
	return &tools.Plan{
		ToolName: ToolEdit,
		Action:   tools.ActionWrite,
		Safety:   safety,
		Target:   res.Path,
		Summary:  fmt.Sprintf("Edit file: %s", res.Path),
		Diff:     res.Diff,
		ReadOnly: false,
	}, nil
}

func (t *editTool) Execute(ctx context.Context, in api.EditInput) (api.EditOutput, error) {
	res, err := t.std.Edit(ctx, std.EditRequest{
		Path:       in.Path,
		OldText:    in.OldText,
		NewText:    in.NewText,
		ReplaceAll: in.ReplaceAll,
		Force:      in.Force,
		DryRun:     false,
	})
	if err != nil {
		return api.EditOutput{}, err
	}
	return api.EditOutput{
		Path:         res.Path,
		Replacements: res.Replacements,
		Additions:    res.Additions,
		Deletions:    res.Deletions,
		Diff:         capToolDiff(res.Diff),
		BeforeHash:   res.BeforeHash,
		AfterHash:    res.AfterHash,
	}, nil
}

type removeTool struct {
	std   *std.Std
	guard *shellguard.Guard
}

func (t *removeTool) Prepare(ctx context.Context, in api.RemoveInput) (*tools.Plan, error) {
	res, err := t.std.Remove(ctx, std.RemoveRequest{
		Path:   in.Path,
		Force:  in.Force,
		DryRun: true,
	})
	if err != nil {
		return nil, err
	}
	safety := tools.SafetyUnknown
	if _, evalSafety := t.guard.EvaluatePath(res.Path, tools.ActionDelete); evalSafety == tools.SafetyUnsafe {
		safety = tools.SafetyUnsafe
	} else if t.guard.IsInWriteDirs(res.Path) {
		safety = tools.SafetySafe
	}
	return &tools.Plan{
		ToolName: ToolRemove,
		Action:   tools.ActionDelete,
		Safety:   safety,
		Target:   res.Path,
		Summary:  fmt.Sprintf("Remove file: %s", res.Path),
		Diff:     res.Diff,
		ReadOnly: false,
	}, nil
}

func (t *removeTool) Execute(ctx context.Context, in api.RemoveInput) (api.RemoveOutput, error) {
	res, err := t.std.Remove(ctx, std.RemoveRequest{
		Path:   in.Path,
		Force:  in.Force,
		DryRun: false,
	})
	if err != nil {
		return api.RemoveOutput{}, err
	}
	return api.RemoveOutput{
		Path:       res.Path,
		Deletions:  res.Deletions,
		Diff:       capToolDiff(res.Diff),
		BeforeHash: res.BeforeHash,
	}, nil
}
