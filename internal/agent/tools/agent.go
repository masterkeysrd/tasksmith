package tools

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
)

// InboxProvider defines the interface to retrieve pending user messages.
type InboxProvider interface {
	PopMessages() []message.Message
}

// SubagentOptions defines the parameters needed to initialize and run a subagent graph.
type SubagentOptions struct {
	AgentRef       string
	Task           string
	SessionID      string
	ParentHandlers *ToolHandlers
	Stdout         io.Writer
	Stderr         io.Writer
	Inbox          InboxProvider
	Mode           string // "transient" or "interactive"
}

// SubagentGraphRunner defines the interface for running a subagent Loom graph,
// avoiding circular dependencies between the tools and graph packages.
type SubagentGraphRunner interface {
	Run(ctx context.Context, opts SubagentOptions) (string, error)
}

// SubagentRunner is the registered implementation of SubagentGraphRunner.
var SubagentRunner SubagentGraphRunner

// AgentRunner implements TaskRunner for subagent executions.
type AgentRunner struct {
	AgentRef string
	Task     string
	Mode     string // "transient" or "interactive"
	TaskID   string
	Handlers *ToolHandlers

	Inbox            []message.Message
	InboxMu          sync.Mutex
	authDecisionChan chan permissions.AuthorizationDecision
	pendingAuths     []permissions.AuthorizationRequest
	cancel           context.CancelFunc
	result           string
}

// PendingAuthorizations returns the subagent's active pending authorizations.
func (ar *AgentRunner) PendingAuthorizations() []permissions.AuthorizationRequest {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()
	return ar.pendingAuths
}

// SetPendingAuthorizations sets the subagent's active pending authorizations.
func (ar *AgentRunner) SetPendingAuthorizations(reqs []permissions.AuthorizationRequest) {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()
	ar.pendingAuths = reqs
}

// WaitForAuthDecision blocks until a permission decision is received or context is canceled.
func (ar *AgentRunner) WaitForAuthDecision(ctx context.Context) (permissions.AuthorizationDecision, error) {
	select {
	case <-ctx.Done():
		return permissions.AuthorizationDecision{}, ctx.Err()
	case d := <-ar.authDecisionChan:
		return d, nil
	}
}

// SubmitAuthorizationDecision submits a user permission decision to the subagent.
func (ar *AgentRunner) SubmitAuthorizationDecision(decision permissions.AuthorizationDecision) {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()
	if ar.authDecisionChan != nil {
		select {
		case ar.authDecisionChan <- decision:
		default:
		}
	}
}

// Result returns the subagent's execution result.
func (ar *AgentRunner) Result() string {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()
	return ar.result
}

// SetResult sets the subagent's execution result.
func (ar *AgentRunner) SetResult(res string) {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()
	ar.result = res
}

// State returns the subagent's current execution state/result details.
func (ar *AgentRunner) State() string {
	return ar.Result()
}

// Start runs the subagent Loom graph.
func (ar *AgentRunner) Start(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
	if SubagentRunner == nil {
		return fmt.Errorf("SubagentRunner is not registered")
	}

	runCtx, cancel := context.WithCancel(ctx)
	ar.cancel = cancel
	defer cancel()

	// Initialize the inbox with the starting task prompt if empty.
	ar.InboxMu.Lock()
	ar.authDecisionChan = make(chan permissions.AuthorizationDecision, 1)
	if len(ar.Inbox) == 0 {
		msg := message.NewUserText(ar.Task)
		u, err := uuid.NewV7()
		if err == nil {
			msg.SetID(fmt.Sprintf("msg_%s", u.String()))
		}
		ar.Inbox = append(ar.Inbox, msg)
	}
	ar.InboxMu.Unlock()

	_, err := SubagentRunner.Run(runCtx, SubagentOptions{
		AgentRef:       ar.AgentRef,
		Task:           ar.Task,
		SessionID:      ar.TaskID,
		ParentHandlers: ar.Handlers,
		Stdout:         stdout,
		Stderr:         stderr,
		Inbox:          ar,
		Mode:           ar.Mode,
	})
	return err
}

// WriteStdin pushes follow-up instructions into the subagent's inbox.
func (ar *AgentRunner) WriteStdin(data string) error {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()

	msg := message.NewUserText(data)
	u, err := uuid.NewV7()
	if err == nil {
		msg.SetID(fmt.Sprintf("msg_%s", u.String()))
	}
	ar.Inbox = append(ar.Inbox, msg)
	return nil
}

// Stop halts the subagent Loom graph execution.
func (ar *AgentRunner) Stop() error {
	if ar.cancel != nil {
		ar.cancel()
	}
	return nil
}

// PopMessages retrieves and clears all pending messages from the subagent inbox.
func (ar *AgentRunner) PopMessages() []message.Message {
	ar.InboxMu.Lock()
	defer ar.InboxMu.Unlock()

	if len(ar.Inbox) == 0 {
		return nil
	}
	msgs := ar.Inbox
	ar.Inbox = nil
	return msgs
}
