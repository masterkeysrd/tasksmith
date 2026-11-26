package acp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

func (a *agent) Cancel(ctx context.Context, r sdk.CancelNotification) error {
	_, err := a.session(r.SessionId)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if turn := a.turns[r.SessionId]; turn != nil {
		turn.cancel()
	}

	return nil
}

func promptInput(ctx context.Context, c *client.Client, r sdk.PromptRequest) (out api.SendRequest, err error) {
	var parts []string
	for _, b := range r.Prompt {
		switch {
		case b.Text != nil:
			parts = append(parts, b.Text.Text)
		case b.ResourceLink != nil:
			parts = append(parts, b.ResourceLink.Name+": "+b.ResourceLink.Uri)
		case b.Resource != nil && b.Resource.Resource.TextResourceContents != nil:
			v := b.Resource.Resource.TextResourceContents
			parts = append(parts, v.Uri+"\n"+v.Text)
		case b.Resource != nil && b.Resource.Resource.BlobResourceContents != nil:
			v := b.Resource.Resource.BlobResourceContents
			parts = append(parts, v.Uri+" (base64)\n"+v.Blob)
		case b.Image != nil || b.Audio != nil:
			var label, encoded, mime string
			if b.Image != nil {
				label, encoded, mime = "Image", b.Image.Data, b.Image.MimeType
			} else {
				label, encoded, mime = "Audio", b.Audio.Data, b.Audio.MimeType
			}

			data, e := base64.StdEncoding.DecodeString(encoded)
			if e != nil {
				return out, sdk.NewInvalidParams(fmt.Sprintf("decode %s: %v", label, e))
			}

			m, e := c.UploadMedia(ctx, string(r.SessionId), data, mime, "")
			if e != nil {
				return out, e
			}

			ref := fmt.Sprintf("[%s #%d]", label, len(out.Attachments)+1)
			parts = append(parts, ref)
			out.Attachments = append(out.Attachments, api.Attachment{
				Ref:    ref,
				Kind:   api.AttachmentMedia,
				Target: m.SHA256,
				Media:  m,
			})
		default:
			return out, sdk.NewInvalidParams("unsupported prompt block")
		}
	}

	out.Content = strings.Join(parts, "\n")
	if len(r.Prompt) == 1 && r.Prompt[0].Text != nil {
		fields := strings.Fields(out.Content)
		if len(fields) > 0 && fields[0] == "/compact" {
			out.Compact = &api.CompactRequest{
				Focus: strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out.Content), "/compact")),
			}
			out.Content = ""
		}
	}

	return
}

func (a *agent) Prompt(ctx context.Context, r sdk.PromptRequest) (out sdk.PromptResponse, err error) {
	id := r.SessionId
	ctx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	loaded := a.sessions[id]
	if loaded == nil {
		a.mu.Unlock()
		cancel()
		return out, notFound(id)
	}

	if a.closing || ctx.Err() != nil {
		a.mu.Unlock()
		cancel()
		return sdk.PromptResponse{StopReason: sdk.StopReasonCancelled}, nil
	}
	if a.turns[id] != nil {
		a.mu.Unlock()
		cancel()
		return out, sdk.NewInvalidRequest("session already has an active prompt")
	}

	turn := &activeTurn{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	c := loaded.Client
	t := newTranslation(c.CWD())
	t.caps = a.clientCaps
	t.usage = loaded.usage
	a.turns[id] = turn
	a.mu.Unlock()

	state := turnStream{
		client: c,
		id:     string(id),
	}
	var settled bool
	abort := sync.OnceValue(func() error {
		cleanup, stop := context.WithTimeout(context.Background(), cleanupTimeout)
		defer stop()
		return sessionError(id, c.Cancel(cleanup, string(id)))
	})

	var stopAbort func() bool
	defer func() {
		aborting := stopAbort != nil && !stopAbort()
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			out.StopReason = sdk.StopReasonCancelled
			err = nil
		}

		cancel()
		if aborting || (state.sent && !settled) {
			err = errors.Join(err, abort())
		}

		a.mu.Lock()
		loaded.usage = t.usage
		delete(a.turns, id)
		close(turn.done)
		a.mu.Unlock()
	}()

	req, err := promptInput(ctx, c, r)
	if err != nil {
		return out, sessionError(id, err)
	}
	state.request = req

	state.scanExisting = t.caps.Subagents != nil
	answered := make(interactionIDs)

	for ev, e := range state.events(ctx) {
		if e != nil {
			return out, e
		}
		if state.sent && stopAbort == nil {
			stopAbort = context.AfterFunc(ctx, func() {
				_ = abort()
			})
		}

		if sess := state.childSession(ev); sess != nil {
			if t.caps.Subagents != nil {
				child := state.children[api.SessionStreamTopic(sess.ID)]
				if err := a.notify(ctx, id, child.announcement(sess)); err != nil {
					return out, err
				}
			}
			continue
		}

		route := interactionRoute{
			parent: id,
			editor: id,
			target: id,
		}
		tr := t
		child := state.children[ev.Topic]
		if child != nil {
			tr = child
			tr.caps = t.caps
			route.target = sdk.SessionId(child.childID)
			if t.caps.Subagents != nil {
				route.editor = route.target
			}
		}

		handled, interactionErr := a.interact(ctx, c, route, t.caps, answered, ev.Payload)
		if interactionErr != nil {
			return out, interactionErr
		}
		if handled || (child != nil && t.caps.Subagents == nil) {
			continue
		}

		t.journalOnly = state.journalOnly
		if err := a.display(ctx, c, route.target, tr, ev.Payload); err != nil {
			return out, err
		}
		if child != nil {
			if u := child.finish(ev.Payload); u != nil {
				if err := a.notify(ctx, id, *u); err != nil {
					return out, err
				}
			}
			continue
		}

		switch p := ev.Payload.(type) {
		case api.EntryAddedEvent:
			if rec := p.Entry.TurnCompleted; p.Entry.Kind == api.TimelineKindInterrupted ||
				(rec != nil && rec.Status == api.TurnStatusCanceled) {
				settled = true
				out.StopReason = sdk.StopReasonCancelled
				return
			}
		case api.ErrorEvent:
			settled = true
			if p.Code == "max_tokens" {
				return sdk.PromptResponse{StopReason: sdk.StopReasonMaxTokens}, nil
			}
			return out, errors.New(p.Error)
		case api.CompletedEvent:
			settled = true
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		}
	}

	return out, errors.New("daemon stream ended before turn completion")
}

