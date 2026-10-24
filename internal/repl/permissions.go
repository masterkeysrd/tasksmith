package repl

import (
	"context"
	"maps"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// PermissionQueue holds the tool permission requests waiting for a decision.
type PermissionQueue struct {
	Count  func() int
	Active func() (api.PermissionRequest, bool)
	Decide func(request api.PermissionRequest, decision api.PermissionDecision)
}

var permissionsContext = kitex.CreateContext[*PermissionQueue](nil)

// UsePermissionQueue returns the pending permission requests of the active
// session.
func UsePermissionQueue() PermissionQueue {
	queue := kitex.UseContext(permissionsContext)
	if queue == nil {
		panic("repl: UsePermissionQueue must be used within a PermissionsProvider")
	}
	return *queue
}

// PermissionsProviderProps configures [PermissionsProvider].
type PermissionsProviderProps struct {
	Children []trail.Node // injected by trail.FCC
}

type permissionDecision struct {
	SessionID string
	Request   api.PermissionRequest
	Decision  api.PermissionDecision
}

// PermissionsProvider queues the active session's permission requests and
// sends the user's decisions. A decided request leaves the queue immediately
// and returns to it only if sending the decision fails.
var PermissionsProvider = trail.FCC("PermissionsProvider", func(props PermissionsProviderProps) trail.Node {
	c := uiclient.UseClient()
	session := uihooks.UseSession()
	conversation := uihooks.UseCurrentConversation()
	transcript := UseTranscript()

	type decisionKey struct{ SessionID, RequestID string }
	decided, setDecided := kitex.UseState(map[decisionKey]bool(nil))

	mutation := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in permissionDecision) (struct{}, error) {
		dec := in.Decision
		return struct{}{}, c.Send(ctx, in.SessionID, api.SendRequest{Decision: &dec})
	}), wind.MutationOptions[permissionDecision, struct{}]{
		OnSuccess: func(_ struct{}, in permissionDecision, _ wind.MutationContext) {
			transcript.Append(components.DecisionEntry(ui.DecisionSummaryProps{
				Request:  in.Request,
				Decision: in.Decision,
			}))
		},
		OnError: func(err error, in permissionDecision, _ wind.MutationContext) {
			next := maps.Clone(decided())
			delete(next, decisionKey{SessionID: in.SessionID, RequestID: in.Request.ID})
			setDecided(next)
			appendError(transcript, err)
		},
	})

	pending := func() []api.PermissionRequest {
		if session.Draft {
			return nil
		}
		var requests []api.PermissionRequest
		for _, request := range conversation.Permissions {
			if !decided()[decisionKey{SessionID: session.ID, RequestID: request.ID}] {
				requests = append(requests, request)
			}
		}
		return requests
	}

	queue := &PermissionQueue{
		Count: func() int { return len(pending()) },
		Active: func() (api.PermissionRequest, bool) {
			requests := pending()
			if len(requests) == 0 {
				return api.PermissionRequest{}, false
			}
			return requests[0], true
		},
		Decide: func(request api.PermissionRequest, decision api.PermissionDecision) {
			if session.Draft {
				return
			}
			key := decisionKey{SessionID: session.ID, RequestID: request.ID}
			if decided()[key] {
				return
			}
			next := maps.Clone(decided())
			if next == nil {
				next = make(map[decisionKey]bool)
			}
			next[key] = true
			setDecided(next)
			decision.RequestID, decision.ToolCallID = request.ID, request.ToolCallID
			mutation.Mutate(permissionDecision{SessionID: session.ID, Request: request, Decision: decision})
		},
	}
	return permissionsContext.Provider(queue, props.Children...)
})
