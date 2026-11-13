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
		Name:     "sh",
		Args:     []string{"-c", req.Command},
		Dir:      req.Dir,
		Env:      req.Env,
		Timeout:  req.Timeout,
		ReadOnly: req.ReadOnly,
	}
	if req.StdinReader != nil {
		cmd.Stdin = req.StdinReader
	} else if req.Stdin != "" {
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

// Exec runs a program directly inside the sandbox's process capability without
// shell interpretation, so argument values reach the process verbatim. It is
// the execution path for command-backed custom tools, whose argv comes from a
// vetted manifest rather than a shell string. Like [Std.Bash], it immediately
// calls [Std.SyncExternalChanges] so any tracked files modified or deleted by
// the program are recorded with [SourceExternal].
func (s *Std) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}

	cmd := sandbox.Command{
		Name:     req.Name,
		Args:     req.Args,
		Dir:      req.Dir,
		Env:      req.Env,
		Stdin:    req.Stdin,
		Timeout:  req.Timeout,
		ReadOnly: req.ReadOnly,
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

	return &ExecResult{
		Stdout:          string(execRes.Stdout),
		Stderr:          string(execRes.Stderr),
		ExitCode:        execRes.ExitCode,
		Duration:        execRes.Duration,
		ExternalChanges: extChanges,
	}, nil
}