type interactionRoute struct {
	parent, editor, target sdk.SessionId
}

type interactionIDs map[string]bool

func (a *agent) interact(
	ctx context.Context,
	c *client.Client,
	route interactionRoute,
	caps sdk.ClientCapabilities,
	answered interactionIDs,
	payload api.EventPayload,
) (bool, error) {
	visibleChild := route.editor != route.parent
	forms := caps.Elicitation != nil && caps.Elicitation.Form != nil

	once := func(id string, respond func() error) error {
		if id != "" && answered[id] {
			return nil
		}
		if visibleChild {
			waiting := sdk.StateUpdate{
				RequiresAction: &sdk.StateUpdateRequiresAction{},
			}
			if err := a.notify(ctx, route.parent, childState(route.target, waiting)); err != nil {
				return err
			}
		}

		if err := respond(); err != nil {
			return err
		}
		answered[id] = true
		if visibleChild {
			running := sdk.StateUpdate{
				Running: &sdk.StateUpdateRunning{},
			}
			return a.notify(ctx, route.parent, childState(route.target, running))
		}
		return nil
	}

	unsupported := func(ids []string) error {
		if len(ids) > 0 && !slices.ContainsFunc(ids, func(id string) bool { return id == "" || !answered[id] }) {
			return nil
		}

		message := "This turn requires a TaskSmith question or plan review. Turn cancelled; retry in the terminal UI."
		if route.target != route.parent {
			message = "Child turn cancelled: this editor cannot show the requested form; retry in the terminal UI."
			if err := c.Cancel(ctx, string(route.target)); err != nil {
				return err
			}
		}
		if err := a.notify(ctx, route.editor, sdk.UpdateAgentMessageText(message)); err != nil {
			return err
		}
		for _, id := range ids {
			answered[id] = true
		}
		if route.target == route.parent {
			return context.Canceled
		}
		return nil
	}

	switch p := payload.(type) {
	case api.PermissionRequestedEvent:
		for _, q := range p.Permissions {
			if err := once(q.ID, func() error { return a.permission(ctx, c, route, q) }); err != nil {
				return true, err
			}
		}
	case api.QuestionRequestedEvent:
		if !forms {
			var ids []string
			for _, q := range p.Questions {
				ids = append(ids, q.ID)
			}
			return true, unsupported(ids)
		}
		for _, q := range p.Questions {
			if err := once(q.ID, func() error { return a.question(ctx, c, route, q) }); err != nil {
				return true, err
			}
		}
	case api.PlanReviewRequestedEvent:
		for _, q := range p.PlanReviews {
			if q.ID != "" && answered[q.ID] {
				continue
			}
			if caps.Plan != nil {
				plan := sdk.NewPlanUpdateContentMarkdown(sdk.PlanId(q.Path), q.Content)
				if err := a.notify(ctx, route.editor, sdk.SessionUpdate{
					PlanUpdate: &sdk.SessionPlanUpdate{Plan: plan},
				}); err != nil {
					return true, err
				}
			}
			if forms {
				if err := once(q.ID, func() error { return a.review(ctx, c, route, q) }); err != nil {
					return true, err
				}
			}
		}
		if !forms {
			var ids []string
			for _, q := range p.PlanReviews {
				ids = append(ids, q.ID)
			}
			return true, unsupported(ids)
		}
	default:
		return false, nil
	}
	return true, nil
}

