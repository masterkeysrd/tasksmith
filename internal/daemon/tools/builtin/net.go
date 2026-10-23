package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

type webFetchTool struct {
	std *std.Std
}

func (t *webFetchTool) Prepare(ctx context.Context, in api.WebFetchInput) (*tools.Plan, error) {
	res, err := t.std.Fetch(ctx, std.FetchRequest{
		URL:    in.URL,
		Method: in.Method,
		DryRun: true,
	})
	if err != nil {
		return nil, err
	}
	return &tools.Plan{
		ToolName: ToolWebFetch,
		Action:   tools.ActionNet,
		Safety:   tools.SafetyUnknown,
		Target:   res.URL,
		Summary:  fmt.Sprintf("Fetch URL: %s", res.URL),
		ReadOnly: true,
	}, nil
}

func (t *webFetchTool) Execute(ctx context.Context, in api.WebFetchInput) (api.WebFetchOutput, error) {
	var timeout time.Duration
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	var body []byte
	if in.Body != "" {
		body = []byte(in.Body)
	}
	res, err := t.std.Fetch(ctx, std.FetchRequest{
		URL:      in.URL,
		Method:   in.Method,
		Headers:  in.Headers,
		Body:     body,
		Timeout:  timeout,
		MaxBytes: in.MaxBytes,
	})
	if err != nil {
		return api.WebFetchOutput{}, err
	}
	return api.WebFetchOutput{
		URL:         res.URL,
		StatusCode:  res.StatusCode,
		Status:      res.Status,
		Headers:     res.Headers,
		ContentType: res.ContentType,
		Text:        res.Text,
		Truncated:   res.Truncated,
	}, nil
}

type downloadTool struct {
	std   *std.Std
	guard *shellguard.Guard
}

func (t *downloadTool) Prepare(ctx context.Context, in api.DownloadInput) (*tools.Plan, error) {
	res, err := t.std.Download(ctx, std.DownloadRequest{
		URL:      in.URL,
		Path:     in.Path,
		Headers:  in.Headers,
		Force:    in.Force,
		MaxBytes: in.MaxBytes,
		DryRun:   true,
	})
	if err != nil {
		return nil, err
	}
	safety := tools.SafetyUnknown
	if t.guard.IsProtectedPath(res.Path) {
		safety = tools.SafetyUnsafe
	}
	return &tools.Plan{
		ToolName: ToolDownload,
		Action:   tools.ActionWrite,
		Safety:   safety,
		Target:   res.Path,
		Summary:  fmt.Sprintf("Download file: %s", res.Path),
		ReadOnly: false,
	}, nil
}

func (t *downloadTool) Execute(ctx context.Context, in api.DownloadInput) (api.DownloadOutput, error) {
	var timeout time.Duration
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	res, err := t.std.Download(ctx, std.DownloadRequest{
		URL:      in.URL,
		Path:     in.Path,
		Headers:  in.Headers,
		Force:    in.Force,
		Timeout:  timeout,
		MaxBytes: in.MaxBytes,
		DryRun:   false,
	})
	if err != nil {
		return api.DownloadOutput{}, err
	}
	return api.DownloadOutput{
		URL:          res.URL,
		Path:         res.Path,
		StatusCode:   res.StatusCode,
		ContentType:  res.ContentType,
		BytesWritten: res.BytesWritten,
		Created:      res.Created,
		BeforeHash:   res.BeforeHash,
		AfterHash:    res.AfterHash,
		IsBinary:     res.IsBinary,
	}, nil
}
