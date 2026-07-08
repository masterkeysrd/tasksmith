package chat

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
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
			maps.Copy(newDecisions, current)
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

	// Parse attachments XML from any block containing it
	var attachmentsXML string
	for _, block := range content {
		if tb, ok := block.(*message.TextBlock); ok {
			if tb.Extras != nil && tb.Extras["is_attachments"] == true {
				attachmentsXML = tb.Text
				break
			}
		}
	}

	var filesMap map[string]XMLFile
	var symbolsMap map[string]XMLSymbol
	var skillsMap map[string]XMLSkill
	if attachmentsXML != "" {
		if parsed := parseAttachmentsXML(attachmentsXML); parsed != nil {
			filesMap = make(map[string]XMLFile)
			for _, f := range parsed.Files {
				filesMap[f.Path] = f
				filesMap[filepath.Base(f.Path)] = f
			}
			symbolsMap = make(map[string]XMLSymbol)
			for _, s := range parsed.Symbols {
				symbolsMap[s.Name] = s
				symbolsMap[fmt.Sprintf("@sym:%s:%s", s.Name, s.Kind)] = s
				symbolsMap[s.Name+":"+s.Kind] = s
			}
			skillsMap = make(map[string]XMLSkill)
			for _, sk := range parsed.Skills {
				skillsMap[sk.Name] = sk
				skillsMap["@skill:"+sk.Name] = sk
			}
		}
	}

	// Only render the first TextBlock for non-assistant roles (e.g. user).
	// User messages are always structured as [userText, attachmentsXML?] by session.go —
	// the attachments block is context for the model and should not be displayed in the chat.
	var nodes []kitex.Node
	for _, block := range content {
		if tb, ok := block.(*message.TextBlock); ok {
			// Skip the attachments XML block itself
			if tb.Extras != nil && tb.Extras["is_attachments"] == true {
				continue
			}
			cleaned := tryExtractTextFromJSON(tb.Text)
			if cleaned == "" {
				continue
			}

			var onAttachmentClick func(refType, rawValue string)
			if props.OnViewPreview != nil && (len(filesMap) > 0 || len(symbolsMap) > 0 || len(skillsMap) > 0) {
				onAttachmentClick = func(refType, rawValue string) {
					if refType == "file" {
						f, found := filesMap[rawValue]
						if !found {
							f, found = filesMap[filepath.Base(rawValue)]
						}
						if !found {
							// Try suffix-matching if rawValue is relative but keys are absolute
							for k, file := range filesMap {
								if strings.HasSuffix(filepath.ToSlash(k), filepath.ToSlash(rawValue)) {
									f = file
									found = true
									break
								}
							}
						}
						if found {
							if f.Reason != "" {
								var explanation string
								if f.Mime != "" && f.Mime != "text/plain" {
									explanation = fmt.Sprintf("Attachment is a binary file (%s).\nUse the view_file tool to inspect.", f.Mime)
								} else {
									explanation = fmt.Sprintf("Attachment is too large to preview inline (%d lines).\nUse the view_file tool to inspect.", f.Lines)
								}
								props.OnViewPreview(
									fmt.Sprintf("Attachment: %s", filepath.Base(f.Path)),
									preview.DefaultTextPreview{
										Text: explanation,
									},
								)
							} else {
								props.OnViewPreview(
									fmt.Sprintf("Viewing %s", filepath.Base(f.Path)),
									preview.FileViewPreview{
										Path:      f.Path,
										Content:   stripLinePrefixes(f.Content),
										IsBinary:  f.Mime != "" && f.Mime != "text/plain",
										MimeType:  f.Mime,
										StartLine: 1,
									},
								)
							}
						}
					} else if refType == "sym" {
						s, found := symbolsMap[rawValue]
						if !found {
							// Split rawValue (e.g. "Message:Interface") into name and optional kind
							parts := strings.SplitN(rawValue, ":", 2)
							targetName := parts[0]
							var targetKind string
							if len(parts) > 1 {
								targetKind = parts[1]
							}

							for _, sym := range symbolsMap {
								nameMatches := sym.Name == targetName || strings.HasSuffix(sym.Name, "."+targetName)
								kindMatches := targetKind == "" || strings.EqualFold(sym.Kind, targetKind)
								if nameMatches && kindMatches {
									s = sym
									found = true
									break
								}
							}
						}
						if found {
							props.OnViewPreview(
								fmt.Sprintf("Symbol: %s", s.Name),
								preview.SymbolViewPreview{
									Name:            s.Name,
									Kind:            s.Kind,
									File:            s.File,
									Snippet:         s.Content,
									Docs:            s.Docs,
									Diagnostics:     s.Diagnostics,
									References:      s.References,
									Implementations: s.Implementations,
								},
							)
						}
					} else if refType == "skill" {
						sk, found := skillsMap[rawValue]
						if found {
							props.OnViewPreview(
								fmt.Sprintf("Skill: %s", sk.Name),
								preview.DefaultTextPreview{
									Text: sk.Content,
								},
							)
						}
					}
				}
			}

			nodes = append(nodes, components.Markdown(components.MarkdownProps{
				Source:            cleaned,
				OnAttachmentClick: onAttachmentClick,
			}))
		}
	}

	if len(nodes) == 0 {
		return nil
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1),
	}, nodes...)
})
