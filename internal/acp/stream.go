package acp

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

type streamItem struct {
	ready      bool
	subscriber string
	event      api.Event
	err        error
}

func subscribe(ctx context.Context, c *client.Client, cursor string, topics []string) <-chan streamItem {
	ch := make(chan streamItem, 1)

	go func() {
		defer close(ch)

		deliver := func(item streamItem) bool {
			select {
			case ch <- item:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for event, err := range c.EventsWithReady(ctx, "", cursor, func(subscriber string) {
			deliver(streamItem{
				ready:      true,
				subscriber: subscriber,
			})
		}, topics...) {
			if !deliver(streamItem{
				event: event,
				err:   err,
			}) {
				return
			}
		}
	}()

	return ch
}

type turnStream struct {
	client       *client.Client
	id           string
	request      api.SendRequest
	sent         bool
	cursor       string
	snapshot     bool
	lastEntry    string
	seen         map[string]bool
	subscriber   string
	children     map[string]*translation
	journalOnly  bool
	scanExisting bool
	syncChildren bool
}

func (s *turnStream) childSession(e api.Event) *api.Session {
	var sess api.Session
	switch p := e.Payload.(type) {
	case api.SessionCreatedEvent:
		sess = p.Session
	case api.SessionUpdatedEvent:
		sess = p.Session
	default:
		return nil
	}

	if sess.ParentID != s.id || sess.ID == "" {
		return nil
	}
	return &sess
}

func (s *turnStream) forward(ctx context.Context, e api.Event, yield func(api.Event, error) bool) (error, bool) {
	sess := s.childSession(e)
	if sess == nil {
		return nil, !yield(e, nil)
	}

	topic := api.SessionStreamTopic(sess.ID)
	if _, known := s.children[topic]; known {
		return nil, !yield(e, nil)
	}

	child := newTranslation(cmp.Or(sess.WorkDir, s.client.CWD()))
	child.childID = sess.ID
	s.children[topic] = child
	if !yield(e, nil) {
		return nil, true
	}

	_, err := s.client.Subscribe(ctx, s.subscriber, e.ID, topic)
	if client.IsNeedSync(err) {
		_, err = s.client.Subscribe(ctx, s.subscriber, "", topic)
		if err == nil {
			err, stop := s.catchUpChild(ctx, topic, child, yield)
			s.syncChildren = err != nil
			return err, stop
		}
	}
	if err == nil && e.ID == "" {
		err, stop := s.catchUpChild(ctx, topic, child, yield)
		s.syncChildren = err != nil
		return err, stop
	}
	if err != nil {
		s.syncChildren = true
	}
	return err, false
}

func (s *turnStream) subscribeActiveChildren(
	ctx context.Context,
	sess *api.Session,
	yield func(api.Event, error) bool,
) (error, bool) {
	for _, child := range sess.Subagents {
		if child.ParentID != s.id || child.Status == "" || child.Status == api.StatusIdle {
			continue
		}
		if _, known := s.children[api.SessionStreamTopic(child.ID)]; known {
			continue
		}
		if err, stop := s.forward(ctx, api.Event{
			Type:    api.EventSessionUpdated,
			Payload: api.SessionUpdatedEvent{Session: child},
		}, yield); err != nil || stop {
			return err, stop
		}
	}
	return nil, false
}

func hasActiveDelegatedChild(sess *api.Session) bool {
	for _, child := range sess.Subagents {
		if child.CommandDelegated && child.Status != "" && child.Status != api.StatusIdle {
			return true
		}
	}
	return false
}

func (s *turnStream) catchUpChild(
	ctx context.Context,
	topic string,
	child *translation,
	yield func(api.Event, error) bool,
) (error, bool) {
	sess, err := s.client.GetSession(ctx, child.childID)
	if err != nil {
		return err, false
	}
	return nil, !emitPending(sess, func(p api.EventPayload) bool {
		return yield(api.Event{
			Type:    api.EventType(p.EventType()),
			Topic:   topic,
			Payload: p,
		}, nil)
	})
}

func emitPending(sess *api.Session, emit func(api.EventPayload) bool) bool {
	if len(sess.PendingPermissions) > 0 && !emit(api.PermissionRequestedEvent{
		SessionID:   sess.ID,
		Permissions: sess.PendingPermissions,
	}) {
		return false
	}
	if len(sess.PendingQuestions) > 0 && !emit(api.QuestionRequestedEvent{
		SessionID: sess.ID,
		Questions: sess.PendingQuestions,
	}) {
		return false
	}
	if len(sess.PendingPlanReviews) > 0 && !emit(api.PlanReviewRequestedEvent{
		SessionID:   sess.ID,
		PlanReviews: sess.PendingPlanReviews,
	}) {
		return false
	}
	return true
}

func (s *turnStream) events(ctx context.Context) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		s.seen = map[string]bool{}
		s.children = make(map[string]*translation)
		s.request.ID = client.NewCursor()

		if s.request.Compact != nil {
			history, err := s.client.Timeline(ctx, s.id)
			if err != nil {
				yield(api.Event{}, sessionError(sdk.SessionId(s.id), err))
				return
			}
			if len(history) > 0 {
				s.lastEntry = history[len(history)-1].ID
			}
		}

		for attempt := 0; ; attempt++ {
			streamCtx, closeStream := context.WithCancel(ctx)
			err, finished := s.consume(streamCtx, yield)
			closeStream()
			if finished {
				return
			}
			if ctx.Err() != nil {
				yield(api.Event{}, ctx.Err())
				return
			}

			if attempt >= 5 {
				yield(api.Event{}, fmt.Errorf("reconnect session stream: %w", err))
				return
			}
			if s.cursor == "" || client.IsNeedSync(err) {
				s.cursor = ""
				s.snapshot = s.sent
			}

			select {
			case <-ctx.Done():
				yield(api.Event{}, ctx.Err())
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}

func (s *turnStream) consume(ctx context.Context, yield func(api.Event, error) bool) (error, bool) {
	topics := []string{
		api.SessionStreamTopic(s.id),
		api.TopicSessions,
	}
	topics = slices.AppendSeq(topics, maps.Keys(s.children))
	for item := range subscribe(ctx, s.client, s.cursor, topics) {
		switch {
		case item.err != nil:
			return item.err, false
		case item.ready:
			s.subscriber = item.subscriber
			if s.syncChildren || s.snapshot {
				for topic, child := range s.children {
					if err, stop := s.catchUpChild(ctx, topic, child, yield); err != nil || stop {
						return err, stop
					}
				}
				s.syncChildren = false
			}
			if !s.sent {
				if err := ctx.Err(); err != nil {
					yield(api.Event{}, sessionError(sdk.SessionId(s.id), err))
					return nil, true
				}
				if s.scanExisting {
					sess, err := s.client.GetSession(ctx, s.id)
					if err != nil {
						yield(api.Event{}, sessionError(sdk.SessionId(s.id), err))
						return nil, true
					}
					if err, stop := s.subscribeActiveChildren(ctx, sess, yield); err != nil || stop {
						return err, stop
					}
				}
				if err := s.client.Send(ctx, s.id, s.request); err != nil {
					s.sent = ctx.Err() != nil
					yield(api.Event{}, sessionError(sdk.SessionId(s.id), err))
					return nil, true
				}
				s.sent = true
			} else if s.snapshot {
				if err, stop := s.recover(ctx, yield); err != nil || stop {
					return err, stop
				}
				s.snapshot = false
			}
			continue
		}

		e := item.event
		if e.ID > s.cursor {
			s.cursor = e.ID
		}
		if s.children[e.Topic] != nil {
			if !yield(e, nil) {
				return nil, true
			}
			continue
		}
		if s.journalOnly && e.Type == api.EventChunk {
			continue
		}
		if p, ok := e.Payload.(api.EntryAddedEvent); ok && p.Entry.ID != "" {
			if s.seen[p.Entry.ID] {
				continue
			}
			s.seen[p.Entry.ID] = true
			s.lastEntry = p.Entry.ID
		}

		if err, stop := s.forward(ctx, e, yield); err != nil || stop {
			return err, stop
		}
	}

	return io.EOF, false
}

func (s *turnStream) recover(ctx context.Context, yield func(api.Event, error) bool) (error, bool) {
	sess, err := s.client.GetSession(ctx, s.id)
	if err != nil {
		yield(api.Event{}, sessionError(sdk.SessionId(s.id), err))
		return nil, true
	}

	entries, err := s.client.Timeline(ctx, s.id)
	if err != nil {
		yield(api.Event{}, sessionError(sdk.SessionId(s.id), err))
		return nil, true
	}

	emit := func(p api.EventPayload) bool {
		return yield(api.Event{
			Type:    api.EventType(p.EventType()),
			Payload: p,
		}, nil)
	}

	start := slices.IndexFunc(entries, func(e api.TimelineEntry) bool {
		if s.lastEntry != "" {
			return e.ID == s.lastEntry
		}
		return e.Message != nil && e.Message.ID == s.request.ID
	})
	if start < 0 && (s.lastEntry != "" || s.request.Compact == nil) {
		start = len(entries) - 1
	}

	s.journalOnly = true
	for _, e := range entries[start+1:] {
		if s.seen[e.ID] {
			continue
		}
		s.seen[e.ID] = true
		s.lastEntry = e.ID

		if !emit(api.EntryAddedEvent{
			SessionID: s.id,
			Entry:     e,
		}) {
			return nil, true
		}
		if rec := e.TurnCompleted; rec != nil && rec.Status == api.TurnStatusFailed {
			emit(api.ErrorEvent{
				SessionID: s.id,
				Error:     "daemon turn failed during stream interruption",
			})
			return nil, true
		}
	}

	if err, stop := s.subscribeActiveChildren(ctx, sess, yield); err != nil || stop {
		return err, stop
	}
	if !emitPending(sess, emit) {
		return nil, true
	}
	if len(sess.PendingPermissions)+len(sess.PendingQuestions)+len(sess.PendingPlanReviews) == 0 &&
		sess.Status == api.StatusIdle && len(sess.Queue) == 0 &&
		!hasActiveDelegatedChild(sess) {
		emit(api.CompletedEvent{SessionID: s.id})
		return nil, true
	}

	return nil, false
}
