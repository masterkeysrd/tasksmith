package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

type MessageProps struct {
	Role                  message.Role
	Content               message.Content
	ToolResponses         map[string]*message.Tool
	ReasoningTokens       int
	ThinkingDuration      time.Duration
	PendingAuthorizations []permissions.AuthorizationRequest
	SessionID             string
	OnViewFullOutput      func(title, cachedPath string)
	OnViewPreview         func(title string, p preview.ToolPreview)
}

var Message = kitex.FC("Message", func(props MessageProps) kitex.Node {
	role := props.Role
	content := props.Content
	toolResponses := props.ToolResponses

	if role == message.RoleAssistant {
		client := tuiapi.UseClient()
		windClient := wind.UseClient()

		// Local state to accumulate decisions for pending authorizations
		localDecisions, setLocalDecisions := kitex.UseState(make(map[string]permissions.AuthorizationDecision))

		// Reset local decisions when pending authorizations list changes (e.g. from the backend)
		// Or when the session changes.
		kitex.UseEffect(func() {
			setLocalDecisions(make(map[string]permissions.AuthorizationDecision))
		}, []any{props.SessionID, len(props.PendingAuthorizations)})

		// Helper to submit the batch of decisions
		submitBatch := func(decisions []permissions.AuthorizationDecision) {
			promise.New(func(ctx context.Context) (bool, error) {
				_, err := client.SubmitAuthorizationDecision(ctx, api.SubmitAuthorizationDecisionRequest{
					SessionID: props.SessionID,
					Decisions: decisions,
				})
				return err == nil, err
			}).Then(func(success bool) {
				windClient.InvalidateQueries(api.GetSessionMessagesRequest{SessionID: props.SessionID})
				windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: props.SessionID})
				windClient.InvalidateQueries(api.GetFileChangesRequest{SessionID: props.SessionID})
			}, func(err error) {
				log.Error(fmt.Sprintf("Failed to submit batch authorization decisions: %v", err))
			})
		}

		onDecision := func(dec permissions.AuthorizationDecision) {
			current := localDecisions()
			newDecisions := make(map[string]permissions.AuthorizationDecision)
			for k, v := range current {
				newDecisions[k] = v
			}
			newDecisions[dec.ToolCallID] = dec
			setLocalDecisions(newDecisions)

			// Check if all pending requests have been decided
			allDecided := true
			var decisionList []permissions.AuthorizationDecision
			for _, req := range props.PendingAuthorizations {
				if d, ok := newDecisions[req.ToolCallID]; ok {
					decisionList = append(decisionList, d)
				} else {
					allDecided = false
				}
			}

			if allDecided && len(props.PendingAuthorizations) > 0 {
				submitBatch(decisionList)
			}
		}

		// Determine the active undecided pending authorization (if any)
		var activeToolCallID string
		for _, req := range props.PendingAuthorizations {
			if _, decided := localDecisions()[req.ToolCallID]; !decided {
				activeToolCallID = req.ToolCallID
				break
			}
		}

		var children []kitex.Node
		for _, block := range content {
			var node kitex.Node
			switch b := block.(type) {
			case *message.TextBlock:
				if strings.TrimSpace(b.Text) != "" {
					node = components.Markdown(components.MarkdownProps{Source: b.Text})
				}
			case *message.ThinkingBlock:
				if strings.TrimSpace(b.Thinking) != "" {
					node = CollapsibleThinking(CollapsibleThinkingProps{
						Content:  b.Thinking,
						Duration: props.ThinkingDuration,
						Tokens:   props.ReasoningTokens,
					})
				}
			case *message.ToolCall:
				var pendingReq *permissions.AuthorizationRequest
				for _, req := range props.PendingAuthorizations {
					if req.ToolCallID == b.ID {
						pendingReq = &req
						break
					}
				}

				if pendingReq != nil {
					var localDec *permissions.AuthorizationDecision
					if dec, decided := localDecisions()[b.ID]; decided {
						localDec = &dec
					}

					isActive := pendingReq.ToolCallID == activeToolCallID && localDec == nil

					node = AuthorizationWidget(AuthorizationWidgetProps{
						Request:       *pendingReq,
						SessionID:     props.SessionID,
						IsActive:      isActive,
						OnDecision:    onDecision,
						LocalDecision: localDec,
					})
				} else {
					var toolMsg *message.Tool
					if toolResponses != nil {
						toolMsg = toolResponses[b.ID]
					}
					node = ToolExecution(ToolExecutionProps{
						ToolCall:         b,
						ToolMessage:      toolMsg,
						OnViewFullOutput: props.OnViewFullOutput,
						OnViewPreview:    props.OnViewPreview,
					})
				}
			}
			if node != nil {
				children = append(children, node)
			}
		}
		if len(children) == 0 {
			return nil
		}
		return kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)).
				MinWidth(style.Percent(0)).
				Gap(1),
		}, children...)
	}

	// Combine all text blocks for other roles
	var texts []string
	for _, block := range content {
		if tb, ok := block.(*message.TextBlock); ok {
			cleaned := tryExtractTextFromJSON(tb.Text)
			texts = append(texts, cleaned)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	return components.Markdown(components.MarkdownProps{Source: strings.Join(texts, "\n")})
})
