package exec

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

// readStdin decodes caller control frames from stdin and hands them to the
// loop. It always ends with a close frame (real or synthesized at EOF) so the
// loop stops selecting on the actions channel without racing a channel close.
func (r *run) readStdin() {
	reader := api.NewExecFrameReader(r.opts.Stdin)
	for {
		frame, err := reader.NextInputFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				r.warn(fmt.Sprintf("stdin: %v", err))
			}
			select {
			case r.actions <- api.ExecInputFrame{Type: api.ExecInputClose}:
			case <-r.ctx.Done():
			}
			return
		}
		select {
		case r.actions <- frame:
		case <-r.ctx.Done():
			return
		}
		if frame.Type == api.ExecInputClose {
			return
		}
	}
}

// applyFrame maps one control frame onto its daemon operation. Frames map 1:1
// onto api payloads so an SDK speaks the same vocabulary as the HTTP API.
func (r *run) applyFrame(frame api.ExecInputFrame) error {
	targetSession := strings.TrimSpace(frame.SessionID)

	switch frame.Type {
	case api.ExecInputSend:
		payload := frame.SendPayload()
		if payload == nil {
			return fmt.Errorf("send frame carries no content")
		}
		// A bare content line targeting the root session goes through the same
		// dispatcher as the initial prompt so local slash commands and @mentions
		// behave identically mid-session.
		if (targetSession == "" || targetSession == r.sess.ID) &&
			payload.Command == "" && payload.Decision == nil && payload.Question == nil &&
			payload.Compact == nil && len(payload.Blocks) == 0 && len(payload.Attachments) == 0 {
			return r.submit(payload.Content, payload.Content)
		}
		if payload.Content != "" && payload.Command == "" {
			payload.Attachments = r.resolveMentions(payload.Content, payload.Attachments)
		}
		if payload.ID == "" {
			payload.ID = newMessageID()
		}
		sid := cmp.Or(targetSession, r.sess.ID)
		if sid == r.sess.ID {
			r.pendingSends++
		}
		if err := r.c.Send(r.ctx, sid, *payload); err != nil {
			if sid == r.sess.ID {
				r.pendingSends--
			}
			return err
		}
		return nil

	case api.ExecInputPermission:
		if frame.Permission == nil {
			return fmt.Errorf("permission frame carries no decision")
		}
		sid := cmp.Or(targetSession, r.permissionSessionID(frame.Permission), r.sess.ID)
		return r.c.Send(r.ctx, sid, api.SendRequest{Decision: frame.Permission})

	case api.ExecInputQuestion:
		if frame.Question == nil {
			return fmt.Errorf("question frame carries no response")
		}
		sid := cmp.Or(targetSession, r.questionSessionID(frame.Question), r.sess.ID)
		return r.c.Send(r.ctx, sid, api.SendRequest{Question: frame.Question})

	case api.ExecInputConfig:
		if frame.Config == nil {
			return fmt.Errorf("config frame carries no update")
		}
		sid := cmp.Or(targetSession, r.sess.ID)
		sess, err := r.c.UpdateSession(r.ctx, sid, *frame.Config)
		if err != nil {
			return err
		}
		if sess != nil && sid == r.sess.ID {
			r.sess = sess
		}
		return nil

	case api.ExecInputCancel:
		if targetSession != "" && targetSession != r.sess.ID {
			return r.c.Cancel(r.ctx, targetSession)
		}
		r.requestCancel()
		return nil

	case api.ExecInputClose:
		return nil

	default:
		return fmt.Errorf("unknown input frame type %q", frame.Type)
	}
}
