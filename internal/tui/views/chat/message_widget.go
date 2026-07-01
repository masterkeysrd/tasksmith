package chat

import (
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

type MessageProps struct {
	Role                  message.Role
	Content               message.Content
	ToolResponses         map[string]*message.Tool
	ReasoningTokens       int
	ThinkingDuration      time.Duration
	PendingAuthorizations []permissions.AuthorizationRequest
	CurrentPageIndex      int
	FocusedItem           FocusItem
	SelectedScopeIndex    int
	SelectedOptions       map[string]int
	SelectedDirs          map[string]int
	OnPreview             func()
	CurrentPendingIndex   int
	IsInsert              bool
	LocalDecisions        map[string]permissions.AuthorizationDecision
	IsSubmitting          bool
	OnSelectVertical      func(FocusItem)
	OnSelectScope         func(int)
	OnSelectOption        func(int)
	OnSelectDir           func(int)
	OnApprove             func()
	OnDeny                func()
	OnHardCancel          func()
	OnViewFullOutput      func(title, cachedPath string)
	OnViewPreview         func(title string, p preview.ToolPreview)
	IsProvidingFeedback   bool
	FeedbackText          string
	OnFeedbackChange      func(string)
	OnDenyWithFeedback    func(string)
	OnCancelFeedback      func()
	OnStartFeedback       func()
}

var Message = kitex.FC("Message", func(props MessageProps) kitex.Node {
	role := props.Role
	content := props.Content
	toolResponses := props.ToolResponses

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
						Request:             *pendingReq,
						CurrentPageIndex:    props.CurrentPageIndex,
						FocusedItem:         props.FocusedItem,
						SelectedScopeIndex:  props.SelectedScopeIndex,
						SelectedOptions:     props.SelectedOptions,
						SelectedDirs:        props.SelectedDirs,
						OnPreview:           props.OnPreview,
						IsActive:            isActive,
						IsFocused:           isActive && (!props.IsInsert || props.IsProvidingFeedback),
						IsDecided:           isDecided,
						Decision:            decision,
						IsSubmitting:        props.IsSubmitting,
						OnSelectVertical:    props.OnSelectVertical,
						OnSelectScope:       props.OnSelectScope,
						OnSelectOption:      props.OnSelectOption,
						OnSelectDir:         props.OnSelectDir,
						OnApprove:           props.OnApprove,
						OnDeny:              props.OnDeny,
						OnHardCancel:        props.OnHardCancel,
						IsProvidingFeedback: props.IsProvidingFeedback,
						FeedbackText:        props.FeedbackText,
						OnFeedbackChange:    props.OnFeedbackChange,
						OnDenyWithFeedback:  props.OnDenyWithFeedback,
						OnCancelFeedback:    props.OnCancelFeedback,
						OnStartFeedback:     props.OnStartFeedback,
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
