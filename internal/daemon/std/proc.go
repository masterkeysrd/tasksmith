package std

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

// Bash executes a shell command via "sh -c" inside the sandbox's process
// capability and immediately calls [Std.SyncExternalChanges] so any tracked
// files modified or deleted by the command are recorded with [SourceExternal].
func (s *Std) Bash(ctx context.Context, req BashRequest) (*BashResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		return nil, fmt.Errorf("%w: command is required", ErrInvalidArgument)
	}

	cmd := sandbox.Command{
		Name:    "sh",
		Args:    []string{"-c", req.Command},
		Dir:     req.Dir,
		Env:     req.Env,
		Timeout: req.Timeout,
	}
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}
	if req.Output != nil {
		cmd.Stdout = req.Output
		cmd.Stderr = req.Output
	}

	execRes, execErr := s.sandbox.Proc().Exec(ctx, cmd)

	syncCtx := ctx
	if syncCtx.Err() != nil {
		syncCtx = context.WithoutCancel(syncCtx)
	}
	extChanges, _ := s.SyncExternalChanges(syncCtx)

	if execErr != nil {
		return nil, execErr
	}

	return &BashResult{
		Stdout:          string(execRes.Stdout),
		Stderr:          string(execRes.Stderr),
		ExitCode:        execRes.ExitCode,
		Duration:        execRes.Duration,
		ExternalChanges: extChanges,
	}, nil
}
