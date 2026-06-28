package chat

import (
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

type MessageProps struct {
	Role                  message.Role
	Content               message.Content
	ToolResponses         map[string]*message.Tool
	CurrentDots           string
	OneDotCurrentDots     string
	ReasoningTokens       int
	ThinkingDuration      time.Duration
	PendingAuthorizations []permissions.AuthorizationRequest
	SelectedIndex         int
	SelectedScopeIndex    int
	OnPreview             func()
	CurrentPendingIndex   int
	IsInsert              bool
	LocalDecisions        map[string]permissions.AuthorizationDecision
	IsSubmitting          bool
	OnSelectVertical      func(int)
	OnSelectHorizontal    func(int)
	OnApprove             func()
	OnDeny                func()
	OnViewFullOutput      func(title, cachedPath string)
}

var Message = kitex.FC("Message", func(props MessageProps) kitex.Node {
	role := props.Role
	content := props.Content
	toolResponses := props.ToolResponses
	currentDots := props.CurrentDots

	if role == message.RoleAssistant {
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
					isActive := len(props.PendingAuthorizations) > 0 &&
						props.CurrentPendingIndex < len(props.PendingAuthorizations) &&
						pendingReq.ToolCallID == props.PendingAuthorizations[props.CurrentPendingIndex].ToolCallID

					decision, isDecided := props.LocalDecisions[pendingReq.ToolCallID]

					node = AuthorizationWidget(AuthorizationWidgetProps{
						Request:            *pendingReq,
						SelectedIndex:      props.SelectedIndex,
						SelectedScopeIndex: props.SelectedScopeIndex,
						OnPreview:          props.OnPreview,
						IsActive:           isActive,
						IsFocused:          isActive && !props.IsInsert,
						IsDecided:          isDecided,
						Decision:           decision,
						IsSubmitting:       props.IsSubmitting,
						OnSelectVertical:   props.OnSelectVertical,
						OnSelectHorizontal: props.OnSelectHorizontal,
						OnApprove:          props.OnApprove,
						OnDeny:             props.OnDeny,
					})
				} else {
					var toolMsg *message.Tool
					if toolResponses != nil {
						toolMsg = toolResponses[b.ID]
					}
					dots := currentDots
					if b.Name == "bash" {
						dots = props.OneDotCurrentDots
					}
					node = ToolExecution(ToolExecutionProps{
						ToolCall:         b,
						ToolMessage:      toolMsg,
						CurrentDots:      dots,
						OnViewFullOutput: props.OnViewFullOutput,
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