func (a *agent) display(
	ctx context.Context,
	c *client.Client,
	id sdk.SessionId,
	tr *translation,
	payload api.EventPayload,
) error {
	for _, update := range tr.display(string(id), payload) {
		if err := a.notify(ctx, id, update); err != nil {
			return err
		}
		if update.CurrentModeUpdate != nil {
			sess, err := c.GetSession(ctx, string(id))
			if err != nil {
				return err
			}
			opts, err := configuration(ctx, c, sess)
			if err != nil {
				return err
			}
			if err := a.notify(ctx, id, sdk.SessionUpdate{
				ConfigOptionUpdate: &sdk.SessionConfigOptionUpdate{ConfigOptions: opts},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *agent) permission(
	ctx context.Context,
	c *client.Client,
	route interactionRoute,
	q api.PermissionRequest,
) error {
	opts := []sdk.PermissionOption{
		{
			OptionId: "reject",
			Kind:     sdk.PermissionOptionKindRejectOnce,
			Name:     "Reject",
		},
		{
			OptionId: "allow_once",
			Kind:     sdk.PermissionOptionKindAllowOnce,
			Name:     "Allow once",
		},
	}
	reject := api.PermissionDecision{
		RequestID:  q.ID,
		ToolCallID: q.ToolCallID,
		Kind:       api.DecisionReject,
	}
	choices := map[sdk.PermissionOptionId]api.PermissionDecision{
		"reject": reject,
		"allow_once": {
			RequestID:  q.ID,
			ToolCallID: q.ToolCallID,
			Kind:       api.DecisionAllowOnce,
		},
	}
	denials := make(map[sdk.PermissionOptionId]string)

	for i, pattern := range q.GrantPatterns {
		key := sdk.PermissionOptionId(fmt.Sprintf("deny:%d", i))
		denials[key] = pattern
		choices[key] = reject
		opts = append(opts, sdk.PermissionOption{
			OptionId: key,
			Kind:     sdk.PermissionOptionKindRejectAlways,
			Name:     "Always reject " + pattern + " in this project",
		})

		for _, scope := range []api.PermissionScope{
			api.ScopeSession,
			api.ScopeProject,
		} {
			key := sdk.PermissionOptionId(fmt.Sprintf("%s:%d", scope, i))
			opts = append(opts, sdk.PermissionOption{
				OptionId: key,
				Kind:     sdk.PermissionOptionKindAllowAlways,
				Name:     fmt.Sprintf("Allow %s in this %s", pattern, scope),
			})
			choices[key] = api.PermissionDecision{
				RequestID:    q.ID,
				ToolCallID:   q.ToolCallID,
				Kind:         api.DecisionAllowSession,
				GrantPattern: pattern,
				Scope:        scope,
			}
		}
	}

	title := q.ToolName + ": " + q.Summary
	if q.Target != "" {
		title += " — " + q.Target
	}
	if q.Unsandboxed || q.BypassSandbox {
		title = "Outside sandbox: " + title
	}

	tool := sdk.ToolCallUpdate{
		ToolCallId: sdk.ToolCallId(q.ToolCallID),
		Title:      sdk.PatchSet(title),
		Kind:       sdk.PatchSet(kind(q.ToolName)),
	}
	if q.Diff != "" {
		tool.Content = sdk.PatchSet([]sdk.ToolCallContent{sdk.ToolContent(sdk.TextBlock(q.Diff))})
	}

	res, err := a.conn.RequestPermission(ctx, sdk.RequestPermissionRequest{
		SessionId: route.editor,
		ToolCall:  tool,
		Options:   opts,
	})
	if err != nil {
		return err
	}
	if res.Outcome.Cancelled != nil {
		if route.target != route.parent && ctx.Err() == nil {
			return c.Send(ctx, string(route.target), api.SendRequest{Decision: &reject})
		}
		return context.Canceled
	}

	decision := reject
	if v := res.Outcome.Selected; v != nil {
		d, ok := choices[v.OptionId]
		if !ok {
			return sdk.NewInvalidParams("unknown permission option")
		}
		if pattern, persistent := denials[v.OptionId]; persistent {
			if _, err := c.AddPermissionRule(ctx, api.PermissionRuleRequest{
				Scope:  api.ScopeProject,
				Effect: api.EffectDeny,
				Rule:   pattern,
			}); err != nil {
				return err
			}
		}
		decision = d
	}

	return c.Send(ctx, string(route.target), api.SendRequest{Decision: &decision})
}
